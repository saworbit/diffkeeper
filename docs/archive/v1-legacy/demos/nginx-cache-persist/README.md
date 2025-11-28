# Nginx Cache Demo (DiffKeeper)

Persist Nginx disk cache and static assets across ungraceful restarts.

## Run
```bash
cd demo/nginx-cache-persist
docker compose up -d --build
```

- App: http://localhost:8080
- Metrics: http://localhost:9912/metrics (look for `diffkeeper_*`)

Warm the cache and capture state:
```bash
curl -i http://localhost:8080/cache-test/
docker compose exec nginx sh -c 'echo "hello $(date)" >> /data/index.html'
```

Crash and verify:
```bash
docker compose kill -s KILL nginx
sleep 3
curl -i http://localhost:8080/cache-test/   # expect X-Cache-Status: HIT after restart
curl http://localhost:8080                  # shows appended line from /data/index.html
```

Clean up:
```bash
docker compose down -v
```

## How it works
- DiffKeeper wraps Nginx (`--state-dir=/data`, `--store=/deltas/nginx.bolt`).
- `default.conf` writes cache to `/data/cache` and serves static files from `/data`.
- On restart, RedShift replays cached objects and file changes from the Bolt store so the cache and edited HTML survive `kill -9`.
