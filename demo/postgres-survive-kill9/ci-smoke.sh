#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

cleanup() {
  docker compose down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_pg() {
  echo "Waiting for Postgres to be ready..."
  local ready=0
  for _ in {1..120}; do
    if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 2
  done
  if [[ "$ready" != "1" ]]; then
    echo "Postgres did not become ready in time" >&2
    exit 1
  fi
}

wait_pgbench_tables() {
  echo "Waiting for pgbench tables..."
  for _ in {1..120}; do
    if docker compose exec -T postgres psql -U postgres -d bench -t -A -c "SELECT 1 FROM pg_tables WHERE tablename='pgbench_history';" 2>/dev/null | grep -q 1; then
      return
    fi
    sleep 2
  done
  echo "pgbench tables did not appear in time" >&2
  exit 1
}

wait_metrics() {
  echo "Waiting for metrics endpoint..."
  for _ in {1..180}; do
    # Prefer in-cluster check via loader; fallback to host port mapping if needed
    if docker compose exec -T loader curl -sf http://postgres:9911/metrics 2>/dev/null | grep -q diffkeeper_recovery_total; then
      return
    fi
    if curl -sf http://localhost:9911/metrics 2>/dev/null | grep -q diffkeeper_recovery_total; then
      return
    fi
    sleep 2
  done
  echo "Warning: metrics endpoint did not respond with diffkeeper_recovery_total; continuing" >&2
  return 1
}

docker compose up -d
wait_pg
wait_pgbench_tables
wait_metrics || true

baseline=$(docker compose exec -T postgres psql -U postgres -d bench -t -A -c "SELECT count(*) FROM pgbench_history;" 2>/dev/null || echo "0")
echo "Baseline transactions: ${baseline}"

get_count() {
  docker compose exec -T postgres psql -U postgres -d bench -t -A -c "SELECT count(*) FROM pgbench_history;" 2>/dev/null | tr -d '\r'
}

# Give the workload a moment to settle and flush durable state
sleep 2
docker compose exec -T postgres psql -U postgres -d bench -c "CHECKPOINT;" >/dev/null

# Re-read after checkpoint to ensure a stable baseline
baseline=$(get_count)
echo "Stable baseline transactions: ${baseline}"

docker compose kill -s KILL postgres

echo "Waiting for Postgres to restart..."
docker compose up -d --force-recreate postgres >/dev/null 2>&1
wait_pg
wait_pgbench_tables
wait_metrics || true

after=$(get_count)

# Allow a short window for replay to catch up before failing hard
for _ in {1..30}; do
  if (( after >= baseline )); then
    break
  fi
  sleep 2
  after=$(get_count)
done

echo "Post-restart transactions: ${after}"
if (( after < baseline )); then
  echo "Transaction count decreased after kill -9" >&2
  exit 1
fi

echo "CI smoke test: PASS"
