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
- Space, Retro 80s, Christmas, Halloween and Valentine's Day ask for a dark left half: their text is light.

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
| Christmas | `christmas` | done |
| Halloween | `halloween` | done |
| Easter | `easter` | done |
| Midsummer | `midsummer` | done |
| Summer | `summer` | done |
| Winter | `winter` | done |
| Spring | `spring` | done |
| Autumn | `autumn` | done |
| Valentine's Day | `valentines` | done |

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

### christmas

```
A cosy, magical children's-book illustration for the cover of a Christmas party game, 16:9 landscape, 1600×900.
Scene: a decorated Christmas tree in the right third of the image, hung with red baubles and warm fairy lights and topped by a glowing gold star, with wrapped presents tied in red ribbon at its foot and a small red wooden house with a lit window beside it, snow on its roof.
Background: a deep pine-green winter night with large soft snowflakes falling, snowy fir trees fading into the distance, a gentle glow of warm light around the tree, getting darker, emptier and calmer towards the left.
Palette (keep to these colours): Christmas red #d62828, warm gold #f2c14e, deep pine green background #0f2a1d, darker green #173826, snow white #fdf6ec for the snow and highlights.
Style: soft painterly digital illustration, warm glowing light from the tree on the right, rounded cute shapes, festive but calm, high detail, suitable for children and adults. Keep the left half fairly simple and dark, because light-coloured titles and buttons may sit on top of it.
No text, no letters, no Santa, no logos, no watermark, no frame or border.
```

### halloween

```
A spooky-cute children's-book illustration for the cover of a Halloween party game, 16:9 landscape, 1600×900.
Scene: a cluster of glowing carved jack-o'-lanterns with friendly smiling faces in the right third of the image, on a little hill beside a crooked bare tree, a small cartoon ghost peeking out from behind it and a few bats flying past a big round moon above.
Background: a deep purple night sky with soft stars and wisps of mist, a warm orange glow around the pumpkins, getting darker, emptier and calmer towards the left.
Palette (keep to these colours): pumpkin orange #f97316, burnt orange #c2410c, slime green #a3e635 for small accents, deep purple background #1a0f2e, violet #5b21b6, dark plum #3b0764, warm cream #fff4e6 for the moon and highlights.
Style: soft painterly digital illustration, warm glowing light from the pumpkins on the right, rounded cute shapes, playful not scary, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and dark, because light-coloured titles and buttons may sit on top of it.
No text, no letters, no blood, no skulls, no logos, no watermark, no frame or border.
```

### easter

```
A fresh, cheerful children's-book illustration for the cover of an Easter party game, 16:9 landscape, 1600×900.
Scene: a woven basket full of painted Easter eggs in pastel pink, yellow, mint and lilac in the right third of the image, with two fluffy yellow chicks beside it, one just hatching from a cracked egg, tulips and daffodils around them and a vase of birch twigs decorated with colourful feathers behind.
Background: a soft spring morning, a pale yellow sky with light clouds, a fresh green meadow along the bottom with small flowers, getting lighter, emptier and calmer towards the left.
Palette (keep to these colours): raspberry pink #db2777, fresh green #15803d, pale butter background #fffbea, soft yellow #fde68a, pastel pink #fbcfe8, mint #bbf7d0, lilac #ddd6fe, dark brown for any lines #3b2f12.
Style: soft painterly digital illustration, gentle morning light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and light, because dark titles and buttons may sit on top of it.
No text, no letters, no logos, no watermark, no frame or border.
```

### midsummer

```
A bright, joyful children's-book illustration for the cover of a Swedish Midsummer party game, 16:9 landscape, 1600×900.
Scene: a Swedish midsummer maypole (a tall cross-shaped pole with two hanging rings, wrapped in green leaves and wild flowers, with blue and yellow ribbons) in the right third of the image, standing in a flowering meadow beside a small red wooden cottage with white corners, a picnic blanket in front with a bowl of strawberries and a flower crown of daisies, buttercups and cornflowers.
Background: a soft, light Nordic summer evening sky in pale gold and blue, a calm lake and birch trees in the distance, a meadow full of daisies, clover and buttercups along the bottom, getting lighter, emptier and calmer towards the left.
Palette (keep to these colours): cornflower blue #1e5aa8, leaf green #3f7d20, pale meadow background #f6fbef, buttercup yellow #fde047, sky blue #93c5fd, clover pink #f9a8d4, lime green #bef264, falu red for the cottage, dark green for any lines #1f2d16.
Style: soft painterly digital illustration, warm low sunlight from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and light, because dark titles and buttons may sit on top of it.
No people, no text, no letters, no flags, no logos, no watermark, no frame or border.
```

### summer

```
A sunny, carefree children's-book illustration for the cover of a summer party game, 16:9 landscape, 1600×900.
Scene: a beach corner in the right third of the image: a striped turquoise-and-coral beach umbrella, a towel with a sand bucket and spade, a half-built sandcastle, a slice of watermelon and an inflatable ring, with a small wooden jetty reaching into the water behind.
Background: a clear summer sky over a calm turquoise sea with soft gentle waves, warm golden sand along the bottom, getting lighter, emptier and calmer towards the left.
Palette (keep to these colours): turquoise #0e7490, coral orange #d9480f, warm sand background #fff7e6, pale aqua #d5f2f3, dark brown for any lines #2b2112, with watermelon pink and green as small accents.
Style: soft painterly digital illustration, bright warm sunlight from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and light, because dark titles and buttons may sit on top of it.
No people, no text, no letters, no logos, no watermark, no frame or border.
```

### winter

```
A crisp, peaceful children's-book illustration for the cover of a winter party game, 16:9 landscape, 1600×900.
Scene: a cheerful snowman with a knitted scarf and a carrot nose in the right third of the image, beside a wooden sled and a pair of mittens on a snowy hill, snow-covered fir trees behind and a frozen lake glinting below.
Background: a pale icy-blue winter sky with soft northern lights in gentle violet and blue near the top right, light snowflakes falling, smooth white snow along the bottom, getting lighter, emptier and calmer towards the left.
Palette (keep to these colours): winter blue #1d6fb8, aurora violet #5b4bc4, icy background #eef6fc, pale blue #93c5fd, lavender #c7d2fe, navy for any lines #12304a, with a small red-and-white accent on the scarf.
Style: soft painterly digital illustration, cool clear light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and light, because dark titles and buttons may sit on top of it.
No Christmas decorations, no people, no text, no letters, no logos, no watermark, no frame or border.
```

### spring

```
A fresh, hopeful children's-book illustration for the cover of a spring party game, 16:9 landscape, 1600×900.
Scene: a blossoming cherry tree with pink flowers in the right third of the image, petals drifting in the breeze, a little birdhouse on its trunk with a bird singing, a bumblebee over a patch of crocuses and wood anemones at its foot, a puddle reflecting the sky.
Background: a pale clear spring sky with soft clouds, fresh light-green hills with new grass along the bottom, a few petals floating across, getting lighter, emptier and calmer towards the left.
Palette (keep to these colours): fresh green #2f855a, blossom magenta #b83280, pale sky background #f2f8ff, light pink #f9a8d4, blush #fbcfe8, mint #bbf7d0, dark green for any lines #1d2e22.
Style: soft painterly digital illustration, gentle morning light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and light, because dark titles and buttons may sit on top of it.
No Easter eggs, no people, no text, no letters, no logos, no watermark, no frame or border.
```

### autumn

```
A warm, cosy children's-book illustration for the cover of an autumn party game, 16:9 landscape, 1600×900.
Scene: a big maple tree in full autumn colour in the right third of the image, leaves falling, a pile of leaves at its foot with a curious hedgehog peeking out, a basket of apples, a few mushrooms and a pair of rubber boots beside it.
Background: a soft warm autumn sky in pale cream and peach, gentle hills with rust and golden trees in the distance, a forest path covered in leaves along the bottom, a few leaves drifting across, getting lighter, emptier and calmer towards the left.
Palette (keep to these colours): rust orange #b4441c, deep plum #7a2e3a, warm parchment background #fbf3e8, pumpkin orange #d9480f, golden yellow #e9a23b, chestnut #9a3412, dark brown for any lines #3a2414.
Style: soft painterly digital illustration, low golden afternoon light from the upper right, rounded cute shapes, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and light, because dark titles and buttons may sit on top of it.
No pumpkins carved with faces, no people, no text, no letters, no logos, no watermark, no frame or border.
```

### valentines

```
A sweet, festive children's-book illustration for the cover of a Valentine's Day party game, 16:9 landscape, 1600×900.
Scene: a bunch of shiny heart-shaped balloons in red and pink in the right third of the image, tied to a small gift box with a big bow, beside a bouquet of red roses in a glass vase and a heart-shaped box of chocolates, a few envelopes sealed with little hearts.
Background: a deep wine-red evening backdrop with soft glowing bokeh lights and drifting pink and red confetti, getting darker, emptier and calmer towards the left.
Palette (keep to these colours): hot pink #ff4d6d, blush pink #ffb3c6, deep wine background #4a0d1f, darker wine #5e1328, soft pink-white #fff0f5 for highlights, with small touches of gold.
Style: soft painterly digital illustration, warm glowing light from the upper right, rounded cute shapes, romantic but playful, high detail but calm, suitable for children aged 5–10 and adults. Keep the left half fairly simple and dark, because light-coloured titles and buttons may sit on top of it.
No people, no cupid, no text, no letters, no logos, no watermark, no frame or border.
```
