#!/bin/bash
set -uo pipefail

echo "Chaos monkey for Redis - sends SIGKILL at random intervals."
echo "Watch the 'counter' key; it should keep increasing after restarts."

while true; do
  sleep $((RANDOM % 8 + 3))
  target=$(docker compose ps -q redis)
  if [ -z "$target" ]; then
    echo "$(date -Iseconds) redis container not ready."
    continue
  fi
  echo "$(date -Iseconds) kill -9 ${target}"
  docker kill -s KILL "$target" >/dev/null 2>&1 || true
done
