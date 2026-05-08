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
//   - the request carries X-Forwarded-Proto: https AND the immediate
//     peer is in the trusted-proxy set
//
// Centralising the decision means every cookie-emitting site handles
// dev-on-HTTP and prod-behind-HTTPS uniformly without operator help.
//
// X-Forwarded-Proto is only honoured behind a trusted proxy; otherwise
// an attacker on a plaintext deployment could stamp the header to coax
// the server into setting Secure cookies that the browser then refuses
// (a cookie-clear DoS). Operators behind a proxy advertise it via
// SetTrustedProxies; everyone else falls back to r.TLS / force.
func CookieSecure(r *http.Request, force bool) bool {
	if force {
		return true
	}
	if r != nil && r.TLS != nil {
		return true
	}
	if r != nil && IsTrustedPeer(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}
