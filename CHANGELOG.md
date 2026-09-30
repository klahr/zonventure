# Changelog

All notable changes to Zonventure will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The repository, Go module, and CLI binary are named `zonventure`; the
product is **Zonventure**. Versions tagged here correspond to git tags
`<version>` on [github.com/klahr/zonventure](https://github.com/klahr/zonventure).

## [Unreleased]

### Added — a static site at /shop

- **zv serves the `shop/` folder of `ZV_WEB_OVERRIDE_DIR` at `/shop/`**,
  file for file, so an operator can host a small static site (the
  Zonventure book shop) next to the landing page. `/shop/book` serves
  `shop/book.html`, and unknown paths get `shop/404.html`. Without a
  `shop/` folder, `/shop/` is a 404 as before.

### Added — item slots

- **A puzzle can be answered with items from the backpack** (answer
  kind `items`, and "Item slots" in the add-step menu). Each slot
  wants one item, in order; the phone shows the slots' names and the
  backpack, never which item goes where. The server checks the fill
  and that the team carries those items, then takes the first answer
  route and — unless the author turns it off — uses the items up. A
  wrong fill is a wrong guess and costs nothing. The validator flags
  slots with no item or an undeclared one, and warns about an item no
  step ever gives.
- **A team without the items is told, and can go and find them.** The
  phone says something is missing — or, if the author chooses, which
  items — and a new route trigger, "Not carrying the items", shows as
  a button while that is so. Leaving records nothing for the puzzle,
  so it is presented again when the team comes back. The validator
  warns when that route leaves an ordinary hub's branch, which cannot
  be re-entered; a home screen's can.

### Added — reorder steps in the designer's list view

- **Steps can be dragged by a handle, or moved with ↑/↓, anywhere in
  the list** — on the main line, into a branch or out of one — not
  only in a straight-line adventure. A step where the flow splits
  stays put.

### Changed — the shared catalog is switched off

- **Listing an adventure in the catalog no longer opens it to every
  organisation**, and the publisher's puzzle library stays with the
  publisher. Other organisations reach its adventures only through a
  commission or an event pass the operator grants, so existing events
  keep running. The console hides the Catalog tab and its links, and
  old `#/catalog` URLs open Adventures. Curation still works; one
  switch on each side (`admin.CatalogEnabled`, and `CATALOG_ENABLED`
  in the console's router) turns the catalog back on.

### Added — API tokens for the REST API

- **The tokens under Settings → Account → API tokens now work on
  `/api/admin`**, not just `/mcp`. Scripts and agents send
  `Authorization: Bearer zvmcp_…` and act as the user who minted the
  token, with no session cookie or CSRF header. A `read` token may
  only use GET; mutations need `write`. The card is shown even when
  `ZV_MCP_ENABLED` is off; the MCP endpoint field only appears when
  it is on.
- **Account and credential routes stay session-only**: tokens get
  403 on token management, password and email changes, TOTP,
  sessions, account deletion, data export, the billing portal and
  admin password/TOTP resets.

### Added — redeem Live it and TrueStory gift boxes

- **Buyers redeem a gift box at `/redeem`** (or an organisation's own
  `/o/{slug}/redeem`): they pick the partner, enter the box's code (a
  Live it box number and security code, or a 9-character TrueStory
  experience code), choose the adventure and give a postal address.
  A code can be redeemed once; attempts are capped per IP.
- **Organisers approve each code** under **Box redemptions** after
  checking it in the partner's portal. Approval creates a team on the
  product's self-guided adventure and emails the team link, valid for
  a year; rejecting or cancelling emails the buyer and frees the code.
  Organisers can also track when to post the envelope, whether it has
  been posted, and a note, and enter phone or email orders by hand.
- **Org admins set up partners and products**: which partners they take
  boxes from, and which self-guided adventure (and team size) each
  product gives access to. Migration `0072_partner_redemptions`.

### Fixed — live monitoring

- **The public board's socket no longer drops every 40 seconds.** The
  server never read from it, so its keep-alive ping always timed out
  and the TV reconnected about 90 times an hour.
- **The public board no longer shows where the puzzles are** while the
  event can still be played. It frames the map on the play area
  instead and shows the puzzle pins once the event is over.
- **The completion screen crowns the top-ranked team**, by points for a
  designed adventure, rather than whoever finished first. When time
  runs out with no finisher, the leader is still named.
- **Race control keeps up during a busy event.** Pushes are coalesced
  to at most one rebuild a second per viewer, the page re-renders at
  most every 1.5 s and never stalls under a steady stream of updates,
  a slow render can no longer overwrite a page you have left, and the
  map keeps your pan and zoom.
- **Tracks stay small.** A team's trail is capped at about 120 points,
  widening the time buckets on long events, instead of resending
  every 10-second point on each push.
- **Elapsed time leaves pauses out**, on the public board and in race
  control, and both run on the server's clock, so a device whose clock
  is off no longer shows a wrong time. The board shows a pause banner.
  Migration `0071_event_paused_seconds` starts tracking pause time;
  pauses before the upgrade count as zero.
- **The public board shows every team.** With more teams than fit, it
  keeps the leaders pinned and pages through the rest every 10 s. A
  team keeps its colour when it changes place, and withdrawn teams
  rank last with a "withdrew" label.
- **The public board recovers on its own.** A heartbeat and a watchdog
  replace a socket left half-open (a TV waking from sleep, say), and
  the board polls until the socket is back. It also has its own rate
  limit bucket, so at a venue where everyone shares one address the TV
  and the players' phones no longer use up each other's allowance.
- **Race control shows how long since each team last did anything**,
  as "5m ago", from the server rather than a capped feed, so the
  quietest teams no longer show "—". The feed's filter for setup noise
  works again.
- **Race control draws from what the server pushes** instead of
  fetching it again, keeps one map whose markers and trails update in
  place (the view and an open popup stay put), and keeps the scroll
  position of its lists.
- **A watch stops when access ends.** Race control re-checks every 30 s
  that its user may still see the event; logging out or leaving the
  organisation stops the updates, and the page says so.
- The live board's footer shows a readable time instead of an ISO
  timestamp.

## [0.2.0] - 2026-09-28

### Upgrading from 0.1.x

- **Back up first.** Migrations 0056–0070 run on first start;
  `0065_catalog_publisher` and `0066_adventure_org_not_null` rewrite
  existing rows (see the breaking change below).
- **The platform admin no longer authors or runs events.** After the
  upgrade, add an org admin to the new **Zonventure Catalog** org to
  keep editing the catalog.
- **Moving to a new install** (e.g. running it alongside the old one):
  restore the 0.1.x dump into an *empty* database before 0.2.0 has
  started against it, and keep the same `ZV_TOTP_KEY`. A dump can't be
  restored on top of a database already migrated to 0.2.0.
- **Compose volumes are named after the checkout directory.** A clone
  into a directory other than the old one starts on an empty database.
- The repository moved from `git.sr.ht/~klahr/zv` to
  `github.com/klahr/zonventure`; update the checkout's remote before
  `make upgrade`.

### Added — twelve new blocks, and more ways to hide a clue

- **New blocks:** split secret (one part to each player's phone),
  bearing lock, shake to reveal, incoming call, suspect board, photo
  challenge, spot the difference, Morse flasher, letter tiles, team vote,
  live bulletin (the organisers' newest message) and random draw.
- **Photo challenges** are reviewed from the event's control page: an
  approved photo adds its points to the team's score, a denied one can
  be retaken, and the team moves on without waiting. Approved photos
  show on the team's recap. Photos and votes are pruned with location
  pings, and "Forget me" deletes a player's.
- **Options on existing blocks:** torch styles (scratch card, UV lens,
  red lens), a mirror-writing note, a Caesar wheel the team turns, and
  audio that plays backwards or shows its spectrogram.
- **Conditions:** "Metres walked on this step" and "Card drawn".
- **Hide the score:** an adventure setting keeps the team's running
  score off its phone. Points still count, and the end screen,
  standings and recap still show them.
- **Every block can be switched off** and kept for later, the title and
  points too. Title and points are still added to new steps, but can now
  be deleted, and deleting any block asks first.

### Changed

- A typed answer no longer has a QR button; only a QR scan puzzle scans.

### Changed — the classic console understands designed adventures

An adventure with a published graph plays the graph; its lane pages,
kept from before, are never shown to a team again. The console used to
show — and edit — those pages anyway.

- **Adventure and catalog pages** describe the published graph: its
  steps, puzzle count, zones on the map, places to reach and extent.
  The lane preview, the lane simulator and the classic editor button
  are gone for these adventures; authors get "Test play in designer".
- **Event page, race control and team window** show a graph team's
  points, place (the shared ranking, level teams sharing it) and
  current step; the activity feed names steps instead of "puzzle 1";
  race control's map draws the graph's zones. The event detail payload
  gains `event.engine`, per-team `score`/`rank`/`node`/`outcome`, and
  `graph` (step titles, puzzle count, zones); positions gain `node`.
  The HTTP and live-push event detail are now built by one function.
- **PDF report and team recap** rank graph events by points, like the
  CSV; the recap shows points, rank, the steps reached, zone pins and
  the quiz walk.
- **Copying, exporting and importing an adventure carry its graph**
  (published and draft) and the media it references. Exports gain the
  optional `graph` and `graph_media` fields; older exports import as
  before.
- **Lane edits to a graph adventure are refused** (409 `graph_adventure`)
  over HTTP and MCP, and the classic editor says why. "Skip this
  puzzle", a lane move that could finish a graph team behind the
  engine's back, is refused for graph events and no longer offered.

### Removed — the puzzle library

The shared puzzle library (`#puzzles`, `GET/POST /api/admin/puzzles`,
`GET /api/admin/puzzles/{id}`, `POST …/pages/copy-from/{id}` and the
editor's "Copy from library…") is gone. Library puzzles stay in the
database, unreachable.

### Fixed — "Call a webhook" and "Send an email" steps are delivered

- **Webhook and email steps were never sent.** Passing one queued a row
  in `graph_effect_outbox` that nothing read. A worker
  (`internal/effects`) now sends them after the team's move commits,
  retries failures with backoff for about a day, and gives up after six
  attempts (migration `0069`).
- **An email step has a subject and a message** in place of the
  "Template" name, which named nothing. It goes to the team's contact
  email through the configured mailer (`ZV_SMTP_*`); `{{team}}`,
  `{{event}}` and `{{step}}` are filled in when it is sent. A team
  without a usable address is given up on at once, not retried. An
  email step missing either field fails validation
  (`effect.email.missing`), and an unknown placeholder is a warning
  (`effect.email.placeholder`).
- **Method and headers.** A webhook step has a method (GET, POST — the
  default —, PUT, PATCH, DELETE) and custom headers, such as an
  Authorization token. POST, PUT and PATCH send the team and step as
  JSON; GET and DELETE send them as query parameters. Every request
  carries `X-Zv-Delivery-Id`, the same on retries. Header values are
  stored in the adventure and readable by anyone who can open or export
  it.
- **No calls to the server's own network.** The host is resolved and
  pinned at send time and must be public; `localhost` is refused unless
  `ZV_EFFECT_WEBHOOKS_ALLOW_LOOPBACK=1` (development only).

### Added — content blocks on graph steps

A puzzle's, story's and end's content is an ordered list of blocks the
author arranges in the inspector (drag the handle, or arrow keys),
instead of one text and a media list. The answer stays below them.

- **Kinds.** Text, note (paper, telegram, text message, newspaper,
  sticky), picture, sound, video, divider; compass (distance optional),
  map (fixed or fitted zoom, the team's position optional, labelled
  markers), hot-and-cold radar; chat, cipher sheet (Morse, pigpen,
  semaphore, Caesar, custom), hotspots, torch; look-through camera
  overlay, level, noise meter; countdown, status (items, counters),
  and standings.
- **Title and points are blocks** every puzzle has (a story, its title):
  added where missing when a graph is read, moved but never removed.
  Points show what the best answer is worth.
- **Quiz walk.** A quiz block (1, X, 2) and one tie-breaker (closest
  number) per adventure. A pick can change until the team leaves the
  step, then it is final; picks are stored in
  `team_block_picks` (migration `0068`); a story waits for its
  questions; the right answers stay off the phone until the team
  finishes, when right answers score and the end shows the review.
  Among finished teams on the same score, the closer tie-break guess
  ranks first. New player action `pick`.
- **Graph events rank by points everywhere.** One rule (`play.Ahead`) —
  points, then the tie-breaker between finished teams, then the earlier
  finish — ranks the standings block, the live leaderboard and the
  results CSV. The leaderboard shows a graph team's points, and its
  tie-break guess once the event is over; the CSV has `points`,
  `tiebreak_guess` and `tiebreak_off_by` columns. Both used to rank
  graph events by lane pages solved, which graph play never counts.
- **Show only when…** Any block can wait for a condition — near a zone,
  a while after arriving, carrying an item — optionally showing a
  teaser meanwhile. The server withholds a hidden block and its file;
  `recheck_at` and `watch_position` tell the phone when to ask again.
- **Tokens.** `{{score}}`, `{{counter:<slug>}}`, `{{item:<slug>}}` and
  `{{team}}` in texts.
- **Old graphs keep working.** A step's `prompt`/`text` and `media` are
  read as blocks (media first, as shown before) wherever a graph is
  decoded, and their translations move to the text block's key. Graphs
  are written back as blocks only. The player view still carries a
  flattened `text` and `media` for phones on the previous bundle.
- **Video uploads**, capped by `ZV_MAX_VIDEO_BYTES` (default 30 MiB),
  are never prefetched. `/media/{id}` now answers Range requests.
- **Organiser messages.** Information to an event's teams, beside the
  game rather than in the adventure: to every team or chosen ones,
  shown in the player app's inbox (a bell with an unread count, and a
  banner as one arrives). `GET`/`POST /api/admin/events/{id}/messages`,
  `DELETE …/messages/{messageID}`, and the MCP tools
  `list_event_messages`, `send_event_message`, `delete_event_message`;
  migration `0067_event_messages`.
- Map tiles still come from `tile.openstreetmap.org`; move to a tile
  provider before maps reach players at scale.

### Added — choosing how a puzzle is answered

- **Answer kind picker.** Every puzzle's inspector has an answer part —
  typed ones too — that starts with *How the team answers*: typed, QR
  scan, options, lock, tile picture, search, or the step's quiz
  questions.
  Switching keeps the routes' ports, so the canvas stays connected;
  typed answers become a choice's options, and kinds that need no
  matchers drop them.
- **Puzzles answered by their quiz questions** (`answer.kind:
  "questions"`, preset *Quiz post*). The answer bar becomes a button,
  "2 of 3 answered" until every shown question has a pick, then
  Continue, which takes the first answer route. A quiz walk post can now
  have a zone, time limit, hints and a timeout route. Continuing early
  is refused with `unanswered` and is not a wrong guess.
- **The story's Continue** counts the same way instead of waiting for
  the server to refuse it.
- **The answer bar's own settings** (`puzzle.bar`), for a typed
  answer: placeholder, button label, keyboard (automatic, letters or
  numbers — the author's choice wins over the guess from the answers)
  and whether the QR button shows. Placeholder and button translate
  under `bar:placeholder` and `bar:button`.
- **Scan-only puzzles** (`answer.kind: "scan"`). The bar is one labelled
  scan button; the decoded text goes through the routes' answers as a
  typed guess does. "Type the code instead" falls back to the box when
  the camera fails.
- **A box per part.** A list answer of two to six parts gets a box for
  each, joined with commas as the answers split them, instead of one
  comma-separated line.
- **Validation.** A warning for quiz questions on a puzzle answered
  another way (the team can leave them behind), for a questions puzzle
  with no questions or more than one answer route; an error for quiz
  questions on an end, which is marked before they could be answered.
  A warning for answer-bar settings on a puzzle that shows no bar, and
  an error for an unknown keyboard.

### Changed — platform admin no longer authors or runs events (breaking)

The global admin is now purely the operator: it manages organisations
and watches the system. Authoring and running events belong to
organisations.

- **Admin is refused on content and event routes.** `/api/admin`
  adventure, graph, puzzle, page, media, event, team, webhook,
  assignment-list and simulator routes, `POST /purchases` and
  `/ws/arrange` answer the admin with 403. MCP tools carry an audience:
  an admin token only sees and calls `grant_slots_to_event`,
  `list_purchases`, `get_purchase` and `describe_event_lifecycle`.
- **The catalog has a publisher org.** Catalog adventures used to be
  the admin's ownerless (`org_id IS NULL`) rows; now they belong to the
  one org flagged `organizations.is_publisher`, whose org admins author
  them. Migration `0065_catalog_publisher` moves existing ownerless
  adventures and library puzzles into a new "Zonventure Catalog"
  publisher org; `0066_adventure_org_not_null` then makes
  `adventures.org_id` mandatory and requires library puzzles to have
  an owner.
- **Curation moved to the operator's endpoints.** New
  `GET /api/admin/catalog`, `PUT /api/admin/catalog/{slug}/published`,
  `/api/admin/catalog/{slug}/shares` (replacing
  `/api/admin/adventures/{slug}/shares`) and
  `PUT /api/admin/organizations/{id}/publisher`. Authors can no longer
  set `published` via `PATCH /api/admin/adventures/{slug}` or
  `update_adventure_meta` (400). Event passes and commissions only
  accept catalog adventures. The publisher org can't be deleted while
  it publishes.
- **Operator event actions moved to the org page.**
  `GET /api/admin/organizations/{id}/events` (no live token) and
  `POST /api/admin/organizations/{id}/events/{eventID}/cancel`. The
  admin console's sidebar is Dashboard, Organizations, Logs and
  Settings; an org's page shows its events (Grant slots, Cancel), the
  catalog it publishes, and the adventures commissioned for it.
- **`make seed`** also creates a `publisher` / `publisher` org admin
  (`-seed-publisher`) in the publisher org, which owns the demo
  adventure.

### Fixed — authoring gates

- Creating adventures (form, import, MCP `create_adventure`) and
  library puzzles now requires org admin; org members and users
  without an org were let through, the latter creating ownerless
  catalog rows.
- `GET /api/admin/adventures` and `/puzzles` no longer crash for a
  signed-up user without an organisation.
- Unpublished catalog adventures could be bought, duplicated, read
  and copied from by any org; they are now visible only when listed,
  shared or assigned.

### Hardening — vendor pinning, CSP, durable submits

A round of polish on the player + arranger surfaces. Each item closes
a specific footgun the previous shape exposed.

- **Self-hosted Leaflet (with lazy loader).** Both the live board and
  the recap previously pulled `leaflet@1.9.4` from `unpkg.com` on
  page-open; an unpkg outage or a hostile CDN swap would have either
  broken the live view mid-event or — worse — let an attacker inject
  script into a logged-in arranger session. Bytes now live under
  `/assets/leaflet/` (JS + CSS + the marker / layer PNGs the CSS
  references via relative paths) and ship from the Go binary. The
  arranger surface uses a new `ensureLeaflet()` helper that injects
  the script on first map open — most arranger views never touch a
  map, so the 150 KB cost is paid once per session by the views
  that actually need it. Same helper landed earlier in `play.html`;
  the previous `unpkg` URL there moved to the self-hosted path
  alongside it. All map-using arrange functions
  (`mountAdventureMap`, `initRunMap`, `renderPuzzleLocationMap`,
  `openFullscreenMap`) wrap their bodies in `ensureLeaflet().then()`
  and return synchronously-callable handles so the caller's
  `.remove()` / `.invalidate()` keeps working before Leaflet has
  arrived.

- **In-app `confirm()` / `prompt()` across the arranger surface.**
  Twenty-odd destructive actions (refund, void, lane delete, slug
  rename, mid-event slot rotate, brand-asset removal, 2FA off,
  recovery-code regenerate, …) used the browser's native
  `window.confirm` / `window.prompt`. Mobile browsers style those
  full-screen on top of the page chrome, killing the surrounding
  context the user just chose to act from. Two new helpers,
  `inAppConfirm` and `inAppPrompt`, render through the existing
  `modal()` chrome (focus trap, Esc handling, back-button
  integration). The modal's `setBeforeClose` hook also learned to
  await Promises, so dirty-check guards like "Discard unsaved
  changes?" can use the same async helper without losing their
  block-the-close contract. Power-user keyboard shortcuts (Enter to
  submit a prompt, Esc to cancel) are wired up.

- **At-least-once player submit outbox.** The player surface kept
  queued answer/advance frames in `pendingAnswers` until
  `ws.send()` returned success. That return only proves the OS
  buffer accepted the frame, not that the server processed it — a
  socket that died mid-flight could lose an answer with no error
  path. Submissions now stay on disk until the server explicitly
  acks the matching `client_action_id` on the
  `answer_result` / `answer_error` channel. The server already
  deduped retries via `player_action_dedup`; this change extends
  that contract end-to-end so a retried frame replays the cached
  response. Reconnect re-sends every queued entry; the new
  `ackPending(actionID)` helper drops the matching outbox row when
  the server confirms. Three new unit tests in
  `play-queue.test.js` exercise the ack path.

- **Tightened CSP.** The global header dropped its
  `https://fonts.googleapis.com` / `https://fonts.gstatic.com`
  allowances now that the webfonts ship from `/assets/fonts/*`.
  Player and public-facing pages (play, live, recap, landing,
  pricing, privacy, terms, manual, store, store-event,
  org-landing, 404) gained a stricter `<meta http-equiv>`
  Content-Security-Policy that drops the `unpkg.com` allowance
  entirely — those surfaces no longer load anything from a CDN.
  The `/arrange` admin surface keeps the looser baseline because
  xterm + xterm-addon-fit are still pulled from unpkg; browsers
  intersect the meta and header policies, so a stricter
  per-page meta wins over the header for the surfaces that
  don't need the looser allowance.

- **`server.go` split into `routes_*.go`.** The 1470-line monolith
  shrank to 660 lines; the route registrations moved into
  `routes_pages.go`, `routes_assets.go`, `routes_public.go`,
  `routes_auth.go`, `routes_player.go`, and `routes_admin.go`,
  one helper per surface. `New()` keeps the middleware chain and
  the per-package handler construction; a private `routesEnv`
  struct bundles the captured deps so each helper signature is
  just `(r chi.Router, env *routesEnv)`. Route patterns don't
  overlap, so the call order in `New()` is purely for
  readability — chi treats each pattern independently. Tests
  pass unchanged.

### Added

- **Stripe Connect marketplace flow.** The hosted instance can now
  charge a per-ticket platform fee on /store sales and route the
  rest of the money straight to the selling org's Stripe account.
  Self-host stays inert — the entire path is gated on
  `ZV_PLATFORM_FEE_PERCENT_BPS` / `ZV_PLATFORM_FEE_MIN_CENTS`,
  both default 0, no Connect onboarding required, no behaviour
  change on existing deployments. Fee formula is
  `max(percent_of_price, floor)` so a flat €5 doesn't crush an €8
  ticket and a flat percentage doesn't trail off on a €120
  corporate ticket; matches Eventbrite / Tito conventions.

  Implementation lives behind a new optional `MarketplaceProvider`
  interface that extends `payments.Provider` with three methods:
  `CreateConnectedAccount`, `CreateAccountLink`, `GetConnectedAccount`.
  Stripe satisfies both interfaces (compile-time-asserted); other
  providers (the simulated dev impl, future Mollie / Paddle if/when
  they land) only satisfy Provider and are unaffected. Marketplace
  bits on `CheckoutParams` (`ConnectedAccountID`,
  `ApplicationFeeCents`) and `RefundParams` (`RefundApplicationFee`,
  `ConnectedAccountID`) are ignored by non-marketplace providers.
  The Stripe impl wires `PaymentIntentData.{TransferData,
  ApplicationFeeAmount}` for destination charges and
  `RefundApplicationFee + ReverseTransfer` for proportional
  application-fee clawback on refunds.

  Schema in `0049_stripe_connect.sql`: `organizations` gains
  `stripe_connect_account_id` (unique partial index),
  `stripe_connect_charges_enabled`, `stripe_connect_payouts_enabled`,
  `stripe_connect_synced_at`. `ticket_purchases` gains
  `application_fee_cents BIGINT NOT NULL DEFAULT 0` so the per-row
  fee is captured at checkout creation (not at webhook time) — a
  fee-formula change later doesn't retroactively alter what was
  charged on already-completed sales.

  Org-side onboarding is **lazy** — quest creation works without
  Connect (the quest just stays hidden from /store and /store/buy
  refuses with 409 until the org finishes setup). Three new admin
  endpoints under `/api/admin/organizations/{orgID}/stripe-connect/`:
  `start` (idempotent: creates the account on first call, mints
  fresh hosted-onboarding URL on every call), `status` (cached
  read), `refresh` (force-syncs from Stripe). All three gated to
  org_admins of the named org plus global admins.

  Settings → **Billing** is the new tab where the arranger
  manages it. Four states are surfaced explicitly: marketplace
  off (self-host), no account yet, onboarding incomplete, and
  connected & accepting. The `?stripe_connect=return` query
  param the Stripe redirect URL appends triggers a forced
  server-side sync + welcome toast on landing. Quest detail
  pages get a "Stripe Connect required" banner when the seller
  org isn't onboarded — owner org_admins see an actionable deep
  link to Settings → Billing; cross-org viewers see a softer "the
  seller org needs to finish setup" line.

  The /store listing query and `/store/buy` are both
  marketplace-aware: when the platform fee is configured, the
  WHERE filters out un-onboarded quests, and BuyTicket forwards
  the connected account id + computed application fee into the
  Checkout call. `account.updated` webhook events update the
  cache directly from the payload (no extra Stripe API call) so
  the listing reflects Stripe's reality without waiting for the
  manual refresh button. Operator runbook for turning the flow
  on lives at the bottom of the Stripe Connect section in
  SELFHOSTING.md.

- **Quests** — a tailored arranger surface for always-on, ticket-
  purchased events. Backend rows are still `events` with
  `public_for_sale=true`; the new "Quests" sidebar entry partitions
  them into a dedicated list (`#quests`) so the regular `#events`
  page stays focused on operator-scheduled events the org runs
  itself. ListEvents gains a `?public_for_sale=true|false` filter
  param backing the partition; the Events page passes `false`,
  the Quests page passes `true`.

  Quest creation is a single form (adventure picker, name,
  blurb, ticket price, currency, players-per-ticket, attestation,
  optional cover image) that chains POST /events → PATCH
  public-store fields → optional cover upload → POST /events/start
  in one click. The quest auto-starts so it's immediately listed
  at /store; teams arrive later via ticket purchases. The
  empty-team start guard is relaxed for `public_for_sale=true`
  events — without this, every quest would 409 at start since
  there's no team to add yet.

  `public_for_sale` is now an **immutable type marker** set at
  CreateEvent — UpdateEvent refuses to flip the flag with 409 so
  an arranger can't silently turn a regular event into a quest
  mid-flight (or vice versa). Quest detail pages reuse the
  existing event-detail view with rebranding: a "Quest" pill in
  the lifecycle bar, the Reschedule action hidden (quests are
  always-on), and a quest-only edit panel for ticket price /
  blurb / cover (the legacy "List on /store" toggle on the event-
  detail page is gone — it was a footgun once the type became
  immutable). Visibility on /store is state-driven: pause to
  delist temporarily, cancel to kill.

  MCP coverage: `create_event` accepts `public_for_sale` so an
  LLM can author quests; `start_event` inherits the relaxed
  empty-team guard automatically. `describe_event_lifecycle`
  documents the differences in the transition table and a
  dedicated "Quests vs regular events" section. The whole MCP
  surface is uniform across regular events and quests since
  they're the same DB rows differing only in the type marker.

- **Simulated payment provider.** Second `payments.Provider` impl
  alongside Stripe, picked via `ZV_PAYMENTS_PROVIDER=simulated`.
  Auto-confirms checkouts in-process: the buyer hits a tiny
  `/dev/payments/checkout/{sid}` page, clicks "Pay (simulate)", the
  server fires a synthesised `payments.Event` into its own webhook
  endpoint, and the rest of the flow runs end-to-end — team
  created, magic links emailed, dedupe + idempotency exercised.
  Strictly dev-only (no signature verification, no real money). Its
  primary purpose is validating that the abstraction works: the
  same handler code consumes events from both providers without
  importing either SDK. Lives at `internal/payments/simulated/`.

### Changed

- **Payment-provider abstraction.** Stripe is now a concrete impl
  behind a `payments.Provider` interface (`internal/payments`). The
  rest of the app no longer imports `github.com/stripe/stripe-go`
  — checkout, refund, webhook ingest, and customer-portal calls all
  flow through the four-method interface, and `stripe-go` itself is
  confined to `internal/payments/stripe/`. Future Mollie / Paddle /
  Adyen impls drop in next to it; cmd/server picks one at startup
  based on env, and the rest of the wiring is untouched. The admin
  webhook route is renamed to `PaymentsWebhook` (still mounted at
  `/api/webhooks/stripe` for back-compat with operator-configured
  Stripe URLs); `Handlers.StripeSecretKey` / `StripeWebhookSecret`
  fields are replaced by `Handlers.Provider`. No schema changes —
  the DB columns keep their `stripe_*` names for now and a future
  multi-provider deployment can either widen their meaning or
  alias-rename per provider.

### Added

- **Always-on adventures with public ticket sales.** A new public
  store page at `/store` lists every event with `public_for_sale=true`
  as a buy-card. A buyer types their email, pays via Stripe Checkout,
  and the webhook creates a single team in the event and emails the
  magic links to the buyer. The event itself is just an ordinary
  event the operator manually started without a `scheduled_at`,
  flipped public, and priced — no new event mode, no per-purchase
  progress primitive, no separate "always-on" lifecycle. The buyer
  becomes the team's "captain" and uses the existing team-share QR
  to bring teammates in. Schema in `0047_event_public_store.sql`:
  `public_for_sale`, `ticket_price_cents`, `ticket_currency`,
  `ticket_team_size`, `public_blurb` columns on `events` plus a
  `ticket_purchases` ledger keyed on `stripe_session_id` for
  redelivery dedupe. Per-event hero image lives on the same row
  via `0048_event_cover_image.sql` (`cover_image_*` columns) and
  is served at `/store-cover/event/{id}`; uploads route through
  `imageutil.NormalizeUpload` so the same MIME / SVG / shrink rules
  the rest of the app uses apply uniformly. Endpoints:
  `GET /api/store/items`, `GET /api/store/items/{event_id}`,
  `POST /api/store/buy`, plus admin
  `POST /api/admin/events/{id}/cover` + `DELETE` companion. The
  arrange UI gains a "Public store" panel below the live-view bar
  on every event detail page: cover upload, price + currency +
  team-size + blurb form, one-click "List on /store" toggle.
  Refund handling reuses the existing `charge.refunded` webhook —
  a refunded ticket flips its row to `refunded` and withdraws the
  team it created, so the buyer can no longer act on the magic
  link. Each adventure also gets its own server-rendered detail
  page at `/store/event/{id}` with proper OpenGraph metadata,
  product price markup, and a sticky buy card; this is the
  canonical buy surface, and the listing cards now link straight
  to it. Marketing override seam covers both pages: drop custom
  `store.html` and `store-event.html` into `ZV_WEB_OVERRIDE_DIR`
  and the embedded brand-neutral versions are shadowed at request
  time, same shape as `landing.html` and `pricing.html`. The
  branded variants in the sibling `zv-web` repo carry the hosted
  visual language (Aclonica heading, brand-pink hero gradient,
  hero photo, "how it works" strip, FAQ). Both pages — embedded
  and override — are localised English / Swedish via the shared
  `landingStrings` table; ~50 new keys cover hero copy, "how it
  works" steps, card vocabulary, FAQ Q&A, buy-form labels, JS-
  side toasts and validation. A `{n}` placeholder convention lets
  team-size phrasing live in the locale string ("Up to {n}
  players" / "Upp till {n} spelare") and gets resolved per-event
  server-side. The hosted `zv-web/landing.html` now also surfaces
  a Store link in the topbar, mobile overlay, and footer.
- **MCP connector.** New opt-in `POST /mcp` endpoint exposing
  adventure authoring as Model Context Protocol tools so an
  arranger can connect Claude desktop / claude.ai / other MCP
  clients and author adventures by tool call. Eight tools cover
  full CRUD: `list_adventures`, `get_adventure`,
  `validate_adventure` (dry-run), `create_adventure`, `add_puzzle`,
  `update_adventure_meta`, `set_lane_mode`,
  `delete_adventure` (two-phase, requires `confirm:true`). Per-user
  bearer tokens minted from **Settings → Account → MCP API tokens**;
  plaintext shown once, stored as `sha256(token)` in a new
  `mcp_tokens` table (migration `0045_mcp_tokens.sql`). Off by
  default — operators set `ZV_MCP_ENABLED=1` to mount the route.
  Tool calls inherit the minting user's authoring policy
  (`canAuthor` / `canSeeOwned`) so an MCP session can't reach
  beyond what the user could do in `/arrange`. Binary assets
  (image / audio bytes) are intentionally NOT supported via MCP —
  the model writes alt-text placeholders and the user attaches
  files via the arranger UI. See [MCP.md](MCP.md).
- **MCP: run a whole event without visiting the web.** The
  connector grows from 24 authoring tools to 32 covering the full
  event lifecycle, so an arranger can author → buy slots → schedule
  → invite → start → observe → wrap up an event entirely through
  an MCP client. New phases:
  - **Lifecycle (8 tools):** `list_events`, `get_event`,
    `create_event`, `update_event`, `start_event`, `pause_event`,
    `resume_event`, `cancel_event` (password-gated).
  - **Teams + slots (10 tools):** `create_team`, `bulk_create_teams`
    (JSON; multipart import stays web-only), `update_team`,
    `delete_team` (password-gated), `withdraw_team`,
    `unwithdraw_team`, `unlock_team`, `add_slot`, `remove_slot`,
    `rotate_slot_token`.
  - **Observation (4 tools):** `list_positions`, `list_activity`,
    `list_tracks`, `get_event_state_summary` (synthesised
    poll-friendly leaderboard for "wait until done" loops).
  - **Billing (5 tools):** `list_purchases`, `get_purchase`,
    `create_purchase` (provider hard-coded to `simulated`; refused
    on Stripe-enabled deployments — real-money buys stay in the
    web checkout), `confirm_purchase`, `grant_slots_to_event`
    (admin-only, re-checked at the MCP layer since the route gate
    is bypassed).
  - **Invitations (3 tools):** `send_event_invitations`,
    `send_team_invitation`, `get_event_invite_ics`. Real emails go
    out under the existing org rate limit; descriptions flag
    non-idempotency.
  - **Polish (2 tools):** `export_event_results_csv` (CSV inline;
    recap PDF stays web-only), `describe_event_lifecycle` (static
    Markdown reference covering the state machine, edit-permission
    matrix, polling cadence, and security gates so the model can
    plan transitions without trial-and-error).

  Every new tool delegates to the existing `*admin.Handlers`
  method via a thin in-process adapter (`internal/mcp/adapter.go`),
  so policy, audit, webhook and WebSocket publish work runs in
  exactly one place — no parallel implementation. `mcp.New()`
  gains a fourth argument (`*admin.Handlers`); update embedders.
- **MCP token scopes.** `mcp_tokens` gains a `scopes` JSONB
  column (migration `0046_mcp_token_scopes.sql`); the token-mint
  modal exposes two checkboxes (`read` / `write`). Existing tokens
  default to `["read","write"]` so back-compat is invisible.
  Read-only tokens are useful for dashboards / monitoring clients
  that should never mutate state. Scopes only narrow what the
  underlying user can do — they never grant authority beyond the
  user's role. Tools panic at startup if registered without a
  scope so a new tool can't ship ungated.

### Security (round 2)

- **Webhook SSRF closed.** `validateWebhookURL` now resolves the
  registered host and rejects any IP in the loopback / private /
  link-local / metadata ranges (incl. AWS 169.254.169.254, IPv6
  ULAs, IPv4-mapped loopback, CGNAT, IETF benchmarking). The same
  gate runs again at delivery time so a DNS rebind between
  registration and dispatch can't pivot the worker. Delivery
  client also refuses redirects (`CheckRedirect → ErrUseLastResponse`)
  so a clean public URL that 302s to an internal one fails closed.
- **Webhook delivery body is bounded.** The worker drains at most
  64 KiB of the customer's response before closing — a slow-loris
  / huge-body endpoint can no longer monopolise a worker for the
  full 30 s timeout.
- **ZIP-bomb defence on ods / xlsx imports.** ODS pre-flight rejects
  `content.xml` whose declared uncompressed size exceeds 50 MiB and
  trips a streaming counter on the actual reads; xlsx passes
  `excelize.Options{UnzipSizeLimit: 50 MiB}` so excelize bails on
  decompression-bomb archives.
- **Server-side empty-event guard.** `transitionEvent("start")` now
  rejects events with zero teams (409). Closes a bypass where a
  stale UI cache or scripted client could slip past the new
  client-side guard.
- **Seed flow stamps `password_is_default`.** `cmd/server/seed.go`
  upsertUser now sets the flag for well-known seed passwords (admin,
  rootpw, s3cret, jane), so the in-app red banner fires on demo /
  compose-seeded installs too — not just ZV_SEED_ADMIN flows.
- **QR-share URL builder requires `ZV_PUBLIC_URL`.** Dropped the
  `r.Host` fallback (same poisoned-Host attack as the email links).
  Returns 503 when unset rather than printing a QR pointing at
  whatever the request claimed the host was.
- **Session inactivity timeout.** Lookup now bumps `last_used_at`
  on each request (throttled to 1 write/min/session) and rejects
  sessions whose last-use is older than 7 days regardless of
  `expires_at`. A leaked cookie now expires from real-world
  inactivity, not just from the absolute 30-day TTL.

### Changed

- **Mid-edit save warning is a styled modal.** Replaced the native
  `confirm()` with a proper modal that calls out the categories of
  change (typo / logic / disable-or-reorder) and explicitly tells
  the arranger which kinds are safe vs. risky on a live event.
- **Pause needs confirmation.** Pausing an active event is now
  gated by a small modal that explains what players will see; one-
  tap pause was too easy to misclick on a mobile dashboard.
- **Member-removal warns on owned events.** Removing an org member
  who created scheduled / running / paused events surfaces the
  count and a "remove anyway" / "cancel" choice. Prevents
  accidentally orphaning live events from the audit trail.
- **Live map drops team-name popup.** The Leaflet popup that
  exposed full team names on the public spectator map is gone —
  initials still show on the marker, but pairing precise GPS with
  a child's name (school events sometimes use first names) is
  closed off. Arrangers who need full names + positions still
  have the staff `/api/admin/positions` view.
- **CSV-injection defang on roster import.** Names that start with
  `=`, `+`, `-`, `@`, tab, or `|` get a single-quote prefix at
  parse time so a re-export to CSV (clipboard, leaderboard
  download) doesn't smuggle a formula into Excel/Sheets/Numbers.
  Applied uniformly across csv, ods, xlsx, and IOF-XML importers.
- **Hint reorder via up/down arrows.** Each hint row gains
  Move-up / Move-down buttons so an arranger can rearrange hints
  without delete-and-recreate (which lost any attached images /
  audio). Disabled at the ends; preserves the hint id so attached
  media survives.

### Security

- **GPS plausibility check.** `recordLocation` now computes implied
  speed against the team's previous ping and rejects anything above
  60 m/s. Without this gate, the gateReached check (which trusts the
  latest ping) was trivially bypassable: a player at the start could
  POST "I'm at the gate" coordinates and walk through every gated
  puzzle from a chair. Real GPS jitter clears the bar; teleport
  doesn't.
- **Hint text gated by reveal in player snapshot.** `buildHintDTOs`
  no longer ships the rendered hint body for hints below the team's
  reveal threshold. Devtools can no longer read every locked hint
  off the state DTO; a fresh reveal returns the body in the
  `RevealedText` field of the HTTP response so the UI paints
  instantly without waiting for the next WS state push.
- **Open redirect via `X-Forwarded-Host` closed.** Email links
  (password reset, email verify, email change) and the team-share
  QR URL now read only from the operator-set `ZV_PUBLIC_URL`,
  never from request headers. An attacker behind a hostile
  reverse proxy can no longer inject a phishing host into outbound
  email bodies.
- **Audio uploads are MIME-sniffed server-side** with
  `http.DetectContentType` instead of trusting the client header,
  plus `X-Content-Type-Options: nosniff` on the serve route. Closes
  the polyglot path where an HTML+audio blob could land under the
  audio table with a forged `audio/mpeg` Content-Type.
- **Default-credentials warning banner.** Every page now shows a
  red "Change your password now" banner when the seeded admin
  account still holds `admin/admin` (or `root/rootpw`). Cleared
  the moment the user changes their password through any of the
  password-update paths.
- **WebSocket Origin check is documented load-bearing.** Inline
  comment on `ws.Accept` warns future contributors not to disable
  the default Origin check by passing `OriginPatterns` /
  `InsecureSkipVerify` — the same-origin gate is the WS CSRF
  equivalent.
- **Magic-link exchange is rate-limited.** `MagicLinkLogin` now
  goes through a per-IP burst limiter (20 / minute) so a scripted
  client can't pummel the audit log + DB lookup probing tokens.
- **Live board's GPS map stays off by default**, with an
  explanatory comment near the event-create INSERT so a casual
  refactor can't accidentally enable it. Competing teams who get
  the live URL no longer see opponents' real-time positions
  unless the arranger explicitly flips the toggle.
- **Server-side validation of legal pages.** `/privacy` and
  `/terms` now return 503 with a list of missing env vars when
  `ZV_COMPANY_NAME` / `ZV_CONTACT_EMAIL` / `ZV_JURISDICTION` are
  unset, instead of shipping `[set ZV_COMPANY_NAME]` placeholder
  text to real users.

### Added

- **Lost-magic-link recovery.** Players who closed the tab or
  switched devices can now request a fresh link from the no-session
  screen by entering the team's contact email. New endpoint
  `POST /api/player/recover-link` rotates magic tokens (clearing
  `claimed_at` so the new link is single-use again) and emails the
  fresh links. Always returns 204 to avoid email enumeration.
- **Empty-event start guard.** Tapping Start on an event with zero
  teams now opens a clear "Add at least one team first" modal
  instead of letting the request hit the server and returning a
  generic toast.
- **GPS permission-denied recovery copy.** The play surface's GPS
  help block now distinguishes "permission denied" (no Retry — the
  browser won't re-prompt) from "no signal" (Retry button works),
  with platform-agnostic settings guidance and the arranger-
  override fallback path spelled out.
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

### Reliability — three load-bearing gaps closed

Three places where the existing code was correct in the small but
fragile under load. Each is now sound.

- **Webhook delivery worker no longer holds a Postgres row lock
  across the customer HTTP call.** New migration
  `0054_webhook_delivery_lease.sql` adds `in_flight_until`. The
  worker's `tick` is split into two transactions: claim+lease
  (commits in milliseconds) → customer HTTP without any DB
  connection held → outcome write. A slow-loris customer endpoint
  costs one goroutine + http connection until its 30-second
  timeout, not a Postgres backend pinned for the full window.
  Multiple workers can now coexist (`SKIP LOCKED` + the lease
  guard against double-claim); a worker that crashes mid-send
  leaves the row reclaimable after `leaseWindow` (5 minutes).
  Consumers were already required to dedupe on
  `X-Zv-Delivery-Id`, so a duplicate from a stale lease behaves
  the same as a duplicate from existing redelivery.

- **Durable mail outbox.** Migration `0055_mail_outbox.sql` plus a
  new `internal/mailer/outbox.go`. The post-Stripe ticket-fulfilment
  magic-link send now enqueues to the outbox inside the
  fulfilment transaction; a worker drains the queue with the same
  retry shape as webhooks (1m → 5m → 30m → 2h → 8h → 24h, six
  attempts). Earlier a transient SMTP outage could leave a paid
  buyer stranded with no UI to trigger a resend; now they get the
  link automatically when the relay recovers. Other interactive
  mail flows (signup verification, password reset) keep direct
  `Mailer.Send` calls because the user has UI-driven retry there.
  Falls back to direct `Send` when no outbox is configured (early
  dev setups).

- **live.html DOM reconciliation by `team_id`.** The audience
  leaderboard previously rebuilt every team row on every WS push
  (`replaceChildren` + FLIP capture). On a 30-team TV with rapid
  position pings that meant hundreds of full board rebuilds per
  minute. The render path now reuses rows whose
  `teamRowFingerprint` (rank, page-index, finished, name, avatar,
  members, page count, current puzzle title) is unchanged — DOM
  destruction drops from "all rows" to "only changed rows", and
  in-flight CSS transitions (the progress-bar fill, FLIP) survive
  across renders. Stays smooth at 30+ teams.

### Security & correctness round

A round of audit work landed a set of bugs, hardening seams, and
test coverage gaps. Headline items:

- **Email-change confirmation no longer 500s.**
  `ConsumeEmailChangeToken` was reading `users.role` and
  `users.org_id` — columns dropped by migration 0020. Now reuses
  `loadUserByID`. Regression test drives the issue + confirm flow
  end-to-end against a real database.

- **Stripe webhook handlers gate on `payment_status`.**
  `checkout.session.completed` fires for async methods (ACH / PIX
  / BACS) before funds are captured. Both `handleCheckoutCompleted`
  and `handleTicketCheckoutCompleted` now require
  `payment_status="paid"` or `"no_payment_required"` before
  fulfilling. An unpaid session is logged and skipped, not flipped
  to "paid" optimistically.

- **Partial refunds no longer trigger team withdrawal.**
  `charge.refunded` fires for every refund delta, so a €5 partial
  refund used to flip an entire ticket purchase to `refunded` and
  withdraw the team. The handler now requires
  `Refunded == true && AmountRefunded >= AmountCents` before
  flipping state.

- **Stripe API calls carry `IdempotencyKey` headers.** `Refund`,
  `CreateCheckout`, and `CreateConnectedAccount` derive a stable
  key from the local row id (e.g. `refund-purchase-42`,
  `checkout-purchase-42`, `connect-org-42`). A retried admin
  Refund click no longer cuts a duplicate refund; a transport
  hiccup mid-checkout no longer mints two Stripe sessions. The
  three params types gain an `IdempotencyKey` field on the
  abstract `payments` interface so future provider impls can
  honour it the same way.

- **`X-Forwarded-For` honoured only behind a trusted proxy.** New
  `ZV_TRUSTED_PROXIES` env var (comma-separated CIDR/IP list,
  e.g. `127.0.0.1,::1` for sidecar mode or `10.0.0.0/8` for a
  private VPC). When unset (the safe default), forwarded headers
  are ignored — attackers on a directly-reachable deployment
  can't spoof their source IP to bypass the per-IP rate limiter
  or audit log. Same gate applies to `X-Forwarded-Proto` for
  cookie `Secure` decisions, and chi's stock `middleware.RealIP`
  is replaced by `trustedProxyRealIP` that honours the same gate.
  Operators behind nginx / Caddy / Traefik need to set this on
  upgrade; the fresh `.env.example` documents both common shapes.

- **Migration runner.** `pg_advisory_lock` around the boot-time
  loop so two booting processes serialise. Single-shot `SELECT`
  for applied migrations replaces N×SELECT-COUNT round-trips.
  Boot-time duplicate-prefix check refuses to start when two
  migrations share a 4-digit prefix (the existing `0033_*` pair
  is grandfathered).

- **SMTP CRLF / header-injection guard.** Recipient and From
  addresses are validated for control bytes before being passed
  to `net/smtp`; every header value goes through
  `sanitizeHeader` which strips CR/LF/NUL/etc. before reaching
  the wire. Closes the BCC-smuggling vector if a future caller
  stops sanitising at the boundary.

- **OIDC + TOTP hardening.** OIDC: the IdP-supplied email goes
  through `TrimSpace` + a printable-ASCII gate before becoming
  the username, so unicode confusables can't collide with a
  legitimate user. `amrIncludesMFA` now matches only RFC 8176's
  `mfa` / `mca` (was lenient and accepted single-factor `otp` /
  `swk` / `hwk`). TOTP recovery codes consume in constant time:
  `consumeRecoveryCode` runs Argon2 against every unused hash
  regardless of an early match, so a wrong-guess and a
  right-guess take the same wall-clock time.

- **Fuzz tests for parser surfaces.** New `Fuzz*` tests for
  tokenx, mdx, icscal, orgslug, teamimport CSV + IOFXML, and
  adventure validation. The fuzzer caught and we fixed three real
  bugs in flight: an mdx panic on a malformed autolink (`0<0@0>`)
  that would have crashed any handler rendering author markdown;
  an icscal NUL byte propagating through SUMMARY (rejected by
  some mail clients); an orgslug `Slugify("www")` returning a
  value that fails `Validate` (auto-derived slugs from "www
  Inc." were unsavable).

### Service worker + asset caching

- **Static assets get ETag + revalidation.** `serveAsset`
  previously set `Cache-Control: no-store` despite the comment
  claiming long-cached. Every navigation re-downloaded
  `topbar.css`, `page-tokens.css`, `i18n.js`, `fsmap.css`, and
  the 130 KB `jsqr.js` blob. Now: ETag computed from the served
  bytes at handler-construction time + `public, max-age=3600,
  must-revalidate`. Repeat visits get 304s; a deploy mints fresh
  ETags (process restart → new sums) so users pick up new
  assets within the cache window. Single biggest repeat-visit
  perf win in the product.

- **Service worker cache rotates per deploy.** The cache version
  is now keyed to the server-injected build version (was hardcoded
  `-v1`). The `/play-sw.js` route substitutes `__ZV_BUILD_VERSION__`
  with the running version stamp; `activate` purges any stale
  shell or per-team asset cache whose suffix doesn't match
  current. An operator no longer has to remember to bump a
  literal in the source on every release.

- **Mid-event SW hot-swaps disabled.** Removed `clients.claim()`
  from `activate`. A fresh SW no longer takes over open player
  tabs the moment it activates — existing clients keep their
  current SW until a natural reload, so a regression in the new
  worker can't strip offline state out from under a team during
  their event. New tabs land on the fresh worker as expected.

- **Fetch-handler scope guard.** The SW registers at scope `/`
  because it needs to intercept `/adventure-assets/*` and
  `/org-logo/*` (siblings of `/play`, not children). Defence in
  depth: an explicit allowlist at the top of the fetch handler
  — `HANDLED_PREFIXES` + `HANDLED_EXACT` — early-returns on
  anything outside the player's surface so a future bug in one
  handler can't accidentally widen the SW's reach into
  `/arrange`, `/api/admin/*`, etc. `prefetchAssets` is also
  capped at 200 URLs per message.

### Player a11y, mobile, i18n

- **Player modals are real dialogs.** Hint sheet, menu drawer,
  welcome dialog, and QR scanner all now carry `role="dialog"`,
  `aria-modal="true"`, `aria-labelledby` on the heading, focus
  trap (Tab / Shift-Tab cycle within the panel), focus restore
  on close, and focus the right initial element on open (the
  close button by default; the primary CTA for the welcome
  dialog). The same pattern is exposed as `wireModal()` for
  reuse. Earlier these were "fake modals" — keyboard users
  tabbed straight out into the page behind.

- **iOS soft-keyboard no longer hides the submit dock.** The
  `viewport` meta on `play.html` gets
  `interactive-widget=resizes-content` so the layout viewport
  shrinks when the keyboard slides up. Combined with new
  answer-input attributes (`autocapitalize="off"`,
  `autocorrect="off"`, `spellcheck="false"`,
  `enterkeyhint="send"`) the central interaction — typing an
  answer — works correctly on iOS without the keyboard's
  autocorrect mangling the input.

- **44 px tap targets across the player.** Every dismiss control
  (hint-sheet-close, welcome panel-close, team-share panel-close,
  menu-btn, menu-close, hint-toggle pill) now meets the WCAG
  2.5.5 floor that the file's own `--tap-target` token had been
  specifying. The visible glyph stays compact via `padding`;
  `min-width`/`min-height` carries the hit area for outdoor /
  gloved use.

- **`prefers-reduced-motion` honoured across the arranger surface.**
  arrange.html previously had ZERO reduced-motion handling
  despite shipping 24+ keyframe animations. Now matches the
  global pattern in page-tokens.css. AA contrast on the `--muted`
  token is bumped from `#6b7f96` (3.86:1, fails AA on light bg)
  to `#4f6478` (~5.4:1) — same value play.html already used.

- **Live page memory + CPU on long-running TVs.** The clock
  interval pauses when `document.hidden`. The `lastPct` map is
  pruned against the current seen-team set so a TV cycling
  through back-to-back events doesn't accumulate entries forever.
  WS `snapshot`+`tracks` bursts coalesce via
  `requestAnimationFrame` so the back-to-back pair triggers one
  render per frame instead of two.

- **Arranger pollers go quiet when the tab is hidden.** Simulator
  status (1.5 s) and webhook-log (4 s) pollers are now
  self-rescheduling `setTimeout` loops that skip ticks when the
  tab is buried and double their delay (cap 30 s) on failure —
  no more thundering herd against a recovering server.

- **Swedish translation sweep.** A round of mechanical fixes to
  `sv.json` closed pervasive grammar errors: `en event` →
  `ett event` (event is neuter — 14 hits); `genevent` →
  `genomgång` (a machine-translation typo at the top of the SV
  manual); `Läser in` → `Laddar` (literal calque);
  `exportet` → `exporten` (gender); `spelartak`/`lagtak` → `Max
  antal spelare`/`Max antal lag` (calques). Nuance calls
  (`Uppdrag` overloading, `Recap` untranslated, `Hint`
  inconsistency, brand split) deferred to a copywriting pass.

- **404 page localised.** Routes through a new `render404` handler
  that substitutes the `NotFound*` strings into the embedded
  template; SV translations land in `landing/sv.json`. The
  English-only minimal-card design is preserved so the failure
  floor stays self-contained.

### Operator-visible config

- **`ZV_TRUSTED_PROXIES`** (new). Comma-separated CIDR/IP list of
  reverse proxies whose `X-Forwarded-*` headers are honoured.
  Empty (default) means "trust nothing": forwarded headers are
  ignored and per-IP rate limits / audit log see the immediate
  peer. Operators behind nginx / Caddy / Traefik / Cloudflare
  Tunnel need to set this on upgrade. See `.env.example` for
  shape examples.

- **Makefile reorganised.** `make` (no args) now prints a short
  menu grouped by purpose. New: `make rebuild` (clean image
  build without wiping the DB), `make reset` (wipe Postgres
  volume + rebuild — destructive, confirms first). Removed:
  `make deploy` (was misnamed and duplicated `make up`),
  `make seed-compose` (folded into `make seed`),
  systemd-specific `make restart`. Existing targets kept.

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

[Unreleased]: https://github.com/klahr/zonventure/compare/0.2.0...main
[0.2.0]: https://github.com/klahr/zonventure/releases/tag/0.2.0
[0.1.5]: https://github.com/klahr/zonventure/releases/tag/0.1.5
[0.1.4]: https://github.com/klahr/zonventure/releases/tag/0.1.4
[0.1.3]: https://github.com/klahr/zonventure/releases/tag/0.1.3
[0.1.2]: https://github.com/klahr/zonventure/releases/tag/0.1.2
[0.1.1]: https://github.com/klahr/zonventure/releases/tag/0.1.1
[0.1.0]: https://github.com/klahr/zonventure/releases/tag/0.1.0
