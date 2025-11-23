# DiffKeeper Architecture

Deep dive into how DiffKeeper captures, stores, and replays state so containers can survive `kill -9` without data loss.

## Goals and Guarantees
- **Crash-safe restarts**: Restore watched paths on process start before the wrapped command runs.
- **Low overhead**: eBPF-first file change capture with <1% CPU; fsnotify fallback where eBPF is unavailable.
- **Integrity + dedup**: Content-addressable storage (CAS) with Merkle verification and optional chunking for large files.
- **Cross-platform**: Linux + Windows; Docker, Kubernetes, and bare-metal friendly.

## Major Components
- **Agent CLI (`diffkeeper`)**: Wraps your command/entrypoint, handles lifecycle, and exposes metrics on `--metrics-addr` (default `:9911`).
- **Watch layer**:
  - **eBPF manager** (preferred): Attaches to VFS write paths; supports CO-RE/BTF cache (see `docs/btf-core-guide.md`).
  - **fsnotify fallback**: Recursive directory watches with permission/error handling.
- **Capture engine (BlueShift)**:
  - Reads changed files, optionally splits large files into chunks, computes binary diffs (bsdiff), and hashes chunks using multihash.
  - Stores objects in a BoltDB-backed CAS with reference counts for deduplication.
- **Replay engine (RedShift)**:
  - On start, fetches metadata, rehydrates files from CAS (or legacy full snapshots), verifies Merkle roots, and writes to disk.
- **Storage layout (BoltDB)**
  - `deltas` / `meta`: Legacy full-file entries and metadata.
  - `cas`: Raw content-addressed blobs (diffs or chunks).
  - `cas_refs`: Reference counts keyed by file path.
  - `snapshots`: Optional periodic full snapshots for fast forward recovery.
  - `hashes`: File path -> latest root hash for quick-change detection.
- **Metrics/observability**:
  - Prometheus metrics via `internal/metrics` (capture/recovery counters, CAS size gauges, GC totals).
  - Structured logs for every restore and capture attempt.

## Lifecycle Flow
1. **RedShift (startup)**: Read store -> verify Merkle tree (if present) -> reconstruct files -> log recovery stats.
2. **Watch setup**: Start eBPF manager; if unsupported, fall back to fsnotify recursive watches.
3. **Metrics server**: Launch HTTP handler on `--metrics-addr`.
4. **BlueShift (steady state)**: For each write:
   - Load previous version (if any) from CAS or snapshot.
   - If file size exceeds `chunk-max` threshold, chunk before diffing.
   - Compute diff patch; write blob(s) into CAS; update `cas_refs` and `hashes`.
   - Optionally take full snapshots every N versions (configurable).
5. **Shutdown / crash**: No special hooks required; stored deltas are already durable in BoltDB.

## Deployment Patterns
- **Wrapper entrypoint (Docker/Kubernetes)**: Prepend `diffkeeper --state-dir=/data --store=/deltas/db.bolt -- your-cmd`.
- **Sidecar**: Run DiffKeeper as a sidecar with a shared volume mount for `--state-dir` and `--store`; useful for unmodified images.
- **InitContainer**: In Kubernetes, run RedShift as an init container to pre-hydrate PVCs, then run BlueShift alongside the main container.
- **Native Windows**: Uses fsnotify-only path watching and Win32 long path handling (`internal/platform`). Metrics are identical to Linux.

## Performance + Reliability Notes
- **Chunking**: Enabled for large files to keep memory bounded; tune via `--chunk-*` flags.
- **Dedup + GC**: Reference counts prevent CAS bloat; garbage collection removes unreferenced blobs.
- **eBPF portability**: If BTF is missing or `MEMLOCK` is low, agent transparently drops to fsnotify while logging the reason.
- **Integrity**: Merkle verification blocks replay if corruption is detected; logs include the failing CID/root for triage.
- **Backwards compatibility**: RedShift still understands legacy full-file entries to ease migrations.

## Related Docs
- Quick start and flags: `docs/quickstart.md`
- eBPF/CO-RE + BTF cache: `docs/btf-core-guide.md`, `docs/ebpf-guide.md`
- Kernel compatibility: `docs/supported-kernels.md`
- Project layout: `docs/reference/project-structure.md`
