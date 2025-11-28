# DiffKeeper Binary Diffs Implementation Plan

**Created:** November 4, 2025
**Based on:** Binary Diffs Specification v1.0
**Target:** v1.0 Release (2-4 weeks)

---

## Executive Summary

This document outlines the phased implementation plan for adding binary diffs, chunking, CAS, and Merkle trees to DiffKeeper, transitioning from MVP (full-file gzip) to production-ready delta storage.

**Key Metrics:**
- Target storage reduction: 50-80% for partial file updates
- Max overhead: <1% CPU/memory vs. current implementation
- Recovery time: Maintain <100ms for 1GB files

---

## Phase 1: Foundation & Core Diffing (Week 1)

### Milestone 1.1: Project Structure & Dependencies
**Duration:** 1-2 days
**Goal:** Reorganize codebase for scalability

**Tasks:**
1. Create package structure:
   ```
   pkg/
   ├── diff/       # Binary diff operations (bsdiff/xdelta)
   ├── cas/        # Content-addressable storage
   ├── merkle/     # Merkle tree operations
   ├── chunk/      # File chunking logic
   └── config/     # Configuration management
   ```

2. Add dependencies to `go.mod`:
   ```bash
   go get github.com/gabstv/go-bsdiff        # bsdiff Go bindings
   go get github.com/multiformats/go-multihash  # CAS hashing
   go get github.com/cbergoon/merkletree      # Merkle trees
   ```

3. Create configuration model:
   ```go
   type DiffConfig struct {
       Library      string  // "bsdiff" or "xdelta"
       ChunkSizeMB  int     // Default: 4
       HashAlgo     string  // "sha256" or "blake3"
       DedupScope   string  // "container" or "cluster"
       EnableDiff   bool    // Feature flag
   }
   ```

**Deliverables:**
- [ ] Package structure created
- [ ] Dependencies added and vendored
- [ ] Configuration struct with YAML/ENV loading
- [ ] Unit tests for config parsing

**Integration Points:**
- Add to `DiffKeeper` struct: `config *DiffConfig`
- CLI flags: `--enable-diff`, `--chunk-size`, etc.

---

### Milestone 1.2: Binary Diff Engine
**Duration:** 2-3 days
**Goal:** Implement core diff/patch operations

**Tasks:**
1. Create `pkg/diff/diff.go`:
   ```go
   type DiffEngine interface {
       ComputeDiff(oldData, newData []byte) ([]byte, error)
       ApplyPatch(baseData, patchData []byte) ([]byte, error)
   }

   type BsdiffEngine struct{}
   type XdeltaEngine struct{}
   ```

2. Implement bsdiff wrapper:
   ```go
   func (e *BsdiffEngine) ComputeDiff(old, new []byte) ([]byte, error) {
       return bsdiff.Diff(old, new)
   }

   func (e *BsdiffEngine) ApplyPatch(base, patch []byte) ([]byte, error) {
       return bsdiff.Patch(base, patch)
   }
   ```

3. Add fallback logic:
   ```go
   func NewDiffEngine(library string) (DiffEngine, error) {
       switch library {
       case "bsdiff":
           return &BsdiffEngine{}, nil
       case "xdelta":
           return &XdeltaEngine{}, nil
       default:
           return nil, fmt.Errorf("unsupported diff library: %s", library)
       }
   }
   ```

**Deliverables:**
- [ ] `DiffEngine` interface implemented
- [ ] Bsdiff and xdelta adapters
- [ ] Factory pattern for engine selection
- [ ] Unit tests: 1MB file with 10% changes, compression ratio validation

**Integration Points:**
- Call in BlueShift™: `diffEngine.ComputeDiff(prevData, newData)`
- Call in RedShift™: `diffEngine.ApplyPatch(baseData, patchData)`

---

### Milestone 1.3: Chunking Support
**Duration:** 2 days
**Goal:** Split large files into manageable chunks

**Tasks:**
1. Create `pkg/chunk/chunk.go`:
   ```go
   type ChunkMetadata struct {
       FileHash     string
       ChunkIndex   int
       ChunkSize    int
       TotalChunks  int
       ChunkHash    string
   }

   func SplitFile(data []byte, chunkSize int) [][]byte
   func ReassembleChunks(chunks [][]byte) []byte
   func ShouldChunk(fileSize int64, threshold int64) bool
   ```

2. Implement chunking logic:
   ```go
   func SplitFile(data []byte, chunkSizeMB int) [][]byte {
       chunkSize := chunkSizeMB * 1024 * 1024
       var chunks [][]byte
       for i := 0; i < len(data); i += chunkSize {
           end := min(i+chunkSize, len(data))
           chunks = append(chunks, data[i:end])
       }
       return chunks
   }
   ```

3. Per-chunk diff computation:
   ```go
   func ComputeChunkedDiff(oldData, newData []byte, chunkSize int) ([]ChunkDiff, error) {
       oldChunks := SplitFile(oldData, chunkSize)
       newChunks := SplitFile(newData, chunkSize)

       var diffs []ChunkDiff
       for i := range newChunks {
           diff, _ := diffEngine.ComputeDiff(oldChunks[i], newChunks[i])
           diffs = append(diffs, ChunkDiff{Index: i, Data: diff})
       }
       return diffs, nil
   }
   ```

**Deliverables:**
- [ ] Chunking functions implemented
- [ ] Per-chunk diff computation
- [ ] Chunk reassembly logic
- [ ] Unit tests: 5GB file, verify chunk boundaries

**Integration Points:**
- BlueShift™: Check file size → chunk if >1GB → compute per-chunk diffs
- RedShift™: Fetch chunks → reassemble → apply diffs

---

## Phase 2: CAS & Merkle Trees (Week 2)

### Milestone 2.1: Content-Addressable Storage
**Duration:** 3 days
**Goal:** Implement CAS layer with deduplication

**Tasks:**
1. Create `pkg/cas/store.go`:
   ```go
   type CASStore struct {
       db *bbolt.DB
   }

   type CASObject struct {
       CID  string    // Content identifier (multihash)
       Data []byte
       Size int
   }

   func (c *CASStore) Put(data []byte) (cid string, err error)
   func (c *CASStore) Get(cid string) ([]byte, error)
   func (c *CASStore) Has(cid string) bool
   func (c *CASStore) Delete(cid string) error
   ```

2. Implement hashing with multihash:
   ```go
   func computeCID(data []byte) string {
       mh, _ := multihash.Sum(data, multihash.SHA2_256, -1)
       return mh.B58String()
   }

   func (c *CASStore) Put(data []byte) (string, error) {
       cid := computeCID(data)

       // Check if already exists (deduplication)
       if c.Has(cid) {
           return cid, nil
       }

       // Store in BoltDB cas bucket
       err := c.db.Update(func(tx *bbolt.Tx) error {
           bucket := tx.Bucket([]byte("cas"))
           return bucket.Put([]byte(cid), data)
       })
       return cid, err
   }
   ```

3. Add reference counting for garbage collection:
   ```go
   type CASRefCount struct {
       CID   string
       Refs  int
       Files []string  // Which files reference this CID
   }
   ```

**Deliverables:**
- [ ] CAS storage interface implemented
- [ ] Multihash-based CID generation
- [ ] Deduplication logic
- [ ] Reference counting for GC
- [ ] Unit tests: Store 100 chunks, verify dedup rate

**Integration Points:**
- BlueShift™: `casStore.Put(diffData)` → get CID
- RedShift™: `casStore.Get(cid)` → retrieve diff data
- New BoltDB bucket: `BucketCAS = "cas"`

---

### Milestone 2.2: Merkle Tree Integration
**Duration:** 2-3 days
**Goal:** Add integrity verification

**Tasks:**
1. Create `pkg/merkle/tree.go`:
   ```go
   type MerkleManager struct {
       treeCache map[string]*merkletree.MerkleTree
   }

   func (m *MerkleManager) BuildTree(chunks [][]byte) (*merkletree.MerkleTree, error)
   func (m *MerkleManager) VerifyProof(index int, proof [][]byte, root []byte) bool
   func (m *MerkleManager) GetRoot(filePath string) []byte
   ```

2. Build tree from chunk CIDs:
   ```go
   func (m *MerkleManager) BuildTree(cids []string) (*merkletree.MerkleTree, error) {
       var contents []merkletree.Content
       for _, cid := range cids {
           contents = append(contents, merkletree.NewContent(cid))
       }
       return merkletree.NewTree(contents)
   }
   ```

3. Verification during RedShift™:
   ```go
   func (dk *DiffKeeper) verifyFileIntegrity(filePath string, cids []string) error {
       tree, _ := dk.merkle.BuildTree(cids)
       expectedRoot := dk.getStoredMerkleRoot(filePath)

       if !bytes.Equal(tree.MerkleRoot(), expectedRoot) {
           return fmt.Errorf("merkle verification failed for %s", filePath)
       }
       return nil
   }
   ```

**Deliverables:**
- [ ] Merkle tree wrapper implemented
- [ ] Tree building from CIDs
- [ ] Proof generation and verification
- [ ] Unit tests: Corrupt chunk → verification fails

**Integration Points:**
- BlueShift™: Build tree → store root in metadata
- RedShift™: Verify root before applying diffs
- Metadata bucket: Store `{filePath: {cids: [], merkleRoot: "..."}}`

---

## Phase 3: Integration & Migration (Week 3)

### Milestone 3.1: BlueShift™ Integration
**Duration:** 2-3 days
**Goal:** Update capture engine to use binary diffs

**Tasks:**
1. Extend BlueShift™ function ([main.go:163-214](main.go#L163-L214)):
   ```go
   func (dk *DiffKeeper) BlueShift(path string) error {
       // ... existing hash check ...

       if prevHash == newHash {
           return nil
       }

       // NEW: Check if diff mode enabled
       if dk.config.EnableDiff {
           return dk.blueShiftDiff(path, data, relPath)
       }

       // LEGACY: Fall back to full compression
       compressed, _ := compressData(data)
       return dk.storeDelta(relPath, compressed, newHash)
   }
   ```

2. Implement `blueShiftDiff`:
   ```go
   func (dk *DiffKeeper) blueShiftDiff(path string, data []byte, relPath string) error {
       // 1. Check if large file (>1GB)
       shouldChunk := len(data) > 1*1024*1024*1024

       // 2. Fetch previous version from CAS
       prevData := dk.fetchPreviousVersion(relPath)

       // 3. Compute diff (chunked if needed)
       var cids []string
       if shouldChunk {
           chunks := chunk.SplitFile(data, dk.config.ChunkSizeMB)
           prevChunks := chunk.SplitFile(prevData, dk.config.ChunkSizeMB)

           for i, c := range chunks {
               diff, _ := dk.diffEngine.ComputeDiff(prevChunks[i], c)
               cid, _ := dk.cas.Put(diff)
               cids = append(cids, cid)
           }
       } else {
           diff, _ := dk.diffEngine.ComputeDiff(prevData, data)
           cid, _ := dk.cas.Put(diff)
           cids = append(cids, cid)
       }

       // 4. Build Merkle tree
       tree, _ := dk.merkle.BuildTree(cids)

       // 5. Store metadata
       meta := FileMetadata{
           CIDs:       cids,
           MerkleRoot: tree.MerkleRoot(),
           IsChunked:  shouldChunk,
           Timestamp:  time.Now(),
       }
       return dk.storeMetadata(relPath, meta)
   }
   ```

3. Add periodic snapshots:
   ```go
   func (dk *DiffKeeper) shouldSnapshot(relPath string) bool {
       versionCount := dk.getVersionCount(relPath)
       return versionCount%10 == 0  // Every 10 versions
   }
   ```

**Deliverables:**
- [ ] BlueShift™ extended with diff logic
- [ ] Chunking integrated for large files
- [ ] CAS storage for diffs
- [ ] Merkle tree building
- [ ] Periodic snapshots

---

### Milestone 3.2: RedShift™ Integration
**Duration:** 2 days
**Goal:** Update recovery engine to apply binary diffs

**Tasks:**
1. Extend RedShift™ function ([main.go:122-161](main.go#L122-L161)):
   ```go
   func (dk *DiffKeeper) RedShift() error {
       log.Println("[RedShift] Restoring state from deltas...")

       // Iterate metadata bucket instead of deltas
       err := dk.db.View(func(tx *bbolt.Tx) error {
           metaBucket := tx.Bucket([]byte(BucketMetadata))
           c := metaBucket.Cursor()

           for k, v := c.First(); k != nil; k, v = c.Next() {
               relPath := string(k)

               // Parse metadata
               var meta FileMetadata
               json.Unmarshal(v, &meta)

               // Verify Merkle tree
               if err := dk.verifyIntegrity(relPath, meta); err != nil {
                   log.Printf("Warning: integrity check failed for %s: %v", relPath, err)
                   continue
               }

               // Reconstruct file
               data, err := dk.reconstructFile(meta)
               if err != nil {
                   log.Printf("Warning: failed to reconstruct %s: %v", relPath, err)
                   continue
               }

               // Write file
               fullPath := filepath.Join(dk.stateDir, relPath)
               os.WriteFile(fullPath, data, 0644)
           }
           return nil
       })
       return err
   }
   ```

2. Implement file reconstruction:
   ```go
   func (dk *DiffKeeper) reconstructFile(meta FileMetadata) ([]byte, error) {
       // 1. Fetch base snapshot
       baseData := dk.fetchBaseSnapshot(meta.FilePath)

       // 2. Fetch diff chunks from CAS
       var diffs [][]byte
       for _, cid := range meta.CIDs {
           diff, _ := dk.cas.Get(cid)
           diffs = append(diffs, diff)
       }

       // 3. Apply diffs
       if meta.IsChunked {
           baseChunks := chunk.SplitFile(baseData, dk.config.ChunkSizeMB)
           var reconstructedChunks [][]byte

           for i, diff := range diffs {
               patched, _ := dk.diffEngine.ApplyPatch(baseChunks[i], diff)
               reconstructedChunks = append(reconstructedChunks, patched)
           }

           return chunk.ReassembleChunks(reconstructedChunks), nil
       } else {
           return dk.diffEngine.ApplyPatch(baseData, diffs[0])
       }
   }
   ```

**Deliverables:**
- [ ] RedShift™ updated to use metadata
- [ ] Merkle verification integrated
- [ ] Chunk reassembly logic
- [ ] Diff application with fallback

---

### Milestone 3.3: Schema Migration
**Duration:** 1-2 days
**Goal:** Migrate existing MVP data to new schema

**Tasks:**
1. Add schema version to metadata bucket:
   ```go
   const SchemaVersion = 2  // MVP = 1, Binary Diffs = 2

   func (dk *DiffKeeper) detectSchemaVersion() int {
       var version int
       dk.db.View(func(tx *bbolt.Tx) error {
           meta := tx.Bucket([]byte(BucketMetadata))
           v := meta.Get([]byte("schema_version"))
           if v == nil {
               version = 1  // Legacy MVP
           } else {
               version = int(v[0])
           }
           return nil
       })
       return version
   }
   ```

2. Migration logic:
   ```go
   func (dk *DiffKeeper) migrateToV2() error {
       log.Println("[Migration] Converting MVP snapshots to binary diffs...")

       // For each file in deltas bucket:
       // 1. Decompress full file
       // 2. Create base snapshot
       // 3. Store in CAS
       // 4. Create metadata entry

       return dk.db.Update(func(tx *bbolt.Tx) error {
           deltas := tx.Bucket([]byte(BucketDeltas))
           meta := tx.Bucket([]byte(BucketMetadata))
           cas := tx.Bucket([]byte(BucketCAS))

           deltas.ForEach(func(k, v []byte) error {
               // Decompress legacy snapshot
               data, _ := decompressData(v)

               // Store as base snapshot in CAS
               cid := computeCID(data)
               cas.Put([]byte(cid), data)

               // Create metadata
               metadata := FileMetadata{
                   CIDs:       []string{cid},
                   IsSnapshot: true,
                   Version:    1,
               }
               meta.Put(k, marshalMetadata(metadata))

               return nil
           })

           // Clear legacy deltas bucket
           tx.DeleteBucket([]byte(BucketDeltas))

           // Update schema version
           meta.Put([]byte("schema_version"), []byte{2})

           return nil
       })
   }
   ```

3. Add backward compatibility flag:
   ```bash
   ./diffkeeper --enable-diff=false  # Use legacy mode
   ```

**Deliverables:**
- [ ] Schema versioning implemented
- [ ] Migration script for MVP → v1.0
- [ ] Backward compatibility flag
- [ ] Migration unit tests

---

## Phase 4: Testing & Validation (Week 4)

### Milestone 4.1: Unit Tests
**Duration:** 2-3 days
**Goal:** Comprehensive test coverage (target: 80%+)

**Test Suites:**
1. **Diff Engine Tests** (`pkg/diff/diff_test.go`):
   ```go
   func TestBsdiffComputeDiff(t *testing.T)
   func TestBsdiffApplyPatch(t *testing.T)
   func TestDiffLargeFile(t *testing.T)  // 100MB file
   func TestDiffCompressionRatio(t *testing.T)  // Verify 50-80% reduction
   ```

2. **Chunking Tests** (`pkg/chunk/chunk_test.go`):
   ```go
   func TestSplitFile(t *testing.T)
   func TestReassembleChunks(t *testing.T)
   func TestChunkBoundaries(t *testing.T)  // 5GB file
   func TestPerChunkDiff(t *testing.T)
   ```

3. **CAS Tests** (`pkg/cas/store_test.go`):
   ```go
   func TestCASPutGet(t *testing.T)
   func TestCASDeduplication(t *testing.T)  // Store same data twice
   func TestCASRefCounting(t *testing.T)
   func TestCASGarbageCollection(t *testing.T)
   ```

4. **Merkle Tests** (`pkg/merkle/tree_test.go`):
   ```go
   func TestMerkleTreeBuild(t *testing.T)
   func TestMerkleVerification(t *testing.T)
   func TestMerkleCorruptedData(t *testing.T)  // Should fail verification
   func TestMerkleProofGeneration(t *testing.T)
   ```

**Deliverables:**
- [ ] All pkg/ modules have >80% coverage
- [ ] Edge case tests (empty files, large files, corrupted data)
- [ ] Benchmark tests for diff computation

---

### Milestone 4.2: Integration Tests
**Duration:** 2 days
**Goal:** End-to-end validation

**Test Scenarios:**
1. **Full Capture-Replay Cycle**:
   ```go
   func TestFullCycleWithDiffs(t *testing.T) {
       // 1. Initialize DiffKeeper with diff mode
       // 2. Capture multiple file changes
       // 3. Verify CAS deduplication
       // 4. Simulate crash
       // 5. Verify RedShift™ restores correctly
       // 6. Verify Merkle trees checked
   }
   ```

2. **Large File Test**:
   ```go
   func TestLargeFileChunking(t *testing.T) {
       // 1. Create 5GB test file
       // 2. Modify 10% of chunks
       // 3. Verify only changed chunks stored
       // 4. Verify recovery reassembles correctly
   }
   ```

3. **Migration Test**:
   ```go
   func TestMVPToV1Migration(t *testing.T) {
       // 1. Create MVP database with legacy snapshots
       // 2. Run migration
       // 3. Verify all data accessible
       // 4. Verify schema version updated
   }
   ```

4. **Kubernetes Test** (manual):
   ```bash
   # Deploy to minikube
   kubectl apply -f k8s-statefulset-v1.yaml

   # Verify deduplication across pods
   # Crash pod, verify recovery with diffs
   ```

**Deliverables:**
- [ ] Full cycle tests pass
- [ ] Large file tests (>1GB)
- [ ] Migration tests
- [ ] K8s manual test guide updated

---

### Milestone 4.3: Performance Benchmarks
**Duration:** 1-2 days
**Goal:** Validate NFRs from specification

**Benchmarks:**
1. **Storage Reduction**:
   ```go
   func BenchmarkStorageReduction(b *testing.B) {
       // Measure: 1GB ML checkpoint with 20% changes
       // Expected: 50-80% reduction vs. full snapshot
   }
   ```

2. **Diff Computation Time**:
   ```go
   func BenchmarkDiffComputation(b *testing.B) {
       // Measure: 1GB file diffing time
       // Target: <100ms
   }
   ```

3. **Recovery Time**:
   ```go
   func BenchmarkRedShiftWithDiffs(b *testing.B) {
       // Measure: Full recovery of 10 files (1GB total)
       // Target: <100ms
   }
   ```

4. **Overhead Measurement**:
   ```bash
   # Use pprof for CPU/memory profiling
   go test -cpuprofile=cpu.prof -memprofile=mem.prof
   go tool pprof cpu.prof
   ```

**Deliverables:**
- [ ] Storage reduction benchmark: 50-80% verified
- [ ] Diff computation: <100ms for 1GB file
- [ ] Recovery time: <100ms
- [ ] CPU overhead: <1% vs. MVP

---

## Phase 5: Documentation & Release (Ongoing)

### Milestone 5.1: Documentation Updates
**Duration:** Ongoing throughout phases

**Documents to Update:**
1. **README.md**:
   - Remove "MVP stores full files" disclaimers
   - Add binary diffs feature description
   - Update architecture diagrams
   - Add configuration examples

2. **ARCHITECTURE.md** (new):
   - Detailed design of diff engine
   - CAS implementation details
   - Merkle tree usage
   - Migration guide

3. **API.md** (new):
   - DiffConfig options
   - CLI flags
   - Environment variables
   - Kubernetes annotations

4. **CONTRIBUTING.md**:
   - Package structure guide
   - Testing requirements
   - Code style guide

**Deliverables:**
- [ ] README.md updated
- [ ] ARCHITECTURE.md created
- [ ] API.md created
- [ ] CONTRIBUTING.md updated

---

### Milestone 5.2: Release Preparation
**Duration:** 2-3 days (end of Week 4)

**Tasks:**
1. Version tagging:
   ```bash
   git tag -a v1.0.0 -m "Binary diffs, chunking, CAS, Merkle trees"
   ```

2. Release artifacts:
   - Compile binaries for linux/amd64, linux/arm64
   - Create Docker images
   - Generate SBOM (Software Bill of Materials)

3. Changelog:
   ```markdown
   ## v1.0.0 - Binary Diffs Release

   ### Features
   - Binary diff storage (bsdiff/xdelta)
   - Chunking for large files (>1GB)
   - Content-addressable storage (CAS)
   - Merkle tree integrity verification
   - 50-80% storage reduction for partial updates

   ### Breaking Changes
   - New BoltDB schema (automatic migration provided)
   - Requires Go 1.23+

   ### Migration
   See MIGRATION.md for upgrading from v0.1 MVP.
   ```

4. GitHub release:
   - Upload binaries
   - Publish Docker images to GHCR
   - Announce in discussions

**Deliverables:**
- [ ] v1.0.0 tagged
- [ ] Binaries built and uploaded
- [ ] Docker images published
- [ ] Changelog published
- [ ] GitHub release created

---

## Risk Management

### High-Risk Items

1. **Memory exhaustion on large files**:
   - **Risk:** Diff computation on 5GB+ files
   - **Mitigation:** Streaming diff algorithms, chunking threshold tuning
   - **Contingency:** Increase chunk size, add memory limits

2. **Migration data loss**:
   - **Risk:** Bugs in MVP → v1.0 migration
   - **Mitigation:** Extensive testing, backup creation before migration
   - **Contingency:** Rollback script, keep legacy data until verified

3. **Performance regression**:
   - **Risk:** Diff overhead exceeds 1% CPU
   - **Mitigation:** Benchmark-driven optimization, profiling
   - **Contingency:** Feature flag to disable diffs, optimize hot paths

4. **BoltDB lock contention**:
   - **Risk:** Concurrent CAS reads/writes
   - **Mitigation:** Read-only transactions for lookups, batch writes
   - **Contingency:** Connection pooling, consider etcd for cluster mode

---

## Success Criteria

### Functional
- [ ] Binary diffs compute correctly for all file types
- [ ] Chunking works for files >10GB
- [ ] CAS deduplication reduces storage by 50%+ on test workloads
- [ ] Merkle verification detects all corrupted data
- [ ] Migration from MVP completes without data loss

### Performance
- [ ] Storage reduction: 50-80% for ML checkpoints
- [ ] Diff computation: <100ms for 1GB file
- [ ] Recovery time: <100ms for 10 files (1GB total)
- [ ] CPU overhead: <1% vs. MVP
- [ ] Memory overhead: <10MB additional per container

### Quality
- [ ] Test coverage: >80% for all pkg/ modules
- [ ] All integration tests pass
- [ ] K8s manual tests verified on minikube
- [ ] Zero critical bugs in QA
- [ ] Documentation complete and reviewed

---

## Timeline Summary

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| **Phase 1: Foundation** | Week 1 | Diff engine, chunking |
| **Phase 2: CAS & Merkle** | Week 2 | CAS storage, Merkle trees |
| **Phase 3: Integration** | Week 3 | BlueShift™/RedShift™ updates, migration |
| **Phase 4: Testing** | Week 4 | Unit tests, integration tests, benchmarks |
| **Phase 5: Documentation** | Ongoing | README, ARCHITECTURE.md, release prep |
| **Release** | End of Week 4 | v1.0.0 tagged and published |

**Total Estimated Duration:** 4 weeks (20 working days)

---

## Next Steps

1. **Review this plan** with stakeholders
2. **Set up project tracking** (GitHub Projects or similar)
3. **Begin Phase 1, Milestone 1.1** (package structure)
4. **Daily standups** to track progress and blockers

---

**Plan Author:** Claude
**Reviewers:** Shane Anthony Wall
**Status:** Ready for Implementation
