# Changelog

All notable changes to Zonventure will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The repository, Go module, and CLI binary are named `zv`; the product is
**Zonventure**. Versions tagged here correspond to git tags `v<version>` on
[git.sr.ht/~klahr/zv](https://git.sr.ht/~klahr/zv).

## [Unreleased]

### Tests

- **Client-side offline-queue unit tests.** Extracted the player's
  offline action queue from `play.html` into `play-queue.js` so
  it can be unit-tested in isolation; `play-queue.test.js` runs
  under Node's built-in test runner (`node --test`) — no Jest, no
  Vitest, no `package.json`. 12 cases cover `clientActionId`
  uniqueness + crypto/fallback paths, the localStorage round-trip,
  cap enforcement under hand-edited storage, malformed-JSON
  tolerance, and quota-exceeded `setItem` failures. CI gains a
  `nodejs` package + a `test-js` task in `.builds.yml`.
- **Regression coverage for the 0.1.6 server work.** Ten new
  Postgres-backed integration tests pin the recent fixes:
  - `TestRevealHint_RejectsBeforeUnlockTime` / `_AllowsAtZeroDelay`
    cover the new server-side hint-timer 403, including the
    `Retry-After` header the offline replay queue depends on.
  - `TestSubmitAnswer_ReplaysCachedResponseOnRetry` and the hint
    counterpart pin the dedup contract — same `client_action_id`
    replays the cached body, a fresh id runs against current state.
  - `TestAudio_PageUploadRoundTrip`, `_PageRejectsNonAudioMime`,
    `_PageVisibleAcrossPagesInSameAdventure`, `_HintGatedByReveal`
    cover the new audio handlers + the puzzle/hint visibility split
    introduced when we relaxed `canViewPageImage` for prefetch.
  - `TestManifest_IncludesAllAdventureAssets` / `_RequiresSlot`
    cover the new `/api/player/manifest` endpoint.
  Tests skip without `ZV_TEST_DSN`, run on CI.

### Changed

- **Server error messages are now localized.** The client `api()`
  helper auto-translates `body.error` through the i18n table — every
  call site that already shows `r.body?.error` in a toast or result
  line now surfaces the Swedish translation when one exists, no
  per-site changes needed. The translation table grew by ~80 entries
  covering the player + arranger surfaces (auth, signup, TOTP,
  asset uploads, gameplay errors, validation messages, common
  arranger CRUD errors). Pure-internal 500s like `"begin tx"` and
  `"lookup failed"` stay English on purpose — the operator reading
  logs is the audience there. WS `answer_error` frames also run
  through `t()` for consistency.

### Added

- **Offline-tolerant play.** The player UI now caches the adventure's
  images + audio up-front (service worker + new
  `GET /api/player/manifest`), persists queued answer/advance actions
  to localStorage so a reload doesn't lose them, and tags every
  mutating action with a stable `client_action_id`. The server's new
  `player_action_dedup` table replays the cached response on a retry
  with the same id — so a queued submit that flushes after a
  reconnect doesn't double-process even if the original landed
  before the connection died. Visibility for puzzle assets relaxed
  to "any team member of this adventure" (down from "team is on
  this exact page") so the SW can prefetch all puzzles up-front;
  hint assets stay reveal-gated.
- **Server-side hint timer validation.** `RevealHint` now checks
  `now - team.current_page_started_at >= hint.delay_seconds` and
  returns 403 with `Retry-After` if the hint isn't unlocked yet.
  Closes a pre-existing gap where the client gated the reveal
  button on `unlocks_in_seconds` but the server never re-checked,
  so a misclocked or malicious client could POST early.
- **Audio attachments on puzzles and hints.** Arrangers can now upload
  audio (`.mp3`, `.m4a`, `.ogg`, `.wav`, anything `audio/*`) alongside
  images on a puzzle or a hint. Players see a native HTML5 `<audio
  controls>` element; the page-level audio is visible to any team on
  the puzzle, hint audio is reveal-gated like hint images. Each clip
  carries an optional `description` field for a transcript / summary
  fallback. New routes: `POST /api/admin/pages/{id}/audio`,
  `PATCH/DELETE /api/admin/audio/{id}`, plus the `hints/.../audio`
  + `hint-audio/{id}` mirrors. Size cap defaults to 15 MiB
  (`ZV_MAX_AUDIO_BYTES` overrides). Page-clone copies audio rows
  blob-for-blob, same as images.
- **"Add media" replaces "Add image"** in the puzzle and hint editors.
  Single picker accepts `image/*,audio/*` and routes the upload to the
  matching endpoint by MIME — the existing image flow stays
  unchanged, audio takes a new code path that skips client-side
  downscaling.

## [0.1.5] - 2026-05-01

### Added

- **Sidebar version line.** Every signed-in user now sees the running
  build's version in the sidebar footer (fetched once per session from
  the public `/api/version`).
- **One-click upgrade affordance.** Admins with a newer release
  available now get an "Update to v\<latest\>" button under the version
  line that jumps straight to Settings → Software update.
- **`make upgrade` target.** One command that fetches tags, checks out
  the newest release, and rebuilds the Docker image at that commit —
  replacing the three-line recipe operators were copy-pasting. The
  in-app upgrade card and `SELFHOSTING.md` lead with this; the
  underlying `git`/`docker` commands are kept as the equivalent.

### Changed

- **Plain-language copy on the Software update page.** The lede now
  says "Checks once a day whether a newer version of Zonventure is
  available" instead of "Compares the running binary against the
  upstream CHANGELOG.md" — same behaviour, less jargon for non-technical
  admins.
- **Brand naming clarified.** "Zonventure" is the platform; "Zonventure
  Studio" is the arranger tool (used only in the `/arrange` `<title>`,
  the browser-unsupported message, and the welcome modal); "Zonventure
  AB" is the legal entity. Player-facing pages, marketing surfaces,
  emails, and the TOTP issuer label all use plain "Zonventure".

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
[0.1.5]: https://git.sr.ht/~klahr/zv/refs/0.1.5
[0.1.4]: https://git.sr.ht/~klahr/zv/refs/0.1.4
[0.1.3]: https://git.sr.ht/~klahr/zv/refs/0.1.3
[0.1.2]: https://git.sr.ht/~klahr/zv/refs/0.1.2
[0.1.1]: https://git.sr.ht/~klahr/zv/refs/0.1.1
[0.1.0]: https://git.sr.ht/~klahr/zv/refs/0.1.0
