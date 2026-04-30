# Changelog

All notable changes to Zonventure will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The repository, Go module, and CLI binary are named `zv`; the product is
**Zonventure**. Versions tagged here correspond to git tags `v<version>` on
[git.sr.ht/~klahr/zv](https://git.sr.ht/~klahr/zv).

## [Unreleased]

## [0.1.4] - 2026-04-30

### Fixed

- **2FA login was broken.** The TOTP challenge form sent an empty
  `recovery_code` instead of the typed TOTP code: the DOM helper's
  `h({ hidden: '' })` coerced the empty string to `false` when assigning
  to the boolean `hidden` property, so the recovery-code input was
  visible from the start and the form's `hasAttribute('hidden')` check
  picked the recovery branch with an empty payload. Anyone who enrolled
  in 2FA in 0.1.0–0.1.3 was effectively locked out. The same latent bug
  affected the TOTP-enrolment QR panel and the email-change revert
  banner; both now hide their initial state correctly.
- **In-app upgrade instructions matched no real upgrade path.** The
  Settings → Software update card showed `docker compose pull && docker
  compose up -d`, neither of which actually upgrades the locally-built
  `app` image. The card and `SELFHOSTING.md` now show one recipe that
  fetches tags, checks out the latest release, and rebuilds with
  `docker compose up -d --build` — producing a clean version stamp in
  `/api/version`.

## [0.1.3] - 2026-04-30

### Fixed

- **Docker build no longer stamps every binary as `-dirty`.** The 0.1.2
  fix re-included `.git` in the build context, but `.dockerignore` still
  strips other tracked files (`Dockerfile`, `compose.yaml`, `.gitignore`,
  `TODO.md`, `.dockerignore`), and `git describe --dirty` saw them as
  deleted inside the container. The build now runs `git checkout -- .`
  to restore those files before describing, so a clean tagged checkout
  produces a clean version string. Affects every operator on the Docker
  Compose path.

## [0.1.2] - 2026-04-30

### Fixed

- **Docker build no longer stamps every binary as `dev`.** The build
  context's `.dockerignore` was excluding `.git`, so the in-container
  `git describe --tags --always --dirty` couldn't see any history and
  fell back to `echo dev`. Tagged Docker builds now report their real
  version via `/api/version`, and the Settings → Software update page
  no longer flags every Docker-built binary as a development build.
  Affects every operator on the Docker Compose path; binary builds via
  `make build` were unaffected.

## [0.1.1] - 2026-04-30

### Added

- **In-app update detection.** Admins see a new **Settings → Software
  update** page that compares the running binary against the
  upstream `CHANGELOG.md`, shows the changes since the running
  version, and offers a **Check now** button. A small dot on the
  hamburger menu and the Settings entry surfaces the badge when an
  update is available. The check runs daily in the background; the
  fetch URL is configurable via `ZV_CHANGELOG_URL`.
- **`make tag BUMP=...`** helper for bumping the version and
  creating an annotated tag locally based on the latest existing
  tag. Prints the push command rather than pushing for you.

## [0.1.0] - 2026-04-30

First tagged release.

### Added

- **Player experience.** Magic-link join with no app install, multi-puzzle
  adventures with markdown prompts and images, GPS / location puzzles,
  hints with timed reveal, and a real-time live audience view.
- **Arranger UI.** Full authoring of adventures, puzzles, hints, and
  teams; event scheduling; per-adventure branding; a race simulator for
  testing pacing without real players.
- **Identity & multi-tenancy.** Admin / org-admin / org-member roles,
  multiple organisations per user, TOTP two-factor authentication, and
  an audit log.
- **Localisation.** English and Swedish throughout, with a build-time
  gate that fails when a string is missing a translation.
- **Self-host.** Single static Go binary with embedded migrations and
  assets, packaged as a one-command Docker Compose stack on top of
  Postgres. Configurable retention, optional Prometheus metrics, and
  pg_dump-portable backup / restore.

[Unreleased]: https://git.sr.ht/~klahr/zv/log/master
[0.1.4]: https://git.sr.ht/~klahr/zv/refs/0.1.4
[0.1.3]: https://git.sr.ht/~klahr/zv/refs/0.1.3
[0.1.2]: https://git.sr.ht/~klahr/zv/refs/0.1.2
[0.1.1]: https://git.sr.ht/~klahr/zv/refs/0.1.1
[0.1.0]: https://git.sr.ht/~klahr/zv/refs/0.1.0
