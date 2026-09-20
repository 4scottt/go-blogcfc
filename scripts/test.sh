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

# The suite truncates every table before each test, so it gets a database
# of its own beside the dev blog's (the same server, the same user).
$COMPOSE exec -T db mariadb -uroot -pgoblogcfcroot -e \
  "CREATE DATABASE IF NOT EXISTS goblogcfc_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; GRANT ALL ON goblogcfc_test.* TO 'goblogcfc'@'%';" >/dev/null

export TEST_DSN="${TEST_DSN:-goblogcfc:goblogcfc@tcp(127.0.0.1:3307)/goblogcfc_test?parseTime=true&loc=UTC&multiStatements=true}"
exec go test "$@" ./...
