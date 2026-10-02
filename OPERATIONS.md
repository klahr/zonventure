# Operations runbook

Day-2 ops for Zonventure. Self-host friendly: every recipe assumes a
vanilla Postgres 14+ install and the `psql` / `pg_dump` client tools.
No managed-snapshot dependency.

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

`psql` is invoked with `ON_ERROR_STOP=1` in a single transaction, so
any failed statement rolls the whole replay back — including the
dump's `--clean` DROPs — rather than leaving a half-restored DB.

Stop the app first: its scheduler, retention and outbox workers keep
writing while the dump replays.

Under Docker Compose the app image has no `psql`; replay through the
`db` container instead, with the same flags:

```sh
docker compose stop app
gunzip -c zv-20260426-0300.sql.gz \
  | docker compose exec -T db psql -U zv -d zv --single-transaction -v ON_ERROR_STOP=1
docker compose start app
```

The target database must exist before restore. The dump's `--clean
--if-exists` prefix drops existing zv objects on the way in, so
restoring on top of a populated DB is supported (and is the expected
shape for "roll back to last night's snapshot"). To restore into a
fresh DB, create it first:

```sh
createdb zv && zv -dsn "postgres://zv@/zv" -restore < dump.sql
```

Restoring on top only works when the dump and the database are on the
same schema. A dump from an older release must go into an empty
database: `--clean` only drops the tables the dump knows about, and
tables added by newer migrations still reference them, so the replay
aborts. Restore first, then start the new release to migrate it. Keep
the same `ZV_TOTP_KEY` (see [Two-factor authentication](#two-factor-authentication-totp)).

## Health probe

`GET /api/health` pings the database; returns `200` with `{"status":"ok"}`
on success and `503` with `{"status":"db unavailable"}` if the connection
is down. Wire this into your load balancer or `docker-compose
healthcheck`.

## Sessions

Emergency revocation: `zv -revoke-sessions all` drops every staff +
slot session, forcing every user to re-authenticate. Scope to one user
with `zv -revoke-sessions <username>`.

## Login throttling

Failed-login state — the per-(ip, username) and per-ip lockout counters
that slow down credential-stuffing — lives **in process memory only**.
A restart (deploy, container recycle, OOM kill) wipes the counters and
gives a patient attacker a clean slate.

This is fine for the supported single-instance topology: lockouts of
the order of an hour matter much more than counters that survive a
weekly redeploy, and the in-memory implementation has zero dependencies
to fail.

If you run multiple replicas, or you want lockouts to survive restarts:

- Front the app with a reverse proxy (nginx, Caddy, Traefik) and use
  its rate-limit module against `/api/auth/login` and
  `/api/auth/forgot`. The proxy keeps state across app restarts and
  shares it across replicas natively.
- Or: pin the deployment to one replica and accept the restart wipe.

Either way, keep `ZV_PUBLIC_URL` set to your real origin so password
reset / verification mails carry working links — `X-Forwarded-For`
must reach the app for the per-IP bucket to identify the right caller
(see `auth.ClientIP` for the parsing rule: leftmost XFF entry wins).

## Two-factor authentication (TOTP)

Optional, opt-in per user. Requires a server-side secret to wrap each
user's TOTP secret at rest:

```sh
# Generate once and store like a database credential — losing this
# locks every enrolled user out and forces them to re-enrol.
ZV_TOTP_KEY=$(openssl rand -base64 32) zv
```

When unset, the enrolment surface is disabled (the Settings card shows
a placeholder explaining why) and the login flow stays password-only —
existing users keep working unchanged.

When set, the Settings → Two-factor authentication card lets each user
register an authenticator app (1Password, Bitwarden, Aegis, Google
Authenticator, …). Confirmed enrolment also generates ten single-use
recovery codes; the user must save them at that moment because the
plaintext is only displayed once.

A user who has lost both the device and the recovery codes can ask
an admin or org_admin in their org to factory-reset the second factor
via `POST /api/admin/users/{userID}/totp/disable` (requires the acting
admin's password in `X-Confirm-Password`). This is audit-loud: rows
in `auth_events` with `event=totp_admin_reset` carry both the actor
and the target.

Backups must include the value of `ZV_TOTP_KEY` alongside the database
dump: a restored database paired with a different key is equivalent to
losing the key entirely. Treat the key like a database credential —
keep it in the same secrets store, never in the repo, never in CI logs.

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

## Platform admin vs organisations

The global admin is the operator: organisations, admins, settings,
logs, migration, billing (refunds, invoices) — and nothing that
authors content or runs an event. `/api/admin` routes for adventures,
puzzles, events, teams, webhooks and the simulator answer the admin
with 403; so do the matching MCP tools.

Operator actions on an organisation's events live on its
**Organizations** page:

- **Grant slots** — `POST /api/admin/events/{id}/grant-slots`. Free for
  orgs without invoice billing; priced grants land on the next invoice.
- **Cancel event** — `POST /api/admin/organizations/{org}/events/{id}/cancel`
  with `X-Confirm-Password`. The event must belong to that org (404
  otherwise).

The catalog is authored by the one **catalog publisher** org
(`organizations.is_publisher`). The operator curates it:

- `PUT /api/admin/organizations/{org}/publisher` — flag / unflag
  (unflag needs `X-Confirm-Password`; 409 while another org publishes).
  The publisher can't be deleted while it publishes.
- `GET /api/admin/catalog` — the publisher's adventures with their
  shares.
- `PUT /api/admin/catalog/{slug}/published` — list / unlist.
- `POST|DELETE /api/admin/catalog/{slug}/shares[/{org}]` — commission
  an adventure for one org without listing it.
