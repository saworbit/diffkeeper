# Technical Spec: Project "Time Machine" (Pebble + Flight Recorder)

## 1. High-Level Goals
* **Optimize for Write-Throughput:** The agent must ingest syscall events at line rate (100k+ ops/sec) without blocking the container.
* **Append-Only by Default:** We are recording history, not updating state.
* **Seekable Replay:** Users must be able to restore the filesystem to state `T = timestamp`.

## 2. Storage Engine Swap: BoltDB to Pebble
We are replacing `bbolt` (B+Tree) with `pebble` (LSM Tree) to handle the "firehose" of write events.

### 2.1 Why Pebble?
* **LSM Tree Structure:** Writes are appended to a log immediately (fast) and compacted later. This matches our "record now, replay later" access pattern.
* **Pure Go:** Maintains our single-binary distribution goal (no CGO/RocksDB dependencies).
* **Batching:** Excellent support for atomic batch writes.

### 2.2 Schema Migration
BoltDB uses "Buckets." Pebble is a flat Key-Value store. We will simulate buckets using **Key Prefixes**.

| Bucket (Old) | Key (Old) | Pebble Key (New) | Value |
| :--- | :--- | :--- | :--- |
| `cas` | `{hash}` | `c:{hash}` | Compressed Chunk/Diff Data |
| `meta` | `{path}` | `m:{path}:{timestamp}` | Metadata (File info, pointers to CAS) |
| `events` | `{uuid}` | `l:{timestamp}:{uuid}` | Raw Syscall Event (Op, Path, Size) |

*Note: Adding `{timestamp}` to the metadata key allows us to store *every* version of a file, not just the latest.*

### 2.3 Implementation Plan (`pkg/cas/store.go`)
1.  **Replace Imports:** Remove `go.etcd.io/bbolt`, add `github.com/cockroachdb/pebble`.
2.  **Rewrite `Put`:** Use Pebble writes (`pebble.Sync` for CAS) to store compressed blobs under `c:{cid}`.
3.  **Implement `Iterate`:** Add a `PrefixIterator` helper to scan `m:{path}:*` to find file history and `c:` keys for CAS stats/GC.

## 3. The "Flight Recorder" Mode (`BlueShift`)

### 3.1 The "Journal" Concept
Instead of trying to calculate diffs *synchronously* (which slows down the app), we will implement a two-stage pipeline:

**Stage 1: The Firehose (Ingest)**
* eBPF captures `vfs_write`.
* Agent writes the raw data into a `journal` (Pebble WAL) using `l:{timestamp}:{random}` keys with `pebble.NoSync`.
* **Goal:** 0ms latency added to the application.

**Stage 2: The Compactor (Async)**
* A background goroutine reads the `journal` (prefix `l:`).
* It performs the CPU-heavy tasks: Hashing (SHA256) and Diffing (bsdiff).
* It moves data into the permanent `cas` prefix and writes metadata `m:{path}:{timestamp}`.

### 3.2 CLI Changes
* **New Command:** `diffkeeper record -- <cmd>`
    * Starts the agent in background.
    * Execs the command.
    * On exit (or failure), flushes the Pebble WAL to disk.
* **New Command:** `diffkeeper export --time="2m30s" --out=/tmp/debug`
    * Reads the Pebble store.
    * Reconstructs the filesystem as it existed exactly at `2m30s` into execution.

## 4. CI/CD Integration
To prove the value immediately, we will add a GitHub Actions wrapper.

```yaml
# Example Usage
- name: Run Flaky Test with DiffKeeper
  run: |
    diffkeeper record --state-dir=/tmp/record -- go test ./...

- name: Upload Recording on Failure
  if: failure()
  uses: actions/upload-artifact@v4
  with:
    name: crash-recording
    path: /tmp/record/  # Contains the Pebble DB
```

## 5. Migration Checklist
[ ] Remove BoltDB from go.mod.

[ ] Add pebble to go.mod.

[ ] Refactor NewCASStore to accept a Pebble Options struct.

[ ] Update ebpf/diffkeeper.bpf.c to ensure high-precision nanosecond timestamps are attached to every event.

[ ] Create pkg/recorder to handle the asynchronous ingestion pipeline.
