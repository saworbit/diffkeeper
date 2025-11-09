# Auto-Injection via Lifecycle Tracing

DiffKeeper v2.0 can automatically attach itself to new containers or pods by watching Kubernetes Container Runtime Interface (CRI) events. This avoids wrapping every `docker run` or `kubectl` command manually.

## How It Works

1. The eBPF lifecycle tracer attaches to `tracepoint/sched/sched_process_exec` and observes CRI binaries (containerd, cri-o, dockerd).
2. When a new container process spawns, the Go runtime receives a `LifecycleEvent` (PID, state, runtime, container ID, namespace).
3. If `--auto-inject` is enabled, DiffKeeper executes the `--injector-cmd` with metadata so you can configure namespaces, volumes, or sidecars dynamically.

## Configuring Injection

```
./diffkeeper \
  --enable-ebpf \
  --auto-inject \
  --injector-cmd=/opt/diffkeeper/inject.sh \
  --state-dir=/data \
  --store=/deltas/db.bolt \
  -- my-bootstrap-command
```

`inject.sh` receives:

- `argv[1] = container ID`
- Environment variables:
  - `DIFFKEEPER_CONTAINER_ID`
  - `DIFFKEEPER_RUNTIME` (dockerd, containerd, cri-o, etc.)
  - `DIFFKEEPER_NAMESPACE` (Kubernetes namespace when available)
  - `DIFFKEEPER_STATE` (`create`/`start`)

### Sample Injector Script

```bash
#!/usr/bin/env bash
set -euo pipefail

cid="$1"
ns="${DIFFKEEPER_NAMESPACE:-default}"

echo "[injector] Attaching DiffKeeper to container ${cid} (ns=${ns})"

nerdctl exec --namespace "${ns}" "${cid}" \
  /opt/diffkeeper/bin/dk-agent --attach \
    --state-dir=/data \
    --store=/deltas/db.bolt
```

## Security Notes

- The injector runs with the same privileges as the DiffKeeper process. Keep the script minimal, audit inputs, and avoid passing untrusted data to shells.
- Consider storing the injector at `/opt/diffkeeper/inject.sh` with `0750` permissions and limited sudo rules.
- For clusters, run DiffKeeper as a DaemonSet with `hostPID: true` and the required capabilities so it can discover workloads on each node.

## Disabling or Filtering Events

- Disable all auto-injection logic via `--auto-inject=false`.
- For kernels without ring buffer support or environments that forbid lifecycle tracing, set `DIFFKEEPER_EBPF_LIFECYCLE_TRACING=false` in the environment.
- Future releases will expose a `--inject-namespace` allowlist; for now, filter inside your injector script.

## Observability

- Enable `--debug` to view lifecycle events (`[AutoInject] Detected start for ...`).
- Use `bpftool perf` or `bpftool prog tracelog` to ensure the tracepoint program is attached.
- Metrics for lifecycle queue depth and injector success rates are planned for v2.1.
