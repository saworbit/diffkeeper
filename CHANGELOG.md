# Changelog

## v2.1 - Timeline UX + CI Action (2025-11-28)
- New `timeline` command to list all file events with relative timestamps (no more guessing export times).
- Added composite GitHub Action (`action.yml`) for one-line CI adoption: `uses: saworbit/diffkeeper@v1`.
- Included flaky CI demo (`demo/flaky-ci-test`) and updated docs/readme/quickstart to show the full record ➜ timeline ➜ export loop.
- eBPF write capture moved from kprobe to fentry; for portability we now record filenames via dentry names (no `bpf_d_path`), avoiding kernel helper restrictions on CI.

## v2.0 - Time Machine Preview (2025-11-28)
- Pivoted from BoltDB persistence to Pebble-based flight recorder with key prefixes (`l:/c:/m:`).
- Added journal ingestion + async worker that hashes/compresses into CAS and metadata.
- Introduced `record` / `export` CLI for time-travel debugging of flaky CI workloads.
- Dogfood CI pipeline now records a flaky script and verifies exports (self-test of Time Machine).
- Archived BoltDB-era docs/demos/workflows under `docs/archive/v1-legacy/`.

## v1.x
- See `docs/releases/release-notes.md` for detailed v1 history and binary diff milestones.
