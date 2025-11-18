#!/bin/bash

set -uo pipefail

echo "Connecting every 2s - transaction count should only increase."
while true; do
  count=$(docker compose exec -T postgres psql -U postgres -d bench -t -A -c "SELECT count(*) FROM pgbench_history;" 2>/dev/null | tr -d '[:space:]')
  if [ -z "$count" ]; then
    count="down"
  fi
  echo "$(date +%H:%M:%S) transactions: $count"
  sleep 2
done
