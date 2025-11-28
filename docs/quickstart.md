# Quickstart

Debug a flaky script end-to-end: record, inspect the timeline, and rewind to the exact moment it broke.

## 1) Build DiffKeeper (or install via the GitHub Action)

```bash
# Build from source (Go 1.23+)
go build -o diffkeeper .
```

CI users can skip this and reference `uses: saworbit/diffkeeper@v1` in a workflow.

## 2) Run the Flaky Demo Under DiffKeeper
The repo ships with a tiny flaky test that silently corrupts `status.log` after 2 seconds.

```bash
./diffkeeper record --state-dir=./trace -- go run ./demo/flaky-ci-test
```

The process exits with a failure after a few seconds (expected).

## 3) Read the Timeline (no more guesswork)

```bash
./diffkeeper timeline --state-dir=./trace
[00m:00s] WRITE    status.log (13B)
[00m:01s] WRITE    db.lock (6B)
[00m:02s] WRITE    status.log (22B)   <-- corruption point
```

Now you know the precise timestamp to rewind to.

## 4) Export the Crash Site

```bash
./diffkeeper export --state-dir=./trace --out=./restored --time="2s"
cat ./restored/status.log
# Output: ERROR: Connection Lost
```

You have successfully captured the filesystem history, located the offending write, and restored the exact failing state.
