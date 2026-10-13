package auth

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"strings"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/settings"
)

// Middleware resolves the session cookie into a User and attaches it to the
// request context. If no valid session is present it is a no-op; handlers
// that require a user should use RequireUser or RequireRole.
// lookupFailed, when set, answers a request whose session can't be looked
// up (the database is down), so a page request gets an error page; nil
// answers with a JSON error.
func Middleware(db *sql.DB, lookupFailed http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(CookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			u, ok, err := Lookup(r.Context(), db, c.Value)
			if err != nil && lookupFailed != nil {
				lookupFailed(w, r)
				return
			}
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
// change itself (POST .../me/password) and the harmless language choice
// (PUT .../me/language), so a choice made on the forced-change screen
// isn't lost; /api/auth/whoami stays reachable
// outside this subtree so the console can detect the state and render
// the forced-change screen. Clearing the flag (any real password set)
// lifts the block immediately.
func RequireNonDefaultPassword(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := FromContext(r.Context())
		if ok && u.PasswordIsDefault &&
			!(r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/me/password")) &&
			!(r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/me/language")) {
			httpx.JSON(w, http.StatusForbidden, map[string]string{
				"error": "change your default password before continuing",
				"code":  "password_change_required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TOTPSetupRequired reports whether u must enrol in two-factor
// authentication before doing anything else: they have none, the
// session isn't an SSO one (SSO satisfies the requirement), and
// either u is a global admin (full or Support) while the instance
// requires it of admins, or any organisation u belongs to requires it.
func TOTPSetupRequired(ctx context.Context, db *sql.DB, u User) (bool, error) {
	if u.TOTPEnabled || u.ViaSSO {
		return false, nil
	}
	by, err := TOTPRequiredBy(ctx, db, u)
	return by != "", err
}

// Who requires two-factor authentication of a user (TOTPRequiredBy).
const (
	TOTPRequiredByOrg   = "org"
	TOTPRequiredByAdmin = "admin"
)

// TOTPRequiredBy reports who requires u to use two-factor
// authentication, whether or not they have it: TOTPRequiredByAdmin
// for a global admin (full or Support) while the instance requires it
// of admins, TOTPRequiredByOrg when any organisation u belongs to
// requires it, else "".
func TOTPRequiredBy(ctx context.Context, db *sql.DB, u User) (string, error) {
	if u.IsAdmin {
		if settings.AdminTOTPRequired(ctx, db) {
			return TOTPRequiredByAdmin, nil
		}
		return "", nil
	}
	var required bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM org_memberships m JOIN organizations o ON o.id = m.org_id
		               WHERE m.user_id = $1 AND o.require_totp)
	`, u.ID).Scan(&required)
	if err != nil || !required {
		return "", err
	}
	return TOTPRequiredByOrg, nil
}

// TOTPSetupRequiredBody is the 403 body for a user who must enrol in
// two-factor authentication, from the console API and API tokens alike.
var TOTPSetupRequiredBody = map[string]string{
	"error": "set up two-factor authentication first",
	"code":  "totp_setup_required",
}

// RequireTOTPSetup blocks a user whom TOTPSetupRequired covers from
// everything but enrolling: reading their 2FA status, starting and
// confirming the enrolment (which returns the recovery codes) and the
// language choice. /api/auth/whoami and logout sit outside this
// subtree, so the console can detect the state and show the setup.
// API tokens get the same 403: they carry no second factor. Without a
// TOTP key (ready false) nobody could enrol, so nothing is enforced.
func RequireTOTPSetup(db *sql.DB, ready bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := FromContext(r.Context())
			if !ready || !ok ||
				(r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/me/totp")) ||
				(r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/me/totp/setup")) ||
				(r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/me/totp/confirm")) ||
				(r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/me/language")) {
				next.ServeHTTP(w, r)
				return
			}
			required, err := TOTPSetupRequired(r.Context(), db, u)
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "two-factor check failed")
				return
			}
			if required {
				httpx.JSON(w, http.StatusForbidden, TOTPSetupRequiredBody)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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
