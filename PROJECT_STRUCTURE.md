# DiffKeeper Project Structure

Complete file layout for the MVP implementation.

**Author:** Shane Anthony Wall | **Contact:** shaneawall@gmail.com

```
diffkeeper/
├── README.md                    # Main documentation (your updated version)
├── QUICKSTART.md               # Getting started guide
├── LICENSE                     # Apache 2.0
├── .gitignore                  # Git ignore rules
├── go.mod                      # Go module definition
├── go.sum                      # Dependency checksums (auto-generated)
│
├── main.go                     # Core agent implementation
├── main_test.go               # Unit tests
│
├── Dockerfile                  # Base DiffKeeper image
├── Dockerfile.postgres         # Postgres demo image
├── demo.sh                     # E2E demo script (executable)
├── Makefile                    # Build automation
│
├── k8s-statefulset.yaml       # Kubernetes example
│
├── .github/
│   └── workflows/
│       └── ci.yml             # GitHub Actions CI pipeline
│
├── bin/                        # Build output (gitignored)
│   ├── diffkeeper             # Native binary
│   └── diffkeeper-linux-amd64 # Linux binary
│
├── docs/                       # Additional documentation
│   ├── ARCHITECTURE.md        # Technical deep dive
│   ├── CONTRIBUTING.md        # Contribution guidelines
│   └── PERFORMANCE.md         # Benchmarks and tuning
│
└── examples/                   # Example integrations
    ├── minecraft/
    │   └── Dockerfile
    ├── nginx/
    │   └── Dockerfile
    └── redis/
        └── Dockerfile
```

## File Descriptions

### Core Files

**main.go** (400 lines)
- `DiffKeeper` struct - Main agent
- `RedShift()` - State restoration
- `BlueShift()` - Delta capture
- `WatchLoop()` - fsnotify integration
- `compressData()` / `decompressData()` - Gzip helpers

**main_test.go** (250 lines)
- Unit tests for compression
- Lifecycle tests (create, capture, restore)
- Multi-file restoration tests
- Benchmarks for BlueShift/RedShift

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
- `make demo` - Full E2E test
- `make docker` - Build images
- `make clean` - Remove artifacts

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

# Quick local test
make test-local

# Full E2E test
make demo
```

### Debugging

```bash
# Run with verbose logging
go run main.go --state-dir=./test-data --store=./test.bolt -- cat test.txt

# Check delta storage
go run -tags=bolt ./tools/inspect-db.go test.bolt
```

## Next Steps for v0.2

Files to add:
- `cmd/replay/main.go` - Standalone replay tool
- `internal/diff/bsdiff.go` - True binary diffs
- `internal/storage/interface.go` - Pluggable backends
- `pkg/api/client.go` - Go client library
- `tools/inspect-db.go` - Debug utility

See [ROADMAP.md](ROADMAP.md) for full v1.0 plan.

## Questions?

- Issues: https://github.com/saworbit/diffkeeper/issues
- Discussions: https://github.com/saworbit/diffkeeper/discussions
- Email: shaneawall@gmail.com

**Maintainer:** Shane Anthony Wall