#!/bin/bash
set -uo pipefail

echo "Chaos monkey for Minecraft - sends SIGKILL to the server container."
echo "Join the server on localhost:25565, place a sign/block, then watch it survive crashes."

while true; do
  sleep $((RANDOM % 12 + 5))
  target=$(docker compose ps -q minecraft)
  if [ -z "$target" ]; then
    echo "$(date -Iseconds) minecraft container not ready."
    continue
  fi
  echo "$(date -Iseconds) kill -9 ${target}"
  docker kill -s KILL "$target" >/dev/null 2>&1 || true
done
