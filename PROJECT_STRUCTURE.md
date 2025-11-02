# DiffKeeper Project Structure

Complete file layout for the MVP implementation.

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
|-- bin/                # build output (gitignored)
|-- test-data/          # sample input fixtures
|-- test-deltas/        # sample delta output
|-- .claude/            # local editor settings (ignored)
```


## File Descriptions

### Core Files

**main.go** (~320 lines)
- `DiffKeeper` struct - core agent wiring (BoltDB + fsnotify)
- `addWatchRecursive()` - attaches watchers to nested directories on every platform
- `RedShift()` - restores files from stored deltas
- `BlueShift()` - captures updates with compression and hashing
- Cobra CLI with `--debug` flag and process hand-off via `syscall.Exec`

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
    github.com/fsnotify/fsnotify v1.7.0
    github.com/spf13/cobra v1.8.1
    go.etcd.io/bbolt v1.3.10
)
```

**go.sum** - Auto-generated dependency checksums

### Build & CI

**Makefile** - Build targets:
- `make build` - Local binary
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

## Next Steps for v0.2

Roadmap items live in [IMPLEMENTATION_CHECKLIST.md](IMPLEMENTATION_CHECKLIST.md). Highlights include:
- Binary diff support for lower storage overhead
- Pluggable storage backends and remote sinks
- Standalone replay tooling (`cmd/replay`) for offline restores
- Additional observability (metrics, debug utilities)

## Questions?

- Issues: https://github.com/saworbit/diffkeeper/issues
- Discussions: https://github.com/saworbit/diffkeeper/discussions
- Email: shaneawall@gmail.com

**Maintainer:** Shane Anthony Wall


