#!/usr/bin/env bash
# Bring up MariaDB and run the server against it.
#   scripts/dev.sh            # http://localhost:8081
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${PORT:-8081}"
COMPOSE="docker compose -f deploy/compose.yaml"

$COMPOSE up -d db

printf 'waiting for the database'
for _ in $(seq 1 60); do
  status="$($COMPOSE ps --format '{{.Health}}' db 2>/dev/null || true)"
  if [ "$status" = "healthy" ]; then echo; break; fi
  printf '.'
  sleep 1
done

export PORT
export BLOG_BASE_URL="${BLOG_BASE_URL:-http://localhost:$PORT}"
export DB_HOST=127.0.0.1
export DB_PORT=3307
export DB_NAME=goblogcfc
export DB_USER=goblogcfc
export DB_PASSWORD=goblogcfc
export ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin}"
export SESSION_SECRET="${SESSION_SECRET:-dev-only-not-secret}"
export DATA_DIR="${DATA_DIR:-./.data}"
mkdir -p "$DATA_DIR"

echo "serving on $BLOG_BASE_URL (admin password: $ADMIN_PASSWORD)"
exec go run ./cmd/go-blogcfc serve
