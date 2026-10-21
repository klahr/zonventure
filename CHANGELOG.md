# Changelog

All notable changes to Zonventure will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The repository, Go module, and CLI binary are named `zonventure`; the
product is **Zonventure**. Versions tagged here correspond to git tags
`<version>` on [github.com/klahr/zonventure](https://github.com/klahr/zonventure).

## [Unreleased]

### Added

- Designer: parallel paths. A "Split up" step divides the team into
  groups, one per path, each walking its own steps on its own phones
  until they meet again at a "Meet again" step, where each group waits
  for the last to arrive and the team goes on together. A path's role
  takes its player down it; everyone else is shared out evenly, so a
  team of a hundred with no roles splits in even halves. What any group
  finds — items, notes, points, answers — is the whole team's. Every
  phone sees where each group is; a step done together waits for its
  group, a place is reached by one of the group, and a role no one in
  the group holds is everyone's in it. A player in two groups walks
  whichever of them they choose, and can switch between them at any
  step, so one group can fetch what the other needs; a group whose
  players have all gone, or whose phones have all gone quiet for three
  minutes with the app closed, is open to anyone and walked by whoever
  finishes first, so a team of any size can play a split. The time
  limit, and an ending on any path, end it for every group. A path may
  not be entered from outside its split — by going back to a home
  screen or by the time limit either — nor lead past its join to
  anything the team walks together after it.
  The preview, the simulator and the AI assistant play splits too.
  Race control follows each group: on its map a trail and marker per
  group, from what the phones walking it sent while they did — the live
  audience map too — rather than one between them where no one is; in
  the leaderboard each group's step and how long it has been there,
  pauses aside, flagged after a quarter of an hour; and in a team's
  dialog each player's roles and group, and a button to move one stuck
  group on from any step of its path — at a split too, setting it off.
  Splits nest: a group that reaches a split on its
  path divides again among its own players, and its groups meet at a
  join of their own before going on — "North › Tower" — up to four
  deep.
- AI assistant: splits throughout. `simulate_adventure` says which
  group plays next and where every group is; `get_event_state_summary`
  lists a split team's groups; `unlock_team` takes a group (`group`)
  or its step (`node`) to move one stuck group on; the playtest can
  `walk` another of its groups; `dev_go_to` puts the dev phone on a
  split.
- Designer: a step done in parallel. Its answer is "Tasks": each task
  block is one role's part, answered only by that role's player, on
  their own phone and at their own place where it has one. Every phone
  sees whose each task is and when it is done, and the team moves on
  with the last. A task whose role nobody on the team holds is
  anyone's, so a small team is never stuck.
- Designer: players per team. An adventure can say how many players a
  team needs and takes: a team waits at its first step, its phones
  saying how many have joined, until the least are in, and no one
  beyond the most can join. The preview offers only team sizes the
  adventure takes.
- Designer: a secret role's cover. Every other player of a team with
  the secret role is told the cover in its place — "Loyal" beside
  "Police" — so every player's roles read alike and no one takes
  longer to read theirs.

### Changed

- Designer: the multi-phone preview shows every phone small underneath,
  each always in its own place, the chosen one highlighted; and each
  phone gets its own part of a split secret and sees its own roles
  marked as its own, as on real phones.

### Fixed

- Designer: in a translation, renaming a step, picking its place,
  marking where you stand or making a new item no longer writes the
  step's translated words over its originals; in a suggestion, it no
  longer drops the suggestion's other edits.
- A move sent from a phone while offline that the game then refuses no
  longer holds up every move after it.
- AI assistant: `set_text` no longer renames what a step's words name —
  a route's port, a role, an item, a post — when the words match it,
  which could cut a wire or a reference.
- A step with a mission can no longer be opened by its keys before the
  mission is sealed.
- Designer: in the multi-phone preview, a mission pick not yet
  confirmed, or a task answer being typed, no longer stays on screen
  when another player's phone is shown.

## [0.34.0] - 2026-10-20

### Added

- Designer: the preview plays as several phones at once. Pick 2 to 6
  players: each is a phone of its own, with its roles and its place on
  the map, and acts as itself; the chosen phone shows large and the
  others small underneath, marked when their screen changed. A secret
  role is dealt at random from three players, or given to the player
  the author picks, so roles, sabotage and missions can be tried
  before anyone walks.
- Designer: a mission block, for hidden-role adventures. Each player
  inside the mission's post picks one of two choices — Deliver or
  Tamper, or the author's own words — once, until as many as it waits
  for have, and the step waits until then. Only a secret role's
  betrayal counts, and every phone shows the same whatever was picked.
  A new condition, "Mission betrayed", asks about it on a later step:
  a tampered key, a detour, an ending.
- Store: a listing speaks the languages its adventure is played in. An
  event's store settings take its name and public description in each
  of the adventure's other languages, reviewed with the rest of the
  listing; the adventure's title, meeting point and area are
  translated in the designer's Translations panel, with its other
  texts, and reach the store with the version the event plays. The
  store, an event's page and its embeddable card show the page's
  language where there is a translation, and say which languages a
  listing is played in. The start screen names the meeting point in
  the team's language too. `update_event` takes `translations`;
  `get_event` lists them and the languages.
- Events in more than one language. A team has a language: set it
  when adding teams, in a team's details, or with `create_team`,
  `bulk_create_teams` and `update_team` (`lang`); a ticket bought in
  the store or a redeemed gift box sets it from the letter's language.
  Its invitation, calendar entry, cancellation notice and ticket
  email are then in that language, with the event's name and meeting
  place, the adventure's title and its note to buyers said in it: an
  event's own words are translated in a new "In other languages" card
  on its page, the adventure's in the designer's Translations panel
  (the note to buyers is new there). A message to teams, and an email
  to them, take a version per language that teams playing in it get
  instead, with the email around it in their language too
  (`send_event_message` takes `translations`). A file for the players
  can be for the teams playing in one language: a team gets the files
  for everyone and those in its language, or, when there are none in
  it, those in the adventure's own. The team's page names the
  adventure and lists the files in the team's language. A team in a
  language the adventure is not played in gets everything as written.

### Changed

- Sabotage: every phone of a team with a saboteur shows the sabotage
  alike, and any player may press it once; only the saboteur's press
  costs the team. A screen shown to a teammate no longer gives the
  saboteur away.

## [0.33.18] - 2026-10-20

### Added

- Translations: the texts players saw in the main language whatever
  they played in can now be translated in the designer's
  "Translations" panel: item names (the backpack, and what a slot
  says is missing), counter names (a status block), a look's own
  words for a right and a wrong answer, an email effect's subject and
  text, and the description of every picture and sound (image, audio,
  video and the other picture blocks, hint pictures, journal
  pictures, a role's picture, a tile picture, an order card's, a lock
  symbol's and a branch's picture), which screen readers read out.
  The panel's adventure-wide section is now "Title, journal, places,
  items and messages". Translations left behind by a deleted item,
  counter or look are counted as stale.

### Fixed

- Player: a puzzle answered on a board (put in order, a lock, a
  choice form, item slots) says "Not there yet" ("Inte framme än") on
  its button while the team is away from the puzzle's place, as a
  typed answer does, instead of only greying out.
- Player: put-in-order cards no longer jump a place too far and slide
  back when dropped, and the cards making way for a dragged card move
  by its own height, so cards of different heights (a line that wraps)
  no longer overlap or leave gaps while one is dragged.
- Player: an older phone that reads a step's pictures apart from its
  blocks gets their descriptions in the team's language too.

## [0.33.17] - 2026-10-20

### Added

- Note blocks: a "Fact box" style, for real history told beside a
  story. It is an upright, flat, cool-blue card with a coloured edge
  and the caption "The real history" ("I verkligheten"), so a team can
  tell the facts from the letters and props around them.

## [0.33.16] - 2026-10-20

### Added

- Designer: a new way to answer a puzzle, "Put in order" ("Sätt i
  ordning"). The team drags a stack of 2 to 8 cards into the right
  order, then tries it. Each card has text on a colour, a picture
  behind it, or both. The author lists the cards in the right order
  and the team gets them shuffled. The phone never learns the answer:
  the server checks the order, and a wrong one gets the wrong-answer
  replies. A finger drags a card by its grip, a mouse drags it from
  anywhere, a tap on one card and then another moves it, and a
  focused card moves with the arrow keys. Card texts are translatable,
  the race simulator and the MCP endpoint can play it, and it is in
  the step picker under puzzles.
- Event page: "Print standings (PDF)", a results sheet to put up
  where the event ends. It has the ranking alone in large type, the
  top three in the adventure's colour, and once the event is over the
  event's results code, so a photo of the sheet is every team's copy.
  Printed while the event runs, it says so. It is the main button once
  the event is over; the report, the CSV and a new JSON export are
  under the menu.
- Results export as JSON: the CSV's rows with the event around them,
  each team's results receipt and the event's fingerprint.
- MCP: `export_event_results_json`, the JSON export, and
  `verify_event_results`, the event page's "Check results".
- Recap: a saved results code can be checked against the record, and
  the code has a copy button. The recap remembers the code it showed
  on each device and warns when a later visit shows another.
- Organisation admins get the event's results fingerprint by email
  once an event is over, and the `event.completed` webhook carries it
  as `results_fingerprint`. A copy outside the database shows even a
  results record rewritten whole.

### Changed

- The results report (PDF) is in the organisation's language, with
  its dates in the organisation's time zone. Swedish is added.
- Recap: the results receipt is a quiet "Receipt for your results"
  line while the results hold, instead of a green "unchanged".

### Fixed

- Recap: a team's receipt no longer says its results are unchanged
  when a score was edited without going through the results record,
  or when another team's result was changed. Both change its place.

## [0.33.15] - 2026-10-19

### Changed

- Player: a walk, an adventure played at home or a party no longer
  shows place and time on its end screen. A game still does.
- Recap: time, points and rank show only for a game, not for a walk,
  an adventure played at home or a party.
- Recap: the results receipt is now one line saying whether the
  results are unchanged since the team finished. The codes are folded
  away under it, open only when the results were changed.

### Fixed

- Player: the compass and the team's position start again when the
  phone comes back to the play page. Before, they could stay stuck
  until the page was reloaded.
- Player: the full-screen button no longer disappears in full screen.
  It turns into the button for leaving it.

## [0.33.14] - 2026-10-19

### Added

- Player: a full-screen button ("Helskärm") at the app bar's right
  end hides the browser's own bars, so more of the step and map are
  in sight outdoors. It isn't shown where the browser can't go full
  screen (iPhone Safari) or when the app is installed and already
  runs full screen.

### Changed

- Player: the adventure's picture, or else the organisation's logo,
  is now the menu button at the app bar's left end. The ☰ shows only
  when there is no picture. The messages bell shows only once the
  organisers have sent a message.

## [0.33.13] - 2026-10-19

### Changed

- Player and store: the code boxes on /play and on an organisation's
  store take a code as six single-character cells, like a sign-in
  code. Typing moves on, Backspace on an empty cell steps back, a
  whole code pasted into any cell spreads over the rest, and the
  sixth character submits. The store's box takes at most six
  characters, as /play's already did.

## [0.33.12] - 2026-10-19

### Changed

- Player: in a walk, skipping a step is "Go on anyway" ("Gå vidare
  ändå"), a button in the walk's own look, and asks "Go on to the next
  stop without this one?" — no puzzle and no points. A game's skip
  link is as it was.

### Fixed

- Store: the ticket page's starting-point map draws on an
  organisation's own domain too. The page's own security policy
  blocked the map's style, tiles and worker there.

## [0.33.11] - 2026-10-19

### Added

- Store: a third kind beside games and walks, "At home" ("Hemma"),
  for an adventure played where the buyer is: the version its event
  plays has no places and the adventure no starting point, so nothing
  is set by hand. Its chip has an amber dot, its ticket page reads
  "Played at home, on your phones", and the filter offers it when the
  store sells more than one kind. The store API's items carry `home`.
- Store: a ticket page shows where the adventure starts — a map with
  its starting point pinned in the organisation's colour, the meeting
  point, and a link to open it on OpenStreetMap. One played at home
  has none.

### Changed

- Store: a listing's "Game" or "Walk" is the store's count chip — a
  glass pill with a coloured dot, green for a walk — on its card, its
  ticket page and its embed, and the Games/Walks filter carries the
  same dots.

### Fixed

- Share & embed: the preview shows for an organisation on its own
  domain too. The console only frames its own address, so the preview
  loads from there; the code to paste still points at the domain.

## [0.33.10] - 2026-10-19

### Added

- Share & embed: a self-guided event's page in the console gives the
  code for a buy button for other web pages, as a card (picture,
  description and button) or a one-row button, in the organisation's
  colours and in Swedish or English, with a preview, plus a plain link
  for newsletters and mail. The embed shows the price and whether the
  event is on sale as it is now; buying opens the store in a new tab.
  Only `/embed/` may be framed by other sites. (Shipped in 0.33.9.)

### Fixed

- The graph package's typecheck, which failed CI on 0.33.9: its test
  of every graph-wide field now includes `tour`.

## [0.33.9] - 2026-10-19

### Added

- An adventure can be a walk, not a game (Adventure settings → Score,
  or MCP `set_option` `tour`): the phone speaks of the walk and of
  you rather than of a game and its players, in English and Swedish,
  and the end screen says you have walked the whole way, without the
  gold star. The designer's simulator speaks the same way.
- Store: every listing says whether it is a game or a walk — a badge
  on its card and on its ticket page, whose format reads "Phone-guided
  walk" for one — and a store selling both can show just games or
  just walks. The store API's items carry `tour`.

### Changed

- Swedish: the player's phone, team page, printed answer cards and
  player emails speak of "rundan" rather than "spelet", and address
  the player as "du", never "ni".

### Fixed

- Store: a ticket page is headed by the listing's name, as its card
  is, with the adventure's name under it when that differs.
- Deleting an adventure takes its authors' dev-ticket test runs with
  it; only a real event still stops the delete.

## [0.33.8] - 2026-10-19

### Changed

- Player: on a find-the-place puzzle with a compass, the compass
  guides (direction and distance) and the box under it confirms
  arrival, instead of both saying the distance and "You are at…".

### Fixed

- Player: an adventure that hides the score no longer leaves an empty
  row above the step, and its hint, bag and journal buttons stay on
  the right.

## [0.33.7] - 2026-10-19

### Added

- MCP: `edit_adventure_graph` has a `set_option` op that turns
  adventure-wide settings on or off — `hide_score`,
  `journal_by_unlock`, `quiz_puzzles` — without sending the whole
  graph.

### Changed

- Designer: wires end in an arrowhead and a step's out port is an
  arrow tab, so the canvas reads as a flow.

### Fixed

- Player: a right answer's "Correct!" (and any other result of an
  answer that moves the team on) no longer shows in the button area
  of the next step. The celebration still plays.
- Dev phone: moving to a step shows the theme that step is reached
  with — from the nearest theme effect before it — instead of keeping
  the look of the step it left.

## [0.33.6] - 2026-10-19

### Fixed

- Designer: changing an adventure's theme switches to it. Adventure
  settings → Theme (also More → Theme) shows the theme players see:
  Change theme… puts another in its place — the default, the
  organisation's or a starter — and Customize… edits it. Before, a
  picked theme was only added beside it, so the old look (falling
  stars and all) stayed. Themes that steps switch to are listed below.
- A starter theme's cover picture now comes with it for a designer
  adventure too, once published, as the gallery shows.

## [0.33.5] - 2026-10-18

### Changed

- Designer: an adventure's look is its main theme, one of its own
  themes. Adventure settings → Theme lists them, with a star on the
  main one: Edit changes a theme, Make main switches, and Add theme…
  copies one from the organisation's or the starters. Editing the main
  theme changes what players see, once published; there is no theme to
  pick again any more. A step's "Change the theme" offers the main
  theme and the adventure's others. Existing adventures keep their look
  as a theme named Main (or the theme it already matched), and steps
  that switched to a built-in Dark, Night, Alarm or Parchment switch to
  a copy of it among their themes.
- Assistant: `edit_adventure_graph` `put_theme` takes `main: true`;
  `update_adventure_meta` refuses look fields for an adventure with a
  main theme.

### Fixed

- Own domain: the branding step no longer asks for a title typeface of
  your own; the site's typeface counts as chosen.

## [0.33.4] - 2026-10-18

### Changed

- Designer: publishing says which version players get now and what it
  is called. A name ending in a number suggests the next one ("v2"
  offers "v3"), selected so typing replaces it.

## [0.33.3] - 2026-10-18

### Changed

- Designer: an edit in the inspector says so beside it — "Saving…",
  then "Saved" for a moment, or "Not saved" when the save failed. The
  toolbar says "Not saved" too, rather than saving on forever.

## [0.33.2] - 2026-10-18

### Changed

- The player's menu holds the only language switch; the one in the
  step's top bar is gone. In a translated adventure each player now
  picks their own language: the adventure's title, intro and steps
  follow along with the app's words on their phone, while teammates
  keep theirs. The team's language stays the default, and what is sent
  to the team (emails, the recap) stays in it.
- Designer: with the top bar's language set to a translation, the
  inspector shows the step in that language and edits its translation;
  what is not text (points, zones, timing) still changes the step.
  Adding or removing parts of a step is done in the main language.

### Fixed

- `{{team}}` is filled in with the team's name in the adventure's closing
  text (on the phone and the recap page), its intro in the lobby, the
  time-up message, a crossroads' text, hints, journal entries, role
  cards and the organisers' messages, not shown as is.
- Designer: switching language shows every translated text of a step
  — chat lines, hints, wrong-answer replies, quiz options and the rest
  — not only its title, text blocks and routes.
- The compass says "You are at …" only once the server places the
  phone there too, so an answer given then is not refused as not
  there; while a compass waits, the phone reports where it is every
  few seconds.

## [0.33.1] - 2026-10-17

### Added

- Gift-box partners take a contact person, a contact email and a link
  to the partner's portal, for the organisation's eyes only. Each code
  waiting for review links to the portal it is checked in.

### Changed

- Orders: ticket sales and the partner setup have their own tabs instead
  of following the list on every tab.

### Fixed

- Editing a gift-box partner's name, codes or hint failed with "invalid
  JSON".

## [0.33.0] - 2026-10-17

### Added

- `ZV_OIDC_OPEN=1` turns single sign-on into "Sign in with Google" for
  everyone, on login and sign-up: a Google sign-in opens the account
  with the same verified email, or makes one in no organisation while
  signup is on. Passwords keep working, and two-factor codes are still
  asked for.
- Console: a session that ran out while a page was open brings up the
  login form instead of "Login required" errors.

## [0.32.11] - 2026-10-16

### Changed

- `.env.example` runs the release image from ghcr.io (`COMPOSE_FILE`
  with `compose.prod.yaml`), so `cp .env.example .env` and
  `docker compose up -d` pull instead of building. Delete the line to
  build from source; add `compose.override.yaml` to it if you use one.
- The party creator, its host page and the cards PDF say to put the
  cards out along a trail, not to hide them.

## [0.32.10] - 2026-10-16

### Changed

- The stylesheets are simpler: rules repeated across the console, the
  player, the party creator and the public pages are shared, fixed sizes
  and colours use the design tokens, and unused rules are gone. Pages
  look the same, apart from slightly taller body lines and a smaller
  footer on the public pages, and small shifts in a few radii and gaps.
- The start-code field takes the adventure's own accent.

### Fixed

- In dark mode, the admin change list, the designer's modal banner, the
  trail chips and the recap receipt no longer show light borders or
  backgrounds.
- On phones, the designer's inspector sheet has no stray left border.
- The quick palette's free-text box is borderless, like its search box.
- The roles tick is readable on a light accent.

### Removed

- The unused `/assets/fsmap.css`.

## [0.32.9] - 2026-10-15

### Added

- `ZV_SHOP_HOST` gives the shop a hostname of its own, such as
  `shop.example.com`: it serves the shop at its root, so `/book` is
  `/shop/book` and an old shop's links keep working; the TLS check lets
  the proxy fetch its certificate; a book bought there comes back to it
  after checkout; and no organisation can claim it as its own domain.

## [0.32.8] - 2026-10-15

### Added

- Books in the shop can be bought. A book page asks for an email address
  and opens Stripe Checkout, which asks where to post the book. The sale
  is the home organisation's (`ZV_HOME_ORG`), paid to its own Stripe
  account with nothing taken by the platform. The books and their prices
  are in `shop/products.json` beside the shop's pages in
  `ZV_WEB_OVERRIDE_DIR`; the price is always read from there. A paid
  order is in the organisation's Orders to post and its Sales, the buyer
  gets an order confirmation, and the organisation's contact address the
  order.

### Changed

- The store's adventure cards look like the ones on the landing page:
  the adventure's icon on the cover, where it starts, the team size, and
  the price per team with "Buy" at the foot.

## [0.32.7] - 2026-10-15

### Fixed

- "Stripe dashboard" in the console opens the organisation's own Stripe
  account, not whichever account the user last had open in Stripe.
- A ticket priced at or below the platform's price for its team is
  refused when the price or team size is saved, with the reason, instead
  of failing when a buyer tries to pay.

## [0.32.6] - 2026-10-15

### Added

- A platform admin can disconnect an organisation's Stripe account from
  its page in the console, say one onboarded with the wrong company. The
  organisation's next "Connect Stripe" creates a new account.

## [0.32.5] - 2026-10-15

### Fixed

- Connecting an organisation's Stripe account failed: Stripe needs the
  account's country. It is now the first country the organisation posts
  letters to, else the platform's (`ZV_SHIP_COUNTRIES`, default SE).

## [0.32.4] - 2026-10-15

### Changed

- Pay per team. Every team that plays an organisation's event costs one
  price (49 kr by default), its first 3 phones included, and 15 kr for
  each phone after: counted once, as its first phone joins a game; a
  team that never plays costs nothing. The same count everywhere: live
  events, hosted games, voucher codes, free tickets. Event passes,
  hosted passes, free players per event and the percentage platform fee
  are gone; passes already bought still work.
- Organisations pay from a balance under Settings → Billing: top up by
  card (bigger top-ups give a bonus), or be billed monthly for last
  month's teams, to a saved card on the 1st or by invoice, within a
  credit limit that grows after paid bills. When the balance runs out,
  new teams can't start; the org admins are emailed before it does.
- Store sales are paid to the organisation's own Stripe account; the
  team's price is the platform's share of each sale, and Stripe's card
  fee is the seller's. A refund before the team played gives that share
  back, also when made in the seller's Stripe dashboard.
- Kids' parties from the public creator: the first 5 teams a game are
  free for 3 games; the buyer picks more teams at checkout, at the team
  price each. A party booked from an organisation's offer picks its
  number of teams.
- Scheduling an event needs no pass: pick an adventure and go. Host it
  is free to start, each team charged as it joins.
- Platform admins set every price under Settings → Pricing, see an
  organisation's balance on its page, credit or charge it with a note,
  set its credit limit, and mark invoice bills paid.

### Fixed

- "Ask for a quote" opens the form in the page's language.

## [0.32.3] - 2026-10-15

### Added

- Requests for a quote. An organisation can turn on a guided form on its
  public page (`/o/{slug}/request`, `/request` on its own domain) where
  a company planning a kickoff, a team day, a conference activity or a
  customer event asks it for a price: one question a screen (the
  occasion, when, how many, where, how much to do, extras, budget,
  contact), then a summary to check. The form wears the organisation's
  look — its colours, title typeface, cover and logo — in English and
  Swedish. The organisation's page shows "Ask for a quote" while the
  form is on.
- Each request is emailed to the organisation's contact address,
  replying to the requester, who gets a copy of what they asked for.
  Org admins find them under Requests in the console, mark each New,
  Answered, Won or Lost, keep notes on it and reply by email. The quote
  itself is written outside Zonventure.
- On the Requests page an organisation turns the form on, writes the
  few lines at its top, and unticks what it doesn't offer (a kind of
  adventure, a game master, a prize ceremony, results on a big screen,
  its own questions, a logo, accessibility, a language); the form then
  doesn't ask for it.
- The form has a spam trap: a hidden field, a signed stamp of when the
  form loaded, and at most 5 requests an hour from one address.
- Requests are deleted a year after their last change, travel with an
  organisation's export, and are covered in the terms and the privacy
  page: each organisation is responsible for its own requests.

## [0.32.2] - 2026-10-14

0.32.0 and 0.32.1 were tagged but their images never published (CI
failed); this release ships their changes.

### Fixed

- The players' rate limiter could be created twice when the first
  requests arrived together, letting a burst slip past it.
- Formatting that newer Go versions and CI disagreed on.

## [0.32.1] - 2026-10-14

### Changed

- An organisation's store page (on its own domain, and on servers
  without their own store page) is redesigned: a full-width hero on the
  organisation's cover picture, or its first adventure's, with its logo,
  name, tagline and how many adventures it has; cards of even height
  with the price on the picture; "How it works" in three steps; a box
  where a code from an envelope, a gift card or an email starts the
  team; and a footer with the organisation's contact address. An
  adventure's page continues the look, its cover across the top and the
  buy card beside the details.
- An organisation's look is set in one place, Settings → Organization
  (Branding): logo, cover, colours (picked from the logo, or fixed when
  text would be hard to read), tagline and now its title typeface, with
  a preview of its store, a ticket email and a player's screen. The
  typeface applies to its store and adventure pages everywhere, not only
  on its own domain, and to its emails' headings. Settings → Domain,
  "Make it yours", ticks off what is done and leads to Branding; the
  name on the domain, the replies address and the terms and privacy
  links stay there.
- Changes to an organisation's page (name, tagline, website, logo,
  cover) no longer wait for review once something of theirs is for
  sale: they apply at once and a reviewer looks afterwards, under
  Content review, keeping a change or rolling it back to what was there
  before with a reason the organisation's admins get by email.
  Organisations on invoice billing or with their own domain, like
  verified creators, aren't reviewed at all.
- On its own domain, an organisation's emails have their headings in
  its store's typeface, in mail apps that show web fonts.

### Fixed

- Dark mode on an organisation's store and adventure pages: the theme
  switch did nothing, and the system's dark mode replaced the
  organisation's colours with the platform's. Its accents now stay,
  lightened where they are text.
- Settings → Domain: the ticket email preview showed no logo when the
  console was open at another address than the server's public one.

## [0.32.0] - 2026-10-14

### Added

- Voucher codes you pay later, for selling adventures as envelopes or
  gift cards in bookstores and shops. On a self-guided event, an
  organisation with invoice billing makes a batch of codes (up to 2000,
  with the players per code) and downloads them as CSV for printing. A
  buyer types the code at /play: its first use starts a team, and
  teammates who type it land on the same team. Each code used is charged
  once, at the price under Settings → Pricing → Voucher codes (default
  29 kr), and unused codes cost nothing. A batch can be voided, so its
  unused codes stop working. Platform admins see what each organisation
  owes under Settings → Purchases and mark it invoiced with the
  invoice's number; the organisation sees what it owes on its Billing
  page.

## [0.31.2] - 2026-10-14

### Added

- Settings → Domain, "Make it yours": pick your own colours (accent,
  secondary accent, background, text) right there, with the preview
  following as you pick. The tagline can be changed there too, not only
  added once.

### Fixed

- A tagline set under Settings → Domain seemed not to save for an
  organisation with something for sale: it waits for review, and now
  says so, with the review panel to send it in. Picking colours there no
  longer throws away a tagline that waits for review.
- The title typeface list shows the site's own typeface, Aclonica, as
  the first choice, in its own face; the previews use it when no other
  is chosen.

## [0.31.1] - 2026-10-14

### Changed

- An organisation given its own domain with no end date may use a
  subdomain of the server's own domain (`adventures.zonventure.com`).
  The server's own hostname and the CNAME names stay refused, and other
  organisations still can't claim names there.

## [0.31.0] - 2026-10-14

### Added

- Organisations' own domains. A platform admin grants one under
  Organizations → Own domain; the organisation's admins then set it up
  under Settings → Domain in four steps: choose the address
  (`adventures.theirbrand.se`), add one CNAME record (with step-by-step
  guides for Loopia, One.com, Binero, GoDaddy, Namecheap, Cloudflare and
  Squarespace, or "send these instructions" to whoever runs their DNS),
  a live check that names the fix when the record is wrong, and a
  branding checklist that can pick colours from the logo. On its own
  domain an organisation's store, tickets and games show only its name,
  logo and colours; its emails go out under its name; every player link
  uses its address. Turned on with `ZV_DOMAIN_CNAME_SUFFIX` and a TLS
  proxy that fetches certificates on demand (see docs/selfhosting.md).
- Settings → Pricing: the own-domain price per year and the link
  organisations use to ask for it, also shown on `/pricing`.
- On its own domain an organisation can also choose the name it goes
  by, its title typeface, and links to its own terms and privacy policy.
  A platform admin can grant an own domain with no end date, for an
  organisation that isn't billed.

### Fixed

- An adventure look's title typeface now shows on its store page; the
  font files were looked up under the wrong name.

## [0.30.4] - 2026-10-14

### Added

- Gift box partners are each organisation's own. Under Gift boxes →
  Partners, "Add partner" sets one up: its name, the one or two codes
  its boxes carry (each with a label in English and Swedish, an example,
  a shortest and longest length, digits only or letters and digits) and
  a hint for buyers. "Edit partner" changes it later. The redeem page
  asks for exactly those codes. Existing Live it and TrueStory partners
  carry over unchanged.
- `ZV_CURRENCY` sets the currency everything is sold in (ISO 4217, two
  decimals; default `sek`). Set it before the first sale.
- `ZV_TIME_ZONE` and `ZV_SHIP_COUNTRIES` set the time zone and the
  countries letters are posted to for an organisation that hasn't chosen
  its own (defaults Europe/Stockholm and SE). `ZV_MAP_CENTER` (`lat,lng`)
  sets where a map opens with nothing to show.

### Changed

- A gift box code can be redeemed once per partner, no longer once
  across every organisation that takes the same partner's boxes.
- Price labels in the console show the instance's currency instead of
  "kr" or "SEK".

### Upgrading

- An operator's own `redeem.html` override must follow the new script:
  partners are picked by `id` and their inputs come from `fields`
  (see `internal/server/web/redeem.html`). The `REDEEM_BOX_NUMBER_LABEL`,
  `REDEEM_SECURITY_CODE_LABEL`, `REDEEM_EXPERIENCE_CODE_LABEL`,
  `REDEEM_LIVEIT_HINT` and `REDEEM_TRUESTORY_HINT` placeholders are gone.

## [0.30.3] - 2026-10-14

### Changed

- Every dialog in the console and the designer shares one design: the
  same head (with an icon and a subtitle where it helps), close button,
  sizes, footer and focus handling. On a phone a dialog opens as a sheet
  from the bottom.
- The Languages dialog is now a translator: the languages and how far
  each has come down the side, every text with the original beside its
  translation, grouped by step, each marked translated, untranslated or
  outdated. Search the texts, show only what is left to do or outdated,
  copy the original, and press Ctrl+Enter to go to the next text. The
  adventure settings show each language's progress and open the
  translator.

## [0.30.2] - 2026-10-14

### Added

- The console's dialogs have a close button in the top-right corner.
- The designer's More menu shows an icon beside each entry.

### Changed

- More → Languages lists every text of the adventure for the chosen translation — the adventure's own, then each step's in the order a team meets them — with the original beside each box. "Only untranslated", on by default, shows just what is missing or outdated.
- The designer's language switch shows the adventure in that language: the canvas, the list view and the phone preview read the translation, and the original where there is none. Steps are still edited in the main language; the inspector no longer turns into a translation form.

## [0.30.1] - 2026-10-14

### Added

- The Hacker, Scary, Sad, Love and Cyberpunk themes have cover pictures, on the web, in print and in emails, as every other starter theme does.
- The party adventure creator offers the Hacker, Cyberpunk, Scary and Love themes.

## [0.30.0] - 2026-10-14

### Added

- Two-factor authentication can be required: per organisation by its admins (Settings → Members) or a global admin (the organisation's page), and for every global admin and Support user (Settings → System). A covered user without 2FA sees only the setup and their API tokens are refused until they enrol; SSO sign-in counts. Members and Admins show who has 2FA on, a required 2FA can't be turned off, and turning a requirement off asks for the password. Needs `ZV_TOTP_KEY`.

### Changed

- Signing listing reviews with a passkey is optional. Approve and reject still ask for the passkey; without one, or with the passkey prompt cancelled, the reviewer confirms deciding unsigned. The log check no longer flags unsigned console decisions.

## [0.29.2] - 2026-10-13

### Added

- Orders has a print queue: each click opens the next letter of the orders shown, and at the end they can be marked as posted together.
- A posted letter's name and address are deleted 90 days after posting, or after a box redemption is turned down; the period is under Settings → Legal and in the privacy policy.

### Changed

- The console's Events are now Live events (Liveevent), set apart from Self-guided and Hosted ones.
- Party offers and booked parties are now party adventures (Partyäventyr), as the public site sells them. In Swedish, Hosted is now Med värd.
- Listing reviews is now Content review (Innehållsgranskning), and its Swedish texts no longer call what is reviewed an advert.

## [0.29.1] - 2026-10-13

### Fixed

- Ad blockers no longer hide parts of the platform admin's pages, such as the form for adding a listing reviewer.

## [0.29.0] - 2026-10-12

### Added

- Store tickets for an adventure that posts a letter ask for the letter's language on the event page and for a postal address at checkout; the ticket email repeats where it goes.
- Organisations choose the countries they post letters to, under Settings → Organization.
- A running self-guided event can move to its adventure's latest published version. One for sale moves once its listing is approved; teams already playing keep their version.
- Box redemptions is now Orders: it lists posted store purchases beside gift boxes, with print buttons and "Mark as posted" for both.
- The redeem page asks for an address and envelope language only when the adventure posts a letter, and for the country when the organisation posts to more than one.
- A gift box buyer gets an email when their code arrives, saying it is being checked and where their envelope goes.
- The box confirmation email includes the adventure's note to buyers, as the ticket email does.

## [0.28.0] - 2026-10-12

### Added

- Gift box buyers choose the language their envelope is printed in when the adventure is translated, and the redemption list shows it.
- A redeemed box's emails are written in the envelope's language, and its team starts playing in it.
- Organisers can change a redemption's envelope language, and pick it when adding a redemption by hand.
- Adventures have print materials: PDFs such as a letter with the first clue, one per language, posted, emailed or kept for organisers. Players never see them.
- Assistants can upload print materials and add them with `put_print_material`.
- Box redemptions have print buttons for the adventure's posted and organiser-only materials, in the envelope's language.
- A "To post" tab lists redemptions not yet posted, the earliest post date first, and the list can be narrowed to one envelope language.
- An event can deliver its adventure's print materials differently from the adventure, such as emailing a letter in summer that is posted in winter.
- Emailed print materials are download links in the ticket and box confirmation emails, in the team's language, until the team is withdrawn.
- Assistants see an event's print materials in `get_event`, change their delivery with `set_print_delivery`, list gift boxes to post with `list_orders` and mark them sent with `mark_order_posted`.

### Fixed

- Video uploads for video blocks are no longer refused.

## [0.27.1] - 2026-10-12

### Changed

- The party adventure creator ends on one "Ready to play?" screen with teams, email, consent and a single button.
- The team count's − and + are always on the last screen, greyed out while payments are closed.
- Free party adventures also email the host link, and the email promises a receipt only when paid.
- Reopening a party adventure shows the theme's cover above the cards.
- Dragging a card follows the finger, the other cards slide aside and the list scrolls near the edge.

### Fixed

- Checkout text on white panels is readable on dark themes.

### Removed

- The creator's separate checkout page, save to email, copy link, My party adventures and delete.

## [0.27.0] - 2026-10-12

### Changed

- The party adventure creator is a fullscreen guide in the chosen theme, asking one thing per screen.
- A reopened party adventure starts on its card overview, and the phone's back button steps back through screens.
- The birthday template asks for the child's name and age and writes them into the title and questions.
- Cards use icons for card kinds, moving and deleting, and a tick marks the right option.
- The price is stated once: how many teams play free and what more costs.

### Fixed

- Free party adventures can be played up to the free team count even while payments are closed.
- Long questions no longer push cards past the edge of a narrow phone screen.

## [0.26.0] - 2026-10-10

### Added

- Listings for sale, such as store events and party offers, go public only after a listing reviewer approves them.
- A rejection is emailed to the organisation with a reason, and anything already public stays up.
- Creators submit listings for review, confirming the content is not offensive or illegal; any later edit needs resubmitting.
- Players get the approved adventure version, even when a newer version is published.
- Platform admins choose listing reviewers, and no one reviews their own organisation's listings.
- AI assistants can review and submit listings over MCP with `list_listing_reviews`, `get_listing_review`, `approve_listing`, `reject_listing` and `submit_listing_for_review`.
- Submissions and decisions go into a tamper-evident hash-chained log, verified under Listing reviews → Recent decisions → "Check the log".
- Platform admins are emailed the review log's latest hash and a check of served content against approvals daily.
- Events keep a tamper-evident record of results, with each team's receipt and "Check results" on the event page.
- Once an unverified organisation sells something, changes to its page and adventure presentation go live only after review.
- New player files on a store event reach players once the listing is approved with them.
- The database refuses changes to saved adventure versions and uploaded media.
- Reviewers sign approvals and rejections with a passkey; decisions made by AI assistants are logged as unsigned.
- Platform admins can mark an organisation as a verified creator, auto-approving its submissions and showing a badge.

### Changed

- The store shows and charges the approved listing; clearing the price, pausing or turning it off still applies at once.
- `/store-cover/event/{id}` serves the approved cover, and `?draft=1` serves the uploaded one to the organisation and admins.
- Store listings in an imported organisation bundle wait for review.

### Upgrading

- Everything for sale at upgrade stays for sale, approved as is, with the adventure version it plays now.

## [0.25.9] - 2026-10-10

### Added

- Organisers can upload up to 10 files of 20 MB each for an event's players, linked from the game and emails.

### Changed

- The party adventure creator shows each card beside a phone preview of how teams see it.
- The creator's start page puts templates and the price first, and a template starts in its own look.
- Draft party adventures say "Get it ready to play" instead of "Done".
- Template cards say they are an example until first edited.
- The right option keeps its 1, X or 2, marked green with a "Right" tag.
- Move earlier, Move later and Delete card keep their words on a phone.
- "My trails" is now "My party adventures" everywhere.

## [0.25.8] - 2026-10-09

### Fixed

- Maps load their tiles again after a map host change instead of failing with "NetworkError".

## [0.25.7] - 2026-10-09

### Changed

- Every map is an OpenFreeMap vector map, and `ZV_MAP_STYLE_URL` points them all at another MapLibre style.
- `ZV_MAP_TILE_URL` is optional and only a fallback for small maps; by default no OpenStreetMap tiles are requested.
- The store, recap and live view use MapLibre served by the instance itself.

### Fixed

- Maps in scrolling pages zoom with Ctrl (⌘ on a Mac) and the wheel, or two fingers on a phone.
- Scripts and styles are served gzipped.

### Removed

- Leaflet.

## [0.25.6] - 2026-10-09

### Fixed

- The adventure area from 0.25.5 now ships; that release was tagged without its code.

## [0.25.5] - 2026-10-09

### Added

- Adventures have an area, set under Edit details or with `update_adventure_meta`, and shown in the store.

## [0.25.4] - 2026-10-09

### Fixed

- A new organisation logo shows in emails at once.

## [0.25.3] - 2026-10-09

### Fixed

- A hub's branch pictures load on the phone.
- A hidden score stays hidden on the end screen.
- An adventure's time limit no longer throws back a team that already reached the step it sends to.

## [0.25.2] - 2026-10-09

### Added

- `{{answer:<step>}}` in a text shows what the team answered at that step.
- A "Falling snow" backdrop in the adventure's colours or chosen ones.

### Fixed

- The start's radius shows on the map under Edit details.

## [0.25.1] - 2026-10-09

### Added

- Adventures can carry a Markdown buyer note in ticket and party-host emails, never shown to players.

## [0.25.0] - 2026-10-09

### Added

- Parties keep results secret until the host finishes the game, then reveal the ranking and each team's recap.
- The surprise reveal is on for new parties, with a host-page checkbox to show results as teams play.

### Changed

- A search step reads as arriving, with a "Vi är framme" button.
- Swedish texts say "du" instead of "ni" in the player, pages, console and emails.
- Events can only be created for a published adventure and stay on that version until moved.
- Self-guided adventures no longer ask for an under-13 attestation.
- Creating a self-guided adventure marks and refuses unpublished adventures and says why.

### Fixed

- A party's result screen names each question and lists typed-answer cards, so the totals add up.
- A party's live scoreboard shows only the current game, with its own clock that stops when finished.
- The "Start here" map draws the start's radius and zooms to fit it.
- A home screen's branch is marked "Visited" only once the team has come back through its end.

## [0.24.5] - 2026-10-09

### Added

- Settings → Purchases lists ticket sales with buyer, amount and state, for an org or every org for admins.
- Platform admins can refund a sale in full, which removes bought teams or calls off the event.

### Changed

- Self-guided events priced in EUR now sell in SEK at the same amount.

### Upgrading

- Check the ticket price of any self-guided event priced before 0.21.0.

## [0.24.4] - 2026-10-08

### Changed

- Confirmations use the page's own dialog instead of the browser's.

## [0.24.3] - 2026-10-08

### Changed

- A party quiz numbers its questions "Question 1", "Question 2" instead of "Card 1".

## [0.24.2] - 2026-10-08

### Changed

- Joining a party asks only for a team name, and each phone counts as one player.
- A 🎲 button next to Team name rolls a random team name.

## [0.24.1] - 2026-10-08

### Changed

- Breaking: Stripe API version is now `2026-09-30.endive`, and the Stripe webhook endpoint must use the same version.

## [0.24.0] - 2026-10-08

### Changed

- Tutorials are seven short, guided lessons that can be done in any order, on any adventure.
- A lesson shows one step at a time with a pulsing arrow, dimming and locking the rest of the designer.
- Lesson cards have Back, Start over, Pause and Stop, and progress is kept per account in the browser.
- A lesson counts only what it adds, so it works on adventures that already have content.

## [0.23.0] - 2026-10-07

### Added

- "Tutorial: your first adventure" fills in an example and docks a ten-step guide over the designer.
- On a phone the tutorial guide is a sheet along the bottom that follows the simple step form.
- The tutorial is in English and Swedish and is offered after signing up.

### Fixed

- The designer no longer goes blank when a step with empty text is opened after a reload.
- A new "Check in at a place" step has a text to write in.
- The theme editor's "Save" is now "Share with organisation", which is what it does.

## [0.22.0] - 2026-10-07

### Added

- Player and host emails show the adventure's cover, and account emails a small picture.
- Email pictures are served from `ZV_PUBLIC_URL`; without it no pictures are sent.
- Illustrations on the player, party and console pages, including empty lists and the 404 page.

### Changed

- The store, recap and live board use the adventure's look, with its cover, colours and title font.
- An adventure's own cover is public at `/adventure-assets/cover/{slug}`.
- Privacy and Terms match the site's style and say what is missing until Settings → Legal is filled in.
- The built-in pages use the marketing site's topbar.
- Party join, host and booking pages wear the adventure's look and cover.
- Console lists show an adventure's look cover where it has no icon.
- Settings → Pricing, Legal and Themes confirm saves or errors in a toast.
- The party adventure creator fills the content area with its card row pinned in view.

### Fixed

- Printed cards stay readable in light looks.

## [0.21.1] - 2026-10-07

### Fixed

- `make upgrade` pins the release as `VERSION=X.Y.Z` in `.env`, so a bare `docker compose up -d` keeps it.

## [0.21.0] - 2026-10-07

### Added

- The theme editor takes an uploaded cover picture, shown on the join page, welcome and printed cover.

### Changed

- Settings → Legal sets company details, contacts, jurisdiction, sub-processors and retention days, with a preview.
- Saving legal settings updates /privacy, /terms and seller lines at once, and the retention sweeper on its next run.
- Settings → Pricing sets the event pass, hosted pass and platform fee in SEK, applied without a restart.
- Breaking: tickets, parties and the event pass are sold in SEK only; other currencies are refused.
- The theme editor shows themes as cards, groups its controls and keeps a phone preview in view.
- Main look offers the organisation's themes and the starters, copying a picked one into the adventure.
- Settings → Themes edits the organisation's theme library outside any adventure.

### Fixed

- A theme saved to the organisation's library no longer keeps the adventure's cover picture.

### Removed

- Breaking: `ZV_PRICE_PER_PLAYER_CENTS`, `ZV_FREE_PLAYERS_PER_EVENT`, `ZV_HOSTED_BLOCK_CENTS`, `ZV_HOSTED_CURRENCY` and `ZV_PLATFORM_FEE_*` are ignored.
- The `-retention-*-days` flags.

### Upgrading

- The first start copies `ZV_COMPANY_*`, `ZV_CONTACT_EMAIL`, `ZV_DPO_EMAIL`, `ZV_JURISDICTION` and other legal vars once, then ignores them.
- `ZV_HOSTING_PROVIDER`, `ZV_EMAIL_PROVIDER`, `ZV_LEGAL_*` and `ZV_*_RETENTION_DAYS` are likewise copied once, then ignored.
- Enter your prices under Settings → Pricing, then drop the old price vars.
- A listing priced in another currency keeps it until its price is next saved (migration `0116_sek_only`).

## [0.20.0] - 2026-10-06

### Added

- Every party adventure look has a cover picture, shown on the start, join, welcome and printed cover pages.

### Changed

- The printed cards follow the adventure's look, with coloured headers and printer-friendly bodies.
- The landing page's creator frame fits its content, so only the page scrolls.
- Looks wrap in rows instead of a sideways strip, as compact swatches on the start page.

### Fixed

- The printed cover's title turns white on dark cover pictures.
- The join page no longer repeats the title when the party and adventure share a name.
- Selecting a card in the creator no longer scrolls the landing page around the frame.

## [0.19.1] - 2026-10-06

### Added

- Site admins switch party adventures on or off per organisation, shown as a badge in the org list.

## [0.19.0] - 2026-10-06

### Added

- Party adventures pick one of 14 starter looks, which the teams' phones also get.
- The start page cycles a sample card through the looks; the one shown or tapped is used.
- Anyone can make a quiz trail without an account at `/o/{slug}/trail`, paying the hosted pass to play.
- Save to email and My trails (`/trail/mine`) keep a browser's trails and open them anywhere.
- Checkout asks for the admin email, teams per game and, for kids' trails, guardian consent.
- Keep it moves a hosted trail into the keeper's own organisation, offered a week into play.
- An hourly sweeper deletes unpaid drafts after 30 days and paid trails once their event ends.
- New adventure → Quiz trail builds an adventure from a row of cards on a phone-friendly creator.
- Quiz trails print as cards and an answer key, with a QR-code cover for hosted events.
- Open in the designer hands a quiz trail over to the full designer for good.
- Host it runs a hosted event of the org's own adventure, paid on the platform's Stripe account.
- Hosted events cost a pass per block of 15 teams per game, set by `ZV_HOSTED_BLOCK_CENTS` and `ZV_HOSTED_CURRENCY`.
- Add 15 teams buys one more block per game from the host page or console.

### Changed

- Party adventures are free for up to 3 teams per game; more cost 149 kr per 15.
- A free public checkout mails the admin link only when the browser is signed in with that address.
- Quiz trails are now called party adventures throughout the site, emails and templates.
- A team's recap opens once the team is done, not when the whole event ends.
- A party's platform fee is the pass price, and an offer must cost more than the pass.
- A party is a hosted event playing game after game for 14 days, joined from one player link.
- Finish game closes a game and mails the host a summary; idle games finish after 3 hours.
- A game takes up to 15 teams with unique names, and the host can remove a team.
- The console's Parties entry is now Hosted, open to every org member, and hosted events leave Events.
- The console no longer makes teams, schedules or starts hosted events.

## [0.18.0] - 2026-10-06

### Changed

- Parties has its own console sidebar entry instead of a Settings tab.

## [0.17.0] - 2026-10-05

### Added

- Orgs offer adventures as children's parties, booked and paid by a parent without an account.
- The booker gets a host page link to add teams, print cards and run the game.
- Parties play in QR mode: teams reach zones by scanning printed cards instead of location.
- Parties are deleted `ZV_PARTY_RETENTION_DAYS` days (default 14) after they end; refunded parties are cancelled.
- Authors mark texts, answers and party words a host may personalise, such as the birthday child's name.
- Park mode lets a host place movable zones in their own park before the party starts.
- `ZV_PLATFORM_FEE_FIXED_CENTS` adds a fixed amount to every ticket's platform fee.
- Settings → Billing has an Open Stripe dashboard button for the org's Express account.

### Changed

- Breaking: orgs now onboard as Stripe Connect Express accounts; the platform must enable Express in Stripe.
- New Express accounts are paid out monthly on the 1st.
- Orgs already on Standard accounts keep selling, but must use dashboard.stripe.com directly.

## [0.16.1] - 2026-10-05

### Changed

- `/o/{slug}` is now the organisation's single page listing every adventure it sells; `/o/{slug}/store` redirects there.
- Breaking: there is no shared store; `ZV_HOME_ORG` names the site's organisation that `/store` redirects to.

### Removed

- `GET /api/store/items` and the `org-landing.html` template.

## [0.16.0] - 2026-10-05

### Added

- Store cards and event pages show the adventure's icon, also used as the link-preview image.

### Changed

- A store event page's cover is 16:9 instead of cropped to 4:3.

### Fixed

- Self-guided adventures no longer close when every team so far has finished; "End event" is refused.

## [0.15.0] - 2026-10-05

### Added

- A zone can be hidden from teams, so finding the place becomes the puzzle.

### Fixed

- A team out of time can no longer go back and play on without a clock.

## [0.14.0] - 2026-10-05

### Added

- Org admins save themes for the whole organisation, offered first wherever a theme starts.
- MCP `list_org_themes` lists the organisation's themes.
- MCP `edit_adventure_graph` gains `set_text` and `patch_node`, editing the dev phone's step by default.
- MCP `get_step` reads one step, by default the one on the dev phone.

### Changed

- Breaking: MCP `get_media_upload_url` is now `get_upload_link`, returning a page to pick a file from a phone.
- MCP `get_upload_link` covers step media, the adventure icon and event covers; `list_adventure_media` lists uploads.
- MCP `update_event` sets ticket price, currency, players per ticket, store blurb and secret roles.
- MCP `create_event` explains when an event pass is needed and links to buying one.
- Breaking: MCP `create_graph_adventure` is now `create_adventure`; the old-format `create_adventure` and `validate_adventure` are gone.
- MCP `get_adventure` returns brief, look, starting point and icon; `list_adventures` says which are editable and playable.
- Breaking: MCP tools renamed to `set_adventure_icon`, `remove_adventure_icon` and `playtest_start`/`playtest_act`/`playtest_end`.
- The MCP assistant gives shorter replies, one question at a time.
- The DEV bar sits at the top of the phone, above the adventure's bar, while scrolling.

### Fixed

- MCP `get_event_state_summary` no longer reports an empty state and no teams.
- MCP `create_purchase` works on servers running simulated payments.
- Changes to an adventure's look or texts show on dev phones at once.
- A dev ticket that reached the end can be played again.
- The QR answer scanner is centred on the screen instead of sitting low.

## [0.13.0] - 2026-10-05

### Added

- Dev tickets play an adventure's draft on a phone, updating within a second of every save.
- The DEV bar jumps to any step or starts over.
- Dev tickets come from the designer's menu as a QR code, link or email, or via MCP tools.
- Arrangers can let a team start anywhere, from the event console, MCP or `POST /api/admin/teams/{id}/start-anywhere`.
- MCP `play_start` and `simulate_adventure` take `anywhere: true` for playtests away from the site.
- New adventure is a four-step guide with a phone preview, opening a playable adventure in the designer.
- Themes choose how a right answer is celebrated: confetti, emoji, a tick or only words.
- Themes set their own words for "Correct!" and "Not quite", and their own answer sounds.

### Changed

- The MCP assistant guides a new adventure through six steps and hands over a dev ticket early.
- The player's bar shows the adventure's own picture, falling back to the organisation's logo.

## [0.12.0] - 2026-10-05

### Added
- Themes can set button shape, fill and size, a bottom or floating answer bar, text size and title style.
- Any adventure image can be the page background, with strength, blur, fade and a fixed option.
- Blocks can have an entrance animation, an attention effect, a duration and a delay.
- A look's step transition sets how each step comes in and how the previous one leaves.
- Phones set to reduce motion show animations and transitions still.
- Show again in the preview replays a step's entrance, animations and transition.
- An adventure can keep its own saved themes, which a Change the theme step can switch to.
- MCP: `edit_adventure_graph` has `put_theme` and `delete_theme`.
- New themes can start from the default look or one of 19 ready-made starter themes.
- Stripes and confetti take up to 8 colours, and retro horizon and lined paper backgrounds are new.
- A "Colour spots" background paints up to 12 soft, blending spots placed by clicking.
- The theme editor has a background pattern designer with presets, colours, strength, size, fade and drift.
- `backdrop` on `PATCH /api/admin/adventures/{slug}` and `update_adventure_meta` sets the background pattern.
- Eleven more typefaces, including playful title fonts, all self-hosted with their licences.
- Text cards can be see-through, optionally frosted, so the background shows through.
- The theme editor warns when text, titles or buttons would be hard to read outdoors.
- Adventures have a "Self-guided adventure" button that opens the new self-guided adventure dialog.
- Editors can test play an adventure's saved draft from its page without opening the designer.

### Changed
- Adventure settings now only picks the main look: the standard look or a saved theme.

### Fixed
- Roles made in the designer's Resources panel are no longer dropped by the next save.
- The designer's Add step search now matches Swedish names, descriptions and search words.

## [0.11.1] - 2026-10-05

_No changes recorded._

## [0.11.0] - 2026-10-05

### Added
- Organisers can give someone a free ticket by email from the event's Teams tab, API or MCP.

## [0.10.0] - 2026-10-05

### Added
- An adventure can require teams to be within a radius of the starting point before they begin.

### Fixed
- Clearing the starting location in the adventure details now actually clears it.

## [0.9.3] - 2026-10-04

### Added
- The per-organisation store page receives the organisation's name, logo and branding.

## [0.9.2] - 2026-10-04

### Security
- Session, CSRF and language cookies are marked `Secure` again behind a trusted reverse proxy.

## [0.9.1] - 2026-10-03

### Added
- Release tags publish `ghcr.io/klahr/zonventure` images, and `make upgrade` can pull instead of build.

## [0.9.0] - 2026-10-03

### Added
- Any `<name>.html` in `ZV_WEB_OVERRIDE_DIR` is served at `/<name>` with the shared topbar and footer.

### Removed
- Breaking: `ZV_LANDING_ENABLED` is gone; `/` and `/pricing` are served only from override `landing.html` and `pricing.html`.

## [0.8.2] - 2026-10-02

_No changes recorded._

## [0.8.1] - 2026-10-02

_No changes recorded._

## [0.8.0] - 2026-10-02

### Added
- Adventures can declare roles that players take, and blocks can show only to a role's holder.
- A role can have a picture, shown beside it on every phone.
- A Roles block shows roles as portraits and can let players choose theirs.
- Roles can be made final at a step, so they no longer move between players mid-game.
- A player can hand a role's kit item to a teammate by QR code.
- Conditions can check how many players are inside a zone, or that zones are held at once.
- Blocks can show only on the phone of the player standing in a zone.
- A secret saboteur role, with Sabotage and Accusation blocks, can be turned off per event.
- A puzzle can be answerable only by one role, and roles can bring items.
- Two-key launch puzzles open when enough players turn their keys at the same time.
- Unanimous answers count only once every player has answered the same way.
- Phones keep reporting their position every 15 seconds while a step waits on a place.
- The simulator splits teams up to open branches that need players in several places.

### Changed
- Breaking: a hub branch waiting on where players are now holds the team at the hub.
- "Inside a zone" now means any player is inside it, not the team's latest ping.
- Every single-picture setting now uses a dashed box: click to upload, Remove below.

### Fixed
- Teammates a few streets apart are no longer rejected as teleporting.
- Pictures in zone-only blocks are no longer refused to a player in that zone.

## [0.7.0] - 2026-10-01

_No changes recorded._

## [0.6.0] - 2026-10-01

### Added
- Players can set their team's picture, shown on the leaderboard and organisers' map.

### Changed
- Team names and picture are fixed for players once the team makes its first move.

## [0.5.4] - 2026-10-01

_No changes recorded._

## [0.5.3] - 2026-09-30

_No changes recorded._

## [0.5.2] - 2026-09-30

_No changes recorded._

## [0.5.1] - 2026-09-30

_No changes recorded._

## [0.5.0] - 2026-09-30

### Added
- MCP: `edit_adventure_graph` applies small all-or-nothing graph edits, and `undo_adventure_graph_edit` reverts them.
- Field mode shares the author's position so an assistant can place a zone where they stand.
- Each adventure has a brief (theme, audience, length) set with `update_adventure_meta`.
- The MCP server explains how to co-author an adventure, with no setup prompt needed.
- The MCP endpoint supports OAuth 2.1, so it works as a claude.ai custom connector.
- The designer shows an assistant's edits live as they are saved.
- A save based on an older draft is refused instead of overwriting newer work.
- An assistant with a shell can upload photos via a short-lived upload link.
- `simulate_adventure` plays the draft, and `upload_adventure_media` adds pictures and sounds.

## [0.4.1] - 2026-09-30

### Fixed
- Blocks and list-view steps can be dragged by their handle again, with mouse or finger.
- A step dragged in the list view lands where it appears to be.
- On a phone, the side menu opens over an open step, also in the designer.

## [0.4.0] - 2026-09-30

### Added
- A step's place can be pinned to where the phone stands in one tap.
- Zone maps have Use my location and Add a corner where I stand.
- Phones get a simple step form, with Take photo and More options for every setting.
- Add block on a phone lists the six common blocks first.
- Test here with my location moves the preview's test team with the phone.
- Sign-up can create your own organisation so you can start making adventures at once.

### Changed
- A puzzle's Routes are now called Answers and next steps.
- An adventure's starting location has a map for placing the pin.
- Compass, map and hot-and-cold blocks can create a new place from their zone list.
- Settings is reached from the account menu only.
- An organisation's logo or initials show beside its name in the top bar.
- Adventure settings shows each section as a card.
- Start a new event and Schedule event appear only once a published adventure exists.
- Use first gated puzzle works on unpublished drafts and follows playing order.
- Logging out lands on the site's front page.
- iPhone HEIC photos are always converted to JPEG before upload.
- The favicon and install icons use the new Zonventure logo.
- Shared links get a real `/og-image.png` preview, replaceable via `ZV_WEB_OVERRIDE_DIR`.

## [0.3.3] - 2026-09-30

### Added
- The `shop/` folder of `ZV_WEB_OVERRIDE_DIR` is served as a static site at `/shop/`.

## [0.3.2] - 2026-09-30

### Added
- Item slots let a puzzle be answered with items from the backpack.
- A team missing the needed items is told and can leave to find them.
- Steps can be reordered anywhere in the designer's list view, into or out of branches.

### Changed
- The shared catalog is switched off; other organisations reach adventures only via commission or event pass.

## [0.3.1] - 2026-09-29

### Added
- API tokens now work on `/api/admin` as `Authorization: Bearer`, with `read` tokens limited to GET.

### Security
- Account and credential routes refuse API tokens and stay session-only.

## [0.3.0] - 2026-09-29

### Added
- Buyers can redeem Live it and TrueStory gift boxes at `/redeem` or `/o/{slug}/redeem`.
- Organisers approve box redemptions, which creates a team and emails its link.
- Org admins choose which partners and products give access to which self-guided adventure.

### Fixed
- The public board's socket no longer drops every 40 seconds.
- The public board hides puzzle locations until the event is over.
- The completion screen crowns the top-ranked team rather than the first finisher.
- Race control keeps up during busy events and keeps map view, popups and scroll position.
- Team trails are capped at about 120 points to stay small.
- Elapsed time leaves out pauses and uses the server's clock; earlier pauses count as zero.
- The public board pages through every team and shows withdrawn teams last.
- The public board recovers on its own from a half-open socket and has its own rate limit.
- Race control shows how long since each team last did anything.
- Race control stops updating once the viewer loses access to the event.
- The live board's footer shows a readable time.

## [0.2.0] - 2026-09-28

### Upgrading

- Back up first: migrations 0056–0070 run on first start, and `0065`/`0066` rewrite existing rows.
- Add an org admin to the new Zonventure Catalog org to keep editing the catalog.
- To move to a new install, restore the 0.1.x dump into an empty database and keep `ZV_TOTP_KEY`.
- Compose volumes are named after the checkout directory, so a new directory starts with an empty database.
- Point the checkout's remote at `github.com/klahr/zonventure` before `make upgrade`.
- Behind a reverse proxy, list it in the new `ZV_TRUSTED_PROXIES` or forwarded headers are ignored.

### Added

- Twelve new blocks, including split secret, bearing lock, incoming call, photo challenge, Morse flasher, team vote and random draw.
- Organisers review photo challenges from the event's control page, and approved photos show on the recap.
- New block options: torch styles, mirror writing, a turnable Caesar wheel, and reversed or spectrogram audio.
- Graph steps are built from ordered content blocks such as text, notes, maps, compass, radar, cipher sheets and standings.
- Any block can wait for a condition, such as being near a zone or metres walked, and show a teaser meanwhile.
- Any block can be switched off and kept for later, and an adventure can hide the running score.
- Quiz walks: 1-X-2 quiz blocks with one tie-breaker question per adventure, revealed when the team finishes.
- Texts can use `{{score}}`, `{{counter:<slug>}}`, `{{item:<slug>}}` and `{{team}}`.
- Video uploads, capped by `ZV_MAX_VIDEO_BYTES` (default 30 MiB).
- Organisers can message all or chosen teams, shown in an inbox in the player app.
- Puzzles let the author choose how the team answers: typed, QR scan, options, lock, tile picture, search or quiz questions.
- Typed answers have their own placeholder, button and keyboard settings, and multi-part answers get one box per part.
- Webhook steps can set method and headers, and email steps have a subject and message sent via `ZV_SMTP_*`.
- A public store at `/store` sells tickets for always-on events, with a detail page per event.
- Quests: a dedicated console page for always-on, ticket-sold events, created and started from one form.
- The hosted instance can take a Stripe Connect fee on store sales via `ZV_PLATFORM_FEE_PERCENT_BPS` and `ZV_PLATFORM_FEE_MIN_CENTS`.
- `ZV_PAYMENTS_PROVIDER=simulated` enables a development-only payment provider that needs no real money.
- MCP server at `POST /mcp` (`ZV_MCP_ENABLED=1`) for authoring adventures and running whole events, with read or write tokens.
- Players who lost their link can request a fresh one with the team's contact email.
- Puzzles and hints can have audio attachments, capped by `ZV_MAX_AUDIO_BYTES` (default 15 MiB).
- Play works offline: the phone caches the adventure's media and keeps queued answers across reloads.

### Changed

- Breaking: the platform admin no longer authors adventures or runs events; organisations do.
- Breaking: catalog adventures belong to a publisher org, and only the operator publishes them.
- Breaking: an event's `public_for_sale` is fixed when it is created.
- Designed adventures show their graph on the adventure, catalog, event, race control and recap pages.
- Graph events rank by points everywhere, with the tie-breaker deciding between finished teams.
- Copying, exporting and importing an adventure now include its graph and media.
- Classic lane edits and "Skip this puzzle" are refused for designed adventures.
- Only a QR scan puzzle has a scan button; typed answers no longer do.
- Destructive actions, pausing an event and live-event edits now ask in an in-app dialog first.
- The public live map no longer shows full team names.
- Hints can be reordered with up and down buttons.
- Server error messages are shown in Swedish when a translation exists.
- Static assets are cached between visits, and a new service worker no longer takes over tabs mid-event.
- `make` lists its targets, adds `make rebuild` and `make reset`, and drops `make deploy`, `seed-compose` and `restart`.

### Fixed

- Webhook and email steps are now actually sent, with retries for about a day.
- Paid ticket emails are retried when the mail server is down, instead of being lost.
- Only org admins can create adventures, and unpublished catalog adventures are no longer open to every org.
- Player answers are kept until the server confirms them, so none are lost on a dropped connection.
- Confirming an email change no longer fails.
- Unpaid Stripe sessions no longer count as paid, and a partial refund no longer withdraws the team.
- Retried Stripe requests no longer create duplicate refunds or checkouts.
- Two servers starting at once no longer run migrations twice.
- Player dialogs work with a keyboard, and player buttons have 44 px tap targets.
- The iOS keyboard no longer hides the answer box, and autocorrect no longer changes answers.
- The console respects reduced motion and has better text contrast.
- The live leaderboard stays smooth with many teams and on screens left running for long.
- Swedish translations are corrected, and the 404 page is translated.

### Removed

- The shared puzzle library and its endpoints.

### Security

- Webhooks can no longer call private or internal addresses, nor follow redirects.
- Webhook header values, such as tokens, are stored in the adventure and readable by anyone who can open or export it.
- Maps load from the server itself, and player pages no longer load scripts from a CDN.
- Spreadsheet imports reject ZIP bombs and defuse names that start with a formula character.
- Email links and the team QR code use only `ZV_PUBLIC_URL`, never request headers.
- Sessions expire after 7 days without use, and magic-link login is rate-limited.
- Implausible GPS jumps are rejected, so players cannot fake reaching a zone.
- Hint texts stay off the phone until revealed, and hint timers are enforced by the server.
- Audio uploads are checked by content, not by the declared type.
- Seeded demo accounts show the change-your-password banner.
- The live board's GPS map stays off unless the organiser turns it on.
- `/privacy` and `/terms` fail until `ZV_COMPANY_NAME`, `ZV_CONTACT_EMAIL` and `ZV_JURISDICTION` are set.
- Email headers reject injected line breaks, and OIDC and recovery-code checks are stricter.

## [0.1.5] - 2026-05-01

### Added

- The sidebar footer shows the running version to every signed-in user.
- Admins see an "Update to v<latest>" button in the sidebar when a newer release exists.
- `make upgrade` fetches tags, checks out the newest release and rebuilds the Docker image.

## [0.1.4] - 2026-04-30

### Fixed

- Two-factor login works again; anyone who enrolled in 0.1.0–0.1.3 was locked out.
- The 2FA enrolment QR panel and email-change revert banner now start hidden as intended.
- The in-app upgrade instructions now show a recipe that actually rebuilds the app image.

## [0.1.3] - 2026-04-30

### Fixed

- Docker builds of a clean tagged checkout no longer report a `-dirty` version.

## [0.1.2] - 2026-04-30

### Fixed

- Docker builds now report their real version in `/api/version` instead of `dev`.

## [0.1.1] - 2026-04-30

### Added

- Admins get a Settings → Software update page that checks daily for new releases and lists the changes.
- `ZV_CHANGELOG_URL` sets where the update check fetches the changelog from.

## [0.1.0] - 2026-04-30

First tagged release.

### Added

- Players join by magic link without installing an app and solve multi-puzzle adventures with GPS puzzles and timed hints.
- A live audience view follows the event in real time.
- Arrangers author adventures, puzzles, hints and teams, schedule events, brand adventures and test pacing with a race simulator.
- Admin, org-admin and org-member roles, multiple organisations per user, TOTP two-factor authentication and an audit log.
- English and Swedish throughout.
- Self-hosting as a single Go binary in a one-command Docker Compose stack on Postgres.
- Configurable retention, optional Prometheus metrics and pg_dump-portable backup and restore.

[Unreleased]: https://github.com/klahr/zonventure/compare/0.27.0...main
[0.2.0]: https://github.com/klahr/zonventure/releases/tag/0.2.0
[0.1.5]: https://github.com/klahr/zonventure/releases/tag/0.1.5
[0.1.4]: https://github.com/klahr/zonventure/releases/tag/0.1.4
[0.1.3]: https://github.com/klahr/zonventure/releases/tag/0.1.3
[0.1.2]: https://github.com/klahr/zonventure/releases/tag/0.1.2
[0.1.1]: https://github.com/klahr/zonventure/releases/tag/0.1.1
[0.1.0]: https://github.com/klahr/zonventure/releases/tag/0.1.0
