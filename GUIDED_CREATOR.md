# Guided creator — spec

The public party adventure creator (`/app/party/trail/…`) becomes a
guide: pick a theme, then a fullscreen walk through one thing per
screen. Replaces QUIZ_TRAIL.md screens 1–2 for the public door only.

Status: approved 2026-10-10.

## Why

- The start page asks for a template above a look, but tapping a
  template creates the draft and leaves at once: the look picker below
  is never reached unless scrolled to first.
- The editor shows title, save state, look panel, card, phone preview,
  timeline, menu and "Get it ready to play" at the same time. Nothing
  says what to do next; problems show only after "Get it ready to play".

## Principles

- **One question per screen.** Each screen has one job, a heading that
  says it, and one primary button at the bottom ("Next").
- **Always a way back.** "Back" on every screen; nothing is lost.
- **Guided first time, direct after.** A new party adventure walks the
  guide from the start. Opening an existing one lands on the Overview,
  where any card is one tap away.
- **Same data, same save.** Cards, validation (`problems`), autosave
  (`useTrailSave`) and theme saving are unchanged. Only the UI changes.

## Flow

```
Start (theme)  →  [fullscreen]  Who is it for  →  Name  →  Card 1 … Card N  →  Overview  →  Ready
                                                             ↑__________________|
```

### 1. Start — pick a theme (`/trail/start/{org}`)

- Heading, one-line lead, price line, "My party adventures" link (as
  today).
- A grid of eight favourite themes, each tile the theme's colours and
  a sample card in it, and "More themes" that shows all 28
  (`TRAIL_THEMES`).
- Tapping a theme opens the guide. No draft is made yet.
- Embedded on zonventure-web, this step stays in the frame; the guide
  opens out of it (`leave`) so it really is fullscreen.

### 2. Who is it for (guide, before a draft exists)

- The templates (Birthday party, Kids' trail, Office/team, Family,
  Blank) as large tiles, each with its blurb and "N example cards".
- Tapping one creates the draft with the chosen theme (today's
  `createDraft` + `setTrailTheme`) and goes to Name. The guide's URL
  becomes `/trail/edit#id` so a reload resumes.
- The chosen theme always wins over the template's default
  (`TEMPLATE_THEMES` is used only if the guide was opened without one).

### 3. Name

- "What's it called?" — the title field, large, prefilled by the
  template. The phone preview's title shows it live.
- Next is disabled while empty.

### 4. Cards — one per screen

- Header: "Card 3 of 8" and a thin progress bar (cards only).
- The card editor (`CardEditor`) with its kind switcher, beside the
  phone preview (`PhonePreview`); on a narrow phone the preview is behind
  "See it on a phone", as today.
- First visit to a template card: a one-time note "This is an example.
  Change it to your own, or press Next to keep it."
- Bottom: Previous, Next. Next is allowed with problems. A card shows
  what it still needs, except one just added and not yet left.
- On the last card, the bottom also has "+ Add a card" and Next reads
  "That's all".
- Adding elsewhere: "+ Add a card" on Overview, then move it.
- Delete: in the card's own toolbar, with the existing Undo toast.

### 5. Overview

- "Your party adventure" — the title (tap to rename), the theme (tap
  to change; opens `ThemePicker` in a sheet), then every card as a row:
  number, excerpt, a warning dot when incomplete.
- Tap a row → that card's screen; from there Next/Back walk on, and a
  "To overview" link returns.
- Reorder: drag a row by its handle; "Move earlier/later" in the card
  screen stays as the accessible fallback.
- "+ Add a card" at the end of the list.
- Primary button: "Next" → Ready. If anything is incomplete it is
  disabled and says "Card 4 and 7 need finishing", each a link.

### 6. Ready

- What happens now, in three short lines: print the cards, hide them,
  teams join with their phones.
- "Save to email" (the existing panel) when no email yet.
- Primary: "Get it ready to play" → the existing Checkout.
- Secondary links: "Copy link to this party adventure", "Delete".

### Chrome

- Fullscreen: no site header. A slim top bar with Back (left), the save
  state ("Saved on this phone"), and Close (×) → My party adventures.
- The menu goes away; its items live on Overview and Ready.

## Returning and the host door

- `/trail/edit#id` on a draft with `revision > 0` opens Overview;
  revision 0 (just made) opens Name.
- After payment (`state != draft`) and on the host door
  (`/trail/host#token`): Overview, with Ready replaced by "Done" (back
  to the host page). The notes "A game is being played…", "The cards
  changed since you printed them…" and "This party adventure has ended"
  show on Overview.

## The console

`apps/arrange` keeps today's `Editor` (timeline). QUIZ_TRAIL.md's "two
doors, one editor" becomes "two doors, shared parts": both use
`CardEditor`, `PhonePreview`, `ThemePicker`, `problems` and the save
hook. Moving the console to the guide later is a separate decision.

## Code shape

- `packages/trail`: export `CardEditor`, `PhonePreview`, `SaveNote`
  and `Icon`, unchanged.
- `apps/party/src/Guide.tsx`: `GuideShell` (the fullscreen frame) and
  `Guide` (Name, cards, Overview, Ready). The screen is local state (no
  new routes), mirrored in `history.state` so the browser's back button
  steps back. One file in `src/` so locales.test.ts sees its strings.
- `TrailPages.tsx`: `TrailStart` becomes the theme grid.
- `TrailEdit.tsx`: `Creator` renders the guide instead of `Editor`;
  its save, delete, copy and checkout logic stays.
- New strings in `locales/sv.json`.

## Out of scope

- The timeline `Editor` itself, the console, server and API. Card kinds,
  limits and validation. Checkout.

## Decisions

- Start shows eight favourite themes and "More themes".
- "Who is it for" stays its own step: one tap gives example cards to
  change, which is easier than starting from an empty card. Blank is
  one of the choices.
- 2026-10-12, after a first-time-visitor walkthrough:
  - A birthday template asks "Whose birthday is it?" (name, age) before
    the draft is made; the server writes them into the title and the
    example cards (`trail.NewFromTemplateFor`, `{name}`/`{name's}`/`{age}`
    placeholders; `person`/`age` on `POST …/trail/drafts`).
  - Example cards carry a pencil badge until changed; the question text
    looks like a field (tint, dashed border, pencil).
  - Icons over words: card kinds (Options / Typed answer / Info), card
    tools (← → 🗑), a ✓ toggle for the right option instead of "Tap 1, X
    or 2", Ready's print / hide / play row, an icon list for checkout.
  - One back per screen: the guide's foot; the bar keeps only Close.
  - Free play (up to the free teams) never needs payments to be open.
  - Price said once, as a chip: "3 teams free · more from SEK 149".
