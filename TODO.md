# Production-readiness scorecard

Findings from a code review of the repo. Grouped by **gap** (missing
feature) vs **simplification** (existing code that could be smaller),
ordered by recommended sequence. Each item lists rough size in the
`(small / medium / large)` column.

The hosted launch is gated on legal review, not on this list — but
clearing the small items before the launch reduces ongoing friction
for self-hosters and operators.

## Gaps

| # | Item | Size | Status |
| - | - | - | - |
| 1 | This `TODO.md` referenced from `README.md:80` didn't exist | small | **done** |
| 2 | No CI: nothing runs `go test`, `go vet`, `go build` automatically | small–medium | **done** — `.builds.yml` (sourcehut, Postgres-backed) |
| 3 | No `/api/version` or build-info endpoint | small | **done** — `Makefile` + `Dockerfile` + `.builds.yml` inject `git describe`; runtime/debug fills VCS info |
| 4 | No event cancellation — only path is delete-team-by-team | medium | **done** — `POST /api/admin/events/:id/cancel` (password-gated), `cancelled` state in schema + retention sweep, UI button on event detail, player + live screens render the new state |
| 5 | No 2FA for arranger accounts | medium–large | **done** — opt-in TOTP per user (`ZV_TOTP_KEY` enables; AES-GCM at rest), Settings card with QR + manual secret + 10 single-use recovery codes, password-reset gate, admin factory-reset endpoint, new audit events. Login splits into `VerifyPassword` + `CompleteLogin(factors)` so future Google Sign-in (#18) plugs into the same seam |
| 6 | No bulk team/player roster import | medium | **done** — `POST /api/admin/events/:id/teams/bulk` with per-row results (partial success); "Paste roster" mode in the new-team modal accepts comma/tab/semicolon/colon-separated lines |
| 7 | Audit log has no filtering UI | small | **done** — event/result/user/IP filters on `/api/admin/auth-events` + UI |
| 8 | Image `alt` is hardcoded empty (`arrange.html:4543`) | small | **done** — `alt_text` column on `adventure_page_images`, `PATCH /api/admin/images/:id` (500-char cap), inline alt input in puzzle-edit modal, surfaced through player + preview DTOs and rendered in play.html. Team-avatar `alt=""` deliberately kept (decorative). |

## Simplifications

| # | Item | Size | Status |
| - | - | - | - |
| 9  | `internal/admin/simulator.go` is a pure passthrough over `internal/simulator/simulator.go` | small | **done** — typed sentinels (`ErrAlreadyActive`, `ErrNoActive`) + `callerOrgIDOrFail` shortcut; brittle string-equality error check gone |
| 10 | `scheduler` + `retention` reimplement the same ticker shell | small | **done** — extracted `internal/periodic` |
| 11 | Six near-identical token generators across auth / admin / simulator | small | **done** — extracted `internal/tokenx`; six call sites collapsed to one-line wrappers |
| 12 | `db.go` doesn't tune the connection pool | small | **done** — `db.Opts` + `-db-max-open-conns` / `-db-max-idle-conns` flags + `ZV_DB_MAX_*_CONNS` env |
| 13 | `internal/admin/authoring.go` is 1299 lines mixing four concerns | medium | **done** — split into `authoring.go` (adventure CRUD, types, helpers, 541), `pages.go` (page CRUD + reorder, 363), `images.go` (upload + serve + alt edit, 262), `avatars.go` (cover art, 131), `shares.go` (commission to org, 116) |

## Plugin / extensibility candidates

Functionality that's optional, vendor-flavoured, or replaceable, and
is a natural fit for an interface + drop-in implementations. The
direction is "thin core, named seams" — every entry below is sized as
"add a second concrete implementation behind a Go interface", not "build
a generic plugin runtime". Tackle one at a time.

| # | Item | Size | Status |
| - | - | - | - |
| 14 | Roster import: file-format plugins via `roster.Importer` (`internal/roster/`); Content-Type dispatch in `BulkCreateTeams` | medium | **partial** — interface + JSON + CSV shipped; follow-ups: `.xlsx` upload, IOF Interface XML 3.0 (orienteering organisers), vCard 4.0 groups |
| 15 | Mailer recipes for Amazon SES / Postmark / SendGrid / Mailgun — all four expose SMTP submission so the bundled `SMTPMailer` already covers them; only docs were missing | small | **done** — `SELFHOSTING.md` "SMTP recipes" section. A native HTTP-API mailer behind a build tag (per-message tracking IDs, SES IAM auth) remains a future option but isn't load-bearing for the auth/verification mail volumes the product sends today |
| 16 | Payment provider abstraction — extract `payments.Provider`, make Stripe one impl, allow self-host to ship without the Stripe SDK at all (Mollie / Paddle / Adyen as future impls) | medium | pending |
| 17 | Image / avatar storage abstraction — `teams.avatar_content` and `adventure_page_images.content` are DB blobs today; pluggable backends for local FS / S3 / R2 / GCS keep the binary thin and the DB small | medium | **partial** — `storage.Blobs` interface (`internal/storage/`) + `dbblob` impl shipped; team avatars + adventure cover art now go through the seam end-to-end (admin + public/share-page paths). Remaining: `adventure_page_images.content` (more complex shape — multi-row-per-entity with `alt_text` / `idx` metadata), and the actual non-DB impls (`-tags=storage_fs`, `-tags=storage_s3`) |
| 18 | Auth backends — OIDC / SAML / GitHub / Google / passkeys alongside the current username/password; magic-link slot flow stays core | medium–large per backend | pending |
| 19 | Metrics exporter alternates — OTLP and StatsD alongside the Prometheus collectors in `internal/metrics` | small | pending |
| 20 | Webhook formatter plugins — pre-shaped Slack / Discord / MS Teams / Zapier deliveries on top of the generic signed-POST in `internal/webhooks`, so customers don't have to write a translator service | small per integration | pending |
| 21 | i18n language packs — loadable bundles instead of strings baked into `i18n.js`; lets translators contribute without a JS rebuild | medium | pending |
| 22 | SCIM 2.0 provisioning endpoint (RFC 7643/7644) — inbound user/group sync for corporate team-build and school-IT customers; lands rosters straight from Workday / Okta / Azure AD / Google Workspace | large | pending |
| 23 | Installed-components panel — `internal/components.Component` registry, admin-only `GET /api/admin/components`, "Installed components" card on the Settings page (admin role) showing seam → impl → on/off → notes | small | **done** — `cmd/server` builds the snapshot from configured env (`describeComponents`); read-only, no runtime swapping |

## What was checked and is fine

- Refunds are wired (`stripe.go:304` `RefundPurchase` + `handleChargeRefunded` + `KindPurchaseRefunded`).
- Player-side language picker exists (`play.html:3326`, via `zvI18n.makeLangPicker`).
- Org-member roles are validated and editable (`organizations.go:236`,
  `org_admin` / `org_member`).
- `embed`-based assets and migrations are clean.
- No SQL injection or auth-bypass smells in the spot checks.

## Out of scope for now

- Mobile native apps. The whole point is "no app, just a phone browser".
- Multi-tenant billing. Single-org self-host vs hosted-with-Stripe is
  enough; per-org billing portals already exist via Stripe.
- General-purpose ticketing.
