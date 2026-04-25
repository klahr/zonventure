package auth

import (
	"net/http"
	"strings"
)

// CookieSecure decides whether to set the Secure flag on cookies for
// the given request. It returns true if any of:
//
//   - force is set (operator passed -secure-cookie=true: useful when a
//     proxy terminates TLS but doesn't stamp X-Forwarded-Proto)
//   - the request arrived over TLS directly (r.TLS != nil)
//   - the request carries X-Forwarded-Proto: https
//
// Centralising the decision means every cookie-emitting site handles
// dev-on-HTTP and prod-behind-HTTPS uniformly without operator help.
//
// Trust assumption: X-Forwarded-Proto is honoured unconditionally. The
// server is expected to run behind a reverse proxy in production; if a
// future deployment exposes the server directly to clients who can
// forge headers, the flag should be flipped to force=true and any XFF
// header from the public side stripped at the edge.
func CookieSecure(r *http.Request, force bool) bool {
	if force {
		return true
	}
	if r != nil && r.TLS != nil {
		return true
	}
	if r != nil && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}
