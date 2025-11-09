# DiffKeeper eBPF Guide

DiffKeeper v2.0 introduces kernel-level interception for filesystem writes, enabling <1µs capture latency and <0.5% CPU overhead even on workloads that exceed 10K writes/sec. This guide details how to build, load, and troubleshoot the new probes.

## Requirements

- Linux kernel **4.18+** with eBPF enabled (CONFIG_BPF + CONFIG_BPF_JIT)
- `clang/llvm` 12+ and `bpftool`
- Capability to load BPF objects (`CAP_BPF` on 5.8+, or `CAP_SYS_ADMIN` / privileged container)
- Generated `ebpf/vmlinux.h` matching the target kernel (`bpftool btf dump file /sys/kernel/btf/vmlinux > ebpf/vmlinux.h`)

> Containers: run DiffKeeper in a privileged init/sidecar (or use the new `--auto-inject` flow) so probes can attach to host kernels.

## Building the Probes

1. Generate `vmlinux.h` once per kernel:
   ```bash
   sudo bpftool btf dump file /sys/kernel/btf/vmlinux > ebpf/vmlinux.h
   ```
2. Compile the probes (produces `bin/ebpf/diffkeeper.bpf.o`):
   ```bash
   make build-ebpf
   ```
3. Ship the `.bpf.o` artifact alongside the DiffKeeper binary (default lookup path: `bin/ebpf/diffkeeper.bpf.o`). Override via `--ebpf-program=/path/to/custom.o`.

The C source lives in `ebpf/diffkeeper.bpf.c` and contains three probe families:

| Probe | Description | Maps |
|-------|-------------|------|
| `kprobe/vfs_write`, `vfs_writev`, `vfs_pwritev` | Captures write syscalls, extracts canonical paths via `bpf_d_path`, emits `syscall_event` structs | `events` (perf)
| `tracepoint/sched/sched_process_exec` | Detects process/container exec events and emits lifecycle metadata | `lifecycle_events` (ringbuf) |
| Hot-path filters (future) | BPF map stub for profiler hints | `hot_paths` (hash-map placeholder) |

## Runtime Behavior

- At startup, DiffKeeper attempts to load the configured `.bpf.o`. If loading fails (missing file, kernel rejects program, insufficient privileges), it logs the error and automatically falls back to the fsnotify watcher when `--fallback-fsnotify=true` (default).
- Events flow from perf/ring buffers → Go `pkg/ebpf` manager → BlueShift. The adaptive profiler reprograms filters every `--profiler-interval`.
- Lifecycle events trigger `--auto-inject` logic. When `--injector-cmd=/opt/diffkeeper/inject.sh` is set, the command receives the container ID as argv[1] and metadata via `DIFFKEEPER_*` env vars.

## CLI Flags Recap

| Flag | Purpose | Default |
|------|---------|---------|
| `--enable-ebpf` | Toggle kernel interception | `true` |
| `--ebpf-program` | Path to `.bpf.o` | `bin/ebpf/diffkeeper.bpf.o` |
| `--fallback-fsnotify` | Revert to fsnotify when load fails | `true` |
| `--profiler-interval` | EMA sampling interval | `100ms` |
| `--enable-profiler` | Disable profiler without disabling eBPF | `true` |
| `--auto-inject` | Handle lifecycle events for container attach | `true` |
| `--injector-cmd` | Command executed on lifecycle events | `` (disabled) |

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `open eBPF object ... no such file or directory` | `.bpf.o` missing | Run `make build-ebpf` or set `--ebpf-program` |
| `operation not permitted` | Missing CAP_BPF/CAP_SYS_ADMIN | Run in privileged container or grant caps |
| `lifecycle ring buffer error` | Kernel lacks ring buffer support (<5.8) | Disable lifecycle tracing via `--auto-inject=false` |
| No events captured | Path filter mismatch | Confirm state dir matches absolute path seen in `syscall_event`, or review `docs/auto-injection.md` for container namespace mapping |

Enable debug logs with `--debug` to view kernel vs fallback mode at runtime.

## Validation

1. Build probes and run stress test:
   ```bash
   make build
   make build-ebpf
   sudo ./bin/diffkeeper --state-dir=/data --store=/deltas/db.bolt \
     --enable-ebpf --profiler-interval=50ms -- nginx -g 'daemon off;'
   ```
2. Generate churn (>10K writes/sec) with `stress-ng --iomix 4`.
3. Watch metrics:
   - `perf stat -e bpf_outp` to ensure events flow
   - `bpftool prog tracelog` for loader errors
   - DiffKeeper logs should report `<1µs capture latency` and `<0.5% CPU`

For deeper kernel debugging, run `sudo cat /sys/kernel/debug/tracing/trace_pipe` to verify probes are firing.
