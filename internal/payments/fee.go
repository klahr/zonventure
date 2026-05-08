package payments

import (
	"os"
	"strconv"
)

// PlatformFee captures the per-charge platform fee rule for
// marketplace flows. Today it's "max of percent-of-price or floor",
// which covers both extremes: a flat per-ticket fee gets crushing
// against very cheap tickets (a €5 flat fee on a €8 family ticket
// is 62%), and a flat percentage scales away from sensible numbers
// at the top end. Adopting `max(percent, floor)` matches the
// industry shape (Eventbrite, Tito, etc.) without hardcoding a
// specific formula in handlers.
//
// Both knobs are env-configured (ZV_PLATFORM_FEE_PERCENT_BPS,
// ZV_PLATFORM_FEE_MIN_CENTS) so the same binary serves a hosted
// instance (with a non-zero fee) and a self-host deployment (zero
// across the board, no marketplace flow).
type PlatformFee struct {
	// PercentBPS is the percentage component in basis points
	// (100 BPS = 1%). 0 means "no percentage".
	PercentBPS int64
	// MinCents is the per-charge floor in cents. 0 means "no
	// floor". Used to keep small-value tickets from generating
	// fees Stripe wouldn't bother to settle.
	MinCents int64
}

// PlatformFeeFromEnv reads the two ZV_PLATFORM_FEE_* env vars and
// returns the configured fee. Both default to 0 (no marketplace
// flow) which is the right posture for self-host: an operator who
// doesn't want to take a cut leaves the vars unset and the rest
// of the marketplace code path stays inert.
func PlatformFeeFromEnv() PlatformFee {
	var f PlatformFee
	if v, _ := strconv.ParseInt(os.Getenv("ZV_PLATFORM_FEE_PERCENT_BPS"), 10, 64); v > 0 {
		f.PercentBPS = v
	}
	if v, _ := strconv.ParseInt(os.Getenv("ZV_PLATFORM_FEE_MIN_CENTS"), 10, 64); v > 0 {
		f.MinCents = v
	}
	return f
}

// Enabled reports whether the platform fee is configured. When
// false, the /store buy flow skips the connected-account routing
// entirely and the org-Connect-onboarded gate doesn't apply —
// matches the "no platform, just sell" self-host posture.
func (f PlatformFee) Enabled() bool {
	return f.PercentBPS > 0 || f.MinCents > 0
}

// Compute returns the fee in cents for a charge of priceCents.
// max(percent-of-price, floor), clamped strictly below priceCents
// so Stripe (which rejects ApplicationFee >= total) never sees an
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
	percent := priceCents * f.PercentBPS / 10000
	fee := percent
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
