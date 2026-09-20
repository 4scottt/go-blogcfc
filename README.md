# go-blogcfc

A Go rewrite of [BlogCFC](https://github.com/teamcfadvance/BlogCFC5), the
ColdFusion blog by Raymond Camden (Apache-2.0). It keeps BlogCFC's
features, permalink grammar and admin vocabulary, and trades the CFML
application server for one static binary and a MariaDB database. It is
built to be run and compared against the original: the same walk, the same
workload, the same telemetry.

## Install

Go 1.27 or newer:

    go install github.com/4scottt/go-blogcfc/cmd/go-blogcfc@latest

Or from a clone: `go build ./cmd/go-blogcfc`. The binary has no runtime
dependencies beyond the database; templates, stylesheets and migrations
are embedded in it.

## Docker

The image is a distroless static base running as uid 65532, with a
`HEALTHCHECK` that calls the binary's own `healthcheck` subcommand (there
is no curl inside).

    docker build -f deploy/Dockerfile -t go-blogcfc:dev .
    docker compose -f deploy/compose.yaml --profile app up --build

`deploy/compose.yaml` also runs MariaDB alone (`up -d db`) for local
development, and `deploy/.env.example` lists every environment variable
the image reads.

## Running

    go-blogcfc serve        # migrate, seed, then listen on $PORT
    go-blogcfc migrate      # apply migrations and seeds, then exit
    go-blogcfc seed-admin    # create the admin user from $ADMIN_PASSWORD
    go-blogcfc healthcheck  # GET /health on 127.0.0.1, exit 0 when it answers 200

Mail is logged, never sent, unless `MAIL_MODE=smtp` is set explicitly:
`SMTP_HOST` and friends on their own configure a server the blog will not
use, so a demo host can carry real settings without a message ever
reaching an inbox.

`scripts/dev.sh` brings up the database and serves on
<http://localhost:8081>; the admin user is `admin` with the password in
`ADMIN_PASSWORD` (`admin` by default in dev). `scripts/test.sh` runs the
test suite against the same database. Logs are JSON lines on stdout and
`GET /health` answers `ok` without authentication.

## Requirements

Go 1.27+ to build, MariaDB 10.11 (or a MySQL-compatible server with
utf8mb4) to run, and Docker only for the container image and the tests'
database. Configuration is environment variables — `BLOG_BASE_URL`,
`SESSION_SECRET`, `DB_*`, `ADMIN_PASSWORD`, `DATA_DIR`, `MAIL_MODE`,
optional `SMTP_*` and the standard `OTEL_*` — with everything an operator
changes day to day kept in the blog's own settings table.
