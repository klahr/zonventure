package payments

import "github.com/klahr/zonventure/internal/settings"

// PlatformFee captures the per-charge platform fee rule for
// marketplace flows: "percent-of-price plus a fixed amount, but at
// least the floor". The percentage scales with the ticket; the
// fixed part covers the per-ticket costs the platform carries on a
// destination charge (Stripe's fixed card fee, plus the Connect
// per-account and per-payout fees spread over the tickets); the
// floor keeps very cheap tickets from running at a loss. Matches
// the industry shape (Eventbrite, Tito, etc.) without hardcoding a
// specific formula in handlers.
//
// The platform admin sets all three under Settings → Pricing; zero
// across the board (the default) means no marketplace flow.
type PlatformFee struct {
	// PercentBPS is the percentage component in basis points
	// (100 BPS = 1%). 0 means "no percentage".
	PercentBPS int64
	// FixedCents is added to every charge on top of the percentage.
	// 0 means "no fixed part".
	FixedCents int64
	// MinCents is the per-charge floor in cents. 0 means "no
	// floor". Used to keep small-value tickets from generating
	// fees Stripe wouldn't bother to settle.
	MinCents int64
}

// FeeFrom is the platform fee in prices.
func FeeFrom(p settings.Pricing) PlatformFee {
	return PlatformFee{PercentBPS: p.FeePercentBPS, FixedCents: p.FeeFixedCents, MinCents: p.FeeMinCents}
}

// Enabled reports whether the platform fee is configured. When
// false, the /store buy flow skips the connected-account routing
// entirely and the org-Connect-onboarded gate doesn't apply —
// matches the "no platform, just sell" self-host posture.
func (f PlatformFee) Enabled() bool {
	return f.PercentBPS > 0 || f.FixedCents > 0 || f.MinCents > 0
}

// Compute returns the fee in cents for a charge of priceCents.
// max(percent-of-price + fixed, floor), clamped strictly below
// priceCents so Stripe (which rejects ApplicationFee >= total) never sees an
// over-floor fee on a tiny ticket. Negative or zero prices return
// 0 rather than something weird; the caller should already have
// rejected free / negative tickets upstream but defensiveness is
// cheap here.
func (f PlatformFee) Compute(priceCents int64) int64 {
	if !f.Enabled() || priceCents <= 0 {
		return 0
	}
	// Integer math: priceCents * BPS / 10000. Order matters —
	// dividing first would truncate the percentage to zero on
	// small charges. The 64-bit product caps out at ~9 billion
	// ticket-cents × 10000 BPS, far above any plausible single
	// purchase.
	fee := priceCents*f.PercentBPS/10000 + f.FixedCents
	if f.MinCents > fee {
		fee = f.MinCents
	}
	// Clamp strictly below priceCents. Stripe ApplicationFee must
	// be < total charge, so a €5 floor on a €3 ticket would
	// otherwise produce an opaque 400 from Stripe at checkout
	// time. Caller should also filter such tickets out of the
	// public listing — see the audit's MEDIUM note.
	if fee >= priceCents {
		fee = priceCents - 1
	}
	if fee < 0 {
		return 0
	}
	return fee
}
