# BTF & CO-RE Deployment Guide

DiffKeeper's eBPF subsystem is now CO-RE (Compile Once – Run Everywhere), meaning the same `diffkeeper.bpf.o` binary can target any supported kernel (≥4.18) by loading the correct BTF (BPF Type Format) data at runtime. This guide explains how the loader works, how to prime caches, and how to keep things running smoothly in production clusters.

## Runtime Workflow

1. **System BTF probe**: The agent first checks `/sys/kernel/btf/vmlinux`. If present, it is used immediately.
2. **Cache lookup**: If the system lacks embedded BTF, DiffKeeper looks inside `--btf-cache-dir` (default: `/var/cache/diffkeeper/btf`) for a previously downloaded file matching `$(uname -r)`.
3. **BTFHub download**: When allowed (`--disable-btfhub-download=false`), the loader downloads the distro/kernel-specific archive from the [BTFHub-Archive mirror](https://github.com/aquasecurity/btfhub-archive). Archives are `.btf.tar.xz` files containing a single `.btf`.
4. **CO-RE relocation**: The cached/spec'd BTF is injected into `ebpf.CollectionOptions` so cilium/ebpf can perform relocations at load time.
5. **Fallback**: If every step fails, the CLI logs a warning and falls back to `fsnotify` (assuming `--fallback-fsnotify` is true).

## CLI & Env Knobs

| Flag / Env | Purpose | Default |
|------------|---------|---------|
| `--btf-cache-dir` / `DIFFKEEPER_BTF_CACHE_DIR` | Location for cached specs | `/var/cache/diffkeeper/btf` (Unix) or `%TMP%/diffkeeper/btf` |
| `--btfhub-mirror` / `DIFFKEEPER_BTF_MIRROR` | Override base URL for downloads | `https://github.com/aquasecurity/btfhub-archive/raw/main` |
| `--disable-btfhub-download` / `DIFFKEEPER_BTF_ALLOW_DOWNLOAD=0` | Force offline mode (system BTF only) | Download enabled |

> **Tip:** Keep the cache directory on a persistence volume (PVC, hostPath, or `/var/cache`) so repeated startups do not re-download the same archive.

## Pre-Caching for Air-Gapped Clusters

1. In a connected environment, download the required archive(s):
   ```bash
   mkdir -p cache
   curl -L \
     https://github.com/aquasecurity/btfhub-archive/raw/main/ubuntu/22.04/x86_64/5.15.0-92-generic.btf.tar.xz \
     -o cache/5.15.0-92-generic.btf.tar.xz
   ```
2. Extract the archive:
   ```bash
   tar -xf cache/5.15.0-92-generic.btf.tar.xz -C cache
   ```
   This yields `cache/5.15.0-92-generic.btf`.
3. Ship the `.btf` file alongside your container image or bake it into a ConfigMap.
4. Mount the file into `--btf-cache-dir` (or copy during init) so DiffKeeper finds it without hitting the network.
5. Optionally, disable downloads via `--disable-btfhub-download` to ensure offline determinism.

## Generating Minimal BTFs

Full BTFs are small (<1 MB), but you can shrink them further with `bpftool`:

```bash
bpftool gen min_core_btf \
  /path/to/5.15.0-92-generic.btf \
  /path/to/5.15.0-92-generic.min.btf \
  --objects bin/ebpf/diffkeeper.bpf.o
```

Use the `.min.btf` in place of the original to speed up downloads and reduce cache space.

## Common Scenarios

| Scenario | Recommended Action |
|----------|--------------------|
| Mixed-node Kubernetes cluster (Ubuntu 20.04 + Amazon Linux 2) | Pre-populate cache with both kernels; mount `/var/cache/diffkeeper/btf` via hostPath in the DaemonSet |
| Air-gapped edge device | Bundle `.btf` files in the image under `/opt/diffkeeper/btf`; start agent with `--btf-cache-dir=/opt/diffkeeper/btf --disable-btfhub-download` |
| Strict outbound firewall | Mirror `btfhub-archive` behind the firewall and point `--btfhub-mirror` at the internal endpoint |

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| `btfhub download failed ... 404` | Kernel/distribution combo not published | Generate BTF manually via `bpftool btf dump file /sys/kernel/btf/vmlinux > custom.btf` and drop into cache |
| `unsupported architecture for BTFHub` | GOARCH not in built-in map | Manually supply a `.btf` via cache and set `--disable-btfhub-download` |
| `operation not permitted` during load | Missing `CAP_BPF`/`CAP_SYS_ADMIN` or privileged container | Grant capabilities or run as privileged DaemonSet |
| Excessive re-downloads | Cache directory ephemeral (tmpfs, emptyDir) | Persist cache dir or pre-populate during image build |

To inspect what DiffKeeper is doing, run with `--debug` and look for `[eBPF] Loaded BTF spec from ...` messages.

## Verification Checklist

1. `diffkeeper --debug ...` shows the `[eBPF] Loaded BTF spec` log.
2. `ls --color=auto <cache-dir>` contains `${uname -r}.btf`.
3. `bpftool prog` lists the loaded programs attached to `kprobe/*`.
4. `go test ./pkg/ebpf -run TestDownloadAndCacheBTF` succeeds (Linux hosts).

When all four items pass, the CO-RE pipeline is ready for production.
