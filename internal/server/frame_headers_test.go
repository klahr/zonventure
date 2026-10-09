package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Only the quiz trail creator may be framed, and only by the site's own
// pages; everything else stays unframeable.
func TestSecurityHeaders_Framing(t *testing.T) {
	h := securityHeaders(cspFor("", ""))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for path, want := range map[string]string{
		"/app/party/trail/start/zonventure": "SAMEORIGIN",
		"/app/party/trail/edit":             "SAMEORIGIN",
		"/app/party/host/abc":               "DENY",
		"/app/arrange/":                     "DENY",
		"/":                                 "DENY",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		csp := w.Header().Get("Content-Security-Policy")
		if got := w.Header().Get("X-Frame-Options"); got != want {
			t.Errorf("%s: X-Frame-Options %q, want %q", path, got, want)
		}
		if framed := strings.Contains(csp, "frame-ancestors 'self'"); framed != (want == "SAMEORIGIN") || strings.Contains(csp, "frame-ancestors 'none'") == framed {
			t.Errorf("%s: CSP %q", path, csp)
		}
	}
}
