# eBPF Developer Setup

DiffKeeper ships with a CO-RE compatible probe (`ebpf/diffkeeper.bpf.c`) that
targets modern kernels (>= 4.18). The repo already contains the generated
`vmlinux.h`, libbpf helper headers, and a pre-built `diffkeeper.bpf.o`, so
most contributors can build immediately. If you want to iterate on the probe,
follow the steps below.

## 1. Install Tooling

| Requirement | Linux | macOS | Windows |
|-------------|-------|-------|---------|
| Clang/LLVM  | `sudo apt install clang llvm` | `brew install llvm` | Install LLVM 14+ from [releases.llvm.org](https://releases.llvm.org) and add `clang.exe` to `PATH`. |
| bpftool     | `sudo apt install bpftool` or build from source | Build from source | Use WSL2 + bpftool or copy `vmlinux` via BTFHub |

Optional: `docker buildx` and `kubectl` if you plan to test inside Kubernetes.

## 2. Generate `vmlinux.h` (Optional)

The repo bundles a generic `vmlinux.h` derived from 6.1 kernels. When you need
to regenerate it for another kernel:

```bash
sudo bpftool btf dump file /sys/kernel/btf/vmlinux format c > ebpf/vmlinux.h
```

For cross-compilation or air-gapped clusters, download the right BTF from
[btfhub-archive](https://github.com/aquasecurity/btfhub-archive) and copy it to
`ebpf/vmlinux.h`.

## 3. Rebuild the Probe

```bash
make build-ebpf
```

This runs clang with CO-RE enabled flags (`-target bpf -D__TARGET_ARCH_x86`)
and refreshes `ebpf/diffkeeper.bpf.o`. The Go agent embeds this object directly.

If you want extra verification, run:

```bash
bpftool prog load ebpf/diffkeeper.bpf.o /sys/fs/bpf/dk-test
bpftool prog list | grep diffkeeper
bpftool prog detach pinned /sys/fs/bpf/dk-test
rm /sys/fs/bpf/dk-test
```

## 4. Update BTF Cache on Hosts

DiffKeeper automatically downloads CO-RE specs via BTFHub when allowed.
To preload them (air-gapped, production hardening):

```bash
./scripts/prefetch-btf.sh ubuntu 22.04 5.15.0-92-generic x86_64
kubectl create configmap diffkeeper-btf --from-file=btf/5.15.0-92-generic.btf
```

Then start the agent with:

```bash
diffkeeper --btf-cache-dir=/etc/diffkeeper/btf --disable-btfhub-download
```

## 5. Smoke Test

On a Linux host with write access to `/sys/kernel/debug/tracing`:

```bash
sudo ./bin/diffkeeper --state-dir=/tmp/dk --store=/tmp/dk.bolt --debug
```

Touch files in `/tmp/dk` and confirm you see `[eBPF] capture` logs within
milliseconds. Use `sudo bpftool prog` to ensure probes are attached to
`vfs_write*` and `tracepoint:sched:sched_process_exec`.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `load eBPF spec: permission denied` | Ensure the container/pod runs privileged or has `CAP_BPF` + `CAP_PERFMON`. |
| `ringbuf read error: interrupted system call` | Kernel < 5.8 has known ring buffer issues; set `--fallback-fsnotify`. |
| `operation not permitted` when building | Install clang/LLVM 14+ and make sure it is on `PATH`. |
| `Missing ebpf/vmlinux.h` | Follow step 2 or copy from a machine running the target kernel. |

Once all steps pass you can iterate on `ebpf/diffkeeper.bpf.c`, rebuild with
`make build-ebpf`, and the Go tests / CI will embed the fresh bytecode.
