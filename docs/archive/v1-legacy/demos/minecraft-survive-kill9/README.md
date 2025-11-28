# Minecraft Kill-9 Demo (DiffKeeper)

Protect your Minecraft world against hard crashes with DiffKeeper.

## Requirements
- Docker with at least ~2GB free RAM while the server runs
- A local Minecraft client to join `localhost:25565`

## Run
```bash
cd demo/minecraft-survive-kill9
docker compose up -d --build
```

Wait ~60-90s for the first start while the world generates.

- Server: `localhost:25565`
- Metrics: `http://localhost:9914/metrics` (`diffkeeper_*` series)

## Validate
1. Join the server, place a sign or block at spawn.
2. In another terminal, start the chaos monkey:
   ```bash
   ./chaos.sh
   ```
3. Let it `kill -9` the server a few times. Docker restarts it automatically.
4. Rejoin and confirm your placed block/sign is still present.

## Clean up
```bash
docker compose down -v
```

## How it works
- DiffKeeper wraps the stock `/start` script with `--state-dir=/data --store=/deltas/mc.bolt`.
- World data lives in `/data`; deltas are persisted in the `mc_deltas` volume.
- On restart, RedShift rehydrates the world before the server boots, preserving player changes through crashes.
