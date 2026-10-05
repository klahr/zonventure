package payments

import (
	"os"
	"testing"
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

// TestPlatformFeeFromEnv locks in the env-reading semantics: zero /
// missing → zero (self-host), valid integers → respective fields.
// Garbage values are silently ignored (zero) rather than panicking
// at startup so a typo doesn't take a self-host instance down.
func TestPlatformFeeFromEnv(t *testing.T) {
	t.Setenv("ZV_PLATFORM_FEE_PERCENT_BPS", "")
	t.Setenv("ZV_PLATFORM_FEE_FIXED_CENTS", "")
	t.Setenv("ZV_PLATFORM_FEE_MIN_CENTS", "")
	if got := PlatformFeeFromEnv(); got.Enabled() {
		t.Errorf("empty env: Enabled()=true, want false")
	}

	t.Setenv("ZV_PLATFORM_FEE_PERCENT_BPS", "800")
	t.Setenv("ZV_PLATFORM_FEE_FIXED_CENTS", "300")
	t.Setenv("ZV_PLATFORM_FEE_MIN_CENTS", "100")
	got := PlatformFeeFromEnv()
	if got.PercentBPS != 800 || got.FixedCents != 300 || got.MinCents != 100 {
		t.Errorf("set env: %+v", got)
	}

	t.Setenv("ZV_PLATFORM_FEE_PERCENT_BPS", "garbage")
	t.Setenv("ZV_PLATFORM_FEE_MIN_CENTS", "100")
	got = PlatformFeeFromEnv()
	if got.PercentBPS != 0 {
		t.Errorf("garbage percent: PercentBPS=%d, want 0", got.PercentBPS)
	}
	if got.MinCents != 100 {
		t.Errorf("garbage percent shouldn't drop the floor: MinCents=%d", got.MinCents)
	}

	// Tidy up so other tests don't inherit our values. t.Setenv
	// already restores after the test, but unsetting belt-and-
	// braces protects against a future test that misuses os.Getenv.
	os.Unsetenv("ZV_PLATFORM_FEE_PERCENT_BPS")
	os.Unsetenv("ZV_PLATFORM_FEE_FIXED_CENTS")
	os.Unsetenv("ZV_PLATFORM_FEE_MIN_CENTS")
}
