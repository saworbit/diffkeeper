# Quickstart

This guide shows you how to use DiffKeeper to debug a "flaky" script.

## 1. Installation

```bash
# Build from source (requires Go 1.23+)
go build -o diffkeeper .
```

## 2. The "Flaky" Application
Create a script `flaky.sh` that simulates a bug where a file gets corrupted halfway through execution:

```bash
#!/bin/bash
echo "All systems operational" > status.txt
sleep 2
echo "CRITICAL FAILURE" > status.txt  # <--- The Bug
sleep 1
```

## 3. Record the Crash
Run the script wrapped in DiffKeeper:

```bash
./diffkeeper record --state-dir=./trace -- ./flaky.sh
```

## 4. Time Travel
Investigate what the file looked like before the crash (at 1 second) and after (at 3 seconds).

**At 1 Second:**

```bash
./diffkeeper export --state-dir=./trace --out=./restore_1s --time="1s"
cat ./restore_1s/status.txt
# Output: All systems operational
```

**At 3 Seconds:**

```bash
./diffkeeper export --state-dir=./trace --out=./restore_3s --time="3s"
cat ./restore_3s/status.txt
# Output: CRITICAL FAILURE
```

You have now successfully captured and rewound a filesystem state!
