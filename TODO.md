# Production-readiness scorecard

Findings from a code review of the repo. Grouped by **gap** (missing
feature) vs **simplification** (existing code that could be smaller),
ordered by recommended sequence. Each item lists rough size in
parentheses.

The hosted launch is gated on legal review, not on this list — but
clearing the small items before the launch reduces ongoing friction
for self-hosters and operators.

## Plugin / extensibility candidates

Functionality that's optional, vendor-flavoured, or replaceable, and
is a natural fit for an interface + drop-in implementations. The
direction is "thin core, named seams" — every entry below is sized as
"add a second concrete implementation behind a Go interface", not "build
a generic plugin runtime". Tackle one at a time.

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
