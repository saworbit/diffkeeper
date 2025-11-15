package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/saworbit/diffkeeper/pkg/cas"
	"github.com/saworbit/diffkeeper/pkg/config"
	"github.com/saworbit/diffkeeper/pkg/diff"
	"github.com/saworbit/diffkeeper/pkg/ebpf"
	"github.com/saworbit/diffkeeper/pkg/merkle"
	"github.com/spf13/cobra"
	"go.etcd.io/bbolt"
)

const (
	BucketDeltas     = "deltas"    // Legacy: full file storage (MVP)
	BucketHashes     = "hashes"    // File hash tracking
	BucketMetadata   = "meta"      // File metadata (versions, Merkle roots)
	BucketSnapshots  = "snapshots" // Periodic base snapshots
	SchemaVersionKey = "schema_version"

	SchemaVersionMVP  = 1 // MVP: full gzip storage
	SchemaVersionDiff = 2 // v1.0: binary diffs + CAS
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
	monitorCtx context.Context
	cancelMon  context.CancelFunc
	wg         sync.WaitGroup // Tracks active monitoring goroutines
	ebpfMgr    ebpf.Manager
	profiler   *ebpf.Profiler
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

// StartMonitoring enables either eBPF interception or fsnotify fallback based on availability
func (dk *DiffKeeper) StartMonitoring(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	dk.monitorCtx, dk.cancelMon = context.WithCancel(ctx)

	if dk.ebpfMgr != nil {
		if err := dk.ebpfMgr.Start(dk.monitorCtx); err != nil {
			log.Printf("[eBPF] start failed: %v", err)
			if dk.config != nil && dk.config.EBPF.FallbackFSNotify {
				log.Printf("[Monitor] Falling back to fsnotify watcher")
				dk.ebpfMgr.Close()
				dk.ebpfMgr = nil
			} else {
				return err
			}
		} else {
			dk.startEBPFWorkers()
			return nil
		}
	}

	if err := dk.ensureWatcher(); err != nil {
		return err
	}
	dk.startWatcherLoop()
	return nil
}

func (dk *DiffKeeper) ensureWatcher() error {
	if dk.watcher != nil {
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	dk.watcher = watcher
	return nil
}

func (dk *DiffKeeper) startWatcherLoop() {
	dk.wg.Add(1)
	go func() {
		defer dk.wg.Done()
		if err := dk.WatchLoop(); err != nil {
			log.Printf("[Watcher] Loop exited: %v", err)
		}
	}()
}

func (dk *DiffKeeper) startEBPFWorkers() {
	if dk.ebpfMgr == nil {
		return
	}

	events := dk.ebpfMgr.Events()
	if events == nil {
		log.Printf("[eBPF] event channel not available, falling back to fsnotify")
		if dk.config != nil && dk.config.EBPF.FallbackFSNotify {
			if err := dk.ensureWatcher(); err == nil {
				dk.startWatcherLoop()
			}
		}
		return
	}

	if dk.config != nil {
		dk.profiler = ebpf.NewProfiler(&dk.config.EBPF, dk.ebpfMgr)
		if dk.profiler != nil {
			dk.wg.Add(1)
			go func() {
				defer dk.wg.Done()
				dk.profiler.Run(dk.monitorCtx)
			}()
		}
	}

	dk.wg.Add(1)
	go func() {
		defer dk.wg.Done()
		dk.consumeEBPFEvents(events)
	}()

	if dk.config != nil && dk.config.EBPF.AutoInject {
		dk.wg.Add(1)
		go func() {
			defer dk.wg.Done()
			dk.handleLifecycleEvents()
		}()
	}

	log.Printf("[Monitor] eBPF syscall interception active (state dir: %s)", dk.stateDir)
}

func (dk *DiffKeeper) consumeEBPFEvents(events <-chan ebpf.Event) {
	for {
		select {
		case <-dk.monitorCtx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if evt.Path == "" || !dk.shouldCapturePath(evt.Path) {
				continue
			}
			if dk.profiler != nil {
				dk.profiler.Record(evt.Path)
			}
			if err := dk.BlueShift(evt.Path); err != nil {
				log.Printf("[eBPF] capture failed for %s: %v", evt.Path, err)
			}
		}
	}
}

func (dk *DiffKeeper) handleLifecycleEvents() {
	if dk.ebpfMgr == nil {
		return
	}
	ch := dk.ebpfMgr.LifecycleEvents()
	if ch == nil {
		return
	}

	for {
		select {
		case <-dk.monitorCtx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			dk.dispatchLifecycleEvent(evt)
		}
	}
}

func (dk *DiffKeeper) dispatchLifecycleEvent(evt ebpf.LifecycleEvent) {
	if dk.config == nil || !dk.config.EBPF.AutoInject {
		return
	}

	// Focus on creation/start events
	if evt.State != "create" && evt.State != "start" {
		return
	}

	if dk.config.EBPF.InjectorCommand == "" {
		log.Printf("[AutoInject] Detected %s for %s (runtime=%s) but injector command not configured",
			evt.State, evt.ContainerID, evt.Runtime)
		return
	}

	ctx := dk.monitorCtx
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, dk.config.EBPF.InjectorCommand, evt.ContainerID)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("DIFFKEEPER_CONTAINER_ID=%s", evt.ContainerID),
		fmt.Sprintf("DIFFKEEPER_RUNTIME=%s", evt.Runtime),
		fmt.Sprintf("DIFFKEEPER_NAMESPACE=%s", evt.Namespace),
		fmt.Sprintf("DIFFKEEPER_STATE=%s", evt.State),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[AutoInject] injector error for %s: %v (output: %s)", evt.ContainerID, err, string(output))
		return
	}

	log.Printf("[AutoInject] injector completed for %s: %s", evt.ContainerID, strings.TrimSpace(string(output)))
}

func (dk *DiffKeeper) shouldCapturePath(path string) bool {
	if path == "" {
		return false
	}
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(dk.stateDir, path)
	}
	fullPath = filepath.Clean(fullPath)
	state := filepath.Clean(dk.stateDir)

	rel, err := filepath.Rel(state, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	return true
}

// NewDiffKeeper initializes a DiffKeeper instance with the specified state directory,
// BoltDB store path, and configuration. It performs the following initialization steps:
//   1. Opens the BoltDB database at storePath with a 1-second timeout
//   2. Creates required buckets (deltas, hashes, metadata, snapshots) if they don't exist
//   3. Performs an explicit writability test to ensure the database accepts writes
//   4. Initializes the monitoring backend (eBPF or fsnotify) based on configuration
//   5. Initializes the CAS store and diff engine if binary diffs are enabled
//
// Returns an error if:
//   - The database cannot be opened
//   - The database is read-only (e.g., file permissions are 0444)
//   - Required buckets cannot be created
//   - eBPF initialization fails (when fallback is disabled)
//   - CAS or diff engine initialization fails
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

	// Verify database is writable by performing a test write
	// This detects read-only databases that might have opened successfully
	// but would fail on actual write operations (e.g., chmod 0444)
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketMetadata))
		if b == nil {
			return fmt.Errorf("metadata bucket not found")
		}
		// Write and immediately delete a test key
		testKey := []byte("__writability_test__")
		if err := b.Put(testKey, []byte("1")); err != nil {
			return fmt.Errorf("database is read-only: %w", err)
		}
		return b.Delete(testKey)
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	var (
		watcher *fsnotify.Watcher
		ebpfMgr ebpf.Manager
		monitor string
	)

	if cfg != nil && cfg.EBPF.Enable {
		if mgr, err := ebpf.NewManager(stateDir, &cfg.EBPF); err != nil {
			if cfg.EBPF.FallbackFSNotify {
				log.Printf("[eBPF] initialization failed (%v), falling back to fsnotify", err)
			} else {
				db.Close()
				return nil, fmt.Errorf("ebpf initialization failed: %w", err)
			}
		} else {
			ebpfMgr = mgr
			monitor = "ebpf"
		}
	}

	if monitor == "" {
		watcher, err = fsnotify.NewWatcher()
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create watcher: %w", err)
		}
		monitor = "fsnotify"
	}
	log.Printf("[Monitor] configured backend: %s", monitor)

	// Initialize CAS store
	casStore, err := cas.NewCASStore(db, cfg.HashAlgo)
	if err != nil {
		db.Close()
		if watcher != nil {
			watcher.Close()
		}
		if ebpfMgr != nil {
			ebpfMgr.Close()
		}
		return nil, fmt.Errorf("failed to initialize CAS: %w", err)
	}

	// Initialize diff engine
	diffEngine, err := diff.NewDiffEngine(cfg.Library)
	if err != nil {
		db.Close()
		if watcher != nil {
			watcher.Close()
		}
		if ebpfMgr != nil {
			ebpfMgr.Close()
		}
		return nil, fmt.Errorf("failed to initialize diff engine: %w", err)
	}

	dk := &DiffKeeper{
		db:         db,
		stateDir:   stateDir,
		storePath:  storePath,
		watcher:    watcher,
		ebpfMgr:    ebpfMgr,
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
	// Step 1: Cancel context to signal all goroutines to stop
	if dk.cancelMon != nil {
		dk.cancelMon()
	}

	// Step 2: Wait for all monitoring goroutines to exit
	// This ensures no goroutine will try to access the database after we close it
	dk.wg.Wait()

	// Step 3: Close resources (safe because all goroutines have exited)
	if dk.watcher != nil {
		dk.watcher.Close()
	}
	if dk.ebpfMgr != nil {
		dk.ebpfMgr.Close()
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
	// Check if shutdown is in progress before accessing database
	if dk.monitorCtx != nil {
		select {
		case <-dk.monitorCtx.Done():
			return nil // Shutdown in progress, skip capture
		default:
		}
	}

	relPath, err := filepath.Rel(dk.stateDir, path)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if err := ensureReadable(path, info); err != nil {
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
	if err := dk.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketHashes))
		if v := b.Get([]byte(relPath)); v != nil {
			prevHash = string(v)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("hash lookup failed for %s: %w", relPath, err)
	}

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
	if dk.watcher == nil {
		return fmt.Errorf("fsnotify watcher not initialized")
	}

	log.Printf("[Watcher] Watching %s for changes...", dk.stateDir)

	if err := dk.addWatchRecursive(dk.stateDir); err != nil {
		return err
	}

	// Ensure we have a monitor context for coordinated shutdown
	if dk.monitorCtx == nil {
		dk.monitorCtx, dk.cancelMon = context.WithCancel(context.Background())
	}
	done := dk.monitorCtx.Done()

	for {
		select {
		case <-done:
			return nil
		case event, ok := <-dk.watcher.Events:
			if !ok {
				return nil
			}

			// Check if shutdown was requested while event was queued
			select {
			case <-done:
				return nil
			default:
			}

			logDebug("[Watcher] Event %s for %s", event.Op, event.Name)

			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			// Small delay to avoid capturing partial writes
			time.Sleep(100 * time.Millisecond)

			// Check again after sleep in case shutdown was requested
			select {
			case <-done:
				return nil
			default:
			}

			info, err := os.Stat(event.Name)
			if err != nil {
				logDebug("[Watcher] Skipping %s: %v", event.Name, err)
				continue
			}

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

		// eBPF monitoring configuration
		enableEBPF         bool
		profilerInterval   time.Duration
		enableProfiler     bool
		autoInject         bool
		ebpfProgramPath    string
		fallbackFSNotify   bool
		injectorCommand    string
		btfCacheDir        string
		btfHubMirror       string
		disableBTFDownload bool
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
			if cmd.Flags().Changed("enable-ebpf") {
				cfg.EBPF.Enable = enableEBPF
			}
			if cmd.Flags().Changed("profiler-interval") {
				cfg.EBPF.ProfilerInterval = profilerInterval
			}
			if cmd.Flags().Changed("enable-profiler") {
				cfg.EBPF.EnableProfiler = enableProfiler
			}
			if cmd.Flags().Changed("auto-inject") {
				cfg.EBPF.AutoInject = autoInject
			}
			if cmd.Flags().Changed("ebpf-program") {
				cfg.EBPF.ProgramPath = ebpfProgramPath
			}
			if cmd.Flags().Changed("fallback-fsnotify") {
				cfg.EBPF.FallbackFSNotify = fallbackFSNotify
			}
			if cmd.Flags().Changed("injector-cmd") {
				cfg.EBPF.InjectorCommand = injectorCommand
			}
			if cmd.Flags().Changed("btf-cache-dir") {
				cfg.EBPF.BTF.CacheDir = btfCacheDir
			}
			if cmd.Flags().Changed("btfhub-mirror") {
				cfg.EBPF.BTF.HubMirror = btfHubMirror
			}
			if cmd.Flags().Changed("disable-btfhub-download") {
				cfg.EBPF.BTF.AllowDownload = !disableBTFDownload
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

			// Phase 2: Start monitoring (eBPF or fsnotify) in background
			monitorCtx, monitorCancel := context.WithCancel(context.Background())
			defer monitorCancel()

			if err := dk.StartMonitoring(monitorCtx); err != nil {
				return fmt.Errorf("monitor initialization failed: %w", err)
			}

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

	// eBPF monitoring flags
	rootCmd.Flags().BoolVar(&enableEBPF, "enable-ebpf", true, "Enable eBPF-based syscall interception (Linux kernels >= 4.18)")
	rootCmd.Flags().DurationVar(&profilerInterval, "profiler-interval", 100*time.Millisecond, "Sampling interval for adaptive hot-path profiler")
	rootCmd.Flags().BoolVar(&enableProfiler, "enable-profiler", true, "Enable adaptive eBPF profiler to predict hot paths")
	rootCmd.Flags().BoolVar(&autoInject, "auto-inject", true, "Automatically inject DiffKeeper into new containers detected via CRI tracepoints")
	rootCmd.Flags().StringVar(&ebpfProgramPath, "ebpf-program", "", "Path to precompiled eBPF object (defaults to bin/ebpf/diffkeeper.bpf.o)")
	rootCmd.Flags().BoolVar(&fallbackFSNotify, "fallback-fsnotify", true, "Fallback to fsnotify watchers if eBPF initialization fails")
	rootCmd.Flags().StringVar(&injectorCommand, "injector-cmd", "", "Command to execute for auto-injection events (container ID passed as first argument)")
	rootCmd.Flags().StringVar(&btfCacheDir, "btf-cache-dir", "/var/cache/diffkeeper/btf", "Directory for cached BTF specs used during CO-RE relocations")
	rootCmd.Flags().StringVar(&btfHubMirror, "btfhub-mirror", "https://github.com/aquasecurity/btfhub-archive/raw/main", "Base URL for BTFHub-Archive downloads (override for private mirrors)")
	rootCmd.Flags().BoolVar(&disableBTFDownload, "disable-btfhub-download", false, "Disable automatic BTFHub downloads (requires kernel-provided BTF)")

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
