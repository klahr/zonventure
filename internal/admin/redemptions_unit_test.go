package admin

import (
	"testing"
	"time"
)

func TestCheckPartnerCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind, box, code string
		wantBox, want   string
		ok              bool
	}{
		{partnerTrueStory, "", "abc-def-123", "", "ABCDEF123", true},
		{partnerTrueStory, "", " ab cd ef 12 3 ", "", "ABCDEF123", true},
		{partnerTrueStory, "", "ABCDEF12", "", "", false},    // 8 characters
		{partnerTrueStory, "", "ABCDEF1234", "", "", false},  // 10 characters
		{partnerTrueStory, "12", "ABCDEF123", "", "", false}, // TrueStory has no box number
		{partnerTrueStory, "", "ABCDÉF123", "", "", false},
		{partnerLiveit, "123 456", "x9y8", "123456", "X9Y8", true},
		{partnerLiveit, "", "X9Y8", "", "", false},     // box number required
		{partnerLiveit, "12a", "X9Y8", "", "", false},  // box number is numeric
		{partnerLiveit, "123", "", "", "", false},      // security code required
		{partnerLiveit, "123", "X9;Y8", "", "", false}, // letters and digits only
		{"unknown", "", "ABCDEF123", "", "", false},
	}
	for _, c := range cases {
		box, code, ok := checkPartnerCode(c.kind, c.box, c.code)
		if ok != c.ok || box != c.wantBox || code != c.want {
			t.Errorf("checkPartnerCode(%q, %q, %q) = %q, %q, %v; want %q, %q, %v",
				c.kind, c.box, c.code, box, code, ok, c.wantBox, c.want, c.ok)
		}
	}
}

func TestRedeemLimiter(t *testing.T) {
	t.Parallel()
	l := redeemLimiter{perIP: map[string][]time.Time{}}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < redeemPerIPMax; i++ {
		if ok, _ := l.allow(now.Add(time.Duration(i)*time.Minute), "1.2.3.4"); !ok {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	ok, wait := l.allow(now.Add(20*time.Minute), "1.2.3.4")
	if ok {
		t.Fatal("attempt past the cap allowed")
	}
	if wait != 40*time.Minute {
		t.Errorf("wait = %s, want 40m (until the first attempt leaves the window)", wait)
	}
	if ok, _ := l.allow(now.Add(20*time.Minute), "5.6.7.8"); !ok {
		t.Error("another IP refused")
	}
	if ok, _ := l.allow(now.Add(61*time.Minute), "1.2.3.4"); !ok {
		t.Error("attempt after the window refused")
	}
}
