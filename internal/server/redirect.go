package server

import (
	"net/http"
	"strings"

	"git.sr.ht/~klahr/zv/internal/settings"
)

// redirectMode is the top-level cutover middleware: when the operator
// has set a redirect target (via the admin UI or ZV_REDIRECT_TARGET),
// every non-allowlisted request 301s to `target + r.URL.Path`. Used
// by self-hosters cutting over to the hosted product so existing
// token-bearing URLs (printed QR codes, emailed magic links,
// /team/{token} bookmarks) keep working through the cutover window.
//
// The middleware reads from settings.RedirectTarget(), which is an
// in-process atomic pointer seeded at startup and updated in-process
// by the admin toggle endpoint — no DB round-trip on the hot path,
// so the self-host's DB can be on the way down without breaking the
// redirect.
//
// Allowlisted paths (no redirect):
//   - /api/health, /api/version — operator monitoring still works
//   - /api/webhooks/stripe — answered with 200 OK so Stripe stops
//     retrying once the operator unhooks the webhook from their
//     dashboard. Returning a 301 would make Stripe follow the
//     redirect and replay the event into the new instance, which
//     would double-fire fulfilment.
//   - /api/admin/migrate/redirect-mode — admin must still be able
//     to flip the toggle off via API even from inside a redirected
//     instance.
func redirectMode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := settings.RedirectTarget()
		if target == "" {
			next.ServeHTTP(w, r)
			return
		}
		if isRedirectAllowlisted(r.URL.Path) {
			// /api/webhooks/stripe: stop Stripe from retrying without
			// passing the body through to the handler (which would try
			// to verify the signature against credentials the operator
			// has likely already swapped out).
			if r.URL.Path == "/api/webhooks/stripe" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		loc := strings.TrimRight(target, "/") + r.URL.Path
		if r.URL.RawQuery != "" {
			loc += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, loc, http.StatusMovedPermanently)
	})
}

func isRedirectAllowlisted(path string) bool {
	switch path {
	case "/api/health",
		"/api/version",
		"/api/webhooks/stripe",
		"/api/admin/migrate/redirect-mode":
		return true
	}
	return false
}
