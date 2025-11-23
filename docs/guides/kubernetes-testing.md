# Kubernetes Testing Guide for DiffKeeper

**Author:** Shane Anthony Wall | **Contact:** shaneawall@gmail.com

This guide walks you through testing DiffKeeper on Kubernetes to verify the full state recovery lifecycle.

## Prerequisites

You need **ONE** of the following Kubernetes environments:

### Option 1: Docker Desktop Kubernetes (Recommended for Windows)

1. Open Docker Desktop
2. Go to Settings → Kubernetes
3. Check "Enable Kubernetes"
4. Click "Apply & Restart"
5. Wait for Kubernetes to start (green indicator in bottom-left)

### Option 2: Minikube

```bash
# Install minikube (Windows)
choco install minikube

# OR download from: https://minikube.sigs.k8s.io/docs/start/

# Start minikube
minikube start
```

### Option 3: kind (Kubernetes in Docker)

```bash
# Install kind
go install sigs.k8s.io/kind@latest

# Create cluster
kind create cluster --name diffkeeper-test
```

## Verify Kubernetes is Running

```bash
# Check cluster info
kubectl cluster-info

# Expected output:
# Kubernetes control plane is running at https://...
```

## Test Procedure

### 1. Build the Docker Image

```bash
# Build the DiffKeeper + Postgres image
docker build -t diffkeeper-postgres:latest -f Dockerfile.postgres .

# Verify image exists
docker images | grep diffkeeper-postgres
```

### 2. Load Image into Kubernetes

#### For Docker Desktop Kubernetes:
```bash
# Image is automatically available - no loading needed!
```

#### For Minikube:
```bash
# Load image into minikube
minikube image load diffkeeper-postgres:latest

# Verify
minikube image ls | grep diffkeeper
```

#### For kind:
```bash
# Load image into kind
kind load docker-image diffkeeper-postgres:latest --name diffkeeper-test
```

### 3. Deploy to Kubernetes

```bash
# Apply all manifests (PVC, StatefulSet, Service, Test Job)
kubectl apply -f k8s-statefulset.yaml

# Expected output:
# persistentvolumeclaim/diffkeeper-deltas created
# statefulset.apps/postgres-with-diffkeeper created
# service/postgres created
# job.batch/diffkeeper-test-data created
```

### 4. Monitor Deployment

```bash
# Watch pod status
kubectl get pods -w

# Wait for pod to be Running and Ready (1/1)
# Press Ctrl+C to stop watching
```

Expected progression:
```
NAME                          READY   STATUS              RESTARTS   AGE
postgres-with-diffkeeper-0    0/1     ContainerCreating   0          5s
postgres-with-diffkeeper-0    1/1     Running             0          12s
diffkeeper-test-data-xxxxx    0/1     Completed           0          15s
```

### 5. Check Logs

```bash
# Check DiffKeeper logs (should show RedShift and file watching)
kubectl logs postgres-with-diffkeeper-0 | head -20

# Expected output:
# 🔴 RedShift: Restoring state from deltas...
# ✅ RedShift complete: restored X files in Xms
# 👁️  Watching /var/lib/postgresql/data for changes...
# ... Postgres startup logs ...
```

```bash
# Check test data creation job logs
kubectl logs job/diffkeeper-test-data

# Expected output:
# Waiting for postgres...
# CREATE TABLE
# INSERT 0 3
# total
# -------
#     3
# Test data created successfully!
```

### 6. Verify Test Data Exists

```bash
# Connect to Postgres and query test data
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c 'SELECT * FROM test_data;'
```

Expected output:
```
 id |      name       |         created_at
----+-----------------+----------------------------
  1 | Test Entry 1    | 2025-11-01 16:00:00.123456
  2 | Test Entry 2    | 2025-11-01 16:00:00.234567
  3 | Test Entry 3    | 2025-11-01 16:00:00.345678
(3 rows)
```

### 7. Test State Recovery (Critical Test!)

This is the **key test** that proves DiffKeeper works:

```bash
# 1. Delete the pod (simulates crash/eviction)
kubectl delete pod postgres-with-diffkeeper-0

# 2. Watch it restart
kubectl get pods -w

# Wait for pod to be Running and Ready (1/1) again
# Should take 10-30 seconds
```

```bash
# 3. Verify data SURVIVED the pod deletion!
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c 'SELECT COUNT(*) FROM test_data;'
```

**Expected result:**
```
 count
-------
     3
(1 row)
```

If you see 3 rows, **DiffKeeper successfully recovered the state!** 🎉

### 8. Check DiffKeeper Delta Storage

```bash
# Inspect the delta storage PVC
kubectl exec -it postgres-with-diffkeeper-0 -- ls -lh /deltas/

# Expected output:
# -rw------- 1 postgres postgres  45K Nov  1 16:00 postgres.bolt
```

```bash
# Check size of Postgres data directory vs deltas
kubectl exec -it postgres-with-diffkeeper-0 -- \
  du -sh /var/lib/postgresql/data /deltas
```

You should see that `/deltas` is much smaller than the full data directory!

### 9. Additional Tests (Optional)

#### Test Multiple Crash/Recovery Cycles

```bash
# Add more data
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c \
  "INSERT INTO test_data (name) VALUES ('Extra Entry 4'), ('Extra Entry 5');"

# Delete pod again
kubectl delete pod postgres-with-diffkeeper-0

# Wait for restart
kubectl wait --for=condition=ready pod/postgres-with-diffkeeper-0 --timeout=60s

# Verify all 5 rows exist
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c 'SELECT COUNT(*) FROM test_data;'

# Expected: 5 rows
```

#### Inspect DiffKeeper Metadata

```bash
# See what files DiffKeeper is tracking
kubectl exec -it postgres-with-diffkeeper-0 -- \
  find /var/lib/postgresql/data -type f | sort
```

#### Test with Large Dataset

```bash
# Insert 10,000 rows
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c \
  "INSERT INTO test_data (name) SELECT 'Bulk Entry ' || generate_series(1, 10000);"

# Check delta storage size
kubectl exec -it postgres-with-diffkeeper-0 -- du -h /deltas/postgres.bolt

# Delete and recover
kubectl delete pod postgres-with-diffkeeper-0
kubectl wait --for=condition=ready pod/postgres-with-diffkeeper-0 --timeout=120s

# Verify all 10,003 rows exist
kubectl exec -it postgres-with-diffkeeper-0 -- \
  psql -U postgres -d testdb -c 'SELECT COUNT(*) FROM test_data;'
```

## Cleanup

```bash
# Remove all resources
kubectl delete -f k8s-statefulset.yaml

# Or delete everything individually:
kubectl delete statefulset postgres-with-diffkeeper
kubectl delete service postgres
kubectl delete pvc diffkeeper-deltas
kubectl delete job diffkeeper-test-data

# For minikube users: stop cluster
minikube stop

# For kind users: delete cluster
kind delete cluster --name diffkeeper-test
```

## Troubleshooting

### Pod Stuck in Pending

```bash
# Check events
kubectl describe pod postgres-with-diffkeeper-0

# Common causes:
# - PVC provisioning issue
# - Resource constraints
```

**Solution for Docker Desktop:**
```bash
# Ensure you have enough resources allocated
# Settings → Resources → Increase Memory to at least 4GB
```

### Pod Keeps Restarting

```bash
# Check logs for errors
kubectl logs postgres-with-diffkeeper-0

# Check previous crash logs
kubectl logs postgres-with-diffkeeper-0 --previous
```

### Image Pull Errors

```bash
# Error: "ImagePullBackOff" or "ErrImageNeverPull"

# Verify image exists locally
docker images | grep diffkeeper-postgres

# Ensure imagePullPolicy is set to "Never" in k8s-statefulset.yaml
# (Line 34 should say: imagePullPolicy: Never)

# For minikube: reload image
minikube image load diffkeeper-postgres:latest

# For kind: reload image
kind load docker-image diffkeeper-postgres:latest --name diffkeeper-test
```

### Data Not Persisting

```bash
# Check if PVC is bound
kubectl get pvc diffkeeper-deltas

# Should show STATUS: Bound

# Check if deltas directory is mounted
kubectl exec -it postgres-with-diffkeeper-0 -- mount | grep deltas

# Check DiffKeeper logs for errors
kubectl logs postgres-with-diffkeeper-0 | grep -i error
```

### Connection Refused to Postgres

```bash
# Wait longer - Postgres takes 10-15 seconds to start
kubectl wait --for=condition=ready pod/postgres-with-diffkeeper-0 --timeout=60s

# Check if Postgres is actually running
kubectl exec -it postgres-with-diffkeeper-0 -- pg_isready -U postgres
```

## Success Criteria

✅ Pod deploys successfully
✅ Test job completes and creates 3 rows
✅ Data is queryable via psql
✅ Pod deletion + restart completes in <60s
✅ **Data survives pod restart (count still = 3)**
✅ Delta storage is significantly smaller than full data

If all checks pass, **Kubernetes integration is verified!**

## Performance Expectations

From testing:

| Metric | Value |
|--------|-------|
| Pod startup time (cold) | 15-30s |
| Pod restart time (with recovery) | 10-20s |
| Recovery time (RedShift) | <5s |
| Delta storage overhead | ~10-20% of full data |
| Memory usage | ~256MB (typical) |

## Next Steps

After successful testing:

1. Update [docs/history/implementation-checklist.md](../history/implementation-checklist.md) to mark K8s testing complete
2. Document any issues or improvements needed
3. Consider testing with other workloads (Redis, MongoDB, etc.)
4. Explore multi-replica scenarios

---

**Questions?** Open an issue or email: shaneawall@gmail.com

**Maintainer:** Shane Anthony Wall
