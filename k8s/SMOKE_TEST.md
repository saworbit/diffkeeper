# DiffKeeper Kubernetes Smoke Test (kind or any cluster)

Goal: verify pod startup, delta persistence across pod recreation, and recovery on boot.

## Prereqs
- `kubectl` configured to a test cluster (kind/minikube OK)
- Default StorageClass that can provision `ReadWriteOnce` PVCs
- Docker/CRI privileges for eBPF (or rely on fsnotify fallback)

## Steps
```bash
# 1) Create namespace + RBAC
kubectl apply -f k8s/rbac.yaml

# 2) Deploy sample (nginx + DiffKeeper sidecar + init) using the public image
kubectl apply -f k8s/deployment.yaml
kubectl -n diffkeeper rollout status deploy/diffkeeper-sample

# 3) Write sentinel data into the watched path
POD=$(kubectl -n diffkeeper get pod -l app=diffkeeper-sample -o jsonpath='{.items[0].metadata.name}')
kubectl -n diffkeeper exec "$POD" -c workload -- \
  sh -c 'echo "hello from diffkeeper $(date -Iseconds)" >> /usr/share/nginx/html/index.html'

# 4) Exercise the app
kubectl -n diffkeeper port-forward deploy/diffkeeper-sample 8080:80 >/tmp/dk_pf.log 2>&1 &
sleep 2
curl -s http://localhost:8080 | tail -n2

# 5) Force pod recreation to validate restore (PVC keeps deltas, state is emptyDir)
kubectl -n diffkeeper delete pod "$POD"
kubectl -n diffkeeper wait --for=condition=ready pod -l app=diffkeeper-sample --timeout=120s
NEWPOD=$(kubectl -n diffkeeper get pod -l app=diffkeeper-sample -o jsonpath='{.items[0].metadata.name}')

# 6) Verify the sentinel text is restored
kubectl -n diffkeeper exec "$NEWPOD" -c workload -- tail -n2 /usr/share/nginx/html/index.html

# 7) (Optional) Kill the workload container only to mimic kill -9
kubectl -n diffkeeper exec "$NEWPOD" -c workload -- pkill -9 nginx || true
kubectl -n diffkeeper rollout status deploy/diffkeeper-sample
kubectl -n diffkeeper exec "$NEWPOD" -c workload -- tail -n2 /usr/share/nginx/html/index.html
```

## Expected results
- Pod becomes Ready with both containers running.
- After pod deletion, the rebuilt pod still serves the appended sentinel line (RedShift replayed from the PVC-backed store).
- After killing the workload container, it restarts and the sentinel line remains (store + state persisted in the pod).

## Notes and knobs
- Metrics: `kubectl -n diffkeeper port-forward deploy/diffkeeper-sample 9911:9911` then `curl :9911/metrics | grep diffkeeper`.
- BTF/CO-RE: If your cluster nodes lack BTF, the manifest already enables `--fallback-fsnotify=true`; check sidecar logs for eBPF errors.
- Storage: `diffkeeper-store` PVC (1Gi) preserves deltas across pod recreation; the watched state uses `emptyDir` to demonstrate replay on every fresh pod.
- If your cluster requires a specific storage class, set it on the PVC before applying the manifest.
- Image pulls use `ghcr.io/saworbit/diffkeeper:latest` (public). If you fork and push a private image, add an `imagePullSecret` to the `diffkeeper` ServiceAccount.
