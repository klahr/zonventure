# Platform pricing in the console

Status: spec, not built. Nothing is made or edited in Stripe's dashboard:
Zonventure owns every price, and checkout keeps sending it inline
(`PriceData`), as it does today.

## The problem

What the platform charges is deploy config. Changing a price means
editing the server's environment and restarting it, and nobody can see
the current prices without shell access:

| Price | Env var | Default | Read in |
|---|---|---|---|
| Event pass, per player slot | `ZV_PRICE_PER_PLAYER_CENTS` | 500 | `internal/admin/purchases.go` (package vars, at start) |
| Free players per event | `ZV_FREE_PLAYERS_PER_EVENT` | 15 | same |
| Event pass currency | — (hard-coded) | EUR | `internal/admin/purchases.go` |
| Hosted pass, per block of 15 teams | `ZV_HOSTED_BLOCK_CENTS` | 14900 with a platform fee, else 0 | `payments.HostedPassFromEnv` |
| Hosted currency | `ZV_HOSTED_CURRENCY` | SEK | same |
| Platform fee on org sales | `ZV_PLATFORM_FEE_PERCENT_BPS`, `_FIXED_CENTS`, `_MIN_CENTS` | off | `payments.PlatformFeeFromEnv` |

## What we build

**Settings → Pricing**, a tab for the platform `admin` role only (beside
System). One card per price, each saved on its own:

1. **Event pass**: price per player, free players per event. Shows an
   example: "40 players: 15 free, 25 × 49 kr = 1 225 kr".
2. **Hosted pass**: price per block of 15 teams. Example for 1 and 2
   blocks. 0 means hosting is free.
3. **Platform fee** on tickets and parties orgs sell: percent, fixed
   part, minimum. Example on a 199 kr ticket: what the org gets and
   what the platform keeps. Off when all three are 0.

Every price is in **SEK, including VAT**, and the page says so ("kr inkl.
moms" / "SEK incl. VAT") beside each amount. Each card says who pays it
and where the money goes (platform account or the org's Connect
account), in one line.

## How it works

- Prices live in the existing `settings` key/value table
  (`internal/settings`), one key per value. An absent key falls back to
  a built-in default: event pass 49 kr per player after 15 free, hosted
  pass 149 kr per block, platform fee off.
- **No env vars.** `ZV_PRICE_PER_PLAYER_CENTS`, `ZV_FREE_PLAYERS_PER_EVENT`,
  `ZV_HOSTED_BLOCK_CENTS`, `ZV_HOSTED_CURRENCY` and `ZV_PLATFORM_FEE_*`
  are removed from the code, SELFHOSTING.md and compose files. A server
  started with one of them set logs a warning naming the Pricing page.
  Without Stripe keys nothing is charged anyway, so a self-host needs no
  pricing config at all.
- **Currency is SEK for everything**, no setting. The event pass moves
  from EUR to SEK; past purchases keep the currency they were made in.
- Read on each checkout, not at start-up, so a save applies to the next
  purchase without a restart. A short in-process cache is fine, as the
  redirect setting does.
- The package vars `PricePerPlayerCents` / `FreePlayersPerEvent` and
  `adminH.PlatformFee` / `adminH.Hosted` become functions over the
  settings (23 read sites today for the pass alone).
- Purchases already store their amount and currency, so a price change
  never touches past or pending purchases. A checkout opened before the
  change is paid at the old price.
- `/pricing` and the manual already splice the live numbers
  (`pricingSubs`); they read the same functions.
- Every save is logged (`admin.pricing`) with who, and the old and new prices.
- Validation: amounts are whole öre ≥ 0;
  the platform fee must stay below any ticket price an org has set
  (today's `price_below_fee` check keeps refusing sales where it isn't).

## Not in this spec

- **Org offers** (what an org charges players: ticket price, team size,
  party offers). They stay on each event until a later spec for an
  org's reusable offers.
- Stripe Products / Prices. Only needed for subscriptions, which we
  don't sell.
- Per-org price overrides for the platform's prices (a discount for one
  customer). Easy to add later as an org-scoped key; say if wanted now.

## Deploying it

Before the release reaches production, note the env values set there.
After it starts, enter them on Settings → Pricing, then remove the vars
from the server's env file. Until then the built-in defaults apply.

## Org sales in SEK only

For now orgs sell tickets and parties in SEK only, so the platform fee's
fixed part and minimum always match the sale's currency. The ticket and
party currency pickers go; an event already priced in EUR keeps its
listing until its price is next changed, which saves it in SEK. EUR may
come back later, for the platform and orgs together.
