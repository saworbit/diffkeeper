# DiffKeeper Documentation

Quick links to the most useful docs, grouped by purpose.

## Start Here
- [Quickstart](quickstart.md): record/export a flaky script with the Time Machine workflow.
- [Architecture](architecture.md): Firehose → worker → Pebble prefixes (`l:/c:/m:`).
- [Genesis & Pivot](GENESIS_AND_PIVOT.md): why we moved from "stateful containers" to "flight recorder".
- [Main README](../README.md): overview and top-level entry points.

## Specs & Design
- [PIVOT_SPEC_V2](specs/PIVOT_SPEC_V2.md): storage engine swap, journal pipeline, CLI expectations.
- [GENESIS](GENESIS.md): snapshot of the pivot decision.

## Deploy & Operate
- Kubernetes: [`k8s/README.md`](../k8s/README.md), smoke test (`k8s/SMOKE_TEST.md`), troubleshooting (`k8s/TROUBLESHOOTING.md`), and [test guide](guides/kubernetes-testing.md).
- eBPF/auto-injection: [ebpf-guide.md](ebpf-guide.md), [auto-injection.md](auto-injection.md), [supported-kernels.md](supported-kernels.md), [btf-core-guide.md](btf-core-guide.md).
- Reference: [project structure](reference/project-structure.md), [ebpf-dev-setup.md](ebpf-dev-setup.md), [patents](patents.md).

## History & Archives
- v1 BoltDB-era documents and demos live under `docs/archive/v1-legacy/` (architecture, history, workflows, and demos).

## Releases
- [Release notes](releases/release-notes.md)

## Contributing & Security
- [Contributing](../CONTRIBUTING.md), [Security policy](../SECURITY.md), [License](../LICENSE).
