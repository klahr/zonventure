package payments

import (
	"testing"

	"github.com/klahr/zonventure/internal/settings"
)

// TestPlatformFee_Compute exercises the max(percent, floor) rule
// across the cases that bite at the extremes: small charges where
// percent rounds toward zero (the floor saves us), large charges
// where percent dominates (the floor is irrelevant), zero / negative
// inputs (defensive 0), and the disabled case (always 0).
func TestPlatformFee_Compute(t *testing.T) {
	cases := []struct {
		name       string
		fee        PlatformFee
		priceCents int64
		want       int64
	}{
		{"disabled returns zero", PlatformFee{}, 1000, 0},
		{"percent only — small charge", PlatformFee{PercentBPS: 800}, 100, 8},     // 8% of €1.00 = €0.08
		{"percent only — large charge", PlatformFee{PercentBPS: 800}, 12000, 960}, // 8% of €120 = €9.60
		{"floor only — wins regardless", PlatformFee{MinCents: 100}, 5000, 100},   // floor still wins (no percent)
		// max(percent, floor): floor wins when percent rounds below it.
		{"hybrid — floor wins on cheap", PlatformFee{PercentBPS: 800, MinCents: 100}, 800, 100},        // 8% of €8 = €0.64 → €1 floor wins
		{"hybrid — percent wins on expensive", PlatformFee{PercentBPS: 800, MinCents: 100}, 5000, 400}, // 8% of €50 = €4 wins
		{"hybrid — exactly at crossover", PlatformFee{PercentBPS: 800, MinCents: 100}, 1250, 100},      // 8% of €12.50 = €1, tie → floor (since percent > floor is the test)
		{"zero price returns zero", PlatformFee{PercentBPS: 800, MinCents: 100}, 0, 0},
		{"negative price returns zero", PlatformFee{PercentBPS: 800, MinCents: 100}, -100, 0},
		// Clamp: Stripe rejects ApplicationFee >= total. A floor of
		// €1 against a €0.50 ticket clamps to (price-1) so the
		// downstream Stripe call doesn't 400 opaquely. The audit's
		// MEDIUM note recommends the listing page filter such
		// tickets out — the clamp is the safety net.
		{"fixed only", PlatformFee{FixedCents: 300}, 20000, 300},
		{"percent plus fixed", PlatformFee{PercentBPS: 800, FixedCents: 300}, 20000, 1900}, // 8% of 200 kr = 16 kr, + 3 kr
		{"percent plus fixed — floor wins on cheap", PlatformFee{PercentBPS: 800, FixedCents: 300, MinCents: 500}, 1000, 500},
		{"percent plus fixed — beats floor", PlatformFee{PercentBPS: 800, FixedCents: 300, MinCents: 500}, 5000, 700},
		{"fixed exceeds price clamps to price-1", PlatformFee{FixedCents: 300}, 200, 199},
		{"floor exceeds price clamps to price-1", PlatformFee{MinCents: 100}, 50, 49},
		{"floor equals price clamps to price-1", PlatformFee{MinCents: 100}, 100, 99},
		{"floor 1 below price stays untouched", PlatformFee{MinCents: 100}, 101, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.fee.Compute(c.priceCents)
			if got != c.want {
				t.Errorf("Compute(%d) = %d, want %d (fee=%+v)", c.priceCents, got, c.want, c.fee)
			}
		})
	}
}

// TestPlatformFee_Enabled covers the truth table the rest of the
// package keys on for "should marketplace flow run at all?".
func TestPlatformFee_Enabled(t *testing.T) {
	cases := []struct {
		fee  PlatformFee
		want bool
	}{
		{PlatformFee{}, false},
		{PlatformFee{PercentBPS: 1}, true},
		{PlatformFee{FixedCents: 1}, true},
		{PlatformFee{MinCents: 1}, true},
		{PlatformFee{PercentBPS: 800, MinCents: 100}, true},
	}
	for _, c := range cases {
		if got := c.fee.Enabled(); got != c.want {
			t.Errorf("Enabled() = %v for %+v, want %v", got, c.fee, c.want)
		}
	}
}

// TestFeeFrom maps the saved prices onto the fee: none by default.
func TestFeeFrom(t *testing.T) {
	if got := FeeFrom(settings.DefaultPricing); got.Enabled() {
		t.Errorf("default pricing: Enabled()=true, want false")
	}
	got := FeeFrom(settings.Pricing{FeePercentBPS: 800, FeeFixedCents: 300, FeeMinCents: 100})
	if got.PercentBPS != 800 || got.FixedCents != 300 || got.MinCents != 100 {
		t.Errorf("set prices: %+v", got)
	}
}
