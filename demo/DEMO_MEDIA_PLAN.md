# Demo Media Plan (GIF + Video)

Use this checklist to record the short GIF and the narrated video showcasing DiffKeeper.

## Output targets
- GIF (≤15s): `demo/media/diffkeeper-kill9.gif`
- HD video (≤2 min, voiceover optional): `demo/media/diffkeeper-kill9.mp4`

## Recording environment
- Terminal font size 16–18, dark theme, 1080p capture.
- Clean Docker daemon (no stale `diffkeeper` containers).
- Recommended tools: `asciinema` + `agg` for GIF, `ffmpeg` or OBS for video.

## Script (both GIF + video)
1. Clone and start Postgres demo:
   ```bash
   git clone https://github.com/saworbit/diffkeeper.git
   cd diffkeeper/demo/postgres-survive-kill9
   docker compose up -d --build
   ./chaos.sh
   ```
2. Side pane: show counter never resetting:
   ```bash
   watch -n1 'docker compose exec -T postgres psql -U postgres -d bench -c "SELECT count(*) FROM pgbench_history;"'
   ```
3. Metrics preview:
   ```bash
   curl -s http://localhost:9911/metrics | grep diffkeeper_recovery_total | tail -n1
   ```
4. Stop chaos and clean up:
   ```bash
   pkill -f chaos.sh || true
   docker compose down -v
   ```

## Extra B-roll (optional, video)
- Show `demo/nginx-cache-persist` cache surviving `kill -9`:
  ```bash
  cd ../nginx-cache-persist
  docker compose up -d --build
  curl -i http://localhost:8080/cache-test/
  docker compose kill -s KILL nginx
  sleep 3
  curl -i http://localhost:8080/cache-test/ | grep X-Cache-Status
  docker compose down -v
  ```
- Show Redis counter incrementing through chaos:
  ```bash
  cd ../redis-survive-kill9
  docker compose up -d --build
  ./chaos.sh &
  watch -n1 'docker compose exec redis redis-cli GET counter'
  ```

## Converting casts to media
```bash
# Record terminal session
asciinema rec demo.cast

# GIF (15s, fast and legible)
agg --font-size 18 --speed 1.1 demo.cast demo/media/diffkeeper-kill9.gif

# MP4 (HD)
ffmpeg -i demo.cast -vf "crop=iw:ih-50:0:0,scale=1920:1080:force_original_aspect_ratio=decrease" \
  -r 60 -pix_fmt yuv420p demo/media/diffkeeper-kill9.mp4
```

## Notes
- Keep prompt text minimal; show results quickly.
- If metrics port conflicts, adjust `-p` flags in compose before recording.
- After recording, verify files land in `demo/media/` and are <10MB (GIF) / <80MB (video).
