#!/bin/bash

set -uo pipefail

echo "Chaos monkey started - will randomly send SIGKILL to Postgres."
echo "Watch the pgbench count in another terminal - it should never reset."
echo "Press Ctrl+C to stop."

while true; do
  sleep $((RANDOM % 8 + 3))
  target=$(docker compose ps -q postgres)
  if [ -z "$target" ]; then
    echo "$(date -Iseconds) Postgres container not ready yet."
    continue
  fi
  echo "$(date -Iseconds) Sending kill -9 to ${target}"
  docker kill -s KILL "$target" >/dev/null 2>&1 || true
  # Ensure the container comes back up (restart policies sometimes skip manual kills)
  docker compose up -d postgres >/dev/null 2>&1 || true
done
