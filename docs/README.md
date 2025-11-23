# DiffKeeper Documentation

Quick links to the most useful docs, grouped by purpose.

## Start Here
- [Quickstart](quickstart.md): run the 60-second demo or wrap your own app.
- [Architecture](architecture.md): capture/replay flow and storage layout.
- [Main README](../README.md): overview and top-level entry points.

## Deploy & Operate
- Kubernetes: [`k8s/README.md`](../k8s/README.md), smoke test (`k8s/SMOKE_TEST.md`), troubleshooting (`k8s/TROUBLESHOOTING.md`), and [test guide](guides/kubernetes-testing.md).
- eBPF/auto-injection: [ebpf-guide.md](ebpf-guide.md), [auto-injection.md](auto-injection.md), [supported-kernels.md](supported-kernels.md), [btf-core-guide.md](btf-core-guide.md).
- Reference: [project structure](reference/project-structure.md), [ebpf-dev-setup.md](ebpf-dev-setup.md), [patents](patents.md).

## History & Planning
- Roadmap and implementation notes live under `docs/history/`:
  - [implementation-checklist.md](history/implementation-checklist.md)
  - [implementation-plan.md](history/implementation-plan.md)
  - [implementation-summary.md](history/implementation-summary.md)
  - [phase3-complete.md](history/phase3-complete.md)
  - [phase4-test-results.md](history/phase4-test-results.md)
  - [v1.0-implementation-complete.md](history/v1.0-implementation-complete.md)
  - [v1.0-final-complete.md](history/v1.0-final-complete.md)

## Releases
- [Release notes](releases/release-notes.md)

## Contributing & Security
- [Contributing](../CONTRIBUTING.md), [Security policy](../SECURITY.md), [License](../LICENSE).
