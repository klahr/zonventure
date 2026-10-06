# Quiz trail (Tipspromenad) and hosted events — spec

Status: **draft for review**. Nothing here is built yet.

A public, account-free creator for simple linear quiz trails. Anyone
builds a trail on a phone, buys a hosted pass (149 kr), prints the
cards, and runs games from an admin link. Players join from one player
link and answer on their phones.

The trail is one way into a **hosted event**, the purchase unit this
spec also consolidates for parties and private hosting.

## Names

| Where | English | Swedish |
|---|---|---|
| Public product | Quiz trail | Tipspromenad |
| Console sidebar (Events / Self-guided / …) | Hosted | Värdlett |

The console's "Parties" entry becomes "Hosted" (UI copy only; code ids
`party`, `/party/{token}`, `ZV_PARTY_*`, `party_offers` stay). The
Hosted page has two parts: the org's hosted events (bought via offers,
quiz trails or Host it), each opening the console's event page, and
**Offers** (today's party offers). Hosted events no longer appear in
the Events list, which today shows them because nothing filters
`party` out.

## Hosted events

A hosted event is an `events` row with `party = TRUE` (as today), run
by a host from a secret host link, not by org staff. **One purchase is
one hosted event.**

Three ways in:

| Way in | Adventure | Buyer | Customer pays | Zonventure gets |
|---|---|---|---|---|
| Quiz trail | The trail's adventure | Anyone, no account | Pass price | Pass price |
| Party offer | The org's adventure | Parent, no account | The org's price | Pass price, as application fee |
| Host it | Any adventure the account's org owns or was shared | Account user | Pass price | Pass price |

**Pass price:** 149 kr per started block of 15 teams, counted per game
(teams playing in one round, below). Hosted events only; Events and
Self-guided keep their current pricing.

- Price per block is one setting (`ZV_HOSTED_BLOCK_CENTS`, default
  14900 where a platform fee is set, free otherwise; `ZV_HOSTED_CURRENCY`,
  default `sek`), never hard-coded.
- Quiz trails, Host it and added blocks are charged on the platform's
  own Stripe account (the platform is the seller); a party offer goes
  through the org's connected account with the pass price as the fee.
- The buyer picks blocks at checkout: one by default, "More than 15
  teams in one game?" reveals a stepper. More blocks can be bought
  later on the host page ("Add 15 teams").
- Joining stops at `blocks × 15` teams in the current game, with
  "This game is full — ask the host to add teams".
- **Party offers:** the org keeps setting its customer price (base +
  per child). The application fee becomes `blocks × block price`
  instead of the percent formula (`internal/payments/fee.go`); a party
  offer is one block in v1. An offer price below the fee is refused.

**Validity:** the first game must start within 60 days of purchase;
from the first game, the event is playable for 14 days. Both live in
`events.valid_until` (purchase + 60 d, reset to first game + 14 d).

**The host page is a subset of the console's event page.** The console
is the arranger app (`web/apps/arrange`); its event page
(`pages/event/`: Overview, Teams, Lifecycle, Links, Statistics, …) is
the full view of an event. A host sees the hosted subset of the same
views, without an account:

| Console event page | Host page |
|---|---|
| Overview, Statistics | yes |
| Teams | list, remove before start; no editing of players |
| Lifecycle | start game, pause, resume, finish game |
| Links | player link with QR, send by email |
| Personalise, Places (park mode) | yes, when the adventure uses them |
| Messages, Emails, Activity, Control map, settings | no |

- **Personalise and Places move into the console's event page** (today
  they exist only on the party host page), so the host page is a true
  subset and an arranger can set up a hosted event for a customer.
- **Shared components:** the hosted views (player link, games, where,
  personalise, places) live in a workspace package
  (`web/packages/hosted`), with its own Swedish catalog and CSS, used by
  the host app (`web/apps/party`) and the console's event page.
- **One set of handlers, two route lists.** The host page's handlers
  resolve the event either from the host link (`/api/party/{token}/…`)
  or, in the console, from the event id authorised by org
  (`/api/admin/events/{id}/hosted/…`). Each route list names only the
  hosted actions, so a host link reaches nothing else; the console's
  org-scoped event endpoints never accept a host link.
- **Teams join via the player link** in every hosted event and name
  themselves. The party flow where the host adds teams and hands out
  join codes goes away.

**Games are rounds.** A hosted event holds any number of games, one at
a time. A game is a round: `teams.round`. "Finish game" closes the
round and sends its summary; the next team to join starts the next
round. The graph is pinned when a round opens, so edits apply from the
next game. Today a party pins one copy per event (`version =
-eventID`), which the engine caches as immutable
(`internal/play/play.go:154`); rounds need a pin per round instead.

## Principles (creator)

- **For non-techies on a phone.** One thing per screen, large tap
  targets, no jargon (no "zone", "graph", "step", "node"; a step is a
  **card**).
- **Linear.** A trail is an ordered list of cards. No branches, no
  conditions, no scoring settings.
- **The timeline is the source of truth.** The adventure's graph is
  generated from it, one way, on every save. The creator never reads a
  graph.
- **Two doors, one editor.** The same creator runs publicly with no
  account (quiz trail) and in the console for signed-in users (a
  simple adventure).
- **No account, no password** on the public door. Identity is a secret
  link. An email is optional until checkout, where Stripe collects it
  anyway.

## Scope

**v1**
- Hosted pass pricing and validity for all three ways in.
- Rounds in hosted events.
- Host page as a subset of the console's event page (shared package,
  host token on the event API); parties move onto it.
- Personalise and Places in the console's event page.
- Creator with templates, three card kinds, reorderable timeline.
- The same creator in the console for every org; one-way "Open in
  designer".
- Server-side drafts, optional email save, My trails.
- Admin page, player link with QR, send link by email.
- Cards PDF and answer key PDF.
- Finish game → summary email; auto-finish.
- Day-7 keep-it email, deletion email.
- Conversion: move the trail's adventure into a new account.
- "Host it" on an account's adventure.

**Later**
- Places mode (questions on the phone at map spots, as park mode).
- More card kinds (photo, multiple correct answers, …).
- Teams starting at different cards (a loop with per-team start
  offsets). v1: everyone starts at card 1; hosts stagger start times.
- Party offers with more than one block.

**Not planned:** AI help, branching, custom colours or fonts, image
upload.

## Availability

- New column `organizations.quiz_trail_enabled BOOL NOT NULL DEFAULT
  FALSE`. Only the Zonventure org gets it on (set by a site admin; no
  org-admin UI in v1).
- Public entry: `/o/{slug}/trail`. When the flag is off it shows "Not
  available". zonventure-web links to `/o/zonventure/trail`.
- Payments follow `payments_open` as for parties. When payments are
  closed, the creator still works, but checkout says "Booking isn't
  open right now".

## Lifecycle (trail)

```
draft ──pay──▶ paid ──first game──▶ playing (14 days) ──▶ deleted
  │              │                      │
  │              └─ 60 days unused ─▶ deleted
  │                                     └─ keep link ─▶ converted (adventure moves to account)
  └─ 30 days without edits ─▶ deleted
```

| State | Editable | Playable | Ends |
|---|---|---|---|
| draft | yes | preview only | 30 days after last edit |
| paid | yes | yes | 60 days after purchase if no game started |
| playing | yes (applies from next game) | yes, unlimited games | 14 days after the first game |
| converted / deleted | — | — | trail row and event removed |

## Data model

Migration `0109_hosted_trails.sql`.

**`adventures`** gets (simple adventures, either door):
- `cards JSONB NULL`: the timeline (below). NULL means a designer
  adventure.
- `cards_revision INT`: bumped on every save; used for edit conflicts
- `cards_graph_hash TEXT NULL`: hash of the graph last generated from
  the cards (see "Open in designer")
- `printed_revision INT NULL`: revision of the last cards PDF
- `template TEXT NULL`, `kids BOOL NOT NULL DEFAULT FALSE` (from the
  template; asks for guardian consent at checkout)
- `trail_hidden BOOL NOT NULL DEFAULT FALSE`: hides a public-door
  adventure from the org's adventure lists, the store and MCP listings

Title, language and theme use the adventure's existing fields.

**`trails`**: only what the no-account door needs.
- `id BIGSERIAL`, `org_id` (the seller; the Zonventure org)
- `adventure_slug`: the trail's adventure, created with the draft in
  the seller org with `trail_hidden = TRUE`
- `event_id BIGINT NULL`: the hosted event, set on payment
- `secret_hash TEXT UNIQUE`: SHA-256 of the draft secret (edit right)
- `email TEXT NULL`, `email_confirmed_at TIMESTAMPTZ NULL`
- `keep_offer_sent_at`, `created_at`, `updated_at`

**`cards` JSON**, an ordered array:

```json
[
  {"id": "c7f2", "kind": "quiz", "text": "Vilket år föddes Elsa?",
   "options": ["2016", "2018", "2020"], "correct": "X"},
  {"id": "a91b", "kind": "answer", "text": "Vad heter katten?",
   "answers": ["Måns", "Mons"]},
  {"id": "e03d", "kind": "info", "text": "Grattis! Nästa kort hittar ni vid brevlådan."}
]
```

- `quiz`: exactly 3 options, for 1 / X / 2 (the graph's quiz block),
  `correct` is "1", "X" or "2".
- `answer`: typed answer; `answers` lists accepted spellings (up to 5),
  matched case-insensitively with the existing matcher.
- `info`: text only, no answer, no points.
- Card `id`s are stable so reorders keep per-card statistics apart.

**`trail_email_tokens`**: `token_hash`, `email`, `purpose`
('confirm'|'signin'|'keep'), `trail_id NULL`, `expires_at`, `used_at`.
Single use; confirm and signin expire after 1 hour, keep at the
event's `valid_until`.

**`events`** gets:
- `hosted_blocks SMALLINT NULL`: blocks of 15 teams bought
- `player_token TEXT UNIQUE NULL`: short, unguessable, plaintext (it is
  printed as a QR); trail and Host it events
- `first_game_at TIMESTAMPTZ NULL`
- `current_round INT NOT NULL DEFAULT 0`

The host/admin link reuses `events.host_token_hash`.

**`event_rounds`**: `(event_id, round)` PK, `started_at`,
`finished_at`, `summary_sent_at`.

**`teams`** gets `round INT NULL` and `headcount SMALLINT NULL` ("How
many are you?").

`ticket_purchases.kind` gains `'hosted'` (trail and Host it) and
`'hosted_blocks'` (added teams); `ticket_purchases.blocks SMALLINT`.

## Generating the graph

`trail.Compile(cards, lang, theme) → graph.Graph` in a new
`internal/trail` package, saved as the adventure's published graph on
every save (both doors):

- No zones. Quiz and answer cards are `puzzle` nodes without a `zone`
  (the field is optional); info cards are `story` nodes. Linked in
  order, then the end.
- A puzzle shows "Card N" and the answer input (1 / X / 2 buttons or a
  text field) **without the question text**: the question is on paper,
  and answering is the arrival. Info cards show their text and a
  continue button.
- Scoring: one point per correct quiz or answer card. No hints, no
  penalties, one attempt per card (a tipspromenad answer is final).

No engine change: zone-less puzzles and story nodes already exist.

**Open in designer (one-way).** In the console, a simple adventure has
"Open in designer", which warns "You can't go back to the simple
editor", then clears `cards`. The designer owns the graph from then
on. If the graph is changed any other way (designer, MCP co-author,
graph import), its hash no longer matches `cards_graph_hash`: the
creator shows the adventure read-only with "Changed in the designer"
and never overwrites the graph. The public door never offers the
designer.

## Screens

### 1. Start (`/o/{slug}/trail`)
- Heading "Make your own quiz trail", one line on how it works, and
  **"Free to make · 149 kr to play for 14 days"**.
- Template cards (4–5): Birthday party, Kids' trail, Office/team,
  Family, Blank. Each sets theme, `kids`, title placeholder and 3–5
  example cards that show how to write them.
- "My trails" link (top right) when this browser knows any trails or
  has a signed-in email.

### 2. Creator
Two areas: **the card** (top, most of the screen) and **the timeline**
(bottom strip).

- **Top bar:** back, title (tap to rename), save state:
  "Saved on this phone" / "Saved to anna@…". Menu: Save to email,
  Preview, Copy link to this draft, Delete.
- **Card area:** the selected card as it will look printed. Tap to edit
  text. Kind switcher as three icons (Question with options / Question
  with answer / Information). Quiz: three option rows, tap the circle
  to mark the correct one; "+ option"/remove keeps it at 2–3. Answer:
  one field, "+ Other accepted spelling".
- **Timeline:** numbered chips (1, 2, 3 …) with a short excerpt and a
  warning dot on incomplete cards, plus a "+" chip at the end.
  Scrolls sideways. Reorder: long-press and drag; or tap a chip →
  "Move earlier"/"Move later" in the card area (accessible fallback).
  Add: "+" adds after the selected card, kind of the previous card.
- **Bottom action:** "Done" → checkout when valid, otherwise jumps to
  the first incomplete card with the reason.
- After paying, the same creator shows "Changes apply from the next
  game" when a game is running, and a reprint banner (see PDF).

Validation: 1–40 cards; at least one quiz or answer card; text 1–300
chars; options 1–80 chars, all filled, one marked correct; answer
non-empty.

Undo: a single "Undo" toast after delete and reorder. No history.

### 3. Save to email (optional)
Email field → "We've sent you a link". The confirm link marks the
email confirmed and attaches every trail this browser holds. Until
confirmed nothing is attached.

### 4. Checkout
Summary (title, N cards, price, "Play as many games as you like for 14
days from the first game; start within 60 days"), team blocks stepper
(hidden behind "More than 15 teams in one game?"), guardian checkbox
when `kids`, terms. → Stripe Checkout, which collects the email.
Webhook: create the hosted event (state scheduled, `valid_until` =
now + 60 d, tokens), set the trail's `email` (confirmed by payment)
and `event_id`, send `trail_paid`, redirect to the admin page.

### 5. Host page (`/party/{hostToken}`)
The hosted subset of the console's event page (see Hosted events), the
same for every hosted event:
- Title; "Start your first game by 5 Dec", then "Playable until
  20 Oct".
- **Player link:** large QR, link, Copy, "Send by email" (one address
  at a time, rate-limited).
- **Current game:** teams joined (name, headcount, card reached, score)
  of the limit, live scoreboard link, **"Finish game"** (confirm
  dialog). "Add 15 teams" when near the limit.
- **Earlier games:** date, teams, players; each with "Email the summary
  again".
- Personalise / Places, when the adventure uses them.

Trail-only additions:
- **Edit trail** → creator.
- **Print:** "Print cards (PDF)", "Answer key (PDF)".
- **Keep it** (from day 7 of play): "Create a free account to keep this
  trail" → signup flow.

### 6. Player page (`/t/{playerToken}`)
- Title and theme. "Team name" and "How many are you?" (stepper,
  default 1). "Start".
- Joins the current round, opening a new one if none is open, then
  hands over to the play app. One phone per team in v1; another phone
  joining with the same team name is a new team.
- Full: "This game is full — ask the host to add teams". Expired: "This
  quiz trail has ended."

### 7. My trails (`/trail/mine`)
- Lists trails from this browser (localStorage) and, after an email
  sign-in link, every trail for that email. Each: title, state, cards,
  dates; Continue / Admin page / Duplicate / Delete (draft only).
- Signed-out: "Get a link by email" field (always "Check your
  inbox", no account enumeration).
- Duplicate makes a new draft.

### 8. Creator in the console
Adventures → New adventure → **"Quiz trail (simple)"** or **"Designer
(full)"**. The simple choice opens the same creator (screens 1–2:
template, then cards) inside the console, for every org regardless of
`quiz_trail_enabled`. No save-to-email, no checkout: it saves to the
org like any adventure. "Done" returns to the adventure's page, where
it can be printed, hosted (Host it), run as Self-guided or as an
Event, or offered as a party. A simple adventure's page has "Edit
cards" and "Open in designer".

### 9. Host it (console)
On an adventure the org owns: "Host it" → blocks, checkout (pass
price, platform Stripe account, no Connect) → a hosted event with the
same admin and player pages. Listed under Hosted.

## Games (rounds)

- **Opening:** the first team joining when no round is open starts
  round `current_round + 1`, pins the graph, and on the first round
  sets `first_game_at` and `valid_until = first_game_at + 14 d`.
- **Finishing:** "Finish game" or 3 hours without player activity →
  round finished → summary email to the buyer's email.
- **Limits:** `hosted_blocks × 15` teams per round; one open round.
- **Expiry:** at `valid_until` an open round is finished first (summary
  sent), then the event ends.

## PDF

Server-side with `go-pdf/fpdf` (already used for the recap PDF), with a
UTF-8 TTF font so Swedish letters work.

- **Cards PDF:** a cover page for the host (how it works, the player QR
  and link, "Hide the cards in order 1–N"), then one card per A5 half
  of an A4 page: big number, question, options as 1 / X / 2 (or "Write
  the answer on your phone"), theme colour band. Info cards print their
  text.
- **Answer key PDF:** one page listing each card's correct answer.
- Printing sets `printed_revision`. When the order, count or text of a
  printed card changes, the admin page and the creator show "Cards 3–5
  changed since you printed. Print again?".

## Emails

New kinds in `internal/emailtpl`, sv + en, sent via the outbox:

| Kind | When | Content |
|---|---|---|
| `trail_confirm` | Save to email | Confirm link |
| `trail_signin` | My trails sign-in | Sign-in link |
| `trail_paid` | Payment | Link to Stripe receipt, admin link, player link, print links, start-by date |
| `trail_player_link` | "Send by email" on admin page | Player link and QR |
| `hosted_summary` | Round finished | Stats (below), link to admin page |
| `trail_keep` | Day 7 of play | "Keep it forever": signup link, expiry date |
| `trail_ended` | Deletion or conversion | It's gone / it's now in your account; earlier summaries were sent |

`hosted_summary` is shared by all hosted events.

**Summary statistics:** teams, players (sum of headcounts), ranking
with score and time, per card the share of correct answers, hardest
and easiest card.

The buyer's email is the only address that gets admin and summary
emails. Player-link emails go to the address typed and contain nothing
else.

## Conversion ("Keep it")

1. The keep link (`trail_keep` email or admin page) opens signup with
   the email prefilled.
2. Signup creates the user and a personal org.
3. The trail's adventure moves to that org and `trail_hidden` is
   cleared; title, theme, cards and graph kept. It stays editable in
   the simple creator.
4. The trail row and its hosted event are deleted; `trail_ended` is
   sent.
5. It is a plain simple adventure from then on: edited with the
   creator (or opened in the designer), run via Host it, Self-guided
   or Events.

Game results are not moved: the summaries were already emailed.

## Retention

Added to `internal/retention` (hourly sweep):

- Drafts: delete 30 days after `updated_at`, with their hidden
  adventure.
- Trail events: at `valid_until`, finish any open round, send
  `trail_ended`, delete trail, adventure and event (cascades teams,
  answers, pinned graphs).
- Day 7 of play: send `trail_keep` once (`keep_offer_sent_at`).
- Party and Host it events: existing party rules
  (`ZV_PARTY_RETENTION_DAYS` after the event ends at `valid_until`).
- Email tokens: delete when expired.
- Purchase rows stay, email anonymised by the existing trigger.

Documented in OPERATIONS.md and the legal text next to parties.

## Security and abuse

- Draft secret (256-bit) in localStorage and the "copy link" URL
  (`/o/{slug}/trail/edit#{id}.{secret}`, fragment so it is never in
  logs). Host token: hash only, as for parties.
- Limits: 20 new drafts per IP per day; 40 cards; 300 chars per text;
  saves debounced (1 s) and capped per draft per minute.
- Email sends (confirm, signin, player link) rate-limited per IP and per
  address; responses never reveal whether an email has trails.
- Content: private, link-only, deleted within 14 days of play. No
  moderation in v1; the seller org can delete a trail from the console
  (Hosted → Quiz trails, Zonventure org only: title, email, state,
  dates).

## API sketch

Public, under `/api/o/{slug}/trail`:
- `POST /drafts` (template) → `{id, secret}`
- `GET|PUT /drafts/{id}` (secret in header; PUT carries `cards_revision`,
  409 on conflict)
- `POST /drafts/{id}/checkout {blocks}` → Stripe URL
- `POST /email/confirm`, `POST /email/signin`, `GET /mine`

Host: the console's event endpoints with the host token as principal
(allowlist above), plus `POST finish-round`, `POST blocks` (Stripe
URL) and `POST send-player-link` on the event, available to arrangers
too. Trail-only, `/api/trail/{id}` with the host token: `GET
cards.pdf`, `GET answers.pdf`, `POST keep`.

Player, `/api/trail/p/{token}`: `GET`, `POST join {team, headcount}`.

Console: `POST /api/admin/adventures/{slug}/host {blocks}` → Stripe URL.

## Decisions

- Built (Phase D): the public API is `/api/o/{slug}/trail` (start page,
  new draft) and `/api/trail/…` (drafts by id, email, mine, keep); the
  draft secret travels in `X-Trail-Secret`, an email session in
  `X-Trail-Session`. Confirm and sign-in links both open a 30-day
  session, so My trails lists every trail of the email without ever
  holding their secrets. Checkout asks for the email itself (as party
  booking does) rather than reading it back from Stripe. A paid trail
  is a 'hosted' purchase of the seller org's hidden adventure; the
  webhook marks the trail paid (`trails.paid_at`). Retention runs in
  the admin package (`SweepTrails`, hourly) because it mails. Keep it
  is a token link to the console (`#/keep/{token}`): whoever is
  signed in there — after signing up, or into an existing account —
  takes the trail into their org. The pages live in the party app.
- Built (Phase C): the columns are `adventures.trail_*` (`cards` was
  taken by the theme). Saving makes the draft; Done, Host it and a
  hosted game opening publish it, so typing makes no versions. Quiz
  cards compile to a story with the graph's quiz block (scored at the
  end), answer cards to a one-try puzzle, info cards to a story.

- No arrival step: each card is a zone-less puzzle showing "Card N"
  and the answer input; no QR scan, no engine change.
- Everyone starts at card 1 in v1.
- Unpaid drafts are kept 30 days after the last edit.
- Receipt: Stripe's receipt email; `trail_paid` links to it.
- One purchase is one hosted event; games are rounds inside it.
- Pass price: 149 kr per started block of 15 teams per game, hosted
  events only. Org parties: the org sets its price; Zonventure's fee is
  the pass price.
- Validity: start within 60 days, then 14 days from the first game.
- A trail is stored as a hidden adventure; conversion moves it.
- A converted adventure is hosted privately via Host it at the pass
  price (replaces "no special path").
- A party is a privately hosted event, possibly with no account. The
  host page is a subset of the console's event page; the same handlers
  serve the host link and the console on separate route lists; join
  codes go.
- The creator is available in the console for every org. Cards live on
  the adventure; handoff to the designer is one-way.
