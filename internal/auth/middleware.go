package auth

import (
	"database/sql"
	"net/http"
	"slices"

	"github.com/klahr/zonventure/internal/httpx"
)

// Middleware resolves the session cookie into a User and attaches it to the
// request context. If no valid session is present it is a no-op; handlers
// that require a user should use RequireUser or RequireRole.
func Middleware(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(CookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			u, ok, err := Lookup(r.Context(), db, c.Value)
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "session lookup failed")
				return
			}
			if ok {
				r = r.WithContext(WithUser(r.Context(), u))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireUser rejects requests that have no authenticated user.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			httpx.Error(w, http.StatusUnauthorized, "login required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects requests that either have no user or whose user does
// not hold the given role.
func RequireRole(role string) func(http.Handler) http.Handler {
	return RequireAnyRole(role)
}

// RequireAnyRole rejects requests that either have no user or whose user's
// role is not among the accepted set.
func RequireAnyRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := FromContext(r.Context())
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "login required")
				return
			}
			if !slices.Contains(roles, u.Role) {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
