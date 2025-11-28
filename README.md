# DiffKeeper: The Kubernetes Time Machine

[![CI](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml/badge.svg)](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**DiffKeeper is a Black Box Flight Recorder for your containers.**

It watches your application's filesystem in real-time and records every change. When a container crashes—or a CI test flakes—you can rewind the state to any exact moment and see exactly what happened.

> **Note:** The earlier "stateful containers" design is archived. See [Genesis & Pivot](docs/GENESIS_AND_PIVOT.md) for the story.

---

## The Problem: "Why did that test fail?"
You have a flaky test in CI. It fails 1 out of 50 times. You re-run the job, and it passes. You have no idea why.
* Logs only show you *what* the application printed.
* They don't show you that a config file was corrupted, a temp file was locked, or a binary was overwritten.

## The Solution: Instant Replay
DiffKeeper uses eBPF to capture filesystem writes at line-rate and stores them in Pebble. Then it gives you a timeline so you never guess timestamps again.

### 1) Record a Session
Wrap your flaky test (or any command). Minimal overhead.

```bash
diffkeeper record --state-dir=/tmp/trace -- go test ./...
```

### 2) See the Timeline (no blindfolds)
List every write in order to pick the exact second to rewind:

```bash
diffkeeper timeline --state-dir=/tmp/trace
[00m:01s] WRITE    status.log (13B)
[00m:05s] WRITE    db.lock (6B)
[02m:14s] WRITE    status.log (22B)   <-- the failure
```

### 3) Export the Crash Site
Restore the filesystem to the moment of failure:

```bash
diffkeeper export --state-dir=/tmp/trace --out=./debug_fs --time="2m14s"
```

`cd ./debug_fs` and inspect files exactly as they existed at that moment.

## Drop-in GitHub Action
No curl | sh snippets needed—use the composite action directly:

```yaml
steps:
  - uses: actions/checkout@v4
  - name: Record flaky test
    uses: saworbit/diffkeeper@v1
    with:
      command: go test ./...
      state-dir: diffkeeper-trace
```

On failure the trace uploads as an artifact; you can run `diffkeeper timeline` to find the culprit write, then `diffkeeper export` to reconstruct it locally.

## The "Flaky CI" Demo
Run the built-in demo to see the loop end-to-end:

```bash
diffkeeper record --state-dir=./trace -- go run ./demo/flaky-ci-test
diffkeeper timeline --state-dir=./trace
diffkeeper export --state-dir=./trace --out=./restored --time="2s"
cat ./restored/status.log  # ERROR: Connection Lost
```

## Architecture
- **Engine:** Pure Go + eBPF (CO-RE)
- **Storage:** Pebble (LSM) for high-speed ingestion.
- **Diffing:** bsdiff (binary patches) for efficient storage.

## CI / Dogfooding
- GitHub Actions (`.github/workflows/ci.yml`) runs unit/race tests, cross-platform builds, and a functional time-machine test that records a flaky script and verifies exports.
- BoltDB-era workflows remain archived under `docs/archive/v1-legacy/workflows/`.

## Getting Started
See the [Quickstart](docs/quickstart.md) to record, view the timeline, and export your first trace.
