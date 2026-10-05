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
| `baloo2*` | Baloo 2 (variable, 400–700) | SIL OFL 1.1, `OFL-Baloo2.txt` |
| `patrick-hand*` | Patrick Hand (400) | SIL OFL 1.1, `OFL-PatrickHand.txt` |
| `chewy*` | Chewy (400; `latin` only, which covers å ä ö) | Apache 2.0, `LICENSE-Chewy.txt` |
| `luckiest-guy*` | Luckiest Guy (400) | Apache 2.0, `LICENSE-LuckiestGuy.txt` |
| `nunito*` | Nunito (variable, 400–700) | SIL OFL 1.1, `OFL-Nunito.txt` |
| `inter*` | Inter (variable, 400–700) | SIL OFL 1.1, `OFL-Inter.txt` |
| `creepster*` | Creepster (400; `latin` only, which covers å ä ö) | SIL OFL 1.1, `OFL-Creepster.txt` |
| `pacifico*` | Pacifico (400) | SIL OFL 1.1, `OFL-Pacifico.txt` |
| `vt323*` | VT323 (400) | SIL OFL 1.1, `OFL-VT323.txt` |
| `orbitron*` | Orbitron (variable, 400–700; `latin` only, which covers å ä ö) | SIL OFL 1.1, `OFL-Orbitron.txt` |
| `press-start-2p*` | Press Start 2P (400) | SIL OFL 1.1, `OFL-PressStart2P.txt` |

Subsets are Google Fonts' `latin` and `latin-ext`, as served by Google
Fonts, unmodified.
