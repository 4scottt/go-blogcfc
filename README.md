# go-blogcfc

A Go rewrite of [BlogCFC](https://github.com/teamcfadvance/BlogCFC5), the
ColdFusion blog by Raymond Camden (Apache-2.0). It keeps BlogCFC's
features, permalink grammar and admin vocabulary, and trades the CFML
application server for one static binary and a MariaDB database. It is
built to be run *beside* the original rather than instead of it: the same
walk, the same workload and the same OpenTelemetry pipeline point at both,
so the difference between the 2011 application and the rewrite can be read
off measurements instead of argued. It is hosted by the oldbox platform
from a recipe card. What BlogCFC did and where this differs, feature by
feature, is `docs/NOTES.md`.

## Install

Go 1.27 or newer:

    go install github.com/4scottt/go-blogcfc/cmd/go-blogcfc@latest

Or from a clone: `go build ./cmd/go-blogcfc`. The binary has no runtime
dependencies beyond the database; templates, stylesheets, migrations, the
resource bundles and the time zone database are embedded in it.

## Docker

    docker build -f deploy/Dockerfile -t go-blogcfc:dev .
    docker compose -f deploy/compose.yaml --profile app up --build

The image contract, which the platform's card relies on:

- `gcr.io/distroless/static-debian12:nonroot` — no shell, no package
  manager, CA certificates and tzdata from the base
- runs as uid **65532**; `DATA_DIR` is created in the build stage with
  that owner so a named volume seeds writable
- `EXPOSE 8080`; `PORT` changes the port the process listens on
- `HEALTHCHECK CMD ["/go-blogcfc","healthcheck"]` — the binary checks
  itself, because there is no curl inside
- `VOLUME` is **not** declared: the card declares the `DATA_DIR` volume,
  which holds enclosures, uploaded images and slideshows
- durable state lives only in the database and `DATA_DIR`, so
  `docker rm -f` and a redeploy are safe
- multi-arch `linux/amd64` and `linux/arm64` images are built and pushed
  to `ghcr.io/4scottt/go-blogcfc` by `.github/workflows/image.yml`, tagged
  from the git tag, plus `edge` on `main`

`deploy/compose.yaml` also runs MariaDB alone (`up -d db`) for local
development, and `deploy/.env.example` lists every environment variable
the image reads.

## Running

    go-blogcfc serve        # migrate, seed, then listen on $PORT
    go-blogcfc migrate      # apply migrations and seeds, then exit
    go-blogcfc seed-admin   # create the admin user from $ADMIN_PASSWORD
    go-blogcfc healthcheck  # GET /health on 127.0.0.1, exit 0 when it answers 200

Everything is configured by environment; everything an operator changes
day to day lives in the blog's own settings table instead.

| Variable | Meaning |
|---|---|
| `PORT` | listen port, default 8080 |
| `BLOG_BASE_URL` | absolute base for every link, feed, mail and redirect. Never derived from the `Host` header |
| `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD` | MariaDB connection |
| `ADMIN_PASSWORD` | seeds the `admin` user on first start when no user exists; ignored afterwards |
| `SESSION_SECRET` | HMAC key for the session cookie |
| `DATA_DIR` | uploads root (enclosures, images, slideshows), default `/var/lib/go-blogcfc` |
| `MAIL_MODE` | `log` (the default) or `smtp` |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD` | the server used only when `MAIL_MODE=smtp` |
| `TZ` | IANA zone, default UTC |
| `LOG_LEVEL` | `debug` for a noisier run; the default is info |
| `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_METRIC_EXPORT_INTERVAL` | standard OpenTelemetry SDK variables |

**Mail is logged, never sent, unless `MAIL_MODE=smtp` is set explicitly.**
`SMTP_HOST` and friends on their own configure a server the blog will not
use, so a demo host can carry real settings without a message ever
reaching an inbox. A `MAIL_MODE=smtp` with no `SMTP_HOST` also falls back
to logging rather than failing a comment.

**Time.** Datetimes are stored in UTC. `TZ` sets the process zone; the
blog's `timezone` setting (an IANA name such as `America/Los_Angeles`)
decides the zone entries are displayed in, the zone a `posted` date typed
into the editor is read in, and the zone the permalink date segments and
the calendar's "today" are computed in.

**Telemetry.** With no `OTEL_EXPORTER_OTLP_ENDPOINT` the app creates no
exporter at all — telemetry off is a no-op, not a failed connection. Set
it (a base URL, e.g. `http://collector:4318`, with
`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`) and the app emits HTTP server
metrics under the **stable HTTP semantic conventions** — the histogram is
`http.server.request.duration`, in seconds, with `http.response.status_code`,
and attributes limited to route, method and status — plus Go runtime
metrics and traces. A collector that is not up yet is logged at debug and
never fatal. Logs are JSON lines on stdout in every mode, and
`GET /health` answers without authentication (503 when the database is
unreachable).

## Requirements

Go 1.27+ to build, MariaDB 10.11 (or a MySQL-compatible server with
utf8mb4) to run, and Docker only for the container image and the tests'
database.

## Development

    scripts/dev.sh          # MariaDB in Docker, then serve on :8081
    scripts/test.sh         # the whole suite, on its own database beside it

`scripts/dev.sh` serves <http://localhost:8081> with the admin user
`admin` and the password in `ADMIN_PASSWORD` (`admin` by default).
`scripts/test.sh` creates `goblogcfc_test` on the compose server (the
suite empties every table before each test, so it never shares the dev
blog's database) and exports a `TEST_DSN` pointing at it; set `TEST_DSN`
yourself to run `go test ./...` against another server.
Tests run against a real MariaDB rather than a substitute, so there is one
SQL dialect and no passes-here-fails-there. Golden files (feed XML,
sitemap, entry markup, the calendar) are refreshed with
`go test ./... -update`.

`scripts/walk.mjs` is the acceptance walk: a Playwright script that reads
the blog, signs in, creates a category and a released entry, finds the
entry on the home page and in the feed, comments on it through the popup
form, and checks phone width — failing on any console error, page error or
own-origin sub-resource that answers 400 or worse.

    npm install playwright@1
    WALK_URL=http://localhost:8081 ADMIN_PASSWORD=admin node scripts/walk.mjs

| Variable | Meaning |
|---|---|
| `WALK_URL` | the blog to walk, default `http://localhost:8081` |
| `ADMIN_PASSWORD` | the admin password to sign in with |
| `WALK_STEPS` | how many steps to run; 7 is all of them |
| `WALK_BROWSER` | `chrome` (the default) uses the installed Chrome channel |
| `WALK_GATE`, `WALK_GATE_USER` | HTTP basic credentials when the blog sits behind a gate |
| `PLAYWRIGHT_DIR` | where Playwright was installed, if not beside the script |

## Function points

`docs/function-points.md` is the test contract: every row is one thing
BlogCFC does that the rewrite must do too, with the BlogCFC source it was
read from and the kind of test that proves it. `TestFunctionPointsCovered`
reads that file and fails when any id has no test whose name contains it
(`TestFP_P01_…`), or, for the walk-only and CI-only rows, no `FP: <id>`
marker in `scripts/walk.mjs` or the workflows. A row is never deleted,
only marked dropped with a reason.

## XML-RPC

The MetaWeblog endpoint is `POST /xmlrpc`, which any desktop blog editor
that speaks MetaWeblog can post to. Twelve methods are implemented:
`metaWeblog.newPost`, `editPost`, `getPost`, `getRecentPosts`,
`getCategories`, `getUsersBlogs`, `newMediaObject`; `blogger.getUsersBlogs`,
`deletePost`; `mt.getCategoryList`, `getPostCategories`,
`setPostCategories`. Every call authenticates with a blog username and
password, and an unknown method or a bad password comes back as a real
XML-RPC fault. The `?parseMarkup=true` toggle behaves as BlogCFC's did.

## License

Apache-2.0 (`LICENSE`). `NOTICE` carries the attribution: go-blogcfc is a
rewrite of BlogCFC 5.9.8 by Raymond Camden, itself Apache-2.0, and names
the parts reproduced under that license — the resource bundles, the
comment spam term list, and the semantics of the schema, permalink
grammar, settings keys and RSS output. The Arclite theme BlogCFC shipped
is GPL: its look is **recreated here with original CSS**, and nothing of
the theme — no stylesheet, sprite or font — is copied.
