# DiffKeeper Postgres kill -9 Survival Demo

This folder is the 60-second demo that shows DiffKeeper keeping Postgres alive through repeated SIGKILL events with zero data loss.

## Run

```bash
docker compose up -d
```

## Unleash chaos

Linux/Mac:

```bash
./chaos.sh  # restarts the container after each kill
```

Windows:

```bat
chaos.bat   # restarts the container after each kill
```

## Verify zero data loss

```bash
./verify.sh
```

## CI smoke test (one-shot)

For the scripted check that kills Postgres once and asserts the counter never drops:

```bash
./ci-smoke.sh
```

You should see the transaction count increase continuously, even while the container is being killed.

## Metrics

Scrape `http://localhost:9911/metrics` and look for:

- `diffkeeper_recovery_total` increasing
- `diffkeeper_delta_count` growing
- Recovery time p99 below ~80ms

If the container name differs, `docker compose ps` shows the active container ID; the chaos scripts automatically use it.
