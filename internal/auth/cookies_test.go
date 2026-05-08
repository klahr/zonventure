package auth

import (
	"crypto/tls"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// withTrustedProxies temporarily installs the given prefixes for the
// duration of the test, restoring the previous set when the test ends.
// httptest.NewRequest uses 192.0.2.1 (the TEST-NET-1 reserved block)
// as the default RemoteAddr; trusting that subnet lets a test exercise
// the X-Forwarded-* path without depending on the global default.
func withTrustedProxies(t *testing.T, cidrs ...string) {
	t.Helper()
	prev := trustedProxies.Load()
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("bad CIDR %q: %v", s, err)
		}
		prefixes = append(prefixes, p)
	}
	SetTrustedProxies(prefixes)
	t.Cleanup(func() {
		if prev == nil {
			trustedProxies.Store(nil)
		} else {
			trustedProxies.Store(prev)
		}
	})
}

func TestCookieSecure(t *testing.T) {
	cases := []struct {
		name           string
		trustedProxies []string
		setup          func() (forceFlag bool, request *fakeReq)
		want           bool
	}{
		{
			name: "force flag wins on plain HTTP",
			setup: func() (bool, *fakeReq) {
				return true, &fakeReq{}
			},
			want: true,
		},
		{
			name: "no force, no TLS, no XFF: insecure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{}
			},
			want: false,
		},
		{
			name: "direct TLS connection: secure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{tls: true}
			},
			want: true,
		},
		{
			name:           "X-Forwarded-Proto https with trusted peer: secure",
			trustedProxies: []string{"192.0.2.0/24"},
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "https"}
			},
			want: true,
		},
		{
			name:           "X-Forwarded-Proto http with trusted peer: not secure",
			trustedProxies: []string{"192.0.2.0/24"},
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "http"}
			},
			want: false,
		},
		{
			name:           "X-Forwarded-Proto HTTPS uppercase with trusted peer: secure",
			trustedProxies: []string{"192.0.2.0/24"},
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "HTTPS"}
			},
			want: true,
		},
		{
			name: "X-Forwarded-Proto https without trusted peer: insecure (header ignored)",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "https"}
			},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.trustedProxies) > 0 {
				withTrustedProxies(t, c.trustedProxies...)
			} else {
				withTrustedProxies(t) // explicit empty: ensure default
			}
			force, fr := c.setup()
			r := httptest.NewRequest("GET", "/", nil)
			if fr.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if fr.xff != "" {
				r.Header.Set("X-Forwarded-Proto", fr.xff)
			}
			if got := CookieSecure(r, force); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

type fakeReq struct {
	tls bool
	xff string
}

func TestCookieSecure_NilRequest(t *testing.T) {
	if got := CookieSecure(nil, false); got {
		t.Fatalf("nil request without force should be insecure")
	}
	if got := CookieSecure(nil, true); !got {
		t.Fatalf("force should win even with nil request")
	}
}
