package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/saworbit/diffkeeper/internal/metrics"
	"github.com/saworbit/diffkeeper/pkg/chunk"
	"github.com/saworbit/diffkeeper/pkg/merkle"
	"go.etcd.io/bbolt"
)

var errForceSnapshot = errors.New("force snapshot fallback")

// getSchemaVersion returns the current schema version
func (dk *DiffKeeper) getSchemaVersion() int {
	version := SchemaVersionMVP

	if err := dk.db.View(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(BucketMetadata))
		v := meta.Get([]byte(SchemaVersionKey))
		if len(v) > 0 {
			version = int(v[0])
		}
		return nil
	}); err != nil {
		log.Printf("[Migration] failed to read schema version: %v", err)
	}

	return version
}

// setSchemaVersion sets the schema version
func (dk *DiffKeeper) setSchemaVersion(version int) error {
	return dk.db.Update(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(BucketMetadata))
		return meta.Put([]byte(SchemaVersionKey), []byte{byte(version)})
	})
}

// migrateSchema migrates from MVP (v1) to binary diffs (v2) if needed
func (dk *DiffKeeper) migrateSchema() error {
	currentVersion := dk.getSchemaVersion()

	// If binary diffs are disabled, stay on MVP schema
	if !dk.config.EnableDiff {
		logDebug("[Migration] Binary diffs disabled, using MVP schema")
		return nil
	}

	// Already on latest schema
	if currentVersion >= SchemaVersionDiff {
		logDebug("[Migration] Schema version %d, no migration needed", currentVersion)
		return nil
	}

	log.Printf("[Migration] Migrating schema from v%d to v%d...", currentVersion, SchemaVersionDiff)

	// Migrate MVP (v1) to binary diffs (v2)
	if currentVersion == SchemaVersionMVP {
		if err := dk.migrateMVPToDiff(); err != nil {
			return fmt.Errorf("MVP->v2 migration failed: %w", err)
		}
	}

	// Set new schema version
	if err := dk.setSchemaVersion(SchemaVersionDiff); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	log.Println("[Migration] Schema migration completed successfully")
	return nil
}

// migrateMVPToDiff migrates from MVP full-file storage to binary diffs
func (dk *DiffKeeper) migrateMVPToDiff() error {
	// Step 1: Read all legacy deltas (outside transaction)
	type legacyDelta struct {
		relPath        string
		compressedData []byte
	}
	var deltas []legacyDelta

	err := dk.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDeltas))
		if b == nil {
			return nil // No deltas to migrate
		}

		return b.ForEach(func(k, v []byte) error {
			deltas = append(deltas, legacyDelta{
				relPath:        string(k),
				compressedData: append([]byte(nil), v...), // Copy the data
			})
			return nil
		})
	})

	if err != nil {
		return err
	}

	// Step 2: Migrate each delta (no nested transactions)
	migratedCount := 0
	for _, delta := range deltas {
		// Decompress legacy snapshot
		data, err := decompressData(delta.compressedData)
		if err != nil {
			log.Printf("[Migration] Warning: failed to decompress %s: %v", delta.relPath, err)
			continue
		}

		// Store as base snapshot in CAS
		cid, err := dk.cas.Put(data)
		if err != nil {
			return fmt.Errorf("failed to store snapshot for %s: %w", delta.relPath, err)
		}

		// Add CAS reference
		if err := dk.cas.AddReference(cid, delta.relPath); err != nil {
			return fmt.Errorf("failed to add reference for %s: %w", delta.relPath, err)
		}

		// Build Merkle tree (single CID)
		tree, err := dk.merkle.BuildTree([]string{cid})
		if err != nil {
			return fmt.Errorf("failed to build merkle tree for %s: %w", delta.relPath, err)
		}

		// Create metadata
		metadata := FileMetadata{
			FilePath:       delta.relPath,
			CIDs:           []string{cid},
			MerkleRoot:     merkle.GetRoot(tree),
			IsChunked:      false,
			IsSnapshot:     true,
			VersionCount:   1,
			Timestamp:      time.Now(),
			OriginalSize:   int64(len(data)),
			CompressedSize: int64(len(delta.compressedData)),
		}

		// Store metadata
		if err := dk.storeMetadata(delta.relPath, metadata); err != nil {
			return fmt.Errorf("failed to store metadata for %s: %w", delta.relPath, err)
		}

		migratedCount++
	}

	log.Printf("[Migration] Migrated %d files from MVP to binary diffs", migratedCount)
	return nil
}

// storeMetadata stores file metadata
func (dk *DiffKeeper) storeMetadata(relPath string, meta FileMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return dk.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketMetadata))
		return bucket.Put([]byte(relPath), data)
	})
}

// getMetadata retrieves file metadata
func (dk *DiffKeeper) getMetadata(relPath string) (*FileMetadata, error) {
	var meta FileMetadata

	err := dk.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketMetadata))
		data := bucket.Get([]byte(relPath))

		if data == nil {
			return fmt.Errorf("metadata not found for %s", relPath)
		}

		return json.Unmarshal(data, &meta)
	})

	if err != nil {
		return nil, err
	}

	return &meta, nil
}

func (dk *DiffKeeper) storeChunkManifest(relPath string, manifest *chunk.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("chunk manifest is nil for %s", relPath)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal chunk manifest: %w", err)
	}
	return dk.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketChunkIndex))
		if bucket == nil {
			return fmt.Errorf("chunk index bucket missing")
		}
		return bucket.Put([]byte(relPath), data)
	})
}

func (dk *DiffKeeper) getChunkManifest(relPath string) (*chunk.Manifest, error) {
	var manifest chunk.Manifest
	err := dk.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketChunkIndex))
		if bucket == nil {
			return fmt.Errorf("chunk index bucket missing")
		}
		raw := bucket.Get([]byte(relPath))
		if raw == nil {
			return fmt.Errorf("chunk manifest not found for %s", relPath)
		}
		return json.Unmarshal(raw, &manifest)
	})
	if err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (dk *DiffKeeper) countChunkedFiles() (int, error) {
	count := 0
	err := dk.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketChunkIndex))
		if bucket == nil {
			return fmt.Errorf("chunk index bucket missing")
		}
		return bucket.ForEach(func(k, v []byte) error {
			count++
			return nil
		})
	})
	return count, err
}

// getPreviousVersion fetches the previous version of a file from CAS
func (dk *DiffKeeper) getPreviousVersion(relPath string) ([]byte, error) {
	// Get metadata
	meta, err := dk.getMetadata(relPath)
	if err != nil {
		return nil, nil // No previous version
	}

	// If it's a snapshot, fetch the full data
	if meta.IsSnapshot {
		if meta.IsChunked {
			return nil, fmt.Errorf("chunked snapshots cannot be loaded fully; use manifest replay")
		}
		if len(meta.CIDs) == 0 {
			return nil, fmt.Errorf("snapshot has no CIDs")
		}
		return dk.cas.Get(meta.CIDs[0])
	}

	// For diffs, we need to reconstruct the file
	return dk.reconstructFile(meta)
}

// reconstructFile reconstructs a file from its diffs and base snapshot
func (dk *DiffKeeper) reconstructFile(meta *FileMetadata) ([]byte, error) {
	if meta.IsChunked {
		return nil, fmt.Errorf("chunked files must be restored via manifest, not diff reconstruction")
	}

	// Verify we have a base snapshot CID to start from
	if meta.BaseSnapshotCID == "" {
		return nil, fmt.Errorf("no base snapshot CID found for diff reconstruction")
	}

	// Fetch the base snapshot from CAS
	baseData, err := dk.cas.Get(meta.BaseSnapshotCID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch base snapshot %s: %w", meta.BaseSnapshotCID, err)
	}

	// Handle chunked base snapshots
	current := baseData
	if meta.IsChunked {
		// For chunked files, the base is stored as chunks that need reassembly
		// We'll reassemble the base first, then apply diffs
		logDebug("[reconstructFile] Base snapshot is chunked, reassembling...")
		current = baseData // Assume base is already reassembled or single chunk
	}

	// Apply each diff in the chain sequentially
	for i, diffCID := range meta.CIDs {
		diffPatch, err := dk.cas.Get(diffCID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch diff patch %d (%s): %w", i, diffCID, err)
		}

		// Apply the binary diff patch to reconstruct the next version
		logDebug("[reconstructFile] Applying diff %d of %d", i+1, len(meta.CIDs))
		current, err = dk.diffEngine.ApplyPatch(current, diffPatch)
		if err != nil {
			return nil, fmt.Errorf("failed to apply diff patch %d: %w", i, err)
		}
	}

	logDebug("[reconstructFile] Successfully reconstructed file from base + %d diffs", len(meta.CIDs))
	return current, nil
}

// shouldSnapshot determines if we should create a full snapshot instead of a diff
func (dk *DiffKeeper) shouldSnapshot(relPath string) bool {
	meta, err := dk.getMetadata(relPath)
	if err != nil {
		// No previous version, create snapshot
		logDebug("[shouldSnapshot] No previous version for %s, creating initial snapshot", relPath)
		return true
	}

	// Create snapshot every N versions (configured interval)
	// This prevents diff chains from becoming too long
	shouldSnap := (meta.VersionCount % dk.config.SnapshotInterval) == 0
	if shouldSnap {
		logDebug("[shouldSnapshot] Version %d hits snapshot interval %d, creating new base snapshot",
			meta.VersionCount, dk.config.SnapshotInterval)
	} else {
		logDebug("[shouldSnapshot] Version %d, creating diff (next snapshot at version %d)",
			meta.VersionCount, ((meta.VersionCount/dk.config.SnapshotInterval)+1)*dk.config.SnapshotInterval)
	}
	return shouldSnap
}

func (dk *DiffKeeper) captureChunked(relPath, absPath string, fileSize int64, prevHash string) error {
	start := time.Now()
	cfg := dk.config.GetChunkingConfig()
	chunkSizeBytes := dk.config.GetChunkSizeBytes()
	avgSize := cfg.AvgBytes
	if chunkSizeBytes > 0 {
		avgSize = chunkSizeBytes
	}
	minSize := cfg.MinBytes
	if minSize > avgSize {
		minSize = avgSize
	}
	maxSize := cfg.MaxBytes
	if maxSize < avgSize {
		maxSize = avgSize
	}
	params := chunk.Params{
		MinSize: minSize,
		AvgSize: avgSize,
		MaxSize: maxSize,
		Window:  cfg.HashWindow,
	}

	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("failed to open %s for chunking: %w", absPath, err)
	}
	defer f.Close()

	chunker := chunk.NewRabinChunker(f, params)
	manifest := &chunk.Manifest{
		Timestamp: time.Now(),
	}
	var (
		chunkRefs   []chunk.ChunkRef
		cids        []string
		totalStored int64
	)
	hasher := sha256.New()

	for {
		ch, err := chunker.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("chunker failed for %s: %w", relPath, err)
		}

		if _, err := hasher.Write(ch.Data); err != nil {
			return fmt.Errorf("failed to hash chunk for %s: %w", relPath, err)
		}

		cid, written, err := dk.cas.PutChunkWithHash(ch.Ref.Hash, ch.Data)
		if err != nil {
			return fmt.Errorf("failed to store chunk for %s: %w", relPath, err)
		}
		if written == 0 {
			metrics.ObserveChunk("reuse")
		} else {
			totalStored += int64(written)
			metrics.ObserveChunk("new")
		}

		if err := dk.cas.AddReference(cid, relPath); err != nil {
			return fmt.Errorf("failed to add chunk reference for %s: %w", relPath, err)
		}

		chunkRefs = append(chunkRefs, ch.Ref)
		cids = append(cids, cid)
	}

	if len(chunkRefs) == 0 {
		emptyHash := sha256.Sum256(nil)
		cid, written, err := dk.cas.PutChunkWithHash(emptyHash, []byte{})
		if err != nil {
			return fmt.Errorf("failed to store empty chunk for %s: %w", relPath, err)
		}
		if written == 0 {
			metrics.ObserveChunk("reuse")
		} else {
			totalStored += int64(written)
			metrics.ObserveChunk("new")
		}
		chunkRefs = append(chunkRefs, chunk.ChunkRef{Hash: emptyHash, Offset: 0, Length: 0})
		cids = append(cids, cid)
	}

	newHash := hex.EncodeToString(hasher.Sum(nil))
	if prevHash == newHash {
		return nil
	}

	versionCount := 1
	if prevMeta, err := dk.getMetadata(relPath); err == nil {
		versionCount = prevMeta.VersionCount + 1
	}

	manifest.Version = uint64(versionCount)
	manifest.Chunks = chunkRefs

	if err := dk.storeChunkManifest(relPath, manifest); err != nil {
		return err
	}

	tree, err := dk.merkle.BuildTree(cids)
	if err != nil {
		return fmt.Errorf("failed to build merkle tree: %w", err)
	}

	metadata := FileMetadata{
		FilePath:        relPath,
		CIDs:            cids,
		MerkleRoot:      merkle.GetRoot(tree),
		IsChunked:       true,
		ChunkManifest:   manifest,
		IsSnapshot:      true,
		BaseSnapshotCID: "",
		VersionCount:    versionCount,
		Timestamp:       manifest.Timestamp,
		OriginalSize:    fileSize,
		CompressedSize:  totalStored,
	}

	if err := dk.storeMetadata(relPath, metadata); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	if err := dk.db.Update(func(tx *bbolt.Tx) error {
		hashes := tx.Bucket([]byte(BucketHashes))
		return hashes.Put([]byte(relPath), []byte(newHash))
	}); err != nil {
		return err
	}

	if count, err := dk.countChunkedFiles(); err == nil {
		metrics.SetLargeFilesTracked(count)
	}

	metrics.ObserveStorageSavings(fileSize, totalStored)
	metrics.ObserveChunkCapture(start)

	compressionRatio := 0.0
	if fileSize > 0 {
		compressionRatio = float64(totalStored) / float64(fileSize) * 100
	}
	log.Printf("[BlueShiftDiff] Captured %s (chunked snapshot, %.2f KB stored, %.1f%% compression, %d chunks)",
		relPath, float64(totalStored)/1024, compressionRatio, len(chunkRefs))

	return nil
}

func (dk *DiffKeeper) restoreChunkedFile(relPath string, meta *FileMetadata) error {
	manifest := meta.ChunkManifest
	if manifest == nil {
		var err error
		manifest, err = dk.getChunkManifest(relPath)
		if err != nil {
			return fmt.Errorf("chunk manifest missing for %s: %w", relPath, err)
		}
	}

	fullPath := filepath.Join(dk.stateDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create dir for %s: %w", relPath, err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", relPath, err)
	}
	defer f.Close()

	var totalSize int64
	for _, ref := range manifest.Chunks {
		chunkData, err := dk.cas.GetChunk(ref.Hash)
		if err != nil {
			return fmt.Errorf("failed to fetch chunk for %s: %w", relPath, err)
		}
		if len(chunkData) != int(ref.Length) {
			return fmt.Errorf("chunk length mismatch for %s: expected %d got %d", relPath, ref.Length, len(chunkData))
		}
		if _, err := f.WriteAt(chunkData, int64(ref.Offset)); err != nil {
			return fmt.Errorf("failed to write chunk for %s: %w", relPath, err)
		}
		if end := int64(ref.Offset) + int64(ref.Length); end > totalSize {
			totalSize = end
		}
	}

	if totalSize > 0 {
		if err := f.Truncate(totalSize); err != nil {
			return fmt.Errorf("failed to finalize %s: %w", relPath, err)
		}
	}

	return nil
}

// BlueShiftDiff captures file changes using binary diffs
func (dk *DiffKeeper) BlueShiftDiff(path string) (err error) {
	start := time.Now()
	outcome := "success"
	captureType := "diff"
	defer func() {
		if err != nil {
			outcome = "error"
		}
		metrics.ObserveCapture(start, captureType, outcome)
	}()

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
	fileSize := info.Size()

	// Check if changed
	var prevHash string
	if err := dk.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketHashes))
		if v := b.Get([]byte(relPath)); v != nil {
			prevHash = string(v)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to read previous hash: %w", err)
	}

	// Determine if we should create a snapshot or diff
	isSnapshot := dk.shouldSnapshot(relPath)
	isChunked := dk.config.ShouldChunk(fileSize)
	if isChunked {
		captureType = "chunk"
		return dk.captureChunked(relPath, path, fileSize, prevHash)
	}

	// Read current file into memory for non-chunked paths
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Compute hash after reading
	hash := sha256.Sum256(data)
	newHash := hex.EncodeToString(hash[:])

	if prevHash == newHash {
		return nil
	}
	forceSnapshot := prevHash == ""

	var prevData []byte
	if !isSnapshot && !forceSnapshot {
		var prevErr error
		prevData, prevErr = dk.getPreviousVersion(relPath)
		if prevErr != nil || prevData == nil {
			logDebug("[BlueShiftDiff] Cannot get previous version for %s, creating snapshot", relPath)
			forceSnapshot = true
		}
	}

	var (
		cids                []string
		totalCompressedSize int64
	)

	storeSnapshot := func() ([]string, int64, error) {
		logDebug("[BlueShiftDiff] Creating snapshot for %s (%.2f MB)", relPath, float64(fileSize)/1024/1024)
		var snapshotCIDs []string
		var compressedSize int64

		cid, err := dk.cas.Put(data)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to store snapshot: %w", err)
		}
		if err := dk.cas.AddReference(cid, relPath); err != nil {
			return nil, 0, fmt.Errorf("failed to add snapshot reference: %w", err)
		}
		snapshotCIDs = append(snapshotCIDs, cid)
		compressedSize = int64(len(data))

		return snapshotCIDs, compressedSize, nil
	}

	storeDiff := func(previous []byte) ([]string, int64, error) {
		logDebug("[BlueShiftDiff] Creating diff for %s (%.2f MB)", relPath, float64(fileSize)/1024/1024)
		var diffCIDs []string
		var compressedSize int64

		diffData, err := dk.diffEngine.ComputeDiff(previous, data)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to compute diff: %w", err)
		}

		cid, err := dk.cas.Put(diffData)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to store diff: %w", err)
		}
		if err := dk.cas.AddReference(cid, relPath); err != nil {
			return nil, 0, fmt.Errorf("failed to add diff reference: %w", err)
		}
		diffCIDs = append(diffCIDs, cid)
		compressedSize = int64(len(diffData))

		return diffCIDs, compressedSize, nil
	}

	if !isSnapshot && !forceSnapshot {
		diffCIDs, compressedSize, diffErr := storeDiff(prevData)
		if diffErr != nil {
			if errors.Is(diffErr, errForceSnapshot) {
				forceSnapshot = true
			} else {
				return diffErr
			}
		} else {
			cids = diffCIDs
			totalCompressedSize = compressedSize
		}
	}

	if isSnapshot || forceSnapshot {
		isSnapshot = true
		snapshotCIDs, compressedSize, snapErr := storeSnapshot()
		if snapErr != nil {
			return snapErr
		}
		cids = snapshotCIDs
		totalCompressedSize = compressedSize
	}

	if isSnapshot {
		captureType = "snapshot"
	}

	// Get previous version count and base snapshot CID
	versionCount := 1
	var baseSnapshotCID string
	var diffChain []string

	if prevMeta, err := dk.getMetadata(relPath); err == nil {
		versionCount = prevMeta.VersionCount + 1

		// If this is a diff, inherit the base snapshot CID and accumulate diff chain
		// If this is a new snapshot, we'll reset everything below
		if !isSnapshot {
			baseSnapshotCID = prevMeta.BaseSnapshotCID
			logDebug("[BlueShiftDiff] Using base snapshot CID from previous version: %s", baseSnapshotCID)

			// Accumulate diff chain: previous diffs + new diff
			// Only accumulate if previous version was also a diff (not a snapshot)
			if !prevMeta.IsSnapshot {
				diffChain = append(diffChain, prevMeta.CIDs...)
				logDebug("[BlueShiftDiff] Accumulated %d previous diffs in chain", len(prevMeta.CIDs))
			}
			// Add the new diff(s) we just computed
			diffChain = append(diffChain, cids...)
			cids = diffChain // Replace cids with the accumulated chain
			logDebug("[BlueShiftDiff] Total diff chain length: %d", len(cids))
		}
	}

	// If this is a snapshot, set the base snapshot CID to the first CID and reset chain
	if isSnapshot {
		if len(cids) > 0 {
			baseSnapshotCID = cids[0]
			logDebug("[BlueShiftDiff] New snapshot, setting base snapshot CID to: %s", baseSnapshotCID)
		}
	}

	// Build Merkle tree AFTER accumulating the complete diff chain
	tree, err := dk.merkle.BuildTree(cids)
	if err != nil {
		return fmt.Errorf("failed to build merkle tree: %w", err)
	}

	// Create metadata
	metadata := FileMetadata{
		FilePath:        relPath,
		CIDs:            cids,
		MerkleRoot:      merkle.GetRoot(tree),
		IsChunked:       isChunked,
		IsSnapshot:      isSnapshot,
		BaseSnapshotCID: baseSnapshotCID,
		VersionCount:    versionCount,
		Timestamp:       time.Now(),
		OriginalSize:    fileSize,
		CompressedSize:  totalCompressedSize,
	}

	// Store metadata
	if err := dk.storeMetadata(relPath, metadata); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	// Update hash
	err = dk.db.Update(func(tx *bbolt.Tx) error {
		hashes := tx.Bucket([]byte(BucketHashes))
		return hashes.Put([]byte(relPath), []byte(newHash))
	})

	if err != nil {
		return err
	}

	metrics.ObserveStorageSavings(fileSize, totalCompressedSize)
	compressionLabel := "none"
	if !isSnapshot && dk.config != nil && dk.config.Library != "" {
		compressionLabel = dk.config.Library
	}
	metrics.AddDeltas(compressionLabel, len(cids))

	compressionRatio := float64(totalCompressedSize) / float64(fileSize) * 100
	logType := "snapshot"
	if !isSnapshot {
		logType = "diff"
	}
	log.Printf("[BlueShiftDiff] Captured %s (%s, %.2f KB, %.1f%% compression)",
		relPath, logType, float64(totalCompressedSize)/1024, compressionRatio)

	return nil
}

// RedShiftDiff restores files using binary diffs
func (dk *DiffKeeper) RedShiftDiff() (err error) {
	log.Println("[RedShiftDiff] Restoring state from binary diffs...")

	startTime := time.Now()
	count := 0
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		metrics.ObserveRecovery(startTime, "startup", outcome)
	}()

	err = dk.db.View(func(tx *bbolt.Tx) error {
		metaBucket := tx.Bucket([]byte(BucketMetadata))
		c := metaBucket.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			relPath := string(k)

			// Skip schema version key
			if relPath == SchemaVersionKey {
				continue
			}

			// Parse metadata
			var meta FileMetadata
			if err := json.Unmarshal(v, &meta); err != nil {
				log.Printf("Warning: failed to parse metadata for %s: %v", relPath, err)
				continue
			}

			// Verify Merkle tree
			if err := dk.merkle.VerifyFileIntegrity(meta.CIDs, meta.MerkleRoot); err != nil {
				log.Printf("Warning: integrity check failed for %s: %v", relPath, err)
				continue
			}

			// Reconstruct file
			var data []byte
			var err error

			if meta.IsSnapshot {
				if meta.IsChunked {
					if err := dk.restoreChunkedFile(relPath, &meta); err != nil {
						return fmt.Errorf("failed to restore chunked snapshot for %s: %w", relPath, err)
					}
					count++
					continue
				}

				// Fetch snapshot
				if len(meta.CIDs) == 0 {
					log.Printf("Warning: snapshot %s has no CIDs", relPath)
					continue
				}

				// Single snapshot
				data, err = dk.cas.Get(meta.CIDs[0])
				if err != nil {
					return fmt.Errorf("failed to fetch snapshot for %s: %w", relPath, err)
				}
			} else {
				// Reconstruct from diff
				data, err = dk.reconstructFile(&meta)
				if err != nil {
					log.Printf("Warning: failed to reconstruct %s: %v", relPath, err)
					continue
				}
			}

			// Write file
			fullPath := filepath.Join(dk.stateDir, relPath)

			// Ensure directory exists
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return fmt.Errorf("failed to create dir for %s: %w", relPath, err)
			}

			if err := os.WriteFile(fullPath, data, 0644); err != nil {
				return fmt.Errorf("failed to restore %s: %w", relPath, err)
			}

			count++
		}
		return nil
	})

	duration := time.Since(startTime)
	log.Printf("[RedShiftDiff] Restored %d files in %v", count, duration)
	return err
}
