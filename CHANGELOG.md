# Changelog

All notable changes to Zonventure will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The repository, Go module, and CLI binary are named `zv`; the product is
**Zonventure**. Versions tagged here correspond to git tags `v<version>` on
[git.sr.ht/~klahr/zv](https://git.sr.ht/~klahr/zv).

## [Unreleased]

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
[0.1.0]: https://git.sr.ht/~klahr/zv/refs/v0.1.0
