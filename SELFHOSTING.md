# Self-hosting zv

zv ships as a single Go binary plus a Postgres database. There are no
external runtime services required — Stripe, an SMTP relay, and a
metrics scraper are all opt-in. A small VPS, a managed Postgres, and a
domain are enough to put it in front of users.

zv is licensed under the **GNU AGPL v3.0 or later** ([`LICENSE`](LICENSE)).
Self-hosting and modifying are explicitly fine; if you run a *modified*
version as a network service, the AGPL requires you to offer your
users the corresponding source. Running the unmodified upstream as a
service for your own organisation is unaffected.

---

## Prerequisites

- **Go 1.26 or newer** (build only — runtime needs only the binary).
- **Postgres 14+** reachable from the app process. Either install
  locally (`apt install postgresql`, `pacman -S postgresql`,
  `brew install postgresql@16`), use Docker, or point at a managed
  Postgres (RDS, Neon, Supabase, Crunchy, …).
- **`pg_dump` and `psql`** on the same host as the app if you plan to
  use `zv -backup` / `-restore` (most distros bundle them with the
  Postgres client package).
- A **public hostname with TLS** for production. zv runs HTTP behind a
  reverse proxy that terminates TLS — Caddy, nginx, or Traefik all work.

## Build

```sh
git clone https://git.sr.ht/~klahr/zv && cd zv
go build -o zv ./cmd/server
```

Or run straight from source while iterating:

```sh
go run ./cmd/server -dsn "$DATABASE_URL"
```

For a deploy artefact the repo ships a `Dockerfile` and `compose.yaml`
(uses the same env vars listed below). `make up` brings up Postgres +
the app together for local dev.

## Database

Create an empty database and a user. Migrations run automatically on
`zv` startup, so this is the only manual schema step.

```sh
sudo -u postgres psql <<'SQL'
CREATE USER zv WITH PASSWORD 'change-me';
CREATE DATABASE zv OWNER zv;
SQL
```

Connection URL format:

```
postgres://zv:change-me@db.example:5432/zv?sslmode=require
```

`sslmode=require` for any non-localhost link. Local socket auth (no
password) works too: `postgres:///zv?host=/var/run/postgresql`.

## Required environment

| Var | What | Example |
| --- | --- | --- |
| `DATABASE_URL` | Postgres DSN; passed to `pgx` | `postgres://zv:…@db/zv` |
| `ZV_PUBLIC_URL` | Origin used in outbound emails | `https://zv.example.com` |
| `ZV_COMPANY_NAME` | Your operating entity, surfaced on `/privacy` + `/terms` | `Acme Adventures Ltd.` |
| `ZV_CONTACT_EMAIL` | Public contact for legal pages | `hello@example.com` |

## First-run seeding

Create the first global admin (subsequent admins can be added through
the UI):

```sh
zv -seed-admin admin:hunter2
```

Optionally install the demo adventure + sample data:

```sh
zv -seed-demo
```

After that, start the server:

```sh
zv -addr :8080
```

Then point your reverse proxy at `:8080`, log in at
`https://zv.example.com/arrange` as `admin` / `hunter2`, change the
password, and create your first organisation.

## Mailer (optional but recommended)

Without an SMTP mailer configured, signup verification + forgot-
password links land in the server log instead of an inbox. Fine for
single-operator dev; not fine for end-users.

| Var | What |
| --- | --- |
| `ZV_SMTP_HOST` | SMTP server hostname |
| `ZV_SMTP_PORT` | Defaults to `587` |
| `ZV_SMTP_TLS` | `starttls` (default) / `tls` / `none` |
| `ZV_SMTP_USER` | Auth username |
| `ZV_SMTP_PASS` | Auth password |
| `ZV_SMTP_FROM` | `From:` address |

Postmark, SendGrid, SES (SMTP interface), Mailgun all work — anything
that speaks SMTP.

## Payments (optional)

Self-hosters who only run their own org probably don't need money
flowing through zv at all — the simulated checkout creates assignments
directly. Skip Stripe entirely.

If you want real charging:

| Var | What |
| --- | --- |
| `ZV_STRIPE_SECRET_KEY` | `sk_live_…` from Stripe dashboard |
| `ZV_STRIPE_WEBHOOK_SECRET` | `whsec_…` for `/api/webhooks/stripe` |

Both must be set for the live flow to activate. Either being empty
keeps the simulated provider as the only purchase path.

## Reverse proxy

Caddy is the lowest-friction option:

```caddy
zv.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

nginx with TLS terminating in nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name zv.example.com;
    ssl_certificate     /etc/letsencrypt/live/zv.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/zv.example.com/privkey.pem;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;

        # WebSocket upgrade — required for /ws/play, /ws/arrange, /ws/live.
        proxy_set_header   Upgrade           $http_upgrade;
        proxy_set_header   Connection        "upgrade";
        proxy_read_timeout 3600s;
    }
}
```

zv detects `X-Forwarded-Proto: https` and flips its session cookies to
`Secure` automatically — no extra flag required.

## Optional knobs

These are nice-to-haves with sensible defaults; you can ignore them
until you need them.

| Var | Default | What |
| --- | --- | --- |
| `ZV_LOG_FORMAT` | `text` | `json` for one-object-per-line stderr |
| `ZV_METRICS_ADDR` | unset | Bind a Prometheus `/metrics` listener on the given addr (off by default) |
| `ZV_MAX_IMAGE_BYTES` | `5242880` (5 MiB) | Per-upload size cap |
| `ZV_LOCATION_RETENTION_DAYS` | `30` | Auto-delete location pings older than this after the event ends; `0` disables |
| `ZV_AUDIT_RETENTION_DAYS` | `90` | Auto-delete `auth_events` older than this; `0` disables |
| `ZV_SIMULATOR` | unset | Set `1` to expose the race-simulator admin route (dev / demo only) |
| `ZV_DPO_EMAIL` | unset | Surfaced on `/privacy` for EU operators with a designated DPO |
| `ZV_JURISDICTION` | unset | Country / region surfaced on `/privacy` |
| `ZV_LEGAL_LAST_UPDATED` | unset | Date displayed on `/privacy` + `/terms` |

## Upgrades

```sh
git pull
go build -o zv ./cmd/server
sudo systemctl restart zv
```

Migrations are embedded in the binary and run on startup, so a fresh
build automatically rolls the schema forward. **Always take a backup
first** — `zv -backup > pre-upgrade-$(date +%F).sql` is one line and
saves the next day if something regresses.

## Day-2 ops

Backups, restore, metrics, log format, and retention sweepers are
documented in [`OPERATIONS.md`](./OPERATIONS.md).

## Troubleshooting

- **"no postgres DSN"** — set `-dsn` or `$DATABASE_URL`.
- **"password authentication failed"** — wrong creds or the DB user
  doesn't have `LOGIN`. Try `psql "$DATABASE_URL"` to reproduce
  outside zv.
- **WebSockets disconnect after ~60s** — your reverse proxy is
  closing idle upgrades. Bump `proxy_read_timeout` (nginx) or your
  proxy's equivalent.
- **Uploads fail at >5 MB** — raise `ZV_MAX_IMAGE_BYTES`.
- **Verification emails don't arrive** — without `ZV_SMTP_*` set, zv
  prints them to the log. Inspect with `journalctl -u zv` or your
  container log.

## Going to production

The minimum bar:

1. TLS in front of the app (Caddy / nginx / Traefik).
2. Mailer configured so signups can verify.
3. `ZV_PUBLIC_URL` set to the public origin.
4. `ZV_LOG_FORMAT=json` if you ship logs to an aggregator.
5. A daily `zv -backup` cron, output stored off-host.
6. `ZV_METRICS_ADDR=127.0.0.1:9100` if you scrape with Prometheus.

Once those are in place, it's a working SaaS for your own users.
