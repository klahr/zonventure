# Fonts

Self-hosted, so the player app renders in the brand font with no signal
(the service worker caches them with the rest of the build) and no page
depends on a third-party font CDN.

| File | Font | Licence |
|---|---|---|
| `aclonica-latin*.woff2` | Aclonica — the Zonventure wordmark and display face since the first zonventure-web | Apache 2.0, `LICENSE-Aclonica.txt` |
| `open-sans-latin*.woff2` | Open Sans (variable, 300–800) — body text | SIL OFL 1.1, `OFL-OpenSans.txt` |

Subsets are Google Fonts' `latin` and `latin-ext`, which cover Swedish
and every European name a team is likely to type. The browser fetches
latin-ext only when a page actually uses a character in it.
