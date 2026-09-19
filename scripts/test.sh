#!/usr/bin/env bash
# Run the whole suite against a real MariaDB, as CI does.
set -euo pipefail

cd "$(dirname "$0")/.."

COMPOSE="docker compose -f deploy/compose.yaml"
$COMPOSE up -d db

printf 'waiting for the database'
for _ in $(seq 1 60); do
  status="$($COMPOSE ps --format '{{.Health}}' db 2>/dev/null || true)"
  if [ "$status" = "healthy" ]; then echo; break; fi
  printf '.'
  sleep 1
done

export TEST_DSN="${TEST_DSN:-goblogcfc:goblogcfc@tcp(127.0.0.1:3307)/goblogcfc?parseTime=true&loc=UTC&multiStatements=true}"
exec go test "$@" ./...
