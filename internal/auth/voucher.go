package auth

import (
	"crypto/rand"
	"strings"
)

// Voucher codes are a short, typeable alias for a team's magic link: a
// player reads one off a gift card or an email and enters it at /play
// instead of clicking the link. The format is six upper-case
// alphanumeric characters (A–Z, 0–9).
const voucherAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" // 36 symbols

// VoucherCodeLen is the number of characters in a code. Six characters
// of a 36-symbol alphabet is ~31 bits (36^6 ≈ 2.2 billion) — short
// enough to print on a card and type on a phone, and, behind the
// per-IP lookup rate limit, far too large to guess by hand.
const VoucherCodeLen = 6

// NewVoucherCode returns a fresh code: VoucherCodeLen upper-case
// alphanumeric characters. Rejection sampling keeps the distribution
// uniform (256 is not a multiple of 36, so a plain byte%36 would bias
// toward the low letters). Callers store the result as-is and rely on
// the UNIQUE index to catch the rare collision.
func NewVoucherCode() (string, error) {
	out := make([]byte, VoucherCodeLen)
	filled := 0
	buf := make([]byte, VoucherCodeLen*2)
	for filled < VoucherCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= 252 { // 252 = 36*7; drop the biased tail
				continue
			}
			out[filled] = voucherAlphabet[b%36]
			filled++
			if filled == VoucherCodeLen {
				break
			}
		}
	}
	return string(out), nil
}

// NormalizeVoucherCode canonicalises user input for lookup: upper-cases
// and drops anything that isn't A–Z or 0–9 (so spaces, dashes and other
// separators a reader adds don't matter). Returns "" when nothing usable
// remains.
func NormalizeVoucherCode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatVoucherCode returns a code as it should appear on a card or in an
// email. Six characters read fine without grouping, so this just upper-
// cases for safety and returns it unchanged.
func FormatVoucherCode(code string) string {
	return strings.ToUpper(code)
}
