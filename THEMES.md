# Theme cover pictures

Each starter theme (`web/packages/play-ui/src/starterThemes.ts`) can have
a cover picture. A party adventure in that look shows it behind the
sample card on the start page (and the landing page's creator), across
the top of the join page and the players' welcome, and as a banner on
the printed cover with the QR code. A theme without one looks as before.

## An adventure's own picture

A theme in the designer's theme editor can have a **Cover picture** of
its own, uploaded there (`look.Style.Cover`, an adventure media id).
Applying the theme gives the adventure that cover, shown in place of
the starter theme's everywhere above. Picking another starter theme in
the quiz trail creator keeps it. Print cuts it to 16:9 about its
middle; the join page serves it by the player link
(`/api/join/{token}/cover`).

## Adding a picture

1. Generate it with the theme's prompt below (made with Gemini so far).
2. `make theme-cover ID=<theme id> SRC=<image>` writes
   `web/packages/play-ui/src/covers/<id>-800.webp`, `<id>-1600.webp` and
   `internal/trail/covers/<id>.jpg` (print).
3. Add the theme to `THEME_COVERS` in
   `web/packages/play-ui/src/covers.ts`, as `unicorn` is.

## What every picture keeps

- 16:9 landscape (asked as 1600×900; Gemini returns about 2752×1536).
- The subject in the right third, the left half calm: titles, the sample
  card and buttons sit there.
- The theme's own colours, named with their hex values.
- No text, letters, logos, watermark or frame. The prompts for desks,
  notes and paper also say "no readable writing": generators like to
  invent text there.
- Space and Retro 80s ask for a dark left half: their text is light.

Gemini pictures carry an invisible SynthID watermark; check Gemini's
terms for commercial use before shipping.

## Status

| Theme | ID | Picture |
|---|---|---|
| Unicorn | `unicorn` | done |
| Party | `party` | done |
| Sunshine | `sunshine` | done |
| Candy | `candy` | done |
| Rainbow | `rainbow` | done |
| Ice cream | `ice-cream` | done |
| Jungle | `jungle` | done |
| Pirate | `pirate` | done |
| Ocean | `ocean` | done |
| Space | `space` | done |
| Detective | `detective` | done |
| Retro 80s | `retro-80s` | done |
| Office | `office` | done |
| Pen & paper | `pen-and-paper` | done |

## Prompts

### unicorn

```
A whimsical, dreamy children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a friendly, smiling white unicorn — head and neck only, in side profile facing right — in the right third of the image. Eyes closed happily, soft pink blush on the cheek, a golden spiral horn with a gentle glow, a long flowing mane in pastel lavender, pink and mint falling behind the neck. The neck disappears into fluffy clouds at the bottom.
Background: a soft pastel rainbow arching across the left half, puffy lit clouds along the bottom, a sky fading from lavender at the top to warm pink-peach near the horizon, a few twinkling stars and soft bokeh sparkles.
Palette (keep to these colours): magenta #a21caf, violet #7c3aed, pale lilac background #fdf2ff, pastel pink #f9a8d4, lavender #c4b5fd, mint #99f6e4, butter yellow #fde68a, baby blue #a5d8ff, dark plum for any lines #3b1a5a.
Style: soft painterly digital illustration, gentle light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### party

```
A joyful children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a big birthday cake with lit candles and a cluster of shiny balloons in the right third of the image, streamers and confetti drifting through the air.
Background: a warm cream-coloured room with soft bokeh lights and falling confetti, getting calmer and emptier towards the left.
Palette (keep to these colours): raspberry pink #db2777, purple #6d28d9, cream background #fffaf0, deep plum for any lines #2d1b4e, with small accents of gold and mint confetti.
Style: soft painterly digital illustration, gentle warm light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no numbers, no logos, no watermark, no frame or border.
```

### sunshine

```
A cheerful children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a big smiling sun with soft rays in the upper right third, above a summer meadow with daisies, a butterfly and a little picnic blanket in the lower right.
Background: a warm golden sky with gentle sunburst rays fading towards the left, soft rolling green-gold hills along the bottom.
Palette (keep to these colours): burnt orange #c2410c, amber #b45309, sunflower yellow #fbbf24, pale butter background #fffbe8, dark brown for any lines #3b2a00, with soft greens for the meadow.
Style: soft painterly digital illustration, warm summer light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### candy

```
A sweet children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a candy land in the right third of the image: giant swirl lollipops, a gingerbread-like candy house with icing, gumdrops and a candy-cane path.
Background: a pink sky with soft cotton-candy clouds and faint diagonal candy stripes, getting calmer towards the left.
Palette (keep to these colours): candy pink #d81b60, berry purple #8e24aa, light pink #f06292, blush background #fff1f7, dark berry for any lines #4a1838, with small touches of white and mint.
Style: soft painterly digital illustration, glossy candy highlights, light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### rainbow

```
A bright children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a bold rainbow rising from the right third of the image over rolling green hills, with a small waterfall and a few happy birds near its end.
Background: a clear white-blue sky with soft fluffy clouds, the rainbow's arc fading out towards the upper left.
Palette (keep to these colours): royal blue #2563eb, magenta #86198f, white background #ffffff, charcoal for any lines #1f2937, plus the rainbow's full spectrum in clean, slightly soft tones.
Style: soft painterly digital illustration, fresh after-rain light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### ice-cream

```
A delicious children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a tall waffle cone with three wobbly scoops (strawberry, mint, vanilla), sprinkles and a cherry on top, in the right third of the image, a little drip running down.
Background: a soft pink summer backdrop with a faint pattern of polka dots and a few floating sprinkles, calmer towards the left.
Palette (keep to these colours): strawberry pink #be185d, mint teal #0f766e, light pink #f472b6, pale pink background #fff6f9, dark cherry brown for any lines #47202f, with waffle-cone gold.
Style: soft painterly digital illustration, creamy textures, light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### jungle

```
An adventurous children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a friendly colourful parrot on a branch in the right third of the image, framed by big tropical leaves, a hanging vine and a curious monkey peeking out.
Background: a soft green jungle with light filtering through the canopy, layers of leaves fading into a misty, calm left side.
Palette (keep to these colours): leaf green #2e7d32, burnt orange #bf360c, soft greens #81c784 and #aed581, warm yellow #ffe082, pale sage background #f1f6e9, deep forest green for any lines #1b2e12.
Style: soft painterly digital illustration, dappled light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### pirate

```
An adventurous children's-book illustration for the cover of a treasure-hunt party game, 16:9 landscape, 1600×900.
Scene: a friendly wooden pirate ship with red sails sailing on rolling waves in the right third of the image, a treasure chest spilling gold coins on a little island beside it, a seagull overhead.
Background: a parchment-coloured sky like an old treasure map, soft swirling waves along the bottom, calmer towards the left.
Palette (keep to these colours): pirate red #a61e1a, navy blue #173f6e, parchment background #f1e2bf, dark brown for any lines #33210f, with gold for the treasure.
Style: soft painterly digital illustration, warm light from the upper right, rounded cute shapes, playful not scary, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no skull and crossbones, no logos, no watermark, no frame or border.
```

### ocean

```
A serene children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a smiling whale swimming in the right third of the image above colourful coral, with a sea turtle, little fish and rising bubbles.
Background: clear underwater blue with sunbeams from the surface, light shimmering, getting calmer and paler towards the left.
Palette (keep to these colours): ocean blue #0277bd, sea green #00695c, light aqua #4fc3f7, seafoam #80cbc4, sky blue #81d4fa, pale aqua background #e8f6fb, deep navy for any lines #0b2f40, with small coral-pink accents.
Style: soft painterly digital illustration, soft light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### space

```
An exciting children's-book illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a friendly rocket with a glowing trail flying in the right third of the image past a ringed planet and a small smiling moon, with a tiny astronaut waving from the window.
Background: a deep night-blue sky full of soft stars and gentle purple-blue nebula clouds, darker and calmer towards the left.
Palette (keep to these colours): violet #7c4dff, light blue #4fc3f7, deep blue #1565c0, indigo #4527a0, purple #6a1b9a, near-black navy background #0b0d26, pale lavender-white highlights #eceeff.
Style: soft painterly digital illustration, glowing light from the rocket and planet, rounded cute shapes, high detail but calm, suitable for children aged 5–10. Keep the left half fairly simple and dark, because light-coloured titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### detective

```
A mysterious but friendly illustration for the cover of a detective party game, 16:9 landscape, 1600×900.
Scene: a large brass magnifying glass in the right third of the image revealing a trail of footprints, with a detective's hat, an old key and a note pinned with a red string on a cork board behind.
Background: an old study with warm paper tones and a faint grid like a case file, soft lamplight, calmer towards the left.
Palette (keep to these colours): rust red #8b2e16, dark coffee brown #3e2c1c, aged paper background #f3ebd9, near-black brown for any lines #2b2118, with brass gold for the magnifying glass.
Style: soft painterly digital illustration with a vintage storybook feel, warm lamplight from the upper right, cosy not scary, high detail but calm, suitable for children and adults. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no readable writing, no logos, no watermark, no frame or border.
```

### retro-80s

```
A vibrant retro illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: a glowing striped 1980s sunset over a neon perspective grid, with palm-tree silhouettes and a cassette tape and roller skates floating in the right third of the image.
Background: a deep purple night sky with a few stars, the neon horizon glowing across the bottom, darker and calmer towards the upper left.
Palette (keep to these colours): hot pink #ff4fa3, neon yellow #ffd319, electric cyan #2de2e6, deep purple background #1c0e31, pale pink-white highlights #fbeaff.
Style: polished synthwave digital illustration with soft glow and gentle film grain, fun and nostalgic, high detail but calm. Keep the left half fairly simple and dark, because light-coloured titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### office

```
A friendly, modern illustration for the cover of a team-day party game for adults, 16:9 landscape, 1600×900.
Scene: a cosy, tidy desk in the right third of the image with a laptop, a steaming coffee mug, a potted plant, sticky notes and a small trophy, next to a big bright window.
Background: a light, airy modern office with soft daylight, blurred shelves and plants, getting calmer and emptier towards the left.
Palette (keep to these colours): bright blue #1d4ed8, navy #1e3a8a, cool light-grey background #f5f7fa, dark slate for any lines #1f2a37, with fresh greens for the plants and a touch of warm wood.
Style: clean, soft-shaded flat illustration with gentle gradients and daylight from the upper right, professional but playful, high detail but calm. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no logos, no screens with readable content, no watermark, no frame or border.
```

### pen-and-paper

```
A charming hand-drawn illustration for the cover of a party game, 16:9 landscape, 1600×900.
Scene: an open sketchbook in the right third of the image with doodled stars, arrows and a little treasure map, a pencil, an eraser and a fountain pen resting beside it.
Background: lined notebook paper with faint blue rules and a red margin line on the left, a few small doodles and coffee-ring stains, calm and empty towards the left.
Palette (keep to these colours): ink blue #1e4fa3, margin red #c62828, light rule blue #5b8fd4, off-white paper background #fdfcf7, dark navy ink for any lines #1f2a44, with a soft pencil grey.
Style: hand-drawn ink-and-watercolour look, soft paper texture, light from the upper right, playful and tidy, high detail but calm, suitable for children and adults. Keep the left half fairly simple, because titles and buttons may sit on top of it.
No text, no letters, no readable writing, no logos, no watermark, no frame or border.
```
