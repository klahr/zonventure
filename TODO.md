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
| 4 | No event cancellation — only path is delete-team-by-team | medium | pending |
| 5 | No 2FA for arranger accounts | medium–large | pending |
| 6 | No bulk team/player roster import | medium | pending |
| 7 | Audit log has no filtering UI | small | **done** — event/result/user/IP filters on `/api/admin/auth-events` + UI |
| 8 | Image `alt` is hardcoded empty (`arrange.html:4543`) | small | pending; WCAG 2.1 AA gap, needs schema column + authoring UI + render in play.html / live.html |

## Simplifications

| # | Item | Size | Status |
| - | - | - | - |
| 9  | `internal/admin/simulator.go` is a pure passthrough over `internal/simulator/simulator.go` | small | **done** — typed sentinels (`ErrAlreadyActive`, `ErrNoActive`) + `callerOrgIDOrFail` shortcut; brittle string-equality error check gone |
| 10 | `scheduler` + `retention` reimplement the same ticker shell | small | **done** — extracted `internal/periodic` |
| 11 | Six near-identical token generators across auth / admin / simulator | small | **done** — extracted `internal/tokenx`; six call sites collapsed to one-line wrappers |
| 12 | `db.go` doesn't tune the connection pool | small | **done** — `db.Opts` + `-db-max-open-conns` / `-db-max-idle-conns` flags + `ZV_DB_MAX_*_CONNS` env |
| 13 | `internal/admin/authoring.go` is 1299 lines mixing four concerns | medium | pending |

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
