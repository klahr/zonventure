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
| `ZV_STRIPE_SECRET_KEY` / `ZV_STRIPE_WEBHOOK_SECRET` | Real payments (optional) | unset |
| `ZV_LOCATION_RETENTION_DAYS` / `ZV_AUDIT_RETENTION_DAYS` | Privacy sweepers; `0` to disable | `30` / `90` |

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

## Upgrades

```sh
git pull
docker compose up -d --build
```

`--build` rebuilds the app image from the new source; the data
volume is untouched and embedded migrations run on the next start.
**Always take a backup first** — see the `-backup` recipe above.

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

## Going further

- **[OPERATIONS.md](OPERATIONS.md)** — backup, restore, metrics,
  logging format, retention sweepers.
- **[`/manual`](https://zonventure.com/manual)** — in-app arranger
  manual, also bundled offline at `internal/server/web/manual.html`.
