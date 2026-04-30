# Self-hosting Zonventure

Zonventure self-hosts in **one command** with Docker Compose. The
repo ships a `Dockerfile` and a `compose.yaml` that bring up Postgres
and the app together, run schema migrations, and seed an initial
admin so you can log in immediately.

```sh
git clone https://git.sr.ht/~klahr/zv && cd zv
docker compose up -d
```

That's it. The app is now serving on <http://127.0.0.1:8080>. Log in
at `/arrange` with **`admin` / `admin`**, change the password
immediately, and start authoring.

Zonventure is licensed under the **GNU AGPL v3.0 or later**
([`LICENSE`](LICENSE)). Self-hosting and modifying are explicitly
fine; if you run a *modified* version as a network service, the AGPL
requires you to offer your users the corresponding source. Running
the unmodified upstream as a service for your own organisation is
unaffected.

---

## Prerequisites

- **Docker 24+** with the Compose plugin (`docker compose version`).
- A **public hostname with TLS** before you put real users on it —
  Zonventure expects to run behind a reverse proxy (Caddy, nginx,
  Traefik) that terminates TLS.

That's the entire dependency list. No Go toolchain, no Postgres
client, no system packages — the bundled image is `distroless/static`
and the bundled database is `postgres:16-alpine`.

## What `docker compose up -d` does

1. Pulls `postgres:16-alpine` and starts it on a private network with
   a persistent volume (`zv_pg_data`).
2. Builds the app image from the local `Dockerfile`
   (~30 s on first run; cached afterwards).
3. Starts the app once Postgres reports healthy.
4. Runs every embedded migration on startup (idempotent).
5. If `ZV_SEED_ADMIN` is set (default: `admin:admin`) **and** no
   admin user exists yet, creates one. After that this becomes a
   no-op so the env var is safe to leave in place.
6. Binds `127.0.0.1:8080` so a same-host reverse proxy can take it
   from there.

Subsequent `docker compose up -d` runs are diff-only — the data
volume is preserved and the seeded admin is never overwritten.

## Configuration

Everything is environment variables. Defaults live in `compose.yaml`;
override them with a sibling `.env` file.

```sh
cp .env.example .env
$EDITOR .env
docker compose up -d
```

The full annotated list lives in [`.env.example`](.env.example). The
short tour:

| Var | What | Default |
| --- | --- | --- |
| `ZV_SEED_ADMIN` | First-run admin (`user:pass`); ignored once an admin exists | `admin:admin` |
| `POSTGRES_PASSWORD` | Bundled Postgres password — **change before prod** | `zv` |
| `ZV_PUBLIC_URL` | Origin used in outbound emails (set to your real https URL) | `http://localhost:8080` |
| `ZV_BIND_ADDR` | Host interface to bind on; `0.0.0.0` to expose directly | `127.0.0.1` |
| `ZV_PORT` | Host port for the app | `8080` |
| `ZV_LOG_FORMAT` | `text` (dev) or `json` (aggregator) | `text` |
| `ZV_SMTP_*` | SMTP relay for verification + reset mail (optional) | unset |
| `ZV_COMPANY_NAME` / `ZV_CONTACT_EMAIL` | Surfaced on `/privacy` + `/terms` | unset |
| `ZV_DPO_EMAIL` / `ZV_JURISDICTION` | Data-protection contact + governing law on `/privacy` + `/terms` | unset (DPO falls back to contact) |
| `ZV_HOSTING_PROVIDER` / `ZV_EMAIL_PROVIDER` | Sub-processor names listed in `/privacy` | unset (page shows `[set ZV_…]`) |
| `ZV_LEGAL_REVIEWED` | Set to `1` after a lawyer signs off — strips the yellow "draft template" banners | unset (banners visible) |
| `ZV_LEGAL_LAST_UPDATED` | "Last updated" date (YYYY-MM-DD) on the legal pages | today |
| `ZV_STRIPE_SECRET_KEY` / `ZV_STRIPE_WEBHOOK_SECRET` | Real payments + Customer Portal (optional) | unset |
| `ZV_TOTP_KEY` | 32-byte base64 key wrapping each user's TOTP secret at rest. Set to enable opt-in 2FA. **Treat like a database password** — losing it forces every enrolled user to re-enrol | unset |
| `ZV_LOCATION_RETENTION_DAYS` / `ZV_AUDIT_RETENTION_DAYS` | Privacy sweepers; `0` to disable | `30` / `90` |
| `ZV_DB_MAX_OPEN_CONNS` / `ZV_DB_MAX_IDLE_CONNS` | Postgres pool sizing; bump for managed clusters with high `max_connections` | `25` / `5` |
| `ZV_WEB_OVERRIDE_DIR` | Path to a directory of HTML / JSON files that shadow the embedded marketing pages — see [Custom landing page](#custom-landing-page) | unset (use embedded) |

After editing `.env`:

```sh
docker compose up -d        # pick up the new env on running containers
```

## Pre-flight checklist before going public

The defaults are tuned for "first boot works". Before you point real
users at the box:

1. **Change the admin password.** Log in as `admin` / `admin`, change
   it under your account menu. Then either drop `ZV_SEED_ADMIN` from
   `.env` or set it to your real credentials — either way it becomes
   a no-op once any admin exists.
2. **Change `POSTGRES_PASSWORD`.** It's used internally between the
   two containers, but a stray `docker compose port db 5432` makes it
   reachable. Pick something long and unique.
3. **Set `ZV_PUBLIC_URL`** to your real `https://…` origin — it goes
   into outbound email links.
4. **Wire `ZV_SMTP_*`** so signup verification + forgot-password
   actually leave the box. Without it, mail lands in
   `docker compose logs app`. Postmark, SendGrid, SES (SMTP
   interface), and Mailgun all work; anything that speaks SMTP does.
5. **Front the app with TLS.** Caddy is the lowest-friction option:

   ```caddy
   zv.example.com {
       reverse_proxy 127.0.0.1:8080
   }
   ```

   nginx works too — full snippet at the bottom of this doc.
6. **Set `ZV_LOG_FORMAT=json`** if you ship logs to an aggregator.
7. **Schedule a backup.** One line on the host:

   ```sh
   docker compose exec -T app /server -backup | gzip > /backups/zv-$(date +%F).sql.gz
   ```

   Drop that into cron or a systemd timer. Off-host the result.
8. **Optionally bind `ZV_METRICS_ADDR=127.0.0.1:9100`** if you scrape
   with Prometheus from a sidecar.

## Day-to-day commands

```sh
# Bring the stack up / down. `down` keeps the data volume.
docker compose up -d
docker compose down

# Tail logs.
docker compose logs -f --tail=200

# Drop into psql against the bundled DB.
docker compose exec db psql -U zv -d zv

# Run any zv subcommand on the running app container.
docker compose exec -T app /server -seed-demo
docker compose exec -T app /server -revoke-sessions all
```

The app binary inside the image is `/server` — pass any of the flags
documented in `zv -h` (the binary takes the same flags as the native
build).

## Custom landing page

The bundled `/` is brand-neutral by design — a slim hero, three
feature blurbs, a closing CTA. Operators who want a richer marketing
surface (use-case cards, testimonials, custom photos, multi-segment
positioning) point `ZV_WEB_OVERRIDE_DIR` at a directory of override
files; matching filenames there shadow the embedded defaults at
request time, no rebuild required.

The marketing landing used on the hosted Zonventure service lives
in a sibling repo, [`zv-landing`](../zv-landing) — clone it
alongside this checkout and point `ZV_WEB_OVERRIDE_DIR` at it. Use
it as-is, fork it for your own deployment, or roll your own from
scratch.

### Layout

```
/etc/zonventure/web-override/
├── landing.html              # full template — placeholders intact
├── pricing.html              # optional, same {{KEY}} grammar
├── privacy.html              # optional
├── terms.html                # optional
├── manual.html               # optional
├── 404.html                  # optional
├── sitemap.xml               # optional, may use {{HOST}}
├── robots.txt                # optional
└── landing-locales.json      # optional, per-field locale overrides
```

Filenames mirror the user-facing routes — drop `landing.html` to
shadow `/`, drop `pricing.html` to shadow `/pricing`, and so on. Any
file you don't supply falls back to the embedded default.

### Per-field locale overrides

`landing-locales.json` lets you change individual strings without
shipping a custom `landing.html`. Field names match the
`landingStrings` struct in `internal/server/landing.go`:

```json
{
  "en": {
    "HeroHeading": "Real-world quests for real teams",
    "HeroLede": "…"
  },
  "sv": {
    "HeroHeading": "…"
  }
}
```

Missing fields keep the embedded default; unknown languages or struct
fields log a warning and are skipped (a typo doesn't take the service
down).

### Wiring it up

The repo ships a ready-to-use Compose extension. With `zv-landing`
cloned next to `zv`:

```sh
cp compose.override.example.yaml compose.override.yaml
docker compose up -d
```

Compose auto-merges `compose.override.yaml` on top of `compose.yaml`,
adding `ZV_WEB_OVERRIDE_DIR` to the `app` env and mounting
`../zv-landing` read-only at `/var/zv/web-override`. Edit
`compose.override.yaml` (not the example) if your override dir lives
elsewhere — `git pull` leaves the example in place but won't
overwrite the activated copy.

Overrides are loaded at startup and cached for the process lifetime
— restart the container after editing files in the override dir.

## Upgrades

Take a backup first (see the `-backup` recipe above), then:

```sh
make upgrade
```

That fetches release tags, checks out the newest one, and rebuilds the
`app` image at that commit so `/api/version` stamps a clean version
string. The data volume is untouched and embedded migrations run on
next start.

Equivalent without `make`:

```sh
git fetch --tags
git checkout "$(git tag --sort=-v:refname | head -1)"
docker compose up -d --build
```

## Reverse proxy (TLS termination)

The app speaks plain HTTP and reads `X-Forwarded-Proto: https` to
flip session cookies to `Secure` automatically. WebSockets need the
proxy to forward the `Upgrade` + `Connection` headers — required
for `/ws/play`, `/ws/arrange`, and `/ws/live`.

**Caddy** (recommended; auto-TLS via Let's Encrypt):

```caddy
zv.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

**nginx** (TLS terminating in nginx):

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

## Pointing at an external Postgres

If you already run a managed Postgres (RDS, Neon, Supabase, Crunchy,
…) and just want the app container, drop the `db` service entirely
and override `DATABASE_URL`:

```yaml
services:
  app:
    build: .
    restart: unless-stopped
    environment:
      DATABASE_URL: postgres://user:pass@db.example:5432/zv?sslmode=require
      ZV_SEED_ADMIN:    admin:hunter2
      ZV_PUBLIC_URL:    https://zv.example.com
      # … rest as in compose.yaml
    ports:
      - "127.0.0.1:8080:8080"
```

`sslmode=require` for any non-local link. Migrations still run on
the app's first connection.

## SMTP recipes

The four providers most operators land on (Amazon SES, Postmark,
SendGrid, Mailgun) all expose SMTP submission, so the bundled
`SMTPMailer` — selected automatically when `ZV_SMTP_HOST` is non-empty
— talks to any of them with no extra binary or build-tag flag. Pick
the recipe that matches your provider; everything else (verification
mail, password reset, change-email confirmation) just works.

`ZV_SMTP_FROM` has to be a verified sender on the provider's side
otherwise the first message hard-bounces; the rest of the variables
match the credentials the provider issues you.

### Amazon SES

```env
ZV_SMTP_HOST=email-smtp.us-east-1.amazonaws.com   # pick your SES region
ZV_SMTP_PORT=587
ZV_SMTP_USER=AKIA…SMTP                            # SMTP username from SES → SMTP Settings
ZV_SMTP_PASS=…                                    # paired SMTP password
ZV_SMTP_FROM=Zonventure <noreply@example.com>     # must be a verified identity
ZV_SMTP_TLS=starttls
```

Generate the SMTP credentials in the SES console under "SMTP
Settings"; they are *not* the same as your AWS access key. Stay
inside the sandbox until you've requested production access.

### Postmark

```env
ZV_SMTP_HOST=smtp.postmarkapp.com
ZV_SMTP_PORT=587
ZV_SMTP_USER=…   # server token (used as both username and password)
ZV_SMTP_PASS=…   # same server token
ZV_SMTP_FROM=Zonventure <noreply@example.com>
ZV_SMTP_TLS=starttls
```

Postmark uses the **same** server token for SMTP user and password.
Tag traffic to the right "Server" by issuing a dedicated token rather
than reusing one across deployments.

### SendGrid

```env
ZV_SMTP_HOST=smtp.sendgrid.net
ZV_SMTP_PORT=587
ZV_SMTP_USER=apikey                               # literal string, not your SG username
ZV_SMTP_PASS=SG.…                                 # API key with "Mail Send" scope
ZV_SMTP_FROM=Zonventure <noreply@example.com>     # must match a verified Sender Identity
ZV_SMTP_TLS=starttls
```

The username is the literal word `apikey`; the password is the API
key value. Verifying your sending domain (rather than a single
address) gets you DKIM and lower spam scores.

### Mailgun

```env
ZV_SMTP_HOST=smtp.mailgun.org                     # smtp.eu.mailgun.org for the EU region
ZV_SMTP_PORT=587
ZV_SMTP_USER=postmaster@mg.example.com            # your Mailgun sending domain
ZV_SMTP_PASS=…                                    # the SMTP password from the domain page
ZV_SMTP_FROM=Zonventure <noreply@mg.example.com>
ZV_SMTP_TLS=starttls
```

The default `mg.<your-domain>` subdomain is what Mailgun creates;
set up DKIM and SPF records in your DNS the same day you start
sending.

If you'd rather use a provider's HTTP API (per-message tracking IDs,
IAM-rotated credentials for SES, server-side templates) the
`mailer.Mailer` interface in `internal/mailer/` is the seam to plug
into — see [`TODO.md`](TODO.md) item 15.

## Troubleshooting

- **App restarts in a loop** — `docker compose logs app`. Most likely
  causes: DATABASE_URL points at an unreachable Postgres, or
  `ZV_SEED_ADMIN` is malformed (must be `username:password`).
- **WebSockets disconnect after ~60 s** — your reverse proxy is
  closing idle upgrades. Bump `proxy_read_timeout` (nginx) or your
  proxy's equivalent. Compose's bundled stack is fine.
- **Verification emails don't arrive** — without `ZV_SMTP_*` set,
  Zonventure prints them to stderr. `docker compose logs app | grep mail`.
- **"no postgres DSN"** — `DATABASE_URL` not propagated. Confirm with
  `docker compose exec app env | grep DATABASE_URL`.
- **Uploads fail at >5 MB** — raise `ZV_MAX_IMAGE_BYTES` (in bytes).

## Building from source (without Docker)

The Docker path is the supported one. If you'd rather run a native
binary directly on a host (e.g. for a systemd unit), the binary is a
single static Go executable with embedded migrations and assets:

```sh
go build -trimpath -ldflags="-s -w" -o zv ./cmd/server

# Bring your own Postgres 14+ and create a database.
createdb zv
export DATABASE_URL=postgres:///zv
export ZV_SEED_ADMIN=admin:hunter2

./zv -addr :8080
```

The same env vars listed above apply. Migrations run on startup;
`ZV_SEED_ADMIN` honours the same "create-if-missing" semantics as in
the Docker setup. Day-2 ops (backup / restore / metrics / retention)
are covered in [`OPERATIONS.md`](./OPERATIONS.md).

### systemd (sketch)

If you want to run under systemd, write your own unit — the binary is
a plain long-running process and doesn't need anything special. A
minimal sketch:

```ini
[Unit]
Description=Zonventure server
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=zv
Group=zv
ExecStart=/usr/local/bin/zv
EnvironmentFile=/etc/zv/zv.env
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Create a dedicated `zv` system user, drop your `DATABASE_URL` and
`ZV_*` secrets into a root-owned, group-readable `/etc/zv/zv.env`,
install the binary at `/usr/local/bin/zv`, then `systemctl
daemon-reload && systemctl enable --now zv`. Bind on
`127.0.0.1:8080` (the default) and front it with the Caddy or nginx
config from the section above.

## Going further

- **[OPERATIONS.md](OPERATIONS.md)** — backup, restore, metrics,
  logging format, retention sweepers.
- **[`/manual`](https://zonventure.com/manual)** — in-app arranger
  manual, also bundled offline at `internal/server/web/manual.html`.
