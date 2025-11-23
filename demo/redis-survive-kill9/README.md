# Redis Kill-9 Demo (DiffKeeper)

Redis wrapped with DiffKeeper to keep `/data` consistent through crashes.

## Run
```bash
cd demo/redis-survive-kill9
docker compose up -d --build
```

- Redis: `localhost:6379`
- Metrics: `http://localhost:9913/metrics`
- A helper `writer` service increments `counter` continuously.

## Validate
```bash
# Observe the counter
docker compose exec redis redis-cli GET counter

# Kill Redis repeatedly
./chaos.sh
# in another terminal:
watch -n1 'docker compose exec redis redis-cli GET counter'
```
The counter should keep increasing after each ungraceful restart.

## Clean up
```bash
docker compose down -v
```

## How it works
- DiffKeeper wraps `redis-server` (`--state-dir=/data`, `--store=/deltas/redis.bolt`).
- Deltas are stored in the `redis_deltas` named volume.
- On restart, RedShift restores the dataset before Redis starts, so `counter` never resets even with `kill -9`.
