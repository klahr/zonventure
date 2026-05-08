package server

import "testing"

func TestSWCacheSafeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "dev"},
		{"v1.2.3", "v1.2.3"},
		{"v1.2.3-rc1", "v1.2.3-rc1"},
		{"v1.2.3+abcd1234", "v1.2.3abcd1234"},  // '+' dropped
		{`v1'or"1=1`, "v1or11"},                // quotes + '=' dropped
		{"feature/branch", "featurebranch"},    // '/' dropped
		{"!@#$%", "dev"},                       // entirely-illegal → dev
		{"   spaced   ", "spaced"},
	}
	for _, c := range cases {
		if got := swCacheSafeVersion(c.in); got != c.want {
			t.Errorf("swCacheSafeVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
