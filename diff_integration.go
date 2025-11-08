package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/yourorg/diffkeeper/pkg/chunk"
	"github.com/yourorg/diffkeeper/pkg/merkle"
	"go.etcd.io/bbolt"
)

// getSchemaVersion returns the current schema version
func (dk *DiffKeeper) getSchemaVersion() int {
	var version int = SchemaVersionMVP // Default to MVP

	dk.db.View(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte(BucketMetadata))
		v := meta.Get([]byte(SchemaVersionKey))
		if v != nil && len(v) > 0 {
			version = int(v[0])
		}
		return nil
	})

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

// getPreviousVersion fetches the previous version of a file from CAS
func (dk *DiffKeeper) getPreviousVersion(relPath string) ([]byte, error) {
	// Get metadata
	meta, err := dk.getMetadata(relPath)
	if err != nil {
		return nil, nil // No previous version
	}

	// If it's a snapshot, fetch the full data
	if meta.IsSnapshot {
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
	// Fetch all CIDs (diff patches) from CAS
	var diffPatches [][]byte
	for _, cid := range meta.CIDs {
		data, err := dk.cas.Get(cid)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch CID %s: %w", cid, err)
		}
		diffPatches = append(diffPatches, data)
	}

	// For diffs, we need to get the base snapshot and apply the patches
	// The current implementation stores each version as either a snapshot or a diff
	// For diffs, we need to find the most recent snapshot and apply all subsequent diffs

	// Simplified approach: For now, treat this as a snapshot since we're storing
	// full reconstructed data. In a full implementation, we'd track base snapshot CIDs
	// and apply diffs incrementally.

	// TODO: Implement proper diff chain reconstruction with base snapshot tracking

	if meta.IsChunked {
		return chunk.ReassembleChunks(diffPatches), nil
	}
	if len(diffPatches) > 0 {
		return diffPatches[0], nil
	}

	return nil, fmt.Errorf("no data chunks found")
}

// shouldSnapshot determines if we should create a full snapshot instead of a diff
func (dk *DiffKeeper) shouldSnapshot(relPath string) bool {
	_, err := dk.getMetadata(relPath)
	if err != nil {
		// No previous version, create snapshot
		return true
	}

	// TODO: For now, always create snapshots until we implement proper diff reconstruction
	// with base snapshot tracking. Remove this when diff chains are fully implemented.
	return true

	// Create snapshot every N versions (configured interval)
	// return meta.VersionCount%dk.config.SnapshotInterval == 0
}

// BlueShiftDiff captures file changes using binary diffs
func (dk *DiffKeeper) BlueShiftDiff(path string) error {
	relPath, err := filepath.Rel(dk.stateDir, path)
	if err != nil {
		return err
	}

	// Read current file
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fileSize := int64(len(data))

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

	// Determine if we should create a snapshot or diff
	isSnapshot := dk.shouldSnapshot(relPath)
	isChunked := dk.config.ShouldChunk(fileSize)

	var cids []string
	var totalCompressedSize int64

	if isSnapshot || prevHash == "" {
		// Create full snapshot
		logDebug("[BlueShiftDiff] Creating snapshot for %s (%.2f MB)", relPath, float64(fileSize)/1024/1024)

		if isChunked {
			// Split into chunks and store
			chunks := chunk.SplitFile(data, dk.config.GetChunkSizeBytes())
			for _, chunkData := range chunks {
				cid, err := dk.cas.Put(chunkData)
				if err != nil {
					return fmt.Errorf("failed to store chunk: %w", err)
				}
				dk.cas.AddReference(cid, relPath)
				cids = append(cids, cid)
				totalCompressedSize += int64(len(chunkData))
			}
		} else {
			// Store as single snapshot
			cid, err := dk.cas.Put(data)
			if err != nil {
				return fmt.Errorf("failed to store snapshot: %w", err)
			}
			dk.cas.AddReference(cid, relPath)
			cids = append(cids, cid)
			totalCompressedSize = int64(len(data))
		}
	} else {
		// Create binary diff
		prevData, err := dk.getPreviousVersion(relPath)
		if err != nil || prevData == nil {
			// Fallback to snapshot if we can't get previous version
			logDebug("[BlueShiftDiff] Cannot get previous version for %s, creating snapshot", relPath)
			return dk.BlueShiftDiff(path) // Recursive call will create snapshot
		}

		logDebug("[BlueShiftDiff] Creating diff for %s (%.2f MB)", relPath, float64(fileSize)/1024/1024)

		if isChunked {
			// Compute per-chunk diffs
			chunks := chunk.SplitFile(data, dk.config.GetChunkSizeBytes())
			prevChunks := chunk.SplitFile(prevData, dk.config.GetChunkSizeBytes())

			// Ensure same number of chunks (if file grew/shrunk, treat as snapshot)
			if len(chunks) != len(prevChunks) {
				logDebug("[BlueShiftDiff] Chunk count changed, creating snapshot")
				isSnapshot = true
				return dk.BlueShiftDiff(path)
			}

			for i, chunkData := range chunks {
				diffData, err := dk.diffEngine.ComputeDiff(prevChunks[i], chunkData)
				if err != nil {
					return fmt.Errorf("failed to compute diff for chunk %d: %w", i, err)
				}

				cid, err := dk.cas.Put(diffData)
				if err != nil {
					return fmt.Errorf("failed to store diff chunk: %w", err)
				}
				dk.cas.AddReference(cid, relPath)
				cids = append(cids, cid)
				totalCompressedSize += int64(len(diffData))
			}
		} else {
			// Compute single diff
			diffData, err := dk.diffEngine.ComputeDiff(prevData, data)
			if err != nil {
				return fmt.Errorf("failed to compute diff: %w", err)
			}

			cid, err := dk.cas.Put(diffData)
			if err != nil {
				return fmt.Errorf("failed to store diff: %w", err)
			}
			dk.cas.AddReference(cid, relPath)
			cids = append(cids, cid)
			totalCompressedSize = int64(len(diffData))
		}
	}

	// Build Merkle tree
	tree, err := dk.merkle.BuildTree(cids)
	if err != nil {
		return fmt.Errorf("failed to build merkle tree: %w", err)
	}

	// Get previous version count
	versionCount := 1
	if prevMeta, err := dk.getMetadata(relPath); err == nil {
		versionCount = prevMeta.VersionCount + 1
	}

	// Create metadata
	metadata := FileMetadata{
		FilePath:       relPath,
		CIDs:           cids,
		MerkleRoot:     merkle.GetRoot(tree),
		IsChunked:      isChunked,
		IsSnapshot:     isSnapshot,
		VersionCount:   versionCount,
		Timestamp:      time.Now(),
		OriginalSize:   fileSize,
		CompressedSize: totalCompressedSize,
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

	if err == nil {
		compressionRatio := float64(totalCompressedSize) / float64(fileSize) * 100
		logType := "snapshot"
		if !isSnapshot {
			logType = "diff"
		}
		log.Printf("[BlueShiftDiff] Captured %s (%s, %.2f KB, %.1f%% compression)",
			relPath, logType, float64(totalCompressedSize)/1024, compressionRatio)
	}

	return err
}

// RedShiftDiff restores files using binary diffs
func (dk *DiffKeeper) RedShiftDiff() error {
	log.Println("[RedShiftDiff] Restoring state from binary diffs...")

	startTime := time.Now()
	count := 0

	err := dk.db.View(func(tx *bbolt.Tx) error {
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
				// Fetch snapshot
				if len(meta.CIDs) == 0 {
					log.Printf("Warning: snapshot %s has no CIDs", relPath)
					continue
				}

				if meta.IsChunked {
					// Reassemble chunks
					var chunks [][]byte
					for _, cid := range meta.CIDs {
						chunkData, err := dk.cas.Get(cid)
						if err != nil {
							return fmt.Errorf("failed to fetch chunk for %s: %w", relPath, err)
						}
						chunks = append(chunks, chunkData)
					}
					data = chunk.ReassembleChunks(chunks)
				} else {
					// Single snapshot
					data, err = dk.cas.Get(meta.CIDs[0])
					if err != nil {
						return fmt.Errorf("failed to fetch snapshot for %s: %w", relPath, err)
					}
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
