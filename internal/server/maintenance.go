package server

import (
	"net/http"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/settings"
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
//     the bundle export from the GUI without exiting the mode).
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
		// they get from any other 4xx/5xx.
		httpx.Error(w, http.StatusServiceUnavailable,
			"instance is in maintenance mode — mutations are paused while the operator runs an export or upgrade")
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
	// Auth-related paths so a stale-session admin can sign back in
	// to flip the flag. Without these, an operator who triggered
	// maintenance and then got logged out is stuck.
	if strings.HasPrefix(path, "/api/auth/") {
		return true
	}
	return false
}
