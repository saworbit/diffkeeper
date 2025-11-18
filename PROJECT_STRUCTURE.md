# DiffKeeper Project Structure

Complete file layout for the v2.0 preview (eBPF monitoring + auto-injection).

**Author:** Shane Anthony Wall | **Contact:** shaneawall@gmail.com

```
diffkeeper/
|-- README.md
|-- QUICKSTART.MD
|-- PROJECT_STRUCTURE.md
|-- IMPLEMENTATION_CHECKLIST.md
|-- K8S_TESTING_GUIDE.md
|-- main.go
|-- main_test.go
|-- Dockerfile
|-- Dockerfile.postgres
|-- demo.sh
|-- Makefile
|-- k8s-statefulset.yaml
|-- go.mod
|-- go.sum
|-- .gitignore
|-- coverage.out
|-- .github/
|   |-- workflows/
|       |-- ci.yml
|-- docs/
|   |-- auto-injection.md
|   |-- btf-core-guide.md
|   |-- ebpf-guide.md
|   |-- patents.md
|   |-- supported-kernels.md
|-- internal/
|   |-- metrics/       # Prometheus registry, collectors, /metrics server
|-- ebpf/
|   |-- diffkeeper.bpf.c
|-- bin/                # build output (gitignored)
|-- test-data/          # sample input fixtures
|-- test-deltas/        # sample delta output
|-- .claude/            # local editor settings (ignored)
```


## File Descriptions

### Core Files

**main.go** (~500 lines)
- `DiffKeeper` struct - orchestrates BoltDB, CAS, diff engine, and monitoring backends (eBPF-first with fsnotify fallback)
- `StartMonitoring()` - loads eBPF manager, profiler, lifecycle tracer, or falls back to fsnotify
- `startMetricsCollectors()` and `metrics.Serve()` - publish Prometheus metrics on `--metrics-addr` (default `:9911`)
- `addWatchRecursive()` - attaches watchers to nested directories on every platform
- `RedShift()` / `BlueShift()` - restore/capture with diff + CAS
- Cobra CLI now includes eBPF/auto-injection flags plus process hand-off via `syscall.Exec`

**main_test.go** (~900 lines)
- Unit tests for compression, lifecycle, multi-file restore, and large files
- Watcher regression coverage (nested directories, permission errors, read-only stores)
- Benchmarks for BlueShift and RedShift

### Configuration

**go.mod**
```go
module github.com/saworbit/diffkeeper
go 1.23
require (
    github.com/cilium/ebpf v0.13.0
    github.com/ulikunitz/xz v0.5.15
    github.com/fsnotify/fsnotify v1.7.0
    github.com/prometheus/client_golang v1.23.2
    github.com/spf13/cobra v1.8.1
    go.etcd.io/bbolt v1.3.10
)
```

**go.sum** - Auto-generated dependency checksums

**pkg/config/config.go** - `EBPFConfig` now includes `BTFConfig` (cache dir, download toggle, mirror URL) so operators can control BTFHub usage and CO-RE portability directly from CLI/env flags.

### Build & CI

**Makefile** - Build targets:
- `make build` - Local binary
- `make build-ebpf` - Compile eBPF probes (requires clang + ebpf/vmlinux.h)
- `make test` - Run tests with coverage
- `make docker-postgres` - Build the demo Postgres image
- `make demo` - Run the end-to-end demo (depends on the demo image)
- `make clean` - Remove build artifacts and demo containers

**Dockerfile** - Multi-stage build:
1. Builder: Go 1.23 alpine
2. Runtime: Alpine with agent binary

**Dockerfile.postgres** - Demo image with Postgres + DiffKeeper

**.github/workflows/ci.yml** - CI pipeline:
- Test on Go 1.23
- Build for linux/darwin, amd64/arm64
- Docker image builds
- E2E test with demo.sh

### Scripts

**demo.sh** (executable)
- Automated E2E test
- Creates Postgres container
- Inserts test data
- Kills container
- Verifies data survival

### Kubernetes

**k8s-statefulset.yaml** - Complete example:
- PVC for delta storage (100MB)
- InitContainer for RedShift
- StatefulSet with DiffKeeper
- Service definition
- Test job

## .gitignore

```gitignore
# Binaries
/bin/
*.exe
*.dll
*.so
*.dylib

# Test artifacts
*.test
*.out
coverage.html
coverage.out

# Local testing
/test-data/
/test-deltas/
*.bolt

# IDE
.vscode/
.idea/
*.swp
.claude/

# OS
.DS_Store
length.txt

# Go
vendor/
```

> **Note**: `.claude/` is added to ignore Claude Code environment settings (local configuration).

## Setting Up a New Repository

### 1. Initialize Git

```bash
git init
git add .
git commit -m "Initial commit: DiffKeeper MVP v0.1"
```

### 2. Create GitHub Repository

```bash
gh repo create saworbit/diffkeeper --public --source=. --remote=origin
git push -u origin main
```

### 3. Set Up CI

GitHub Actions will automatically run on push (`.github/workflows/ci.yml`).

### 4. Tag Release

```bash
git tag -a v0.1.0 -m "MVP Release: Basic file-level state recovery"
git push origin v0.1.0
```

### 5. Build Binaries

```bash
make build-linux  # For Linux releases
```

## Development Workflow

### Adding a New Feature

1. Create feature branch: `git checkout -b feature/name`
2. Implement in `main.go`
3. Add tests in `main_test.go`
4. Run: `make test`
5. Update docs
6. Submit PR

### Testing Locally

```bash
# Unit tests
make test

# Build binary
make build

# Full E2E test (builds demo image automatically)
make demo
```

### Debugging

```bash
# Run with verbose logging against sample data
go run main.go --debug --state-dir=./test-data --store=./test.bolt -- cat test-data/example.txt
```

## Next Steps for v2.1

Roadmap items live in [IMPLEMENTATION_CHECKLIST.md](IMPLEMENTATION_CHECKLIST.md). Highlights include:
- Expanded profiler models (multi-armed bandit + WASM hints)
- Operator/DaemonSet for cluster-wide auto-injection
- Pluggable storage backends and remote sinks
- Standalone replay tooling (`cmd/replay`) for offline restores
- Metrics + tracing exports (Prometheus / OpenTelemetry)

## Questions?

- Issues: https://github.com/saworbit/diffkeeper/issues
- Discussions: https://github.com/saworbit/diffkeeper/discussions
- Email: shaneawall@gmail.com

**Maintainer:** Shane Anthony Wall


- `pkg/ebpf/` - Go manager, BTF loader, profiler, and lifecycle tracer
- `ebpf/diffkeeper.bpf.c` - Kernel probes compiled via `make build-ebpf`
- `docs/ebpf-guide.md` - Build + troubleshooting doc
- `docs/btf-core-guide.md` - BTFHub + CO-RE rollout checklist
- `docs/supported-kernels.md` - Distro/kernel compatibility snapshot
- `docs/auto-injection.md` - Injector workflows
