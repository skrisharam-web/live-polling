#!/usr/bin/env bash
# Start the local dependencies and both applications for development.
#
#   ./scripts/dev.sh
#
# Stops everything on Ctrl-C.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "==> Starting MongoDB and Redis"
docker compose up -d

echo "==> Waiting for dependencies to report healthy"
for _ in $(seq 1 30); do
  if docker compose ps --format '{{.Health}}' | grep -qv healthy; then sleep 2; else break; fi
done

if [ ! -f backend/.env ]; then
  echo "==> backend/.env not found, copying from backend/.env.example"
  cp backend/.env.example backend/.env
fi
if [ ! -f frontend/.env ]; then
  echo "==> frontend/.env not found, copying from frontend/.env.example"
  cp frontend/.env.example frontend/.env
fi

trap 'kill 0' EXIT

echo "==> Starting backend on :8080"
(cd backend && go run ./cmd/server) &

echo "==> Starting frontend on :5173"
(cd frontend && npm run dev) &

wait
