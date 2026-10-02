package auth

import (
	"regexp"
	"testing"
)

var voucherRe = regexp.MustCompile(`^[A-Z0-9]{6}$`)

func TestNewVoucherCode_Format(t *testing.T) {
	seen := map[string]bool{}
	for range 2000 {
		c, err := NewVoucherCode()
		if err != nil {
			t.Fatalf("NewVoucherCode: %v", err)
		}
		if !voucherRe.MatchString(c) {
			t.Fatalf("code %q is not 6 upper-case alphanumeric chars", c)
		}
		seen[c] = true
	}
	// 2000 draws from a ~2.2e9 space should essentially never collide;
	// a big dup count would mean the generator isn't random.
	if len(seen) < 1999 {
		t.Fatalf("suspiciously many duplicate codes: %d unique of 2000", len(seen))
	}
}

func TestNormalizeVoucherCode(t *testing.T) {
	cases := map[string]string{
		"demo42":    "DEMO42",
		"DEMO42":    "DEMO42",
		"de-mo 42":  "DEMO42",
		" dem o42 ": "DEMO42",
		"d3m0-42":   "D3M042",
		"!!!":       "",
		// Letters that older schemes folded (O/I/L/U) are now kept as-is.
		"OIL42U": "OIL42U",
	}
	for in, want := range cases {
		if got := NormalizeVoucherCode(in); got != want {
			t.Errorf("NormalizeVoucherCode(%q) = %q, want %q", in, got, want)
		}
	}
}
