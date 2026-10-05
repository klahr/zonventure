# Self-hosting Zonventure

Zonventure self-hosts in **one command** with Docker Compose. The
repo ships a `Dockerfile` and a `compose.yaml` that bring up Postgres
and the app together, run schema migrations, and seed an initial
admin so you can log in immediately.

```sh
git clone https://github.com/klahr/zonventure && cd zonventure
docker compose up -d
```

That's it. The app is now serving on <http://127.0.0.1:8080>. Log in
at `/arrange` with **`admin` / `admin`** and change the password
immediately. The admin is the operator: it manages organisations,
curates the catalog and watches the system, but authors nothing and
runs no events. See [First organisation](#first-organisation) for the
path to authoring.

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
| `ZV_HOME_ORG` | Slug of the organisation the site is: `/store` redirects to its page `/o/<slug>`, and the landing page lists its adventures | unset (`/store` not found) |
| `ZV_SITE_NAME` | Name the instance shows players and arrangers (titles, player header, emails, authenticator entry). Set your own brand so players know who runs the service | `Zonventure` |
| `ZV_BIND_ADDR` | Host interface to bind on; `0.0.0.0` to expose directly | `127.0.0.1` |
| `ZV_PORT` | Host port for the app | `8080` |
| `ZV_LOG_FORMAT` | `text` (dev) or `json` (aggregator) | `text` |
| `ZV_SMTP_*` | SMTP relay for verification + reset mail (optional) | unset |
| `ZV_COMPANY_NAME` / `ZV_CONTACT_EMAIL` | Surfaced on `/privacy` + `/terms`; the company name also names the publisher in the landing page's schema.org data | unset |
| `ZV_DPO_EMAIL` / `ZV_JURISDICTION` | Data-protection contact + governing law on `/privacy` + `/terms` | unset (DPO falls back to contact) |
| `ZV_HOSTING_PROVIDER` / `ZV_EMAIL_PROVIDER` | Sub-processor names listed in `/privacy` | unset (page shows `[set ZV_…]`) |
| `ZV_LEGAL_REVIEWED` | Set to `1` after a lawyer signs off — strips the yellow "draft template" banners | unset (banners visible) |
| `ZV_LEGAL_LAST_UPDATED` | "Last updated" date (YYYY-MM-DD) on the legal pages | today |
| `ZV_STRIPE_SECRET_KEY` / `ZV_STRIPE_WEBHOOK_SECRET` | Real payments + Customer Portal (optional) | unset |
| `ZV_PRICE_PER_PLAYER_CENTS` / `ZV_FREE_PLAYERS_PER_EVENT` | Arranger pricing (EUR): slots beyond the free band cost this much each; the free band is also the minimum event size. Shown on `/pricing` and in the manual | `500` / `15` |
| `ZV_TOTP_KEY` | 32-byte base64 key wrapping each user's TOTP secret at rest. Set to enable opt-in 2FA. **Treat like a database password** — losing it forces every enrolled user to re-enrol | unset |
| `ZV_OIDC_ENABLED` | `1` enables single sign-on (see [Single sign-on (Google Workspace)](#single-sign-on-google-workspace)) | unset |
| `ZV_OIDC_ISSUER` / `ZV_OIDC_CLIENT_ID` / `ZV_OIDC_CLIENT_SECRET` / `ZV_OIDC_HOSTED_DOMAIN` / `ZV_OIDC_REDIRECT_URL` | OIDC client config; required when SSO is enabled | unset |
| `ZV_OIDC_ORG_NAME` | Organisation that JIT-provisioned SSO users join. Defaults to `ZV_OIDC_HOSTED_DOMAIN` | unset |
| `ZV_LOCATION_RETENTION_DAYS` / `ZV_AUDIT_RETENTION_DAYS` | Privacy sweepers; `0` to disable | `30` / `90` |
| `ZV_PARTY_RETENTION_DAYS` | Days a children's party is kept after it ends before the sweeper deletes it whole; `0` to disable | `14` |
| `ZV_DB_MAX_OPEN_CONNS` / `ZV_DB_MAX_IDLE_CONNS` | Postgres pool sizing; bump for managed clusters with high `max_connections` | `25` / `5` |
| `ZV_WEB_OVERRIDE_DIR` | Path to a directory of HTML / JSON files that shadow the embedded pages; its `landing.html` and `pricing.html` turn on the public `/` and `/pricing` — see [Custom landing page](#custom-landing-page) | unset (use embedded) |
| `ZV_MCP_ENABLED` | `1` mounts the `POST /mcp` endpoint so users can connect Claude / other MCP clients to author adventures — see [MCP.md](MCP.md) | unset (off) |
| `ZV_OAUTH_REDIRECT_HOSTS` | Extra hosts (comma-separated) MCP clients may return to after signing in with OAuth. Claude's (`claude.ai`, `claude.com`) and loopback are always allowed. OAuth is on with `ZV_MCP_ENABLED` and a real `ZV_PUBLIC_URL` | unset |

After editing `.env`:

```sh
docker compose up -d        # pick up the new env on running containers
```

## First organisation

The platform admin can't author adventures or run events — the API
refuses them with 403. Authoring happens inside an organisation:

1. As admin, create an organisation under **Organizations** and add a
   user to it as org admin (a separate login — admins can't also be
   org members).
2. To offer adventures to other organisations, click **Not the
   catalog publisher** on that organisation's page to make it the
   **catalog publisher** (one per instance). Its org admins author the
   catalog.
3. On the publisher's page, **List in catalog** makes an adventure
   visible to every organisation; **Commission** shares it with one
   organisation without listing it. Event passes (**Assign
   adventure**) only accept catalog adventures.

Upgrading from a release where the admin authored the catalog:
migration `0065_catalog_publisher` moves every admin-owned adventure
and library puzzle into a new **Zonventure Catalog** org flagged as
publisher. Add an org admin to it to keep editing them. The
publisher's org bundle export now contains the catalog.

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
7. **Schedule a backup.** The app image has no `pg_dump`, so dump from
   the `db` container, with the same flags as `zv -backup`:

   ```sh
   docker compose exec -T db pg_dump -U zv -d zv --clean --if-exists --no-owner --no-privileges \
     | gzip > /backups/zv-$(date +%F).sql.gz
   ```

   Drop that into cron or a systemd timer. Off-host the result, and
   restore as in [OPERATIONS.md → Restore](OPERATIONS.md#restore).
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

By default `/` sends visitors who aren't logged in straight to the
login screen, and `/pricing` is a 404. To put a public marketing page
in front, point `ZV_WEB_OVERRIDE_DIR` at a directory of override files
that includes `landing.html` (and `pricing.html` for `/pricing`).
Other matching filenames there shadow the embedded defaults at
request time, no rebuild required.

The embedded `internal/server/web/landing.html` and `pricing.html`
are never served; they are reference templates to copy, showing every
`{{KEY}}` placeholder the server fills in.

The marketing landing used on the hosted Zonventure service lives
in a sibling repo, [`zonventure-web`](https://github.com/klahr/zonventure-web)
— clone it alongside this checkout and point `ZV_WEB_OVERRIDE_DIR` at
its `dist/` directory. Use
it as-is, fork it for your own deployment, or roll your own from
scratch.

### Layout

```
/etc/zonventure/web-override/
├── landing.html              # optional, enables / — placeholders intact
├── pricing.html              # optional, enables /pricing, same {{KEY}} grammar
├── privacy.html              # optional
├── terms.html                # optional
├── manual.html               # optional
├── 404.html                  # optional
├── sitemap.xml               # optional, may use {{HOST}}
├── robots.txt                # optional
├── og-image.png              # optional, 1200x630 link-preview image
├── landing-locales.json      # optional, per-field locale overrides
├── <name>.html               # optional extra page, served at /<name>
└── shop/                     # optional static site, served as is at /shop/
```

Filenames mirror the user-facing routes — drop `landing.html` to
serve `/`, drop `pricing.html` to serve `/pricing`, and so on. Any
other file you don't supply falls back to the embedded default.

Any other `<name>.html` (lowercase letters, digits and dashes) is an
extra page served at `/<name>`, with the same topbar and footer
placeholders as `/manual` (`{{LANG_CODE}}`, `{{NAV_*}}`, …). App routes
always win: a file named after one (`play.html`, `arrange.html`) is
never served, and zv logs a warning at startup. The embedded template
names above are reserved for their own routes.

`shop/` is the exception to fixed filenames: zv serves everything in
it at `/shop/`, mapping an extensionless path to its `.html` sibling
(`/shop/book` serves `shop/book.html`) and unknown paths to
`shop/404.html`. Without a `shop/`, `/shop/` is an ordinary 404.

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

The repo ships a ready-to-use Compose extension. With `zonventure-web`
cloned next to this checkout:

```sh
cp compose.override.example.yaml compose.override.yaml
docker compose up -d
```

Compose auto-merges `compose.override.yaml` on top of `compose.yaml`,
adding `ZV_WEB_OVERRIDE_DIR` to the `app` env and mounting `../zonventure-web/dist` read-only at
`/var/zv/web-override`. Edit
`compose.override.yaml` (not the example) if your override dir lives
elsewhere — `git pull` leaves the example in place but won't
overwrite the activated copy.

Overrides are loaded at startup and cached for the process lifetime
— restart the container after editing files in the override dir.

## Upgrades

Take a backup first (see the backup recipe in the pre-flight checklist), then:

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

### Prebuilt images (no build on the host)

CI publishes `ghcr.io/klahr/zonventure:X.Y.Z` and `:latest` for every
release tag. To run those instead of building, enable `compose.prod.yaml`
in `.env` and log the host in to ghcr.io once (the package is private:
use a classic GitHub token with only `read:packages`):

```sh
echo "COMPOSE_FILE=compose.yaml:compose.override.yaml:compose.prod.yaml" >> .env
docker login ghcr.io -u <github-user> --password-stdin
```

Leave `compose.override.yaml` out of that list if you don't use one.
`make upgrade` then pulls the tag's image instead of building it, so a
1 vCPU / 2 GB host is enough.

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

## Single sign-on (Google Workspace)

For corporate deployments that want to retire local passwords and
have IT manage arranger access alongside the rest of their Google
Workspace, Zonventure speaks OpenID Connect with Google as the
identity provider. One workspace per Zonventure instance — when SSO
is enabled, this instance only accepts arrangers from the configured
`ZV_OIDC_HOSTED_DOMAIN` via SSO. (Non-domain arrangers — e.g. an
external collaborator invited by email — keep the password flow.)

### One-time Google Cloud Console setup

1. Open `https://console.cloud.google.com/` signed in with a
   Workspace admin account.
2. Pick or create a project for "Zonventure SSO".
3. Configure the OAuth consent screen → **Internal** (only members of
   your Workspace can sign in).
4. Credentials → Create credentials → OAuth client ID → Web application.
5. Authorised redirect URI: `https://<your-zv-host>/auth/oidc/callback`.
   No trailing slash. Must exactly match `ZV_OIDC_REDIRECT_URL`.
6. Save. Copy the **Client ID** and **Client secret** — you'll set
   them in `.env` next.

### Zonventure configuration

Add the following to `.env` and `docker compose up -d`:

```sh
ZV_OIDC_ENABLED=1
ZV_OIDC_ISSUER=https://accounts.google.com
ZV_OIDC_CLIENT_ID=<from Google Cloud Console>
ZV_OIDC_CLIENT_SECRET=<from Google Cloud Console>
ZV_OIDC_HOSTED_DOMAIN=example.com           # your Workspace primary domain
ZV_OIDC_REDIRECT_URL=https://zv.example.com/auth/oidc/callback
ZV_OIDC_ORG_NAME=Acme Corp                  # optional; defaults to the hosted domain
```

The app validates the config at boot. Missing or malformed values
abort startup with a clear error — no half-broken sign-in surface.

### What changes for users

- The login screen renders a **"Sign in with Google"** button above
  the username/password fields.
- Arrangers whose email is in the configured Workspace domain **must**
  use SSO. If they type their email into the password field, the
  server returns a 409 and the front-end bounces them to the IdP.
- The first arranger to sign in via SSO becomes `org_admin` of the
  configured org (auto-created on first use). Subsequent SSO
  arrangers join the same org as `org_member`.
- TOTP is **skipped** after a successful SSO sign-in — Google
  Workspace already enforces 2FA at the IdP, and adding a second
  prompt locally is friction without a security benefit.
- Audit logs record `auth_method='oidc'` (or `oidc_idp_error`,
  `oidc_state_mismatch`, etc.) so the admin Audit tab shows whether a
  session came in via SSO or via a local password.

### Verifying the flow

1. Open an incognito window at `https://<your-zv-host>/arrange`.
2. Click **Sign in with Google**.
3. Pick a Workspace account in the configured domain.
4. You should land on `/arrange` already signed in. The Audit tab
   shows the new `login` row with reason `oidc_success`.
5. As a negative check, try signing in from a Google account
   *outside* the Workspace — Google's account picker should refuse
   it; if you bypass that, the Zonventure callback rejects on the
   `hd` claim mismatch and lands on `/arrange#sso-error=domain_not_allowed`.

### Troubleshooting

- **`ZV_OIDC_*: oidc: ZV_OIDC_ENABLED=1 but missing …` at boot** —
  fill in the listed env vars; they're required only when SSO is on.
- **Discovery fails at boot** — the host can't reach
  `https://accounts.google.com/.well-known/openid-configuration`. Check
  egress firewall / proxy.
- **`/arrange#sso-error=state_mismatch`** — the user's browser sent a
  different / stale state cookie back. Usually a browser-extension
  cookie wipe, or two tabs racing through sign-in. Have the user
  retry in a single tab.
- **`/arrange#sso-error=domain_not_allowed`** — the IdP returned an
  `hd` claim that doesn't match `ZV_OIDC_HOSTED_DOMAIN`. Confirm both
  the env value and that the user is in the right Workspace.

### Disabling SSO

Set `ZV_OIDC_ENABLED=0` (or unset) and restart. The SSO endpoints
404 and the sign-in button disappears. Existing user rows stay
linked (the `oidc_provider`/`oidc_subject` columns are preserved);
re-enabling SSO later picks up where it left off.

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

## Migrating to another instance

To move an org from one instance to another — for example from a
small self-host box to a bigger deployment, or to a hosted provider
such as [zonventure.com](https://zonventure.com) — the migration
ships in three pieces:

1. **Org-bundle export.** A portable zip of your org's data —
   adventures, events, teams, player slots, ticket purchases.
2. **ID-rebase import.** The new instance mints a fresh org and
   replays the bundle with new primary keys, but every URL-bearing
   token (`/team/{token}`, `/play?t=…`, `/live/{live_token}`) is
   carried over byte-for-byte.
3. **Redirect bridge.** The old instance flips into a
   301-only forwarder so printed QR codes, emailed magic links,
   and existing bookmarks keep working through the cutover window.

### GUI cutover (recommended)

1. Sign in as a global admin on the old instance and open
   the **Migrate** page in the admin nav.
2. Click **Export bundle** and save the resulting
   `zonventure-org-<name>-<date>.zip`.
3. On the new instance, a global admin uploads the bundle via the
   **Import bundle** button on the same page.
4. If the new instance sells tickets through Stripe Connect, have
   the org admin complete Connect onboarding there (the bundle
   clears the old Stripe identifiers — you re-onboard against the
   new instance's platform account).
5. **Unhook the Stripe webhook** on your old Stripe dashboard so
   future events don't double-fire. The redirect-mode endpoint
   acknowledges any in-flight redeliveries with 200 OK so Stripe
   stops retrying — that's a backstop, not a substitute.
6. Back on the old instance, click **Switch to redirect mode**,
   paste the new instance's URL (e.g. `https://new.example.com`),
   and confirm. Every page on the old instance now 301s to the same
   path on the new one — no restart needed.
7. Leave the old instance running for the cutover window (90 days is
   a reasonable default — long enough that printed materials and
   in-flight emails resolve cleanly). Monitor `/api/health` to
   confirm the instance is alive but redirecting.
8. After the window, shut the old instance down. Existing tokens
   stay valid on the new instance.

### Headless cutover (CLI)

The same operations are available as CLI flags for scripted /
no-browser environments. Mirror of the `-backup` / `-restore`
shape:

```bash
# On the old instance: dump the org bundle.
zv -export-org=1 > zonventure-org-bundle.zip

# On the new instance: replay it.
zv -import-org < zonventure-org-bundle.zip

# Flip the old instance into redirect mode (this can also be set as
# ZV_REDIRECT_TARGET in the env file; the GUI toggle is disabled
# while the env pin is in effect).
ZV_REDIRECT_TARGET=https://new.example.com
```

### What carries over, what doesn't

Carried over:
- Adventures (manifest + images + audio + hints) with their slugs.
- Adventure assignments (capacity records).
- Events with `live_token`, scheduled / started / completed / cancelled
  timestamps, public-store fields, cover images.
- Teams with `token` and `recap_token` preserved verbatim.
- Player slots with `token` preserved verbatim.
- Ticket purchases as historical audit data — Stripe charge ids
  preserved, but the **refund button on the new instance shows a
  "legacy" notice for any pre-migration charge** since its Stripe
  account doesn't have those charges. Refund those out-of-band on
  your old Stripe dashboard.
- Org members (username, email, role). They re-set their password
  via the lost-link / forgot-password flow on first login.

Not carried over:
- Active sessions (`sessions`, `slot_sessions`) — players re-tap
  their preserved magic link, which mints a fresh session. Their
  in-app state (current puzzle, page index) is preserved.
- API tokens — users re-mint them on the new instance.
- Stripe Connect onboarding state — the org re-onboards on the
  new instance's Connect (different account, different
  identifiers).
- Out of scope for v1: per-puzzle event activity log (`event_activity`),
  per-team lane permutations (`team_lane_orderings` — in-flight quests
  with shuffled lanes will lose their per-team order), location pings,
  answer attempts, audit log slice.

## Going further

- **[OPERATIONS.md](OPERATIONS.md)** — backup, restore, metrics,
  logging format, retention sweepers.
- **[MCP.md](MCP.md)** — opt-in `POST /mcp` endpoint that lets
  arrangers connect Claude / other MCP clients and author
  adventures via tool calls. Off until `ZV_MCP_ENABLED=1`.
- **[`/manual`](https://zonventure.com/manual)** — in-app arranger
  manual, also bundled offline at `internal/server/web/manual.html`.
