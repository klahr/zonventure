package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
)

// TestTrustedProxyRealIP_KeepsSecureCookies pins the fix for cookies
// losing Secure behind a proxy: after trustedProxyRealIP rewrites
// RemoteAddr to the client, the request must still count as coming
// from the trusted proxy, so its X-Forwarded-Proto is honoured.
func TestTrustedProxyRealIP_KeepsSecureCookies(t *testing.T) {
	prefixes, err := auth.ParseTrustedProxies("172.16.0.0/12")
	if err != nil {
		t.Fatal(err)
	}
	auth.SetTrustedProxies(prefixes)
	t.Cleanup(func() { auth.SetTrustedProxies(nil) })

	for _, tc := range []struct {
		name, peer string
		wantAddr   string
		wantSecure bool
	}{
		{"trusted proxy", "172.18.0.1:40000", "203.0.113.7", true},
		{"untrusted peer", "198.51.100.9:40000", "198.51.100.9:40000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotAddr string
			var gotSecure bool
			h := trustedProxyRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAddr = r.RemoteAddr
				gotSecure = auth.CookieSecure(r, false)
			}))
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			req.Header.Set("X-Forwarded-Proto", "https")
			h.ServeHTTP(httptest.NewRecorder(), req)
			if gotAddr != tc.wantAddr {
				t.Errorf("RemoteAddr = %q, want %q", gotAddr, tc.wantAddr)
			}
			if gotSecure != tc.wantSecure {
				t.Errorf("CookieSecure = %v, want %v", gotSecure, tc.wantSecure)
			}
		})
	}
}
