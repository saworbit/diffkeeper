package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/yourorg/diffkeeper/pkg/cas"
	"github.com/yourorg/diffkeeper/pkg/config"
	"github.com/yourorg/diffkeeper/pkg/diff"
	"github.com/yourorg/diffkeeper/pkg/merkle"
	"go.etcd.io/bbolt"
)

const (
	BucketDeltas     = "deltas"     // Legacy: full file storage (MVP)
	BucketHashes     = "hashes"     // File hash tracking
	BucketMetadata   = "meta"       // File metadata (versions, Merkle roots)
	BucketSnapshots  = "snapshots"  // Periodic base snapshots
	SchemaVersionKey = "schema_version"

	SchemaVersionMVP  = 1  // MVP: full gzip storage
	SchemaVersionDiff = 2  // v1.0: binary diffs + CAS
)

var debugEnabled bool

func logDebug(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	log.Printf("[DEBUG] "+format, args...)
}

type DiffKeeper struct {
	db         *bbolt.DB
	stateDir   string
	storePath  string
	watcher    *fsnotify.Watcher
	config     *config.DiffConfig
	cas        *cas.CASStore
	merkle     *merkle.MerkleManager
	diffEngine diff.DiffEngine
}

// FileMetadata stores metadata for files using binary diffs
type FileMetadata struct {
	FilePath        string    `json:"file_path"`
	CIDs            []string  `json:"cids"`              // List of CIDs (chunks or diffs)
	MerkleRoot      []byte    `json:"merkle_root"`       // Merkle tree root hash
	IsChunked       bool      `json:"is_chunked"`        // Whether file was chunked
	IsSnapshot      bool      `json:"is_snapshot"`       // Whether this is a full snapshot
	BaseSnapshotCID string    `json:"base_snapshot_cid"` // CID of base snapshot for diff chains
	VersionCount    int       `json:"version_count"`     // Number of versions captured
	Timestamp       time.Time `json:"timestamp"`         // When this version was captured
	OriginalSize    int64     `json:"original_size"`     // Original file size
	CompressedSize  int64     `json:"compressed_size"`   // Compressed/diff size
}

func (dk *DiffKeeper) addWatchRecursive(root string) error {
	if dk.watcher == nil {
		return fmt.Errorf("watcher not initialized")
	}

	// Windows delivers CREATE events only for the top-most directory of a
	// multi-level os.MkdirAll call. Walking lets us attach watchers to
	// every new subdirectory before any files are written.
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if err := dk.watcher.Add(path); err != nil {
			log.Printf("[Watcher] Failed to add watch for %s: %v", path, err)
			return nil
		}
		logDebug("[Watcher] Added watch for %s", path)
		return nil
	})
}

func NewDiffKeeper(stateDir, storePath string, cfg *config.DiffConfig) (*DiffKeeper, error) {
	db, err := bbolt.Open(storePath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open store: %w", err)
	}

	// Initialize buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range []string{BucketDeltas, BucketHashes, BucketMetadata, BucketSnapshots} {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	// Initialize CAS store
	casStore, err := cas.NewCASStore(db, cfg.HashAlgo)
	if err != nil {
		db.Close()
		watcher.Close()
		return nil, fmt.Errorf("failed to initialize CAS: %w", err)
	}

	// Initialize diff engine
	diffEngine, err := diff.NewDiffEngine(cfg.Library)
	if err != nil {
		db.Close()
		watcher.Close()
		return nil, fmt.Errorf("failed to initialize diff engine: %w", err)
	}

	dk := &DiffKeeper{
		db:         db,
		stateDir:   stateDir,
		storePath:  storePath,
		watcher:    watcher,
		config:     cfg,
		cas:        casStore,
		merkle:     merkle.NewMerkleManager(),
		diffEngine: diffEngine,
	}

	// Perform schema migration if needed
	if err := dk.migrateSchema(); err != nil {
		dk.Close()
		return nil, fmt.Errorf("schema migration failed: %w", err)
	}

	return dk, nil
}

func (dk *DiffKeeper) Close() error {
	if dk.watcher != nil {
		dk.watcher.Close()
	}
	if dk.db != nil {
		return dk.db.Close()
	}
	return nil
}

// RedShift: Replay all deltas to restore filesystem state (routes to appropriate implementation)
func (dk *DiffKeeper) RedShift() error {
	// Check schema version to determine which implementation to use
	schemaVersion := dk.getSchemaVersion()

	if schemaVersion >= SchemaVersionDiff && dk.config != nil && dk.config.EnableDiff {
		return dk.RedShiftDiff()
	}

	// Fall back to MVP implementation
	return dk.redShiftMVP()
}

// redShiftMVP: Legacy MVP implementation (full file decompression)
func (dk *DiffKeeper) redShiftMVP() error {
	log.Println("[RedShift] Restoring state from deltas...")

	startTime := time.Now()
	count := 0

	err := dk.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDeltas))
		if b == nil {
			return nil // No deltas to restore
		}

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			relPath := string(k)
			fullPath := filepath.Join(dk.stateDir, relPath)

			// Decompress delta
			data, err := decompressData(v)
			if err != nil {
				log.Printf("Warning: failed to decompress %s: %v", relPath, err)
				continue
			}

			// Ensure directory exists
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return fmt.Errorf("failed to create dir for %s: %w", relPath, err)
			}

			// Write file
			if err := os.WriteFile(fullPath, data, 0644); err != nil {
				return fmt.Errorf("failed to restore %s: %w", relPath, err)
			}

			count++
		}
		return nil
	})

	duration := time.Since(startTime)
	log.Printf("[RedShift] Restored %d files in %v", count, duration)
	return err
}

// BlueShift: Capture file change (routes to appropriate implementation)
func (dk *DiffKeeper) BlueShift(path string) error {
	// Use binary diffs if enabled
	if dk.config != nil && dk.config.EnableDiff {
		return dk.BlueShiftDiff(path)
	}

	// Fall back to MVP implementation
	return dk.blueShiftMVP(path)
}

// blueShiftMVP: Legacy MVP implementation (full file compression)
func (dk *DiffKeeper) blueShiftMVP(path string) error {
	relPath, err := filepath.Rel(dk.stateDir, path)
	if err != nil {
		return err
	}

	// Read current file
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Compute hash
	hash := sha256.Sum256(data)
	newHash := hex.EncodeToString(hash[:])

	// Check if changed
	var prevHash string
	dk.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketHashes))
		if v := b.Get([]byte(relPath)); v != nil {
			prevHash = string(v)
		}
		return nil
	})

	if prevHash == newHash {
		return nil // No change
	}

	// Compress and store (MVP: store full file, not binary diff)
	compressed, err := compressData(data)
	if err != nil {
		return err
	}

	err = dk.db.Update(func(tx *bbolt.Tx) error {
		deltas := tx.Bucket([]byte(BucketDeltas))
		hashes := tx.Bucket([]byte(BucketHashes))

		if err := deltas.Put([]byte(relPath), compressed); err != nil {
			return err
		}
		return hashes.Put([]byte(relPath), []byte(newHash))
	})

	if err == nil {
		log.Printf("[BlueShift] Captured %s (%.2f KB compressed)", relPath, float64(len(compressed))/1024)
	}
	return err
}

// Watch filesystem and capture changes
func (dk *DiffKeeper) WatchLoop() error {
	log.Printf("[Watcher] Watching %s for changes...", dk.stateDir)

	// Add initial directory and all subdirectories
	if err := dk.addWatchRecursive(dk.stateDir); err != nil {
		return err
	}

	for {
		select {
		case event, ok := <-dk.watcher.Events:
			if !ok {
				return nil
			}

			logDebug("[Watcher] Event %s for %s", event.Op, event.Name)

			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			// Small delay to avoid capturing partial writes
			time.Sleep(100 * time.Millisecond)

			info, err := os.Stat(event.Name)
			if err != nil {
				logDebug("[Watcher] Skipping %s: %v", event.Name, err)
				continue
			}

			// If the target is a directory, attach watchers to it (and its children)
			// before any subsequent file writes occur. This ensures nested writes on
			// Windows trigger events even when the directory tree is created in one call.
			if info.IsDir() {
				if err := dk.addWatchRecursive(event.Name); err != nil {
					logDebug("[Watcher] Skipping recursive watch for %s: %v", event.Name, err)
				}
				continue
			}

			if err := dk.BlueShift(event.Name); err != nil {
				log.Printf("[Watcher] Error capturing %s: %v", event.Name, err)
			}

		case err, ok := <-dk.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("[Watcher] Error: %v", err)
		}
	}
}

func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompressData(data []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	return io.ReadAll(gr)
}

func main() {
	var (
		stateDir  string
		storePath string

		// Binary diff configuration
		enableDiff       bool
		diffLibrary      string
		chunkSizeMB      int
		hashAlgo         string
		dedupScope       string
		snapshotInterval int
	)

	rootCmd := &cobra.Command{
		Use:   "diffkeeper [flags] -- <command> [args...]",
		Short: "DiffKeeper - Lightweight state recovery for containers",
		Long: `DiffKeeper captures file-level state changes in containerized workloads.

Binary Diffs (v1.0):
  Uses efficient binary diffing (bsdiff) with content-addressable storage (CAS)
  for 50-80% storage reduction on partial file updates.

Example:
  diffkeeper --state-dir=/data --store=/deltas/db.bolt -- postgres -D /data
  diffkeeper --enable-diff --chunk-size=8 --state-dir=/data -- myapp`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if debugEnabled {
				log.Println("[Debug] Verbose logging enabled")
			}

			// Load configuration
			cfg := config.LoadFromEnv()

			// Override with CLI flags if provided
			if cmd.Flags().Changed("enable-diff") {
				cfg.EnableDiff = enableDiff
			}
			if cmd.Flags().Changed("diff-library") {
				cfg.Library = diffLibrary
			}
			if cmd.Flags().Changed("chunk-size") {
				cfg.ChunkSizeMB = chunkSizeMB
			}
			if cmd.Flags().Changed("hash-algo") {
				cfg.HashAlgo = hashAlgo
			}
			if cmd.Flags().Changed("dedup-scope") {
				cfg.DedupScope = dedupScope
			}
			if cmd.Flags().Changed("snapshot-interval") {
				cfg.SnapshotInterval = snapshotInterval
			}

			// Validate configuration
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid configuration: %w", err)
			}

			if cfg.EnableDiff {
				log.Printf("[Config] Binary diffs enabled (library: %s, chunk size: %dMB, hash: %s)",
					cfg.Library, cfg.ChunkSizeMB, cfg.HashAlgo)
			} else {
				log.Println("[Config] Using legacy MVP mode (full file compression)")
			}

			// Initialize DiffKeeper
			dk, err := NewDiffKeeper(stateDir, storePath, cfg)
			if err != nil {
				return fmt.Errorf("initialization failed: %w", err)
			}
			defer dk.Close()

			// Phase 1: RedShift - Restore state
			if err := dk.RedShift(); err != nil {
				return fmt.Errorf("redshift failed: %w", err)
			}

			// Phase 2: Start watching in background
			go func() {
				if err := dk.WatchLoop(); err != nil {
					log.Printf("[Watcher] Loop error: %v", err)
				}
			}()

			// Phase 3: Execute the wrapped application
			log.Printf("[Exec] Starting application: %v", args)

			binary, err := exec.LookPath(args[0])
			if err != nil {
				return fmt.Errorf("failed to find binary %s: %w", args[0], err)
			}

			// Replace current process with the application
			// This ensures signals are properly forwarded
			return syscall.Exec(binary, args, os.Environ())
		},
	}

	// Core flags
	rootCmd.Flags().StringVar(&stateDir, "state-dir", "/data", "Directory to watch for state changes")
	rootCmd.Flags().StringVar(&storePath, "store", "/deltas/db.bolt", "Path to delta storage file")
	rootCmd.PersistentFlags().BoolVar(&debugEnabled, "debug", false, "Enable verbose debug logging")

	// Binary diff flags
	rootCmd.Flags().BoolVar(&enableDiff, "enable-diff", true, "Enable binary diffs (default: true)")
	rootCmd.Flags().StringVar(&diffLibrary, "diff-library", "bsdiff", "Diff library to use (bsdiff or xdelta)")
	rootCmd.Flags().IntVar(&chunkSizeMB, "chunk-size", 4, "Chunk size in MB for large files")
	rootCmd.Flags().StringVar(&hashAlgo, "hash-algo", "sha256", "Hash algorithm for CAS (sha256 or blake3)")
	rootCmd.Flags().StringVar(&dedupScope, "dedup-scope", "container", "Deduplication scope (container or cluster)")
	rootCmd.Flags().IntVar(&snapshotInterval, "snapshot-interval", 10, "Create full snapshot every N versions")

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
