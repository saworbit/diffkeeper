# Changelog

## v2.0 - Time Machine Preview (2025-11-28)
- Pivoted from BoltDB persistence to Pebble-based flight recorder with key prefixes (`l:/c:/m:`).
- Added journal ingestion + async worker that hashes/compresses into CAS and metadata.
- Introduced `record` / `export` CLI for time-travel debugging of flaky CI workloads.
- Dogfood CI pipeline now records a flaky script and verifies exports (self-test of Time Machine).
- Archived BoltDB-era docs/demos/workflows under `docs/archive/v1-legacy/`.

## v1.x
- See `docs/releases/release-notes.md` for detailed v1 history and binary diff milestones.
