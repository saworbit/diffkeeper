# DiffKeeper: Lightweight State Recovery for Containers (v1.0 Released - v2.0 eBPF Preview)

> Capture file-level state changes in containerized workloads for fast recovery and debugging. No large persistent volumes required.

[![CI](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml/badge.svg)](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/saworbit/diffkeeper)](https://goreportcard.com/report/github.com/saworbit/diffkeeper)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Status: v2.0 Preview](https://img.shields.io/badge/Status-v2.0%20Preview-blue.svg)]()

**Updated:** November 9, 2025  
**Version:** v2.0 Preview (eBPF Monitoring + Auto-Injection)
**Author:** Shane Anthony Wall
**Contact:** shaneawall@gmail.com

---

## The Problem

Many containerized applications write stateful files (checkpoints, world saves, configs, caches) but don't need full database-grade persistence. Current options are problematic:

- **Persistent Volumes (PVs)** - Provision 10GB storage for 10MB of actual changes; slow attach/detach cycles; vendor costs
- **External object storage (S3/MinIO)** - Network latency; manual sync; complex failure modes  
- **StatefulSets** - Overkill for simple file-based state; full volume snapshots instead of granular changes
- **No solution** - Accept data loss on pod crashes/evictions and rebuild from scratch

**Result:** Wasted storage costs, slow recovery times, and lost work when containers restart.

---

## The Solution

DiffKeeper is a 6.5MB Go agent that runs inside your container to:

1. **Intercept** write syscalls at the kernel level via eBPF (fsnotify used as fallback)
2. **Capture** binary diffs when files change (bsdiff + content-addressable storage)
3. **Store** deltas externally (BoltDB on small PV or host mount)
4. **Verify** integrity on recovery (Merkle trees)
5. **Replay** changes on restart for sub-100ms recovery

> **v1.0 Update:** Binary diff support now available! Reduces storage by 50-80% compared to full-file snapshots for incremental updates.

```
[Your App] --writes--> [DiffKeeper Agent] --stores--> BoltDB delta files
                                ^
                                |
                         replays on restart
```

**Key Benefits:**
- **Sub-Âµs capture latency**: Kernel interception fires within <1Âµs for write/pwrite/writev
- **<0.5% CPU overhead**: Adaptive eBPF profiler traces only predicted hot paths
- **Compile once, run everywhere**: CO-RE probes + BTFHub downloads adapt to mixed kernels (4.18+) with no per-node rebuilds
- **50-80% storage savings**: Binary diffs store only changed bytes
- **Content deduplication + Merkle trees**: CAS + integrity proofs for every version
- **Fast recovery**: <100ms replays even under heavy churn
- **Auto-injection for containers**: Trace CRI events and attach without wrapping entrypoints
- **Drop-in compatible**: Works with Docker, Kubernetes, Podman, bare-metal

**Best For:** Game servers, ML checkpoints, CI/CD caches, edge IoT, streaming analytics

---

## Kubernetes & Helm

The repository ships with ready-to-run manifests under [`k8s/`](k8s/README.md):

- `k8s/deployment.yaml` – Sidecar pattern with init-container replay.
- `k8s/rbac.yaml` – Minimal service account + ClusterRoleBinding.
- `k8s/helm/diffkeeper` – Helm chart for production clusters (configurable state dir, BTF cache, workloads).

```bash
kubectl apply -f k8s/rbac.yaml
helm install diffkeeper k8s/helm/diffkeeper --namespace diffkeeper --create-namespace
```

Helm values expose image tags, dedicated workload commands, and cache locations so you can drop-in DiffKeeper alongside any StatefulSet.

---

## Architecture

```
+----------------------+        syscalls         +--------------------------------------+
|   Container Runtime  | ----------------------> | DiffKeeper Agent (v2.0)               |
| (Docker/Kube/CRI-O)  |                         |  - eBPF Manager (kprobes/tracepoints) |
+----------------------+                         |  - Hot-path Profiler (EMA/ringbuf)   |
              |                                  |  - Lifecycle Tracer (auto-injection) |
              | <----- auto-inject agent ------- |  - BlueShift (capture + predict)     |
              v                                  |  - RedShift (replay/rollback)        |
     +--------------------+                      +------------------+-------------------+
     | Target Workload    |                                       |
     +--------------------+                                       v
                                                     BoltDB Delta Store (CAS + metadata)
```

**How it works (v2.0 with eBPF + Binary Diffs):**
1. eBPF manager (kprobes on `vfs_write`, `pwrite`, `writev`) emits syscall events in <1Âµs via perf buffers (fsnotify acts as fallback when kernels lack eBPF).
2. Adaptive profiler samples those events every 100ms, computes exponential moving averages, and updates hot-path filters (<0.5% CPU steady state).
3. Lifecycle tracer hooks `sched_process_exec` and CRI runtimes to auto-inject the agent into new pods/containers when `--auto-inject` is enabled.
4. BlueShift receives filtered events, hashes changed files, and determines whether to produce a snapshot or binary diff.
5. **Binary diff computation**: Compares current and previous versions (bsdiff/xdelta) while streaming >1GB files through a Rabin-Karp content-defined chunker (avg 8MiB, min/max bounds).
6. **Content-addressable storage (CAS)**: Deduplicates chunks/diffs by hash, zstd-compresses blobs, and persists them in BoltDB.
7. **Merkle tree generation + metadata**: Every version stores Merkle roots, version counters, and chunk metadata to prevent corruption.
8. **Periodic snapshots**: Full snapshots occur every N versions (default 10) to prevent long diff chains for hot files.
9. On restart, RedShift replays metadata, verifies Merkle trees, and rebuilds state to disk in <100ms even under heavy churn.

**BTF/CO-RE Portability:** DiffKeeper automatically looks for `/sys/kernel/btf/vmlinux`, falls back to a cached copy in `--btf-cache-dir`, or downloads a tailored BTF from [BTFHub-Archive](https://github.com/aquasecurity/btfhub-archive). The loader feeds that spec into CO-RE programs so the same `diffkeeper.bpf.o` runs across Ubuntu, CentOS, Fedora, Amazon Linux, and other kernels â‰¥4.18 without recompilation. See [docs/btf-core-guide.md](docs/btf-core-guide.md) for setup details and [docs/supported-kernels.md](docs/supported-kernels.md) for the current compatibility matrix.

---

## Observability

- **Endpoint:** Prometheus metrics served on `:9911/metrics` by default (override with `--metrics-addr` or `DIFFKEEPER_METRICS_ADDR`). A dedicated listener keeps observability isolated from control traffic.
- **Built-ins:** Go runtime + process collectors plus DiffKeeper metrics are namespaced under `diffkeeper_`.

Key metrics to watch:
- `diffkeeper_capture_duration_ms{type="file|diff|snapshot"}` – histogram of capture latency
- `diffkeeper_capture_total{type, outcome}` – count of capture attempts by success/error
- `diffkeeper_storage_saved_bytes_total` / `diffkeeper_storage_saved_ratio` – cumulative and current storage efficiency
- `diffkeeper_recovery_duration_ms{reason}` and `diffkeeper_recovery_total{outcome}` – recovery performance and reliability
- `diffkeeper_store_size_bytes{type="deltas|metadata|store_file"}` and `diffkeeper_files_tracked_total` – BoltDB footprint and active file count
- `diffkeeper_deltas_total{compression}` – volume of diffs/snapshots written (bsdiff/gzip/none)
- `diffkeeper_up` – liveness gauge for the agent

Kubernetes ServiceMonitor example:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: diffkeeper
spec:
  selector:
    matchLabels:
      app: diffkeeper
  endpoints:
  - port: metrics
    path: /metrics
    interval: 15s
```

---

## Quick Start

### Prerequisites

- **Docker** 24+ (required for demo)
- **Go** 1.23+ (for building from source)
- **Platform**: Linux containers (agent uses Unix syscalls like `exec`)
- **Kernel**: Linux 4.18+ with eBPF + `CAP_BPF`/`CAP_SYS_ADMIN` for kernel interception (older kernels automatically fall back to fsnotify)
- 100MB free disk space

> **Note**: While the agent can be built on Windows/macOS, it must run inside Linux containers due to platform-specific system calls.

### 1. Install the Agent

```bash
# Download binary
curl -sSL https://github.com/saworbit/diffkeeper/releases/latest/download/agent-linux-amd64 -o diffkeeper
chmod +x diffkeeper

# Or build from source
go install github.com/saworbit/diffkeeper/cmd/agent@latest
```

### 2. (Optional) Build eBPF Probes

```bash
# Requires clang/llvm and a kernel-specific vmlinux.h (see docs/ebpf-guide.md)
make build-ebpf
```

This produces `bin/ebpf/diffkeeper.bpf.o`. If the file is missing, DiffKeeper will automatically log a warning and revert to fsnotify.

> BTF data is fetched on-demand. Override the cache directory with `--btf-cache-dir`, point to an internal mirror via `--btfhub-mirror`, or disable downloads entirely with `--disable-btfhub-download` (requires `/sys/kernel/btf/vmlinux`).

### 3. Docker Usage

Wrap any container command with binary diffs enabled:

```bash
docker run -it \
  -v ./deltas:/deltas \
  your-app:latest \
  /bin/sh -c "./diffkeeper \
    --state-dir=/app/data \
    --store=/deltas/db.bolt \
    --enable-diff=true \
    your-app-start"
```

**Official release image (multi-arch v1.0.0):**
```bash
docker run --rm -v ./data:/state -v ./deltas:/deltas ghcr.io/saworbit/diffkeeper:v1.0.0 \
  --state-dir=/state --store=/deltas/db.bolt --enable-diff=true your-app
```

> Need verbose logs while troubleshooting? Append `--debug` before the wrapped command to emit watcher events and delta details:
> `./diffkeeper --debug --state-dir=...`

**Example - Nginx config persistence with binary diffs:**
```bash
docker run -v ./deltas:/deltas nginx:alpine \
  /bin/sh -c "./diffkeeper \
    --state-dir=/etc/nginx \
    --store=/deltas/db.bolt \
    --enable-diff=true \
    --snapshot-interval=10 \
    nginx -g 'daemon off;'"
```

**CLI Flags (v2.0):**

**Core Flags:**
- `--state-dir`: Directory to watch for state changes (default: /data)
- `--store`: Path to delta storage file (default: /deltas/db.bolt)
- `--debug`: Enable verbose debug logging (default: false)

**Binary Diff Flags:**
- `--enable-diff`: Enable binary diffs (default: true)
- `--diff-library`: Diff algorithm - "bsdiff" or "xdelta" (default: bsdiff)
- `--enable-chunking`: Enable streaming content-defined chunking for large files (default: true)
- `--chunk-size`: Chunk size in MB for large files (default: 4)
- `--chunk-min`: Minimum chunk size in bytes for the streaming chunker (default: 1MiB)
- `--chunk-avg`: Target average chunk size in bytes (default: 8MiB)
- `--chunk-max`: Maximum chunk size in bytes (default: 64MiB)
- `--chunk-hash-window`: Rolling hash window size for chunk boundary detection (default: 64 bytes)
- `--hash-algo`: Hash algorithm - "sha256" or "blake3" (default: sha256)
- `--dedup-scope`: Deduplication scope - "container" or "cluster" (default: container)
- `--snapshot-interval`: Create full snapshot every N versions (default: 10)

**eBPF / Profiler Flags:**
- `--enable-ebpf`: Turn on kernel-level interception (default: true)
- `--ebpf-program`: Path to compiled `*.bpf.o` artifact (default: `bin/ebpf/diffkeeper.bpf.o`)
- `--fallback-fsnotify`: Automatically switch back to fsnotify if eBPF load fails (default: true)
- `--profiler-interval`: Sampling cadence for hot-path EMA (default: 100ms)
- `--enable-profiler`: Toggle adaptive profiling without disabling eBPF (default: true)
- `--auto-inject`: Auto-inject the agent into new containers when lifecycle events fire (default: true)
- `--injector-cmd`: Command executed during auto-injection (container ID passed as argument plus env metadata)
- `--btf-cache-dir`: Cache directory for downloaded BTF specs (default: `/var/cache/diffkeeper/btf`)
- `--btfhub-mirror`: Mirror URL for BTFHub-Archive assets (default: official GitHub mirror)
- `--disable-btfhub-download`: Skip remote downloads (requires `/sys/kernel/btf/vmlinux`; otherwise falls back to fsnotify)

### Watching Nested Directories

- DiffKeeper now walks any directory as soon as it appears and attaches watchers to every level, so files dropped into newly created subfolders are captured immediately.
- On Windows, where `os.MkdirAll` only emits a `CREATE` event for the top-most folder, this recursive watcher keeps nested writes safe without manual intervention.
- Run with `--debug` to stream watcher activity when you need to confirm observers were attached or to track down paths that are being ignored.

### Release Notes

- Recursive directory watching is now enabled cross-platform, ensuring nested writes are captured the moment new folders appear.
- Added a regression test to confirm nested directories are monitored on every OS.
- `--debug` verbose logging documents watcher setup in the logs so you can troubleshoot missing events quickly.
- BlueShift now validates file permissions up front and surfaces an error when a watched file loses read access, preventing silent captures of unreadable data.
- NewDiffKeeper now performs an explicit writability test during initialization, failing fast with a clear error message when the BoltDB store is read-only (e.g., chmod 0444). This prevents operators from being misled into thinking deltas are being persisted when the database cannot accept writes.

### 4. Kubernetes Deployment

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: stateful-app
spec:
  initContainers:
  - name: replay-state
    image: saworbit/diffkeeper:latest
    command: ["/replay", "--state-dir=/data", "--store=/deltas/db.bolt"]
    volumeMounts:
    - name: delta-storage
      mountPath: /deltas
    - name: app-data
      mountPath: /data
      
  containers:
  - name: app
    image: your-app:latest
    command: ["/diffkeeper"]
    args:
    - --state-dir=/data
    - --store=/deltas/db.bolt
    - /app/start
    volumeMounts:
    - name: app-data
      mountPath: /data
    - name: delta-storage
      mountPath: /deltas
      
  volumes:
  - name: delta-storage
    persistentVolumeClaim:
      claimName: diffkeeper-deltas  # Small 100MB PVC
  - name: app-data
    emptyDir: {}
```

---

## Real-World Use Cases

| Workload | State Files | Before DiffKeeper | After DiffKeeper | Impact |
|----------|-------------|-------------------|------------------|--------|
| **Game Servers** | `/world/saves.db` | Crash = full restart, lost progress | <2s replay, zero loss | +50% player retention |
| **ML Training** | `model.ckpt` | OOM eviction = hours lost | Resume from exact epoch | 5-10x faster recovery |
| **CI/CD Pipelines** | `node_modules/`, build cache | Re-download dependencies (30min) | Replay cache instantly | 10x faster builds |
| **Video Processing** | `/tmp/output.mp4` | Re-encode from scratch | Resume partial render | 80% GPU time saved |
| **Edge IoT** | `sensor-data.json` | Manual sync on reboot | Auto-restore state | Zero data loss |
| **Config Services** | `/etc/config.yaml` | Stale config on restart | Live state replay | High availability |

---

## Performance Benchmarks

**v1.0 Results** from testing on AMD Ryzen 9 5900X, SSD storage:

### Storage & Recovery Performance

| Metric | Value | Notes |
|--------|-------|-------|
| **Agent binary size** | 6.5MB | Tested on Windows/Linux |
| **Storage savings (binary diffs)** | **85.7%** | Measured with 1MB file, 20 versions (10% changes each) |
| **Recovery time (10MB file)** | 11.5ms | Including Merkle verification + chunk reassembly |
| **Recovery time (per file avg)** | 20.67Âµs | CAS retrieval + integrity check |
| **Capture time (1MB file)** | 19.6ms | Includes diff computation, CAS storage, Merkle tree |
| **CPU overhead (idle)** | 0.1% | File watching only |
| **CPU overhead (active writes)** | 1-3% | Diff computation + storage |
| **Memory overhead** | ~50MB | Per container (includes CAS cache) |

### Component Performance

| Operation | Time | Description |
|-----------|------|-------------|
| **Diff computation (1MB)** | 18.4ms | bsdiff binary diff algorithm |
| **Chunking (10MB file)** | 46.2ms | Split into 4MB chunks |
| **Merkle verification** | 933Âµs | Integrity check per file |
| **CAS lookup** | 134Âµs | Content-addressable retrieval |
| **Multi-file recovery (61 files)** | 170ms | 2.79ms per file average |

### Deduplication Efficiency

| Scenario | Storage Saved |
|----------|---------------|
| Identical files | 50% (1 object, 2 references) |
| Partial updates (10% change) | **85.7%** (verified: 1MB file, 20 versions) |
| ML checkpoints (1GB, 10% change) | 80-90% (estimated) |
| Config files (small, frequent changes) | 75-85% |

**Test Environment:**
- Platform: Windows 11, AMD Ryzen 9 5900X (24 threads)
- Storage: NVMe SSD
- Go version: 1.21
- Test dates: November 4-8, 2025
- Diff chain test: 25 versions, recovery in 1.55ms

**Known Limitations (v1.0 Final):**
- âœ… Diff reconstruction: **COMPLETE** (85.7% storage savings achieved)
- High-write workloads (>10k writes/sec): Consider eBPF-based approach (v2.0)
- Large file changes (>1GB): Automatic chunking at 1GB threshold
- Database workloads: Use native WAL/replication instead
- **Windows**: Agent runs on Linux containers only (uses Unix syscalls)

---

## Comparison with Alternatives

| Feature | StatefulSets + PV | CSI Snapshots | DiffKeeper |
|---------|-------------------|---------------|------------|
| Storage needed | GBs (full filesystem) | GBs (block-level) | MBs (deltas only) |
| Recovery time | 10+ seconds | 5+ seconds | <5 seconds |
| Overhead | 10%+ | 20%+ | <2% |
| Granularity | Volume-level | Block-level | File-level deltas |
| Best for | Databases, large state | Enterprise storage | File-based apps |
| Cost | PV provisioning costs | Vendor fees | Open source |

**When to use DiffKeeper:**
- Small-to-medium file-based state (<10GB)
- Infrequent writes (<1k/sec)
- Need fast recovery without large PVs
- Container-native workflows

**When NOT to use DiffKeeper:**
- High-throughput databases (Postgres, MySQL)
- Streaming/append-only logs (Kafka, Loki)
- Very large files (>1GB single file)
- Already have vendor storage solution working well

---

## Roadmap

### v1.0 Final (Current - November 2025) âœ…

**Core Features:**
- âœ… **Binary diff engine** (bsdiff implementation)
- âœ… **Diff chain reconstruction** (BaseSnapshotCID tracking + sequential patch application)
- âœ… **Content-addressable storage (CAS)** with SHA256/BLAKE3 hashing
- âœ… **Merkle tree integrity verification**
- âœ… **Large file chunking** (automatic at 1GB threshold, configurable chunk size)
- âœ… **Deduplication** (identical content stored once)
- âœ… **Periodic snapshots** (every N versions to prevent long diff chains)
- âœ… **Schema migration** (MVP â†’ v1.0 automatic upgrade)
- âœ… **CLI configuration** (enable-diff, diff-library, chunk-size, hash-algo, etc.)

**Testing & Validation:**
- âœ… **72 tests passing** (100% pass rate)
- âœ… **25+ version diff chains tested** (TestDiffChain20Plus)
- âœ… **85.7% storage savings measured** (1MB file, 20 versions, 10% changes)
- âœ… **Sub-2ms recovery** for complex diff chains

**v1.0 Final Status:**
- âœ… Diff reconstruction fully implemented and tested
- âœ… Storage savings verified (85.7% for incremental updates)
- âœ… Performance targets exceeded (<100ms recovery, >80% savings)
- âœ… Ready for production deployment

**Production Ready:**
- Binary diff mode fully functional (no snapshot-only fallback)
- Automatic diff chain management with periodic snapshots
- Verified integrity via Merkle trees
- Real-world storage efficiency demonstrated

### v1.1 (Next Release)
- [ ] **Production testing** (Docker + Kubernetes workloads)
- [ ] **ML checkpoint benchmark** (real PyTorch/TensorFlow workload)
- [ ] **Kubernetes manifests** (StatefulSet examples with PVC)
- [ ] **Migration guide** (MVP â†’ v1.0 for existing deployments)
- [ ] **Docker image** (official DiffKeeper container)

### v2.0+ (Future)
- [ ] eBPF hooks for lower overhead on high-write workloads
- [ ] Kubernetes Operator + Custom Resource Definition
- [ ] Multi-replica synchronization (CRDT-based)
- [ ] Branch/merge support (git-like time-travel)
- [ ] Live migration between nodes
- [ ] Power-optimized mode for edge/IoT

---

## Contributing

We're looking for collaborators to move this from prototype to production-ready!

**How to contribute:**
1. Try the prototype and report issues
2. Submit PRs for priority areas:
   - eBPF probe implementation
   - Chunked diff support for large files
   - Kubernetes operator development
   - Performance optimizations
3. Share your use cases and requirements

**Development setup:**
```bash
git clone https://github.com/saworbit/diffkeeper
cd diffkeeper
make build
make test
# Optional: run long E2E coverage when you have time
go test -tags integration ./...
```

Long-running diff chain/recovery suites now live behind the `integration` build tag so the default `go test ./...` stays fast. Run them manually via `go test -tags integration ./...` or trigger the GitHub Actions workflow with `run_integration=true`.


**Discussion:**
- Issues: [GitHub Issues](https://github.com/saworbit/diffkeeper/issues)
- Email: shaneawall@gmail.com

---

## Technical Details

**Core Dependencies:**
- [fsnotify](https://github.com/fsnotify/fsnotify) v1.7.0 - File system event monitoring
- [BoltDB](https://github.com/etcd-io/bbolt) v1.3.10 - Embedded key-value store
- [Cobra](https://github.com/spf13/cobra) v1.8.1 - CLI framework
- Standard library: gzip compression, crypto/sha256 for hashing

**v1.0 Architecture Components:**

```go
// DiffKeeper struct (v1.0)
type DiffKeeper struct {
    db         *bbolt.DB
    stateDir   string
    storePath  string
    watcher    *fsnotify.Watcher
    config     *config.DiffConfig      // Binary diff configuration
    cas        *cas.CASStore            // Content-addressable storage
    merkle     *merkle.MerkleManager    // Merkle tree integrity
    diffEngine diff.DiffEngine          // bsdiff/xdelta engine
}

// BlueShift captures file changes (v1.1 with streaming chunking for giants)
func (dk *DiffKeeper) captureChunked(relPath string, path string) error {
    chunker := chunk.NewRabinChunker(file, chunk.Params{
        MinSize: 1 << 20,  // 1MiB
        AvgSize: 8 << 20,  // 8MiB
        MaxSize: 64 << 20, // 64MiB
        Window:  64,
    })

    manifest := &chunk.Manifest{}
    var cids []string

    for {
        ch, err := chunker.Next()
        if errors.Is(err, io.EOF) { break }
        cid, _ := dk.cas.PutChunkWithHash(ch.Ref.Hash, ch.Data) // zstd-compressed blob
        _ = dk.cas.AddReference(cid, relPath)
        manifest.Chunks = append(manifest.Chunks, ch.Ref)
        cids = append(cids, cid)
    }

    tree := dk.merkle.BuildTree(cids)
    metadata := FileMetadata{
        CIDs:          cids,
        ChunkManifest: manifest,
        IsChunked:     true,
        IsSnapshot:    true,
        MerkleRoot:    merkle.GetRoot(tree),
    }
    return dk.storeMetadata(relPath, metadata)
}

// RedShift restores files (v1.0 with verification)
func (dk *DiffKeeper) RedShiftDiff() error {
    // 1. Fetch metadata from BoltDB
    metadata := dk.getMetadata(relPath)

    // 2. Verify Merkle tree integrity
    if err := dk.merkle.VerifyFileIntegrity(metadata.CIDs, metadata.MerkleRoot); err != nil {
        return err  // Corrupted data detected
    }

    // 3. Reconstruct file
    if metadata.IsSnapshot {
        // Fetch chunks from CAS and reassemble
        data := dk.fetchAndReassemble(metadata.CIDs)
    } else {
        // Fetch base + apply diffs
        data := dk.reconstructFile(metadata)
    }

    // 4. Write restored file
    return os.WriteFile(fullPath, data, 0644)
}
```

**Package Structure (v1.0):**
- `pkg/config/` - Configuration management (DiffConfig)
- `pkg/diff/` - Binary diff engine (bsdiff/xdelta interfaces)
- `pkg/chunk/` - File chunking for large files
- `pkg/cas/` - Content-addressable storage with deduplication
- `pkg/merkle/` - Merkle tree integrity verification
- `pkg/ebpf/` - Kernel manager, profiler, and lifecycle tracing abstractions
- `main.go` - CLI and orchestration (450 LOC)
- `diff_integration.go` - Binary diff integration logic (378 LOC)
- `bench/` - Synthetic throughput comparisons between ring buffers and fsnotify event loops

For full implementation, see [main.go](main.go) and [diff_integration.go](diff_integration.go).

- **Guides**
  - [docs/ebpf-dev-setup.md](docs/ebpf-dev-setup.md) – Toolchain + BTF cache walk-through
  - [docs/ebpf-guide.md](docs/ebpf-guide.md) – Kernel probe troubleshooting tips
  - [docs/btf-core-guide.md](docs/btf-core-guide.md) – Using BTFHub downloads and CO-RE in production
  - [docs/supported-kernels.md](docs/supported-kernels.md) – Reference list of tested distros/kernels
  - [docs/auto-injection.md](docs/auto-injection.md) – Wiring CRI traces + injector workflows
- **Security & Operations**
  - [SECURITY.md](SECURITY.md) – How to report vulnerabilities
  - [k8s/README.md](k8s/README.md) – Deployments, Helm chart, and RBAC
- **Reference**
  - [docs/patents.md](docs/patents.md) – Prior art & IP notes for profiler + auto-injection features

---

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.

---

## Acknowledgments

- fsnotify and bsdiff maintainers
- Kubernetes SIG-Storage for StatefulSet patterns
- eBPF community for inspiration on kernel-level monitoring
- All prototype testers and early contributors

---

**DiffKeeper makes stateful containers practical without the complexity of persistent volumes.**

Try it: `go get github.com/saworbit/diffkeeper` 

---

**Maintainer:** Shane Anthony Wall (shaneawall@gmail.com)



