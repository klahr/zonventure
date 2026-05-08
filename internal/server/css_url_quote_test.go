package server

import "testing"

// TestCSSURLQuote_EscapesAttacks asserts the helper percent-encodes
// the chars that would let a URL break out of a CSS `url('…')`
// literal.
func TestCSSURLQuote_EscapesAttacks(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/store-cover/event/42", "/store-cover/event/42"},
		// Single quote — the immediate-break case.
		{"x'y", "x%27y"},
		{`x"y`, "x%22y"},
		{"a(b)c", "a%28b%29c"},
		{"back\\slash", "back%5Cslash"},
		{"ws ace", "ws%20ace"},
		// All combined.
		{"path\\with 'quoted\"' (parens)", "path%5Cwith%20%27quoted%22%27%20%28parens%29"},
	}
	for _, c := range cases {
		if got := cssURLQuote(c.in); got != c.want {
			t.Errorf("cssURLQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
