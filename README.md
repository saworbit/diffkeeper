# DiffKeeper: The Kubernetes Time Machine

[![CI](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml/badge.svg)](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**DiffKeeper is a "Black Box Flight Recorder" for your containers.**

It watches your application's filesystem in real-time and records every change. When a container crashes—or a CI test flakes—you can rewind the state to any exact moment in time to see exactly what happened.

> **Note:** This project previously focused on "Stateful Containers" and database persistence. That architecture has been archived. See [Genesis & Pivot](docs/GENESIS_AND_PIVOT.md) for the story.

---

## The Problem: "Why did that test fail?"
You have a flaky test in CI. It fails 1 out of 50 times. You re-run the job, and it passes. You have no idea why.
* Logs only show you *what* the application printed.
* They don't show you that a config file was corrupted, a temp file was locked, or a binary was overwritten.

## The Solution: Instant Replay
DiffKeeper uses eBPF to capture filesystem writes at line-rate and stores them in a high-speed log (Pebble).

### 1. Record a Session
Wrap your flaky test (or command) with `diffkeeper record`. It adds minimal overhead.

```bash
# In your local terminal or CI pipeline
diffkeeper record --state-dir=/tmp/trace -- go test ./...
```

### 2. Export the "Crash Site"
The test failed! But the container is gone. No problem—DiffKeeper saved the history. Restore the filesystem to exactly 2 minutes and 14 seconds into the run:

```bash
diffkeeper export --state-dir=/tmp/trace --out=./debug_fs --time="2m14s"
```

Now `cd ./debug_fs` and explore the files exactly as they existed at that moment.

## Architecture
- **Engine:** Pure Go + eBPF (CO-RE)
- **Storage:** Pebble (LSM Tree) for high-speed ingestion.
- **Diffing:** bsdiff (Binary patches) for efficient storage.

## Getting Started
See the [Quickstart](docs/quickstart.md) to record your first trace.
