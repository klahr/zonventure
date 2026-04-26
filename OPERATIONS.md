# Operations runbook

Day-2 ops for `zv`. Self-host friendly: every recipe assumes a vanilla
Postgres 14+ install and the `psql` / `pg_dump` client tools. No
managed-snapshot dependency.

## Backups

`zv -backup` streams a logical dump of the configured database to
stdout. The dump is plain SQL with `--clean --if-exists --no-owner
--no-privileges`, so it can be replayed into a different Postgres role
without ownership conflicts (e.g. local-dev → staging, or shared-host →
self-host migration).

```sh
# One-shot dump to a timestamped file.
zv -dsn "$DATABASE_URL" -backup > zv-$(date +%Y%m%d-%H%M).sql

# Compressed.
zv -backup | gzip > zv-$(date +%Y%m%d-%H%M).sql.gz
```

Schedule via cron, systemd timer, or your platform's scheduler. A daily
job is sufficient for most operators; bump to hourly if the run cadence
is heavy. Keep at least one weekly snapshot offsite.

The DSN is read from `-dsn` or `$DATABASE_URL` (same as the server).
`pg_dump` must be on `$PATH`; on a self-host where the app runs
without client tools you can run the dump from any other machine that
can reach the database.

## Restore

Pipe a dump back in via `zv -restore`:

```sh
zv -dsn "$DATABASE_URL" -restore < zv-20260426-0300.sql

# Compressed dump.
gunzip -c zv-20260426-0300.sql.gz | zv -restore
```

`psql` is invoked with `ON_ERROR_STOP=1`, so any failed statement
aborts the whole replay rather than leaving a half-restored DB.

The target database must exist before restore. The dump's `--clean
--if-exists` prefix drops existing zv objects on the way in, so
restoring on top of a populated DB is supported (and is the expected
shape for "roll back to last night's snapshot"). To restore into a
fresh DB, create it first:

```sh
createdb zv && zv -dsn "postgres://zv@/zv" -restore < dump.sql
```

## Health probe

`GET /api/health` pings the database; returns `200` with `{"status":"ok"}`
on success and `503` with `{"status":"db unavailable"}` if the connection
is down. Wire this into your load balancer or `docker-compose
healthcheck`.

## Sessions

Emergency revocation: `zv -revoke-sessions all` drops every staff +
slot session, forcing every user to re-authenticate. Scope to one user
with `zv -revoke-sessions <username>`.

## Metrics

`/metrics` is **off by default** — set `ZV_METRICS_ADDR` to a listen
address to expose it on a separate listener. The metrics port serves
nothing else, so a misconfigured proxy can't accidentally surface
admin endpoints from it.

```sh
# Bind metrics to localhost only — scrape from a sidecar exporter or
# a node-local Prometheus.
ZV_METRICS_ADDR=127.0.0.1:9100 zv

# Bind on all interfaces — only do this on a closed network or behind
# a network policy that limits who can reach it.
ZV_METRICS_ADDR=:9100 zv
```

Collectors emitted today:

- `zv_http_requests_total{method, status_class, route}` — every HTTP
  response, status bucketed as `2xx`/`3xx`/`4xx`/`5xx`/`other`.
- `zv_http_request_duration_seconds_bucket{method, route}` — request
  latency histogram with sane web-app buckets.
- `zv_auth_logins_total{result}` — login attempts (`success`/`fail`).
- `zv_purchase_events_total{event}` — purchase lifecycle events:
  `created`, `confirmed_simulated`, `confirmed_stripe`, `refunded`.

Routes are templated (e.g. `/api/admin/events/{eventID}`) so
cardinality stays bounded. Unmatched URLs fall under the `unmatched`
label.

## Image upload size

Per-upload cap defaults to 5 MiB and applies to puzzle images, team
avatars, and adventure cover images. Override with `ZV_MAX_IMAGE_BYTES`
(integer, bytes). Lower it on a small self-host box; raise it only if
you have plenty of database storage — image bytes live inline in
`adventure_page_images.content` rather than in object storage.

```sh
ZV_MAX_IMAGE_BYTES=$((2*1024*1024)) zv   # 2 MiB cap
```

## Logging

Default is line-oriented text on stderr (developer friendly, easy to
grep). Set `ZV_LOG_FORMAT=json` for one JSON object per line — the
shape every common log aggregator (Loki, CloudWatch, Datadog) reads
directly.

```sh
ZV_LOG_FORMAT=json zv 2>&1 | tee zv.log
```

The admin UI's live log stream uses its own JSON envelope and is
unaffected.

## Retention

The retention sweeper prunes location pings, answer attempts, and old
`auth_events` on a configurable cadence. Defaults: 30 days for
location/answer data, 90 days for audit events. Tune via
`ZV_LOCATION_RETENTION_DAYS` / `ZV_AUDIT_RETENTION_DAYS` (or the
matching `-retention-*` flags). Set to `0` to disable.
