# DiffKeeper Helm Chart

Deploy DiffKeeper as a sidecar + init container that replays state before your workload starts, then captures changes continuously.

## Quick start
```bash
helm install diffkeeper ./k8s/helm/diffkeeper --namespace diffkeeper --create-namespace
```

Defaults:
- Image: `ghcr.io/saworbit/diffkeeper:latest`
- Sidecar mode: `--no-exec` (daemon; use for Kubernetes)
- Store PVC: created automatically (`diffkeeper-store`, 1Gi, RWO)
- State: `emptyDir` to demonstrate replay on pod restart

## Key values
- `image.repository` / `image.tag` / `image.pullPolicy`
- `noExec` (bool): keep the agent running as a sidecar; set false only if you want to exec a command.
- `stateDir`: path watched inside the pod (mount from your workload)
- `storePath`: path to the Bolt store inside the sidecar/init containers
- `storePVC`:
  - `create`: true to create a PVC; set false to supply `existingClaim`
  - `name`: PVC name when `create=true`
  - `existingClaim`: use this PVC instead of creating one
  - `size`: storage request (e.g., `1Gi`)
  - `storageClassName`: set if your cluster requires a specific class
  - `accessModes`: defaults to `["ReadWriteOnce"]`
- `workload.image`, `workload.command`, `workload.args`: demo workload defaults to nginx; override for your app.
- `btfCacheDir`: path for BTF cache; defaults to `/var/cache/diffkeeper/btf`

## Example overrides
Use an existing PVC and a different workload:
```bash
helm install diffkeeper ./k8s/helm/diffkeeper \
  --namespace diffkeeper --create-namespace \
  --set workload.image=docker.io/library/nginx:stable \
  --set storePVC.create=false \
  --set storePVC.existingClaim=my-existing-pvc \
  --set stateDir=/usr/share/nginx/html
```

Use a private image:
```bash
helm install diffkeeper ./k8s/helm/diffkeeper \
  --namespace diffkeeper --create-namespace \
  --set image.repository=ghcr.io/yourfork/diffkeeper \
  --set image.tag=v2.0.0 \
  --set serviceAccount.imagePullSecrets[0].name=your-regcred
```

## Notes
- eBPF is enabled with fallback to fsnotify by default; kind/minikube may fall back automatically.
- Metrics: `:9911` inside the sidecar; port-forward the deployment to scrape.
- State volume is `emptyDir` by default; for production, mount your own PVC at `stateDir`.
