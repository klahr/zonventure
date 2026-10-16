# Zonventure

Location-based adventure games for treasure hunts, team-building days,
school trips and city walks. You write the puzzles and place them on a
map; teams walk between real places and solve them on their phones while
you follow every team live.

Zonventure is a single Go server with a PostgreSQL database, shipped as
a Docker Compose stack. It is free to self-host under the AGPL. A
hosted version runs at [zonventure.com](https://zonventure.com) on the
same code.

## Features

- **No app for players.** Teams join from a link or QR code in any
  phone browser, with no account.
- **Map-based authoring.** Steps, zones, hints, branching routes and
  time limits, with a simulator and playtest mode before you publish.
- **Live event control.** Team positions and progress in real time;
  message, pause, unlock or end an event from the console.
- **Organisations.** Many organisations on one instance, each with its
  own adventures, users, events and look.
- **Ticket sales** through Stripe (optional), with a public store per
  organisation.
- **Accounts and security.** Email verification, optional TOTP two-factor
  authentication, Google Workspace single sign-on, login throttling.
- **AI authoring** through an optional MCP endpoint for Claude and other
  MCP clients.
- **Operations.** Prometheus metrics, JSON logs, health checks, signed
  outbound webhooks, data retention settings.
- English and Swedish.

## Requirements

- Docker 24 or later with the Compose plugin
- 1 vCPU and 2 GB RAM for a small instance
- For production: a domain name and a reverse proxy that terminates TLS
  (Caddy, nginx, Traefik)

## Quick start

```sh
git clone https://github.com/klahr/zonventure && cd zonventure
git checkout "$(git tag --sort=-v:refname | head -1)"
cp .env.example .env
docker compose up -d
```

This pulls the release image from ghcr.io, starts PostgreSQL and the
app, runs the database migrations and creates a first admin. Open <http://localhost:8080/arrange> and sign in
as `admin` / `admin`; you will be asked to choose a new password.

The admin runs the instance: organisations, users and the catalogue.
Adventures and events belong to an organisation, so create one, add a
user to it and sign in as that user to start authoring.

## Going to production

Configuration is environment variables in a `.env` file next to
`compose.yaml`; [`.env.example`](.env.example) lists them all. Before
real users arrive:

1. Change `POSTGRES_PASSWORD` and the admin password.
2. Set `ZV_PUBLIC_URL` to your public `https://` address.
3. Configure `ZV_SMTP_*` so verification and password-reset mail is sent.
4. Put a TLS-terminating reverse proxy in front of port 8080 and list
   it in `ZV_TRUSTED_PROXIES`.
5. Fill in Settings → Legal, or `/privacy` and `/terms` stay offline.
6. Set `ZV_TOTP_KEY` to enable two-factor sign-in.
7. Schedule backups of the database.

The full checklist, with examples for each step, is in
[docs/selfhosting.md](docs/selfhosting.md#pre-flight-checklist-before-going-public).

## Upgrading

Back up the database first; `make upgrade` neither takes nor checks a
backup:

```sh
docker compose exec -T db pg_dump -U zv -d zv --clean --if-exists --no-owner --no-privileges \
  | gzip > zv-$(date +%F).sql.gz
make upgrade
```

This checks out the newest release and pulls its image (or rebuilds
the app, if you build from source). Migrations run
on start and the data volume is kept. It runs plain `docker compose` in
this directory, so if you start the stack with `-p` or `-f`, follow the
manual steps in [docs/selfhosting.md](docs/selfhosting.md#upgrades)
instead; that section also covers running prebuilt images instead of
building on the host.

## Documentation

- [Self-hosting](docs/selfhosting.md): configuration, TLS, email,
  payments, single sign-on, external PostgreSQL, moving to another
  instance
- [Operations](docs/operations.md): backup and restore, metrics,
  logging, sessions, retention
- [MCP endpoint](docs/mcp.md): connecting Claude and other MCP clients
- The arranger manual ships with the app at `/manual`
- [Changelog](CHANGELOG.md)

## Licence

Zonventure is licensed under the
[GNU Affero General Public License v3.0 or later](LICENSE). You may run,
modify and redistribute it. If you offer a modified version as a
network service, you must make its source available to its users.

For a commercial licence, write to `hello@zonventure.com` with
`[licensing]` in the subject.

Copyright © 2026 Zonventure AB.

## Support

- Bugs and feature requests:
  [GitHub issues](https://github.com/klahr/zonventure/issues)
- Security vulnerabilities: see [SECURITY.md](SECURITY.md); please do
  not open a public issue
- Everything else: `hello@zonventure.com`

To work on Zonventure itself, see [docs/development.md](docs/development.md).
