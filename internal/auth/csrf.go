package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
)

// CSRF protection — double-submit cookie pattern.
//
// On every response the EnsureCSRF middleware ensures the browser holds
// a zv_csrf cookie (NOT HttpOnly: the front-end JS reads it). Mutating
// requests must echo that value back as the X-CSRF-Token header;
// RequireCSRF refuses anything that doesn't match. A cross-site
// attacker can't read the cookie (same-origin policy on document.cookie),
// so they can't forge the header.
//
// Why both this AND SameSite=Lax on session cookies? Defense in depth —
// SameSite=Lax handles modern browsers, but legacy or misconfigured
// (SameSite=None for embeds, etc.) deployments lose that guarantee.
// CSRF tokens are independent of cookie attributes.

const (
	CSRFCookieName = "zv_csrf"
	CSRFHeaderName = "X-CSRF-Token"

	// 30 days. The session itself caps at SessionTTL (7 days at time
	// of writing), so the token outlives the session by design — that
	// way the very first POST after a logout-then-login still finds a
	// usable token without an extra round-trip to refresh.
	csrfMaxAge = 30 * 24 * 60 * 60
)

// EnsureCSRF mints a CSRF cookie if the request doesn't already carry
// one, so JS on the receiving page can read it for subsequent calls.
// Wrap this at the router root: every page load (the GET that fetches
// /arrange or /play) will seed the cookie before any mutation fires.
//
// forceSecure mirrors the -secure-cookie flag: when true the cookie is
// always Secure, otherwise CookieSecure decides per-request based on
// TLS / X-Forwarded-Proto.
func EnsureCSRF(forceSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie(CSRFCookieName); err != nil || c.Value == "" {
				tok, err := newCSRFToken()
				if err != nil {
					// Crypto/rand really should not fail; if it does the
					// safe move is to refuse the request rather than
					// quietly serve without a token.
					logx.Error("auth.csrf.token_failed", "err", err.Error())
					httpx.Error(w, http.StatusInternalServerError, "csrf init failed")
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookieName,
					Value:    tok,
					Path:     "/",
					HttpOnly: false, // JS must read it
					Secure:   CookieSecure(r, forceSecure),
					SameSite: http.SameSiteLaxMode,
					MaxAge:   csrfMaxAge,
				})
				// Stash the new value on the request itself so a
				// RequireCSRF check that runs later in the same chain
				// can still see "the cookie we just set" — this only
				// matters for nested middleware setups; a fresh
				// request's first POST is naturally the next round-trip.
				r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: tok})
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireCSRF rejects mutating requests whose X-CSRF-Token header
// doesn't match the zv_csrf cookie. Safe methods (GET/HEAD/OPTIONS)
// pass through. Wrap routes that change state.
func RequireCSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(CSRFCookieName)
			if err != nil || cookie.Value == "" {
				httpx.Error(w, http.StatusForbidden, "csrf cookie missing")
				return
			}
			header := r.Header.Get(CSRFHeaderName)
			if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
				httpx.Error(w, http.StatusForbidden, "csrf token mismatch")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// newCSRFToken returns 24 random bytes URL-safe-base64-encoded — same
// shape as the slot magic-link tokens. 192 bits of entropy is overkill
// for CSRF but matches the rest of the auth code so there's one less
// "why is this different" question for a future reader.
func newCSRFToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
