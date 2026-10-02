# Adventure fonts

The typefaces an author can pick for an adventure (`FONTS` in
`../brand.ts`, `adventure.Fonts` in Go). Self-hosted so a player's
phone never asks a third-party font CDN for them — that would hand the
player's IP address to Google — and so they work offline once cached.
`adventure-fonts.css` only declares them; a browser downloads a file
only when an adventure's text uses that family.

| File | Font | Licence |
|---|---|---|
| `inconsolata*` | Inconsolata (variable, 400–700) | SIL OFL 1.1, `OFL-Inconsolata.txt` |
| `merriweather*` | Merriweather (variable, 400–700) | SIL OFL 1.1, `OFL-Merriweather.txt` |
| `fredoka*` | Fredoka (variable, 400–700) | SIL OFL 1.1, `OFL-Fredoka.txt` |
| `special-elite*` | Special Elite (400) | Apache 2.0, `LICENSE-SpecialElite.txt` |

Subsets are Google Fonts' `latin` and `latin-ext`, as served by Google
Fonts, unmodified.
