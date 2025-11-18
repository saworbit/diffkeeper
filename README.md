# DiffKeeper: State-Aware Containers

Stateful containers that survive kill -9. Zero data loss. <100ms recovery.

[![CI](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml/badge.svg)](https://github.com/saworbit/diffkeeper/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/saworbit/diffkeeper)](https://goreportcard.com/report/github.com/saworbit/diffkeeper)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Status: v2.0 Preview](https://img.shields.io/badge/Status-v2.0%20Preview-blue.svg)]()

## 60-Second Demo (start here)

```bash
git clone https://github.com/saworbit/diffkeeper.git
cd diffkeeper/demo/postgres-survive-kill9
docker compose up -d          # starts Postgres with DiffKeeper baked in
./chaos.sh                    # randomly kills the container every few seconds
```

Open another terminal and watch the transaction count never reset:

```bash
watch -n 1 'docker compose exec -T postgres psql -U postgres -d bench -c "SELECT count(*) FROM pgbench_history;"'
```

Metrics: `http://localhost:9911/metrics` (look for `diffkeeper_recovery_total` and `diffkeeper_delta_count`).

Even after repeated kill -9 cycles, the count keeps growing. Works on Linux, Mac, and Windows (Docker Desktop or WSL).

Loom video: https://loom.com/share/xxx (record after merge).

Now go read the rest if you want to know how it works.

---

## What is DiffKeeper?

DiffKeeper is a small Go agent that lets stateful containers restart instantly without losing data. It captures file-level changes as content-addressed deltas and replays them on restart.

## How it works (high level)

1. Intercepts writes via eBPF (fsnotify fallback on unsupported kernels).
2. Captures binary diffs instead of full snapshots.
3. Stores deduplicated deltas in a BoltDB-backed store.
4. Verifies integrity with Merkle trees.
5. Replays changes on restart for sub-100ms recovery.

## Where to run it

- **Docker:** start with `demo/postgres-survive-kill9` for the out-of-the-box experience. More demos will land under `demo/` (Redis, Kubernetes kind cluster, etc.).
- **Kubernetes & Helm:** manifests and charts live under [`k8s/`](k8s/README.md). Example:
  ```bash
  kubectl apply -f k8s/rbac.yaml
  helm install diffkeeper k8s/helm/diffkeeper --namespace diffkeeper --create-namespace
  ```
- **CLI and libraries:** see [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) and [QUICKSTART.MD](QUICKSTART.MD) for command-line flags, ebpf options, and integration notes.

## Native Windows support

DiffKeeper ships a zero-dependency Windows executable.

```powershell
scoop bucket add saworbit https://github.com/saworbit/scoop-bucket
scoop install diffkeeper
# Or download directly
iwr https://github.com/saworbit/diffkeeper/releases/download/v1.2.0/diffkeeper-windows-amd64.exe -OutFile diffkeeper.exe
```

Great for local LLM fine-tuning, game servers, ML checkpoints, and Docker Desktop usage without WSL.

## More docs

- Architecture deep dive: [docs/](docs)
- Kubernetes guide: [k8s/README.md](k8s/README.md)
- Security: [SECURITY.md](SECURITY.md)
- Release notes: [RELEASE_NOTES.md](RELEASE_NOTES.md)

## License

Apache License 2.0 - see [LICENSE](LICENSE).

Maintainer: Shane Anthony Wall (shaneawall@gmail.com)
