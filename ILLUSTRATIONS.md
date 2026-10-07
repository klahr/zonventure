# Illustrations

Pictures for the product's own screens: the welcome dialog, sign-in,
empty lists, errors and thank-you pages. These are Zonventure's own
pictures, in one house style. An adventure's look and its cover
pictures (THEMES.md) are something else: they dress what the players
see of one adventure.

A rule for player screens: once the phone knows the adventure, its look
owns the screen (lobby, end step, join). House pictures go only where no
look is known yet (signed out, can't reach the game) or on the
arranger's side.

## Where they go

| # | Spot | Where | Shape | Priority |
|---|---|---|---|---|
| 1 | Welcome dialog | `pages/dashboard/Onboarding.tsx:86`, banner across the top of a 32rem dialog | banner | high |
| 2 | Sign in / sign up | `src/Login.tsx:47`, beside the card on wide screens, above it on phones | scene | high |
| 3 | Check your email | `Login.tsx:263` (reset sent), `:339` (verify sent) | spot | high |
| 4 | No organisation yet | `pages/dashboard/index.tsx:186` | scene | medium |
| 5 | No events yet | `pages/events/index.tsx:28,37` | spot | high |
| 6 | No adventures yet | `pages/adventures/List.tsx:155,248`, `events/NewEventDialog.tsx:336` | spot | high |
| 7 | No teams yet | `pages/event/Teams.tsx:121` | spot | medium |
| 8 | No party offers yet | `pages/parties/index.tsx:143,147` | spot | medium |
| 9 | No purchases yet | `pages/settings/Purchases.tsx:198` | spot | low |
| 10 | No redemptions yet | `pages/redemptions/index.tsx:69` | spot | low |
| 11 | No self-guided adventures | `pages/events/index.tsx:152` | spot | low |
| 12 | Something went wrong / offline | console `src/App.tsx:118`, play `App.tsx:149` | spot | medium |
| 13 | Not found | `internal/server/web/404.html:62`, party `App.tsx:77`, `Book.tsx:44` | spot | medium |
| 14 | Open your invitation link | play `App.tsx:140` | spot | medium |
| 15 | Published | `designer/dz/dialogs.tsx:170` | spot | low |
| 16 | Thank you, party booked / paid | party `Book.tsx:136`, `App.tsx:72`, `TrailPages.tsx:385` | banner | medium |
| 17 | No party adventures here yet | party `TrailPages.tsx:202` | spot | low |
| 18 | Email header | `internal/emailtpl`, emails with no adventure cover | banner | high |
| 19 | Password reset | `PasswordReset` email | spot | medium |
| 20 | Team invitation | team invitation and link emails when the adventure has no cover | banner | high |
| 21 | Gift box | gift-box redemption emails | spot | low |

Left without a picture on purpose: the member, admin, webhook, API-token
and log lists, since an illustration there is noise. Also the play
app's lobby and end step, which wear the adventure's look.

`Empty` (`src/ui/kit.tsx:161`) has no image slot today. Adding an
`illustration` prop there covers spots 5 to 11 at once.

## The three shapes

- **Banner:** a wide strip across the top of a dialog or card.
  Generate at 16:9 (1600×900), with everything that matters in the
  middle third from top to bottom. The app crops it to 3:1 (shown at
  32–40rem wide, so ship 1200×400 and 2400×800).
- **Scene:** a full picture beside a form, or as a page's hero.
  Generate at 4:3 (1600×1200), cropped to fit, subject centred.
- **Spot:** a small picture above an empty list's message. Generate
  at 1:1 (1024×1024). One object or a tiny scene in the middle, with
  wide margins. Shown at 8–10rem.

## What every picture keeps

- **The house style**, word for word in every prompt below, so they
  read as one family.
- **The brand colours with their hex values:** navy #1f3349, deep navy
  #14202e, raspberry #ad1357, pink #e24080, gold #ffd166, mint
  #6dcf92, off-white #fbfbfd.
- **A background baked into the picture.** Spots sit on a soft blush
  tile (#fbeef3) and banners and scenes on their own scene. The app
  shows each one in a rounded frame, so it reads as a picture in dark
  mode too, with no cut-out or transparency needed. Generators are
  poor at transparency anyway.
- **No text, letters, numbers, logos, watermark or frame.** Phones,
  paper, signs and screens show only abstract shapes: generators like
  to invent writing there.
- **People:** small mixed groups (friends, colleagues, a family, a
  school class), simple faces with dot eyes, no famous likenesses.
- **The recurring motifs:** thin topographic contour lines (the
  landing page's map pattern), map pins, dotted walking routes and a
  phone in hand. Use one or two per picture, not all of them.

Made with Gemini, a picture carries an invisible SynthID watermark.
Check the generator's terms for commercial use before shipping, as
with the theme covers.

## Status

Pictures live in `web/packages/ui/src/illustrations` (`SPOTS` and
`WIDE` in `illustrations.ts`); `make illustration ID=… SRC=… [WIDE=1]`
makes them. The 404 page serves its own copy from
`internal/server/web/assets/illustrations`.

| # | Spot | Picture |
|---|---|---|
| 1 | Welcome dialog | in use |
| 2 | Sign in / sign up | in use |
| 3 | Check your email | in use (reset sent) |
| 4 | No organisation yet | in use |
| 5 | No events yet | in use |
| 6 | No adventures yet | in use |
| 7 | No teams yet | in use |
| 8 | No party offers yet | in use |
| 9 | No purchases yet | in use |
| 10 | No redemptions yet | in use |
| 11 | No self-guided adventures | in use |
| 12 | Something went wrong | in use |
| 13 | Not found | in use |
| 14 | Open your invitation link | in use |
| 15 | Published | made, not used: the animated check suits that toast |
| 16 | Thank you | in use |
| 17 | No party adventures here yet | in use |
| 18 | Email header | in use |
| 19 | Password reset | – (uses check-email for now) |
| 20 | Team invitation | – (uses the email header for now) |
| 21 | Gift box | – (uses no-redemptions for now) |

## Prompts

### 1 · welcome (banner)

```
A warm, welcoming wide banner illustration for the welcome screen of an app that runs puzzle adventures in the real world, 16:9 landscape, 1600×900. Everything important sits in the middle third from top to bottom, because the top and bottom will be cropped away.
Scene: a small mixed team of four friends standing at the start of an adventure, seen from slightly behind and to the side, looking out over a stylised town: rooftops, a park, a church tower and a harbour in the distance. One of them holds up a phone whose screen shows only a glowing map pin. A dotted walking route in pink winds from their feet through the town, past three map pins, to a gold star on a hill on the right. Morning light, a few birds, soft clouds.
Background: sky fading from off-white to soft blush, thin topographic contour lines drawn faintly across the sky like a map.
Palette (keep to these colours): navy #1f3349, deep navy #14202e, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, simple faces with dot eyes, gentle light from the upper right, calm and uncluttered, for adults and families alike. No text, letters, numbers, logos, watermark or frame; screens, signs and paper show only abstract shapes.
```

### 2 · sign-in (scene)

```
A friendly illustration beside a sign-in form of an app for real-world puzzle adventures, 4:3, 1600×1200, subject centred with calm margins.
Scene: a big hand-held phone seen at a slight angle, its screen showing a simple stylised map with a dotted pink route and three pins (no writing). Around and behind the phone, small figures of a team are walking out of the map into a real park: one points ahead, one reads a paper clue with no readable writing, a child carries a compass. Gentle confetti of tiny map pins and stars floats around.
Background: soft blush #fbeef3 with faint topographic contour lines, fading to off-white at the edges.
Palette (keep to these colours): navy #1f3349, deep navy #14202e, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, simple faces with dot eyes, gentle light from the upper right, calm and uncluttered, for adults and families alike. No text, letters, numbers, logos, watermark or frame; screens, signs and paper show only abstract shapes.
```

### 3 · check-email (spot)

```
A small spot illustration for a "check your email" message, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a sealed envelope with a raspberry wax seal shaped like a map pin, flying out of a phone like a paper plane, trailing a dotted pink route with two tiny sparkles. The phone screen shows only a soft glow.
Background: one flat soft blush colour #fbeef3 filling the whole square, with very faint topographic contour lines.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; screens and paper show only abstract shapes.
```

### 4 · no-organisation (scene)

```
An inviting illustration for a page that asks a new user to create their organisation in an app for real-world puzzle adventures, 4:3, 1600×1200, subject centred.
Scene: an empty base camp at the edge of a stylised park: a small raspberry tent, a folding table with a rolled-up map and a compass, a pennant flag with no symbol on it, and a signpost with blank arrow boards pointing into the park. One friendly person is planting the flag, inviting others to join; two more are walking towards the camp along a dotted pink path.
Background: soft morning sky from off-white to blush, faint topographic contour lines across the ground like a map.
Palette (keep to these colours): navy #1f3349, deep navy #14202e, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, simple faces with dot eyes, gentle light from the upper right, calm and uncluttered, for adults and families alike. No text, letters, numbers, logos, watermark or frame; signs and paper show only abstract shapes.
```

### 5 · no-events (spot)

```
A small spot illustration for an empty list of scheduled events, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a desk calendar page with a blank grid (no numbers) and one day circled in pink, a map pin standing on top of it, and a small gold starting flag leaning against the calendar. A dotted pink route curls away from the pin.
Background: one flat soft blush colour #fbeef3 filling the whole square, with very faint topographic contour lines.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the calendar shows only an empty grid.
```

### 6 · no-adventures (spot)

```
A small spot illustration for an empty list of adventures, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: an unfolded paper treasure map, still blank apart from faint contour lines, with a raspberry pencil drawing the first dotted route and a single map pin waiting to be placed. A compass lies at the corner of the map.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the map has no readable writing.
```

### 7 · no-teams (spot)

```
A small spot illustration for an empty list of teams, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: three little team pennants on short poles stuck in a grassy mound, in raspberry, mint and gold, with no symbols on them; the middle pole is still empty, a fresh flag folded beside it. A few phones lie in the grass, screens showing only a soft glow.
Background: one flat soft blush colour #fbeef3 filling the whole square, with very faint topographic contour lines.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```

### 8 · no-party-offers (spot)

```
A small spot illustration for an empty list of party offers, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a bunch of three balloons (raspberry, mint, gold) tied to a small gift box with a pink ribbon, a map pin peeking out of the half-open lid, a few pieces of confetti floating around.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```

### 9 · no-purchases (spot)

```
A small spot illustration for an empty list of purchases, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: an empty paper shopping bag in raspberry with rope handles, and a single admission ticket with a perforated edge and a map-pin hole punch sticking out of it, blank, no writing.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the ticket is blank.
```

### 10 · no-redemptions (spot)

```
A small spot illustration for an empty list of gift-box code redemptions, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a closed gift box wrapped in navy paper with a gold ribbon, and a small key-shaped tag hanging from the bow with a map-pin cut-out, blank. Two tiny sparkles.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the tag is blank.
```

### 11 · no-self-guided (spot)

```
A small spot illustration for an empty list of self-guided adventures that teams play whenever they like, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a brass compass lying open next to a small hourglass and a phone, the phone screen showing only a pink map pin; a dotted route loops around all three.
Background: one flat soft blush colour #fbeef3 filling the whole square, with very faint topographic contour lines.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```

### 12 · went-wrong (spot)

```
A small, reassuring spot illustration for "something went wrong" and "can't reach the game, check your connection", square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a phone standing upright like a little character, its screen showing a single map pin with a small loose thread, a tangled dotted route lying in loops at its feet, and a friendly round plug with a cable reaching towards it. Calm, not alarming.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark, warning signs or frame.
```

### 13 · not-found (spot)

```
A small, playful spot illustration for a "page not found" message, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: the corner of a paper map curling up at its edge; a dotted pink walking route runs right off the edge of the map into nothing, and a small signpost with three blank arrow boards points in different directions. A tiny map pin sits puzzled at the end of the route.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the signpost and map have no writing.
```

### 14 · invitation (spot)

```
A small spot illustration for players asked to "open your invitation link", square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: an open envelope with a golden ticket rising out of it; the ticket has a map-pin shaped hole punched in it and is otherwise blank. Two tiny stars sparkle above.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the ticket is blank.
```

### 15 · published (spot)

```
A small celebratory spot illustration for "your adventure is published", square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a rolled map tied with a raspberry ribbon and sealed with a gold map-pin seal, bursting with a small fan of confetti and stars, as if just launched.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```

### 16 · thank-you (banner)

```
A joyful wide banner illustration for a "thank you, your party is booked" page of an app for party adventures, 16:9 landscape, 1600×900. Everything important sits in the middle third from top to bottom, because the top and bottom will be cropped away.
Scene: a garden party seen from the side: a long table with a cake and party hats, balloons in raspberry, mint and gold tied to the chairs, and a group of children and two grown-ups running off along a dotted pink trail towards a map pin by a tree, one child holding a phone. Bunting with no symbols on it hangs across the top.
Background: warm afternoon sky fading from off-white to blush, faint topographic contour lines on the lawn.
Palette (keep to these colours): navy #1f3349, deep navy #14202e, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, simple faces with dot eyes, gentle light from the upper right, calm and uncluttered, for adults and families alike. No text, letters, numbers, logos, watermark or frame; screens, signs and paper show only abstract shapes.
```

### 17 · no-trails (spot)

```
A small spot illustration for an empty list of party adventures, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a small stack of three blank question cards fanned out, the top one with a pink map pin clipped to it, and a party hat and a single balloon beside the stack.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame; the cards are blank.
```

### 18 · email-header (banner)

```
A calm, wide header illustration for the top of transactional emails from an app for real-world puzzle adventures, 16:9 landscape, 1600×900. Everything important sits in a thin horizontal band across the middle, because it will be cropped to 3:1 and shown only 600 pixels wide.
Scene: a stylised town skyline in silhouette layers (rooftops, a tower, trees), with a dotted pink route running left to right across it, linking three map pins, ending at a small gold star. Very simple, few details, readable at small size.
Background: soft gradient from off-white to blush, faint topographic contour lines.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```

### Emails

Email clients show JPEG and PNG, not WebP, and many block pictures until
the reader allows them. So every email reads fine without its picture,
and the server keeps JPEG copies in `internal/server/web/assets/email`.
Banners are generated 16:9 and cut to 3:1 across the middle, like the
dialog banners; shown 600 px wide, so keep the detail large.

### 19 · password-reset (spot)

```
A small spot illustration for a "reset your password" email, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: a friendly round brass padlock, open, with a golden key turning in it; the key's bow is shaped like a map pin. A dotted pink route curls from the key to a tiny sparkle.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```

### 20 · team-invitation (banner)

```
A wide, inviting banner illustration for the top of an email that invites a team to a real-world puzzle adventure, 16:9 landscape, 1600×900. Everything important sits in a horizontal band across the middle third, because the top and bottom will be cropped away and it is shown only 600 pixels wide, so keep shapes large and simple.
Scene: four friends at a meeting point in a town square, side by side, each looking at a phone that lights up with a single pink map pin (screens show nothing else); paper envelopes with map-pin seals fly in from the left along a dotted pink route towards them. A fountain and a few rooftops behind, a gold star on the right end of the route.
Background: soft sky from off-white to blush, faint topographic contour lines.
Palette (keep to these colours): navy #1f3349, deep navy #14202e, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, simple faces with dot eyes, gentle light from the upper right, calm and uncluttered, for adults and families alike. No text, letters, numbers, logos, watermark or frame; screens show only abstract shapes.
```

### 21 · gift-box (spot)

```
A small spot illustration for emails about a gift-box adventure being redeemed, square 1:1, 1024×1024, the subject centred with wide empty margins all round.
Subject: an opened gift box in navy with a gold ribbon; out of it rises a rolled treasure map and a phone showing only a pink map pin, with a little burst of confetti and two stars.
Background: one flat soft blush colour #fbeef3 filling the whole square.
Palette (keep to these colours): navy #1f3349, raspberry #ad1357, pink #e24080, gold #ffd166, mint #6dcf92, off-white #fbfbfd, blush #fbeef3.
Style: Zonventure house style — warm, semi-flat digital illustration with soft gradients and subtle paper grain, rounded friendly shapes, gentle light from the upper right, calm and uncluttered. No text, letters, numbers, logos, watermark or frame.
```
