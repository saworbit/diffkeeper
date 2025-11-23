# Contributing to DiffKeeper

Thanks for helping improve DiffKeeper! This guide covers how to set up a dev environment, coding standards, and what we expect in pull requests.

## Getting Set Up
- Go 1.23+
- Docker 24+ (for demos/tests)
- Optional: clang/llvm + kernel `vmlinux.h` for eBPF builds (see `docs/ebpf-guide.md`)
- Optional: `make` (recommended for repeatable builds)

```bash
git clone https://github.com/saworbit/diffkeeper.git
cd diffkeeper
go mod download
make build          # or: go build ./...
```

### Running Tests and Linters
```bash
go test ./...               # unit + integration tests
golangci-lint run ./...     # uses .golangci.yml defaults
make demo                   # optional: end-to-end demo (Postgres survive kill -9)
```

Please run `gofmt -w` (or `go fmt ./...`) before sending a PR. If you modify Docker or docs, keep examples runnable and verified locally.

## Coding Guidelines
- Prefer small, focused changes with clear rationale in commit messages.
- Add tests for new behavior and regression coverage for bug fixes.
- Keep log messages actionable (what failed, what path, next step).
- Default to eBPF-first behavior but keep fsnotify parity; avoid regressions on Windows.
- Document new flags, env vars, and metrics in `docs/quickstart.md` or relevant docs.

## Pull Request Checklist
- [ ] Tests pass: `go test ./...`
- [ ] Lint clean: `golangci-lint run ./...`
- [ ] New/changed behavior is documented
- [ ] Added/updated examples or demos if applicable
- [ ] Screenshots or logs included for UI/UX changes (if any)

## Filing Issues
Create a GitHub issue with:
- Repro steps (commands, environment, OS/kernel)
- Expected vs. actual behavior
- Relevant logs (`--debug` output) and metrics (`diffkeeper_*`)

## Where to Help
- Open items and roadmap: `docs/history/implementation-checklist.md`
- Architecture: `docs/architecture.md`
- Kubernetes: `k8s/` and `docs/guides/kubernetes-testing.md`
- Demos/examples: `demo/`

## Communication
- Issues: https://github.com/saworbit/diffkeeper/issues
- Discussions: https://github.com/saworbit/diffkeeper/discussions
- Email: shaneawall@gmail.com

Thanks for contributing!
