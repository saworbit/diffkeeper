# DiffKeeper Quick Start Guide

Get DiffKeeper running in under 5 minutes!

## Prerequisites

- Docker 24+ installed
- Go 1.23+ (for building from source)
- 100MB free disk space

## Option 1: Run the Demo (Fastest Way)

This runs a complete end-to-end test with Postgres:

```bash
# Clone the repository
git clone https://github.com/yourorg/diffkeeper
cd diffkeeper

# Run the full demo
make demo
```

**What the demo does:**
1. Builds DiffKeeper + Postgres image
2. Starts Postgres with DiffKeeper watching `/var/lib/postgresql/data`
3. Creates test data (3 users in a table)
4. **Kills the container** (simulating crash/eviction)
5. Restarts the container
6. **Verifies data survived** ✅

Expected output:
```
✅✅✅ SUCCESS! Data survived the nuke! ✅✅✅
```

## Option 2: Use DiffKeeper with Your Own App

### Step 1: Build the Agent

```bash
make build
# Output: bin/diffkeeper
```

### Step 2: Wrap Your Application

```bash
# Example: Wrap any command
./bin/diffkeeper \
  --state-dir=/path/to/watch \
  --store=/path/to/deltas.bolt \
  -- your-app --with-args
```

### Step 3: Docker Integration

Create a `Dockerfile`:

```dockerfile
FROM your-app:latest

# Copy DiffKeeper agent
COPY bin/diffkeeper /usr/local/bin/diffkeeper

# Wrap your entrypoint
ENTRYPOINT ["/usr/local/bin/diffkeeper", \
  "--state-dir=/data", \
  "--store=/deltas/db.bolt", \
  "--", \
  "/your-original-entrypoint"]
```

## Option 3: Kubernetes Deployment

Apply the example StatefulSet:

```bash
# Build and load image into k8s (minikube example)
make docker-postgres
minikube image load diffkeeper-postgres:latest

# Deploy
kubectl apply -f k8s-statefulset.yaml

# Create test data
kubectl apply -f k8s-statefulset.yaml  # Includes test job

# Test state survival
kubectl delete pod postgres-with-diffkeeper-0
kubectl wait --for=condition=ready pod/postgres-with-diffkeeper-0
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c 'SELECT * FROM test_data;'
```

## Understanding the Output

When DiffKeeper runs, you'll see logs like:

```
🔴 RedShift: Restoring state from deltas...
✅ RedShift complete: restored 42 files in 1.2s
👁️  Watching /data for changes...
🔵 BlueShift: captured config.json (3.45 KB compressed)
🔵 BlueShift: captured world.db (125.67 KB compressed)
```

## Common Use Cases

### Game Server (Minecraft Example)

```bash
docker run -v minecraft-deltas:/deltas \
  your-minecraft-image \
  diffkeeper --state-dir=/data --store=/deltas/mc.bolt -- java -jar server.jar
```

### ML Training Checkpoint

```bash
python train.py &
PID=$!

# In parallel, watch checkpoints
diffkeeper --state-dir=/models/checkpoints --store=/deltas/ml.bolt -- sleep infinity
```

### CI/CD Cache

```dockerfile
# In your CI Dockerfile
COPY diffkeeper /usr/local/bin/
RUN diffkeeper --state-dir=/root/.cache --store=/cache-deltas/npm.bolt -- npm install
```

## Troubleshooting

### Container crashes immediately

Check logs:
```bash
docker logs <container-name>
```

Common issue: Insufficient permissions. Ensure the data directory is writable.

### "Failed to open store" error

The delta storage path must exist and be writable:
```bash
docker run -v $(pwd)/deltas:/deltas ...
```

### High CPU usage

DiffKeeper watches files with fsnotify. If you have >10k file changes/second, consider:
- Reducing watch scope to specific subdirectories
- Using file patterns to exclude temp files
- Waiting for v1.0 with eBPF support

### Data not restored after restart

1. Verify delta storage is persistent (not in ephemeral emptyDir)
2. Check logs for "RedShift" messages
3. Ensure `--state-dir` points to the correct path

## Next Steps

- **Production use**: Review [ARCHITECTURE.md](ARCHITECTURE.md) for tuning
- **Contribute**: See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup
- **Report issues**: [GitHub Issues](https://github.com/yourorg/diffkeeper/issues)
- **Ask questions**: discussions@diffkeeper.io

## Performance Expectations

From our testing:

| Workload | Files Watched | Delta Size | Recovery Time |
|----------|--------------|------------|---------------|
| Postgres (1GB DB) | 50 files | 45 MB | 3.2s |
| Minecraft world | 1,200 files | 8 MB | 0.8s |
| ML checkpoints | 5 files | 120 MB | 2.1s |
| Config files | 10 files | 2 KB | 0.1s |

**Your mileage may vary** based on hardware, file change patterns, and compression ratios.

## Clean Up Demo

```bash
make clean
# Or manually:
docker rm -f diffkeeper-postgres-demo
docker volume rm diffkeeper-deltas
```

---

**Questions?** Open an issue or email: support@diffkeeper.io