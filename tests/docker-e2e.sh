#!/usr/bin/env bash
# End-to-end check that the built container image works: brings up
# docker-compose, waits for the healthcheck, round-trips a real message from
# outside the container, then tears everything down.
set -euo pipefail

cd "$(dirname "$0")/.."

ADDR="${KNET_E2E_ADDR:-localhost:9000}"
SERVICE="knet-stress"

cleanup() {
  echo "==> docker compose down"
  docker compose down -v
}
trap cleanup EXIT

echo "==> docker compose up -d --build"
docker compose up -d --build

echo "==> waiting for $SERVICE to be healthy"
for i in $(seq 1 30); do
  status="$(docker compose ps --format '{{.Health}}' "$SERVICE" 2>/dev/null || true)"
  if [ "$status" = "healthy" ]; then
    echo "healthy after ${i}s"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "container did not become healthy in time" >&2
    docker compose logs "$SERVICE" >&2
    exit 1
  fi
  sleep 1
done

echo "==> running echo round-trip against $ADDR from the host"
(cd tests/stress && KNET_STRESS_ADDR="$ADDR" go test -tags docker -run TestDockerStressThroughput -timeout 1m -v ./...)

echo "==> e2e OK"
