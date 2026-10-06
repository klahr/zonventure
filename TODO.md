# Production-readiness scorecard

Findings from a code review of the repo. Grouped by **gap** (missing
feature) vs **simplification** (existing code that could be smaller),
ordered by recommended sequence. Each item lists rough size in
parentheses.

The hosted launch is gated on legal review, not on this list — but
clearing the small items before the launch reduces ongoing friction
for self-hosters and operators.

> Recently-completed production-hardening rounds (security &
> correctness audit, three reliability gaps, a11y / mobile / perf
> work, i18n quality) are summarised in `CHANGELOG.md` under the
> current `[Unreleased]` section. This file tracks what's still
> open.

## Quiz trail & hosted events (in progress)

The spec is `QUIZ_TRAIL.md` (read its **Decisions** section first: it
records what was built and where it differs from the first draft). The
work is in four phases; A–C are built, D is next.

- **A. Hosted foundation — done.** A party is a hosted event: player
  link `/j/{token}`, teams name themselves, games are rounds
  (`event_rounds`, `teams.round`, a graph pin per round), Finish game +
  summary email, sweeper (`SweepHosted`: idle 3 h, lapse at
  `valid_until`), 60 days to start / 14 days of play, console "Hosted"
  page, shared views in `web/packages/hosted`. Handlers serve both the
  host link (`/api/party/{token}/…`) and the console
  (`/api/admin/events/{id}/hosted/…`). Migration 0109.
- **B. Pricing — done.** `ZV_HOSTED_BLOCK_CENTS` (default 14900 when a
  platform fee is set, else free) per block of 15 teams per game; Host
  it (`HostItDialog`), Add 15 teams, party fee = pass price, refunds.
  Charged on the platform's own Stripe account. Migration 0110.
- **C. Creator in the console — done.** `internal/trail` (cards,
  `Problems`, `Compile`, templates), `internal/admin/trails.go` (create,
  save, publish-on-use, designer handover), `trail_pdf.go` (cards +
  answer key), `web/packages/trail` (Editor, autosave, templates),
  console route `#/adventures/{slug}/cards`. Migration 0111.

### Phase D — public quiz trail, no account (next)

Spec sections: Availability, Lifecycle (trail), Data model (`trails`,
`trail_email_tokens`), Screens 1–7, Emails, Conversion, Retention,
Security and abuse, API sketch.

- [ ] Migration 0112: `organizations.quiz_trail_enabled`;
      `adventures.trail_hidden` (hide from org lists, store, MCP,
      party offers); `trails` table (secret hash, adventure, event,
      email + confirmed, keep_offer_sent_at); `trail_email_tokens`.
- [ ] Hide `trail_hidden` adventures everywhere an org lists its
      adventures (console list, `ListPartyOffers`, catalog, MCP
      `list_adventures`, Host it/self-guided pickers).
- [ ] Public API `/api/o/{slug}/trail`: `POST /drafts` (template, lang)
      → `{id, secret}` creating a hidden adventure in the seller org;
      `GET|PUT /drafts/{id}` (secret in a header, revision check — reuse
      `trails.go` save/describe logic, don't duplicate it);
      `POST /drafts/{id}/checkout {blocks}`; `POST /email/confirm`,
      `POST /email/signin`, `GET /mine`. Refuse when the org's flag is
      off. Rate limits: 20 drafts/IP/day, save cap per draft.
- [ ] Checkout: purchase kind for a trail (or reuse `hosted` with the
      hidden adventure); webhook creates the hosted event
      (`createHostedEvent`), sets the trail's email (confirmed by
      payment), sends the host link. Guardian consent when `trail_kids`.
- [ ] Public pages (party app or a new `trail` app): Start
      (`/o/{slug}/trail`, templates, "Free to make · 149 kr to play"),
      creator (reuse `@zonventure/trail` `Editor` + `useTrailSave` with
      a draft API adapter; draft id+secret in localStorage, "Copy link
      to this draft" as `#{id}.{secret}`), checkout, My trails
      (`/trail/mine`).
- [ ] Save to email: confirm link attaches every trail this browser
      holds; sign-in link lists all trails for the email. Never reveal
      whether an email has trails.
- [ ] Host page for a trail: Edit trail (opens the creator with the
      host token as credential), Keep it (from day 7 of play).
- [ ] Emails (`internal/emailtpl`, sv + en): `trail_confirm`,
      `trail_signin`, `trail_paid`, `trail_keep` (day 7), `trail_ended`.
- [ ] Conversion ("Keep it"): signup with email prefilled → personal
      org → move the adventure to it, clear `trail_hidden`, delete the
      `trails` row and its hosted event, send `trail_ended`.
- [ ] Retention (`internal/retention`): unpaid drafts 30 days after the
      last edit (with their hidden adventure); trail events at
      `valid_until` (finish game, `trail_ended`, delete trail + adventure
      + event); day-7 keep email once; expired email tokens.
- [ ] zonventure-web links to `/o/zonventure/trail`; set
      `quiz_trail_enabled` on the Zonventure org.
- [ ] Docs: CHANGELOG, OPERATIONS.md retention paragraph, privacy page.

### Follow-ups and known gaps

- [ ] Timeline drag-to-reorder is untested on a real touch device
      (Move earlier/later is the fallback).
- [ ] Templates set no theme/colours yet (spec: each template sets a
      theme from the existing themes).
- [ ] Party offers must be in the pass currency (SEK) when a platform
      fee is set — blocks orgs selling in EUR. Decide.
- [ ] Production: set `ZV_HOSTED_BLOCK_CENTS=14900` if no platform fee
      is configured there (otherwise hosting is free).
- [ ] Console: a hosted event's page highlights "Events" in the
      sidebar instead of "Hosted"; the Hosted list shows a redundant
      "Hosted" pill per row.
- [ ] A joined player reads "You're playing as Owls on team Owls" (the
      slot is named after the team).
- [ ] Not tested against real Stripe (tests use a fake provider).
- [ ] Later (spec): places mode, more card kinds, teams starting at
      different cards.

### Working on it locally

- DB tests: `IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' zonventure-db-1)`
  then `ZV_TEST_DSN="postgres://zv:zv@$IP:5432/postgres?sslmode=disable" go test -p 2 ./... -count=1`
  (`-p 2`, or Postgres runs out of connections).
- Web: `cd web && npm install && npm test && npm run typecheck`;
  `make web` builds the apps into the server's embedded assets (rebuild
  the server binary after it).
- Live check: create a scratch database, run the server once with
  `-seed-arranger user:password` (it seeds and exits), then run it with
  `-addr 127.0.0.1:8199 -public-url http://127.0.0.1:8199`. Hosting is
  free there (no platform fee), so Host it and Add teams skip payment.

## View-by-view UX review (current focus)

We're walking every view in the product to confirm it's good, simple,
and has proper UX. Each entry below is one self-contained surface;
the review answers the same questions for every one:

- Is this view doing one job clearly, or is it three jobs stacked?
- Is the content right-sized — neither bloated nor missing?
- Are the affordances discoverable, the failure states actionable,
  and the empty states pedagogic?
- Mobile / outdoor / one-handed use covered?
- A11y baseline (keyboard nav, AT announce, focus, motion respect)?
- Localised end-to-end (no English fragments leaking through)?
- Loading + error + empty distinguishable from each other?
- Anything that should be cut or merged with another view?

Each box gets ticked once we've walked it together. Findings flow
into focused fix tasks below the list.

### Public-facing surfaces

- [ ] `/` — Marketing landing (`landing.html`)
- [ ] `/o/{slug}` — Per-organisation landing (`org-landing.html`)
- [ ] `/pricing` — Pricing (`pricing.html`)
- [ ] `/manual` — Public arranger manual (`manual.html`)
- [ ] `/privacy` — Privacy policy (`privacy.html`)
- [ ] `/terms` — Terms of service (`terms.html`)
- [ ] `/store` — Public listing (`store.html`)
- [ ] `/o/{slug}/store` — Org-scoped public listing
- [ ] `/store/event/{id}` — Single event purchase (`store-event.html`)
- [ ] `/o/{slug}/store/event/{id}` — Org-scoped purchase
- [ ] `/live/{token}` — Audience leaderboard (`live.html`)
- [ ] `/recap/{token}` — Post-event recap (`recap.html`)
- [ ] `/team/{token}` — Team share page (slot picker / QR)
- [ ] 404 — Not-found fallback (`404.html`)

### Player surfaces (`play.html`)

The player SPA renders many distinct states; review each independently
since the empty / loading / error UX differs by state.

- [ ] No-session entry / magic-link arrival (`renderNoSession`)
- [ ] First-join welcome dialog (`showWelcomeDialog`)
- [ ] Waiting (event scheduled, not started yet) — `buildWaitingCard`
- [ ] Pre-start countdown (T-minutes ticking down)
- [ ] Playing (puzzle visible) — main interaction state
- [ ] Hint sheet (modal) — `openHintSheet`
- [ ] Menu drawer (modal) — `openMenuDrawer`
- [ ] QR scanner (modal) — `openQrScanner`
- [ ] Team-share sheet (modal) — `openTeamShareSheet`
- [ ] Geo-consent dialog
- [ ] Submit-dock 429 cooldown UX
- [ ] Offline / queued-actions UX (`pendingAnswers` flush feedback)
- [ ] Paused state — `buildPausedCard`
- [ ] Cancelled state — terminal banner
- [ ] Completed / celebration overlay — `celebrate`, summary
- [ ] Withdrawn state (post-refund team)
- [ ] Server error / WS-disconnected banner — `setWSStatus`
- [ ] Map fullscreen overlay (`/play` map button)
- [ ] Email recovery (lost-link recovery form)
- [ ] Spoiler-free preview mode (buyer / share-holder view)
- [ ] Brand strip / org logo — degraded fallback when load fails

### Arranger surfaces (`arrange.html`)

Hash-routed SPA. Mirrors the route table at `renderRoute`.

#### Auth flows
- [ ] `#login` — Sign-in form + OIDC button + forgot-password
- [ ] `#signup` — New account form
- [ ] TOTP challenge / enrol — interstitial flow
- [ ] OIDC callback (transient, redirect-only)
- [ ] `#verify-ok` / `#verify-failed` — email-verification landings
- [ ] `#reset?token=…` — password reset
- [ ] `#change-email?token=…` — email-change confirmation

#### Dashboard family
- [ ] `#dashboard` (lobby user — `viewLobbyDashboard`)
- [x] `#dashboard` (org member / admin — `viewArrangerDashboard`)
- [ ] `#dashboard` (global admin — `viewAdminDashboard`)

#### Adventures (authoring)
- [ ] `#adventures` — Adventure list (`viewAdventures`)
- [ ] `#adventures/{slug}` — Adventure detail / editor (`viewAdventureDetail`)
- [ ] `#puzzles` — Puzzles list (`viewPuzzles`)
- [ ] `#puzzles/{id}` — Puzzle editor (`viewPuzzleEditRoute`)
- [ ] `#library` — Library list (`viewLibrary`)
- [ ] `#library/{slug}` — Library item edit (`viewLibraryEdit`)

#### Events
- [ ] `#events` — Event list (`viewRuns`)
- [ ] `#events/{id}` — Event detail (`viewRunDetail`)
- [ ] `#events/{id}/control` — Race-control cockpit (`viewEventControl`)
- [ ] `#quests` — Public-store quest list (`viewQuests`)

#### Catalog / commerce
- [ ] `#catalog` — Buyable assignments list (`viewCatalog`)
- [ ] `#catalog/{slug}` — Single catalog item (`viewCatalogDetail`)

#### Settings hub (`#settings/<tab>`)
- [ ] `general` — UI prefs + help (`renderSettingsGeneral`)
- [ ] `account` — Personal account cards (password / email / TOTP /
      sessions / audit / export / delete)
- [ ] `members` — Org member roster (`viewMembers`)
- [ ] `admins` — Global admins (`viewAdmins`, admin-only)
- [ ] `audit` — Audit log (`viewAudit`)
- [ ] `purchases` — Purchase history (`viewPurchases`)
- [ ] `billing` — Stripe Connect / payouts (`viewSettingsBilling`)
- [ ] `integrations` — Webhooks list (`viewWebhooks`)
- [ ] `integrations/log` — Per-endpoint delivery log (`viewWebhooksLog`)
- [ ] `system` — Org branding / SMTP test / migration entrypoint
      (`renderSettingsSystem`)
- [ ] `migration` — Cutover wizard (`viewSettingsMigration`)
- [ ] `software-update` — Update checker (`viewSettingsSoftwareUpdate`)

#### Admin-only
- [x] `#organizations` — Cross-org list (`viewOrganizations`)
- [x] `#logs` — Live log tail (`viewLogs`, xterm.js)
- [ ] Simulator panel (mounted on adventure detail; admin-toggleable)

#### Misc
- [ ] `#manual` — In-app localised manual (`viewManual`)
- [ ] Adventure preview / spoiler-free run-as-player
- [ ] Toast + modal + global error-overlay shell

#### Fix tasks (filled in as the review walks each view)

_(empty — populated as we work through the list above)_

## Outstanding from production-hardening rounds

Items the audits identified that are real but didn't fit the
sessions where they were raised. Listed here so they don't fall on
the floor.

### Backend
- [ ] BYTEAs on hot tables (events / teams / organizations carry
      inline blob columns) — move to dedicated child tables to
      avoid TOAST-thrash on accidental `SELECT *`
- [ ] Per-`Provider` Stripe `client.API` instance so concurrent
      goroutines don't race on the package-global `stripego.Key`.
      Real only when multi-tenant per-org Stripe keys land
- [ ] Write `processed_webhook_events` + handler outcome inside one
      outer tx so a `markProcessed` failure can't lead to duplicate
      fulfilment on redelivery (today the action handlers' state
      checks make this a non-issue, but the seam is brittle)

### Frontend
- [ ] **Localise legal + manual + pricing into Swedish.**
      `privacy.html` / `terms.html` / `pricing.html` / `manual.html`
      are hardcoded English. SV users hit an English wall via the
      footer. Copy work, not engineering, but real
- [ ] **Self-host xterm on the Logs view.** Leaflet + Google Fonts
      now ship from the binary (see CHANGELOG); xterm + xterm-addon-fit
      are the last unpkg dependencies, kept whitelisted in the
      baseline CSP so the admin terminal keeps working. Vendoring
      them lets the global CSP drop the unpkg allowance entirely
- [ ] **Real `og:image` PNG** (1200×630) for landing — chat unfurls
      currently render the SVG favicon poorly
- [ ] **Page-tokens migration** for `store.html`, `store-event.html`,
      `org-landing.html`, `manual.html` — earlier work migrated
      `landing.html` and `pricing.html`; the others still ship their
      own copies of the design tokens
- [ ] **Swedish nuance pass.** Mechanical sweep landed; remaining
      taste calls: `Uppdrag` is overloaded for both Quest (consumer-
      facing /store) and Assignment (paid licence) — pick one;
      `Recap` left untranslated; `Hint` rendered as both `tips` and
      `ledtråd`
- [ ] **arrange.html code-split.** 18k-line monolith parses in full
      on every page load regardless of role. Logs / migration /
      simulator are admin-only and could lazy-load
- [ ] **arrange.html inline-style extraction.** ~738 inline `style:`
      strings; the top ~10 recipes (`stack-tight`, `mono-input`,
      etc.) extract into utility classes for ~1-2 KLOC saved

### Manual + content drift
- [ ] **Manual: "run" vs "event" terminology drift.** The concept
      was renamed to Event but `manual.html` still says "scheduled
      run" / "a new run was created" in places. Webhook payload
      `event.scheduled` is glossed as "a new run was created"
- [ ] **Manual: "a Event" grammar** — should be "an Event"
- [ ] **Manual: hardcoded €5 / 15 free pricing** is an
      operator-configurable env var; placeholder it
- [ ] **Privacy ↔ COPPA contradiction.** `privacy.html` says "for
      adults"; `manual.html` references the COPPA path for under-13s
      that arrange.html exposes. Update one to match the other
- [ ] **Terms ↔ refund-promise mismatch.** `store-event.html`
      `StoreEventFeature4` ("Refundable up to event start") is a
      stronger consumer promise than the standard digital-goods
      withdrawal carve-out terms.html sets. Align via the legal
      review

## Plugin / extensibility candidates

Functionality that's optional, vendor-flavoured, or replaceable, and
is a natural fit for an interface + drop-in implementations. The
direction is "thin core, named seams" — every entry below is sized as
"add a second concrete implementation behind a Go interface", not
"build a generic plugin runtime". Tackle one at a time.

- [ ] **Image / avatar storage abstraction** (medium) — `storage.Blobs` interface + `dbblob` impl shipped; team avatars + adventure cover art go through the seam. Remaining: `adventure_page_images.content` (multi-row-per-entity with `alt_text` / `idx` metadata), and the actual non-DB impls (`-tags=storage_fs`, `-tags=storage_s3`)
- [ ] **Auth backends** (medium–large per backend) — OIDC / SAML / GitHub / Google / passkeys alongside the current username/password; magic-link slot flow stays core
- [ ] **Metrics exporter alternates** (small) — OTLP and StatsD alongside the Prometheus collectors in `internal/metrics`
- [ ] **Webhook formatter plugins** (small per integration) — pre-shaped Slack / Discord / MS Teams / Zapier deliveries on top of the generic signed-POST in `internal/webhooks`, so customers don't have to write a translator service
- [ ] **i18n language packs** (medium) — loadable bundles instead of strings baked into `i18n.js`; lets translators contribute without a JS rebuild
- [ ] **SCIM 2.0 provisioning endpoint** (large) — RFC 7643/7644 inbound user/group sync for corporate team-build and school-IT customers; lands rosters straight from Workday / Okta / Azure AD / Google Workspace
- [ ] **Per-org DNS aliases** (medium) — let companies reach the hosted product on a branded host like `my-company.zonventure.com` (and eventually CNAME'd `play.my-company.com`); needs an `org → hostname` mapping, host-aware routing, and automated TLS (Let's Encrypt / on-demand cert) for the wildcard + custom domains

## Product themes

Strategic gaps surfaced from a "what do users actually want?" review.
Each theme expands into several concrete tickets when picked up; kept
at theme level here so the directional choice ("coordination or
memory next?") stays visible above the implementation noise.

- [ ] **In-event team coordination** — Player — (medium) — chat / pings / "I'm here, where are you?" channel for split groups. Today there's no in-app channel between team-mates; the moment a team disperses 50 m apart they go silent and use SMS, which fragments the experience and pulls them out of the app
- [ ] **Post-event memory** — Player — (medium–large) — track replay, team-photo upload at puzzle locations, shareable highlight reel. The share-result button is the start; the full loop ("here's our team's day, in one page") is what converts one-off players into people who book again with their friends
- [ ] **Puzzle analytics** — Creator — (medium) — per-puzzle stall time, hint-reveal rate, completion histograms surfaced to arrangers ("puzzle 4 stalls 70% of teams"). Today they have raw audit logs and have to eyeball patterns; without this they can't iterate on adventure design
- [ ] **Template / shared puzzle library** — Creator — (medium–large) — fight the cold-start problem of "I just bought this product, now I owe my customer 12 puzzles by Saturday". A starter pack of public adventures or a remixable cross-org puzzle pool is what gets a new customer to first-event-shipped
- [ ] **Post-event report** — Both — (medium) — puzzle-level pacing + optional player survey + revenue tally on a single page sent to the arranger when the event completes. Both audiences benefit at once: arrangers know what to fix, players have somewhere to say it was great or that hint #3 was bad. Unblocks the data the analytics / library themes build on
- [ ] **Always-on adventures + public store** — Both — (large) — long-running adventures players can start whenever they want (no fixed event window, individual or self-formed teams, per-purchase progress), paired with a public-facing store that surfaces them with cover art, trailers, screenshots, descriptions, ratings, and a checkout flow. Today every adventure is bound to a scheduled event; this unlocks evergreen revenue for creators and "show up and play" discoverability for players, and it's what gives the template/library theme a customer-visible front door

## What was checked and is fine

- Refunds wire end-to-end (`stripe.go` `RefundPurchase` +
  `handleChargeRefunded` + `KindPurchaseRefunded`); partial-refund
  detection added in the recent audit round, full audit trail
- Player-side language picker (`play.html`, via
  `zvI18n.makeLangPicker`)
- Org-member roles validated and editable (`organizations.go`,
  `org_admin` / `org_member`)
- `embed`-based assets and migrations are clean; duplicate-prefix
  guard added in the recent audit round
- No SQL injection or auth-bypass smells in either round of audit
- Stripe webhook signature verification + `processed_webhook_events`
  dedupe in place; `IdempotencyKey` headers on every state-changing
  call from the recent audit round
- TOTP wrapped in AES-256-GCM at rest (`auth/totp.go`); recovery
  codes consumed in constant time (no early-exit timing leak)
- SSRF guard on outbound webhooks (DNS rebinding: pin literal IP at
  request time via `Transport.DialContext`)
- WebSocket origin check via `coder/websocket` default; no bare
  `OriginPatterns: ["*"]` anywhere
- Reverse-proxy trust gated behind `ZV_TRUSTED_PROXIES` —
  direct-reachable deploys can't be spoofed via `X-Forwarded-For`
- Service worker scope (`/`) is guarded by an explicit fetch-handler
  allowlist so future code can't accidentally widen interception

## Out of scope for now

- Mobile native apps. The whole point is "no app, just a phone browser".
- Multi-tenant billing. Single-org self-host vs hosted-with-Stripe is
  enough; per-org billing portals already exist via Stripe.
- General-purpose ticketing.
