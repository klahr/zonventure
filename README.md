# Zonventure

**Adventures your players will remember.** Treasure hunts, team-builds,
school field trips, city walks: you author the puzzles, schedule an
event, and watch your players walk and solve from any phone.

A single Go binary plus Postgres, packaged as a one-command Docker
Compose stack. Self-host it for free, or use the hosted product at
the price of a coffee per team.

> The repository, Go module, and CLI binary are named `zv`. The
> platform is **Zonventure**; the arranger tool is **Zonventure
> Studio**. The legal entity behind both is **Zonventure AB**.

---

## What it is

- A platform for **arrangers** running location-based adventure games.
- Players join via a magic link — **no app**, no signup, just a phone
  browser.
- Live audience view shows team positions, current puzzle, and
  finishes in real time.
- Adventures are authored once and reused across events; an
  organisation can run many events in parallel.

## What it isn't

- A general-purpose ticketing or event-management tool.
- A consumer game — players don't have accounts; arrangers do.
- A walled garden. The hosted product runs the **same code** that's
  in this repo.

## Quick start

```sh
git clone https://github.com/klahr/zonventure && cd zonventure
docker compose up -d
```

That brings up Postgres + the app, runs migrations, and seeds an
initial admin. Open <http://localhost:8080/arrange>, log in as
`admin` / `admin`, change the password, and you're authoring.

Prefer a native binary? `go build -o zv ./cmd/server` still works —
see [SELFHOSTING.md](SELFHOSTING.md#building-from-source-without-docker).

## Documentation

- **[SELFHOSTING.md](SELFHOSTING.md)** — Docker-first self-host
  onramp: env vars, TLS / mailer / Stripe / external-Postgres
  recipes, upgrades. Native-binary build instructions live there too.
- **[OPERATIONS.md](OPERATIONS.md)** — day-2 ops: backup / restore,
  metrics, log format, retention.
- **[MCP.md](MCP.md)** — opt-in Model Context Protocol endpoint
  for connecting Claude / other LLM clients to author adventures.
- **[`/manual`](https://zonventure.com/manual)** — in-app arranger
  manual (also available offline at `internal/server/web/manual.html`).
- **[`/pricing`](https://zonventure.com/pricing)** — hosted-product
  tiers and FAQ.

## Project layout

```
cmd/server/        # entry point + flag plumbing + backup/restore CLI
internal/admin/    # arranger + admin HTTP handlers
internal/auth/     # login, signup, sessions, audit, rate-limits
internal/mcp/      # opt-in Model Context Protocol endpoint (POST /mcp)
internal/player/   # player-side HTTP + state machine
internal/public/   # landing + live audience pages
internal/server/   # router, embedded web assets, integration tests
internal/db/       # pgx driver + embedded migrations
internal/ws/       # WebSocket hub + connection wrapper
internal/logx/     # structured log with text + JSON renderers
internal/metrics/  # Prometheus collectors (off by default)
internal/mailer/   # SMTP + log-only mailers
```

## Status

In private-beta shape. Hosted launch waits on legal review of the
auto-rendered `/terms` and `/privacy` pages plus marketing screenshots;
self-host is usable today. See [`TODO.md`](TODO.md) for the full
production-readiness scorecard.

## Licence

Zonventure is licensed under the **GNU Affero General Public License
v3.0 or later** (`SPDX-License-Identifier: AGPL-3.0-or-later`). See
[`LICENSE`](LICENSE) for the full text.

Copyright © 2026 Zonventure AB.

The AGPL is a copyleft licence: you can self-host, modify, and
redistribute Zonventure freely, but if you run a modified version as
a network-accessible service, you must offer your users the
corresponding source. The hosted product at zonventure.com runs the
same code in this repository.

Commercial licensing is available on request for organisations whose
procurement policies preclude the AGPL — write to
`hello@zonventure.com` with `[licensing]` in the subject.

## Contributing

Patches, bug reports and translation contributions welcome via
[github.com/klahr/zonventure](https://github.com/klahr/zonventure). Please run
`go test ./...` against a real Postgres before sending a patch — the
DB-bound tests skip silently without one (`ZV_TEST_DSN`).

## Contact

`hello@zonventure.com` for product questions; security reports to the
same address with `[security]` in the subject.
