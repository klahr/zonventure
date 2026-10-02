package auth

import (
	"database/sql"
	"net/http"
	"slices"
	"strings"

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

// RequireNonDefaultPassword blocks a logged-in user who is still on the
// seeded default password (password_is_default) from doing anything
// except changing that password. Without it, an operator who never
// completes the "log in as admin/admin and change the password" step
// ships a fully-functional, takeover-trivial admin: the UI banner is a
// nudge, this is the enforcement. The sole exception is the password
// change itself (POST .../me/password); /api/auth/whoami stays reachable
// outside this subtree so the console can detect the state and render
// the forced-change screen. Clearing the flag (any real password set)
// lifts the block immediately.
func RequireNonDefaultPassword(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := FromContext(r.Context())
		if ok && u.PasswordIsDefault &&
			!(r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/me/password")) {
			httpx.JSON(w, http.StatusForbidden, map[string]string{
				"error": "change your default password before continuing",
				"code":  "password_change_required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSession rejects requests authenticated by an API token.
// Wrap credential- and account-management routes so a leaked token
// can't mint more tokens, change the password, or delete the account.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ViaAPIToken(r.Context()) {
			httpx.Error(w, http.StatusForbidden, "not available with an API token")
			return
		}
		next.ServeHTTP(w, r)
	})
}
