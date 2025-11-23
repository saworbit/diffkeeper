# Kubernetes Troubleshooting

Use these checks when the DiffKeeper sidecar or init flow misbehaves.

## Pod won’t start
- Check image pull and RBAC: `kubectl -n diffkeeper describe pod <pod>` → Events.
- If pulls fail (ImagePullBackOff), ensure the image is public (`ghcr.io/saworbit/diffkeeper:latest`). For private forks, create a dockerconfigjson secret and set it as an imagePullSecret on the `diffkeeper` ServiceAccount.
- eBPF errors? Sidecar logs: `kubectl -n diffkeeper logs <pod> -c diffkeeper-agent | head`. If you see BTF/MEMLOCK errors, rely on the manifest’s `--fallback-fsnotify=true`.
- PVC pending: `kubectl -n diffkeeper get pvc diffkeeper-store`. If pending, set `storageClassName` to one that supports `ReadWriteOnce`.

## Recovery didn’t replay files
- Confirm store volume is mounted: `kubectl -n diffkeeper exec <pod> -c diffkeeper-agent -- ls -lh /var/lib/diffkeeper`.
- Ensure init container ran: `kubectl -n diffkeeper get pod <pod> -o jsonpath='{.status.initContainerStatuses[*].state}'`.
- Look for RedShift logs in init: `kubectl -n diffkeeper logs <pod> -c diffkeeper-replay`.
- If pod was merely restarted (not recreated), the init container does not rerun; delete the pod to force RedShift against a fresh `emptyDir`.

## Captures missing
- Sidecar logs: `kubectl -n diffkeeper logs <pod> -c diffkeeper-agent | grep BlueShift`.
- Verify watched path matches `--state-dir` (`/state` in the sample). Writes outside that path are ignored.
- Permissions: if the workload writes files with restrictive perms, the agent may log permission errors and skip capture.

## Metrics/observability
- Port-forward metrics: `kubectl -n diffkeeper port-forward deploy/diffkeeper-sample 9911:9911`.
- Key series: `diffkeeper_capture_total`, `diffkeeper_recovery_total`, `diffkeeper_delta_count`, `diffkeeper_store_bytes`.
- If metrics server fails to bind, set `--metrics-addr=0.0.0.0:9911` in `deployment.yaml`.

## Cleaning up
```bash
kubectl delete namespace diffkeeper
```
