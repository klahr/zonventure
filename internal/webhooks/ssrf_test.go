package webhooks

import "testing"

// TestIsLoopbackHost_NoPrefixBypass locks down the previous
// `strings.HasPrefix(host, "127.")` shape, which let a hostname like
// `127.evil.com` match the loopback exemption and skip ValidateHost
// entirely — DNS could then resolve to any public IP including the
// AWS metadata endpoint. The new implementation only treats literal
// loopback IPs (and "localhost") as exempt.
func TestIsLoopbackHost_NoPrefixBypass(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true}, // any 127/8 literal
		{"::1", true},
		{"127.evil.com", false},      // the bypass case
		{"127.0.0.1.evil.com", false},
		{"169.254.169.254", false}, // metadata IP — must NOT count as loopback
		{"example.com", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			got := IsLoopbackHost(tc.host)
			if got != tc.want {
				t.Errorf("IsLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

// TestLookupPublic_RejectsLiteralPrivateIPs covers the literal-IP
// short circuit. Hostnames are skipped here because the test can't
// pin DNS responses; isPublicIP is exercised through the literal
// path which is what an attacker would use first.
func TestLookupPublic_RejectsLiteralPrivateIPs(t *testing.T) {
	bad := []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"100.64.0.1",
		"::1",
		"fe80::1",
	}
	for _, h := range bad {
		t.Run(h, func(t *testing.T) {
			if _, err := LookupPublic(h); err == nil {
				t.Errorf("LookupPublic(%q): expected error, got nil", h)
			}
		})
	}
}
