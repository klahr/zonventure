package server

import (
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/settings"
)

// maintenanceMode is the operator-initiated read-only middleware.
// When `settings.MaintenanceMode()` is true:
//   - Reads (GET / HEAD / OPTIONS) pass through, so the UI keeps
//     rendering and the operator can confirm the freeze visually.
//   - Mutations (POST / PUT / PATCH / DELETE) respond 503 with a
//     small JSON body and a Retry-After header.
//   - A small allowlist of paths is exempt: /api/health and
//     /api/version (so monitoring / probes still work even when
//     mutations are paused), and the admin migrate + maintenance
//     toggles (so the operator can still manage the flag and run
//     the bundle export from the GUI without exiting the mode), and
//     the platform admin's own settings saves.
//
// Use case: an org-bundle export should run against a static
// snapshot. Maintenance mode is the operator-visible signal to
// players + arrangers that mutations will be refused until the
// export completes; the export handler itself wraps its queries in
// a REPEATABLE READ transaction as a defense-in-depth correctness
// check.
func maintenanceFreeze(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !settings.MaintenanceMode() {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if isMaintenanceAllowlisted(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", "60")
		// JSON shape mirrors httpx.Error so frontend handling stays
		// uniform — clients see the same `{ "error": "..." }` body
		// they get from any other 4xx/5xx — plus the code the player
		// app turns into its "paused for maintenance" message.
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Saving is paused for maintenance. Try again in a minute.",
			"code":  "maintenance",
		})
	})
}

func isMaintenanceAllowlisted(path string) bool {
	// Operator surfaces that must still mutate while the freeze is
	// on — without these the operator can't end the freeze without
	// restarting the process or hand-editing the DB.
	switch path {
	case "/api/admin/migrate/redirect-mode",
		"/api/admin/migrate/maintenance",
		"/api/admin/migrate/export",
		"/api/admin/migrate/import":
		return true
	}
	// The platform admin's settings (Settings → System, Pricing,
	// Legal): admins often fix them during a maintenance window, and
	// they sit outside any organisation's bundle. The routes
	// themselves are full-admin only.
	switch path {
	case "/api/admin/settings/signup",
		"/api/admin/settings/admin-totp",
		"/api/admin/pricing",
		"/api/admin/legal",
		"/api/admin/legal/preview",
		"/api/admin/logs/level":
		return true
	}
	// A narrow auth allowlist so a stale-session admin can sign back
	// in to flip the flag. The full /api/auth/ prefix used to be
	// allowlisted here, but signup / forgot / reset / oidc-callback
	// all write to users / sessions / email_verifications, which
	// contaminates the snapshot the org-bundle export is supposed
	// to capture. The list below covers only the read or narrow-
	// write surfaces an operator needs to log back in.
	switch path {
	case "/api/auth/login",
		"/api/auth/logout",
		"/api/auth/whoami",
		"/api/auth/totp/login",
		"/api/auth/oidc/login",
		"/api/auth/oidc/callback":
		return true
	}
	return false
}
