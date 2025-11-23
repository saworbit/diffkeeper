#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

cleanup() {
  docker compose down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose up -d

echo "Waiting for Postgres to be ready..."
ready=0
for i in {1..30}; do
  if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 2
done
if [[ "${ready}" != "1" ]]; then
  echo "Postgres did not become ready in time" >&2
  exit 1
fi

baseline=$(docker compose exec -T postgres psql -U postgres -d bench -t -A -c "SELECT count(*) FROM pgbench_history;" 2>/dev/null || echo "0")
echo "Baseline transactions: ${baseline}"

docker kill -s KILL "$(docker compose ps -q postgres)"

echo "Waiting for Postgres to restart..."
docker compose up -d postgres >/dev/null 2>&1
ready=0
for i in {1..30}; do
  sleep 2
  if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
    ready=1
    break
  fi
done
if [[ "${ready}" != "1" ]]; then
  echo "Postgres did not restart" >&2
  exit 1
fi

after=$(docker compose exec -T postgres psql -U postgres -d bench -t -A -c "SELECT count(*) FROM pgbench_history;" 2>/dev/null || echo "0")
echo "Post-restart transactions: ${after}"
if (( after < baseline )); then
  echo "Transaction count decreased after kill -9" >&2
  exit 1
fi

curl -sf http://localhost:9911/metrics | grep diffkeeper_recovery_total
echo "CI smoke test: PASS"
