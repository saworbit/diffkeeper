# Supported Kernels & Distros

DiffKeeper's eBPF pipeline relies on BTF (BPF Type Format) data. Native BTF (`/sys/kernel/btf/vmlinux`) ships with modern distributions, while older kernels use the [BTFHub-Archive](https://github.com/aquasecurity/btfhub-archive) mirror. The loader automatically discovers the current distro via `/etc/os-release`, downloads the matching archive (if allowed), and caches it inside `--btf-cache-dir`.

The table below lists the combinations we have validated end-to-end (syscall capture + profiler + lifecycle tracing). Any kernel >=4.18 with either native BTF or a BTFHub artifact should work.

| Distribution | Version | Kernel(s) | BTF Source | Notes |
|--------------|---------|-----------|------------|-------|
| Ubuntu | 22.04 LTS | `5.15.0-60`, `5.15.0-92` | Native `/sys/kernel/btf/vmlinux` | Ideal baseline for building CO-RE objects |
| Ubuntu | 20.04 LTS | `5.4.0-174`, `5.4.0-182` | BTFHub archive (`ubuntu/20.04/x86_64/...`) | Requires download or pre-cached file |
| Debian | 11 (Bullseye) | `5.10.0-27-amd64` | BTFHub archive (`debian/11/x86_64/...`) | Tested inside containerd + Docker |
| CentOS Stream | 8 | `4.18.0-477.el8` | BTFHub archive (`centos/8/x86_64/...`) | Minimum supported kernel (4.18) |
| Amazon Linux | 2023 | `6.1.50-17.118` | Native BTF | Works for Bottlerocket nodes too |
| Fedora | 39 | `6.5.13-300.fc39` | Native BTF | Ships with CAP_BPF by default on privileged pods |
| Rocky Linux | 9 | `5.14.0-427.el9` | BTFHub archive (`rocky/9/x86_64/...`) | Matches RHEL-compatible clusters |

> **Need another distro?** Check the [BTFHub README](https://github.com/aquasecurity/btfhub-archive#supported-distributions--kernels) for the exact path. If an archive does not exist, capture BTF manually: `bpftool btf dump file /sys/kernel/btf/vmlinux > custom.btf`.

## Adding Internal Coverage

1. Pick a representative node (per distro) and record:
   - `ID` / `VERSION_ID` from `/etc/os-release`
   - `uname -r`
2. Boot DiffKeeper with `--debug --enable-ebpf`.
3. Confirm log line: `[eBPF] Loaded BTF spec from ...`.
4. Update this document (or your internal runbook) with the new row.

## Troubleshooting Unsupported Kernels

| Symptom | Resolution |
|---------|------------|
| 3.x kernel (no eBPF) | Not supported; continue using fsnotify fallback |
| 4.14 kernels with backported BTF | Provide matching `.btf` manually and disable downloads (some vendor kernels expose `/sys/kernel/btf/vmlinux`) |
| ARM64 nodes | Supported when the BTFHub path `.../arm64/...` exists; otherwise capture `vmlinux` from the device and store in cache |

If you encounter a missing distro, please open an issue (or PR) so we can note it here and, when possible, contribute the BTF to BTFHub.
