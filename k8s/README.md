# Kubernetes quick start

1. Create the namespace and service account:

```bash
kubectl create namespace diffkeeper
kubectl apply -f k8s/rbac.yaml
```

2. Prefetch BTFs for your cluster (optional but recommended):

```bash
scripts/prefetch-btf.sh ubuntu 22.04 "$(uname -r)" x86_64 ./btf-cache
kubectl -n diffkeeper create configmap diffkeeper-btf --from-file=btf-cache
```

3. Deploy the sample workload + sidecar (includes PVC for the delta store):

```bash
kubectl apply -f k8s/deployment.yaml
```

DiffKeeper runs as a privileged sidecar, replays the state in an init container,
then keeps intercepting writes via eBPF for the lifetime of the pod.

For production workloads, use the Helm chart under `k8s/helm/diffkeeper/`.

- Smoke test procedure: `k8s/SMOKE_TEST.md` (uses the public image `ghcr.io/saworbit/diffkeeper:latest` and sidecar `--no-exec` mode)
- Troubleshooting guide: `k8s/TROUBLESHOOTING.md`
