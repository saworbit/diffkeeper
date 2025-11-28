# Flaky CI Test Demo

Reproduce a silent file corruption and use `diffkeeper timeline` to find the exact moment it occurred.

## Run the flaky test under DiffKeeper
```bash
diffkeeper record --state-dir=./trace -- go run ./demo/flaky-ci-test
```

The test intentionally fails after a few seconds.

## Inspect the timeline
```bash
diffkeeper timeline --state-dir=./trace
```

Example output (times relative to session start):
```
Session Start: 2025-11-28T21:40:00Z
TIME       OP       PATH
------------------------------------------------
[00m:00s] WRITE    status.log (13B)
[00m:01s] WRITE    db.lock (6B)
[00m:02s] WRITE    status.log (22B)   <-- bug
```

Now you know exactly which timestamp to rewind to: `00m:02s`.

## Export the corrupted state
```bash
diffkeeper export --state-dir=./trace --out=./restored --time="2s"
cat ./restored/status.log
```

Expected content:
```
ERROR: Connection Lost
```

The loop is closed: record ➜ timeline ➜ export.
