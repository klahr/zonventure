# Pre-release hardening — findings

Adversarial/stability pass against an **isolated** instance (compose project
`zvattack`, throwaway volume, port 8099 — the dev stack on :8085 was never
touched). The isolated stack was torn down afterward.

Each item is tagged with how strongly it is established:

- **[LIVE]** — reproduced against the running instance.
- **[CODE]** — confirmed by reading the handler + its helpers; not executed.
- **[REPORTED]** — from the delegated authorization review; **not independently
  verified here** (believable, but treat as a lead until checked).

Date: 2026-10-02.

---

## Part 1 — Fixed in this pass (stability / DoS)

Built, `go vet ./...` clean, `go test ./...` green (36 pkgs), each fix verified
live then the stack torn down. **Not committed.** 25 files changed.

### F1. Unauthenticated disk-fill DoS — multipart body never bounded **[LIVE]**
`ParseMultipartForm(cap)`'s argument only caps the *in-memory* share of the
upload; the remainder of the request body spills to a temp file on disk **during
the parse**, i.e. before the handler's per-file `hdr.Size` / `LimitReader` check
runs. So an oversized part is written to disk in full and only then rejected
with 413. Concurrent requests exhaust disk.

- Reachable **unauthenticated** via `POST /api/public/team/{token}/avatar`
  (`internal/public/team.go`), and by low-barrier slot players via
  `internal/player/teamprofile.go`.
- Only `internal/player/blocks.go` wrapped `r.Body` in `http.MaxBytesReader`
  first (the correct pattern); 13 other call sites did not.
- **Verified:** a real 191 MB upload was fully buffered pre-fix; post-fix it is
  rejected after ~6 MB in 0.03 s.
- **Fix applied:** `r.Body = http.MaxBytesReader(w, r.Body, cap+slack)` before
  every `ParseMultipartForm`, across `public/team.go`, `player/teamprofile.go`,
  `admin/{org_logo,org_profile,teams,teamimport,me_avatar,avatars,media,portability,migrate}.go`.

### F2. Background-goroutine panic crashes the whole process **[CODE]**
`middleware.Recoverer` only wraps HTTP handler goroutines. The mailer outbox,
webhook worker, effects worker, periodic loops (scheduler/retention), the
updates poller, and every WebSocket pump goroutine ran with no `recover()`. A
single panic in any of them takes the server down — and with
`restart: unless-stopped` that becomes a crash loop.

- **Fix applied:** added `logx.Guard(name)` (recovers + logs a stack trace) and
  deferred it at each goroutine entry point: `ws/ws.go` (writer), `ws/hub.go`
  (Coalesce), `mailer/outbox.go`, `webhooks/delivery.go`, `effects/worker.go`,
  `periodic/periodic.go`, `updates/service.go`, `admin/ws.go` (×2),
  `public/ws.go` (×2). Trades a dead worker for a live server; it does not
  self-heal the loop.

### F3. Unauthenticated 500s on slug routes **[LIVE]**
A NUL byte or invalid UTF-8 in a `{slug}` path segment errored the Postgres
query (`invalid byte sequence for encoding "UTF8"`) rather than returning no
rows, surfacing as a 500 plus error-log/metric spam. `/o/{slug}` and the JSON
APIs `/api/o/{slug}/store/items[/{id}]`, `/api/o/{slug}/redeem/options` were all
affected; the `/store` page path was already safe (`orgSlugExists` fails closed).

- **Fix applied:** fail closed to 404 on empty / NUL / non-UTF-8 slugs in
  `server/org_landing.go`, `public/store.go` (`resolveOrgSlug`), and
  `admin/redemptions.go` (`redeemOrg`). **Verified:** all now 404.

### F4. Unbounded JSON body in `CreateMCPToken` **[CODE]**
`internal/admin/mcp_tokens.go` decoded `r.Body` with no cap (admin-authed).
- **Fix applied:** `http.MaxBytesReader(w, r.Body, 1<<20)`, matching `httpx`.

### Verified already-robust (no change)
Image pixel-bomb pre-check (`imageutil` `DecodeConfig` gate); CSRF double-submit
(192-bit, constant-time, SameSite=Lax); WS same-origin check + 64 KiB read
limit; coordinate range + NaN/Inf handling (JSON rejects NaN; overflow→Inf is
range-caught); SQL parameterized throughout.

---

## Part 2 — Security findings (ALL FIXED)

Authorization / account-integrity track. Every item below is now fixed in the
working tree (uncommitted); `go build`, `go vet`, `go test ./...` (36 pkgs) and
`-race` on the touched packages all pass. S1 and S2 were additionally verified
live on the isolated stack. New migration: `0095_user_created_by_org.sql`. New
env var: `ZV_WEBHOOKS_ALLOW_LOOPBACK` (default off).

### Critical — fixed

- **S1. Default credentials fully functional, no forced rotation** — FIXED
  New `auth.RequireNonDefaultPassword` middleware on `/api/admin`: a user still
  on `password_is_default` can do nothing but `POST /me/password`; everything
  else 403s with `code:"password_change_required"`. `admin/admin` is now
  unusable until rotated (first-boot onboarding intact — change still works with
  current=`admin`). **Verified live:** list-orgs 403 → change password 204 →
  list-orgs 200.

- **S2. Org-admin escalation → account takeover** — FIXED
  - Migration `0095` adds `users.created_by_org_id`, set in `CreateOrgMember`'s
    new-user flow. `ResetPassword` / `AdminDisableUserTOTP` org_admin branch now
    requires `orgAdminMayManageCredentials`: the target must have been created
    by the caller's org AND belong to no other org. Self-signup logins and
    shared accounts are no longer resettable by an org_admin (self-service email
    reset / global admin remain).
  - `CreateOrgMember` existing-user branch now refuses to pull in an account
    that already belongs to another org (that needs an invitation flow).
  - **Verified live:** an org_admin resetting a cross-org user → 403.

- **S3. `ConfirmPurchase` / `CreatePurchase` payment bypass** — FIXED
  New `simulatedActive()` gate. `CreatePurchase` refuses caller-specified
  `provider:"simulated"` unless simulated is the wired provider, and refuses a
  paid buy with no real provider (was silently "simulated"). `ConfirmPurchase`
  refuses non-admin confirms unless simulated is active — on Stripe/prod,
  payment settles only via the signed webhook.
  Regressed by e12a9fd ("Fix tests"), which removed the gate; restored with
  the test fixtures enabling the simulated provider (`ZV_ENV=dev`) instead.
  A paid buy with no provider now gets 503; `TestPurchase_PaidRefusedWithoutProvider`.

### High — fixed

- **S4. `CreateAdmin` ungated** — FIXED. `/admins` POST+DELETE (and member /
  transfer-ownership routes) now `RequireSession` (cookie-only, no API token);
  `CreateAdmin` also requires `X-Confirm-Password`, matching `DeleteAdmin`.
- **S5. Stored XSS via import** — FIXED. `ImportAdventure` re-encodes the
  imported avatar through `imageutil.NormalizeUpload` and graph-media images
  through `ShrinkKeepAlpha` (refuses SVG/non-raster), mirroring the upload path.
  Since e12a9fd, bytes that fail to decode are kept when the declared type is
  a raster image (png/jpeg/webp/gif) for round-trip fidelity; SVG and other
  types are still refused. Safe because every response carries `nosniff`
  (`securityHeaders`), so such bytes can't be rendered as active content.

### Medium — fixed

- M1 — `ResetPassword` self path now requires `X-Confirm-Password`.
- M2 — `orgFromCaller` now requires `org_admin` for all webhook management.
- M3 — org webhook loopback gated behind `ZV_WEBHOOKS_ALLOW_LOOPBACK`
  (registration + delivery; default off), matching effect webhooks.
- M4 — imported graph-media images re-encoded; SVG refused (same fix as S5).
- M5 — API/MCP tokens revoked on password change/reset, set-password, TOTP
  disable, admin TOTP reset, and account deletion; `tokenUser` rejects
  scheduled-for-deletion accounts.
- M6 — `get_adventure` (MCP) now uses `AuthorizeAdventureEdit`, so the full
  envelope (answers/hints) is author-only.
- M7 — `requirePasswordConfirm` is now a rate-limited method
  (`ConfirmLimiter`, per IP+user).
- M8 — addressed transitively: S2 stops the forced-membership that made another
  org's auth-event history reachable (`ListAuthEvents` scope unchanged).

### Low / defence-in-depth — fixed or dispositioned

- `CreatePortalSession` → now `org_admin` only. FIXED.
- `delete_adventure` over MCP → left as-is: write-scoped + `AuthorizeAdventureEdit`
  + two-phase `confirm`; password can't be replayed by a token, and removing it
  would break authoring. DISPOSITIONED.
- `get_event` (MCP) → player/team capability tokens stripped unless the token
  has `write` scope. FIXED.
- MCP edits logged as human → MCP dispatch now sets `auth.WithAPIToken`. FIXED.
- `audienceAllows` lobby default → lobby (no-org) users excluded from org
  tools. FIXED.
- `ClientIP` / XFF → now rightmost-untrusted (`RightmostUntrustedXFF`), defeating
  client-forged left-side entries when a proxy appends. FIXED.
- Stripe fulfilment → webhook now cross-checks session id, amount and currency
  against the stored purchase before granting. FIXED.
- `ImportTeams` cross-org oracle → already gated by `requireEventOp` (report
  line numbers were stale). NO CHANGE NEEDED.
- Slot `slot_sessions`/`player_slots.expires_at` → `LookupSlot` enforces the
  session TTL; enforcing the *slot* link-expiry on every lookup would evict
  players mid-event, so left as intentional design. Plaintext slot tokens are
  short-lived capability URLs by design. DISPOSITIONED (no change).
