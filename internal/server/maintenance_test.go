package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/settings"
)

// pinMaintenanceMode flips the in-process maintenance flag via the
// env-pin path so the test can drive the middleware without a
// Postgres handle. Mirror of pinRedirectTarget in redirect_test.go.
func pinMaintenanceMode(t *testing.T, on bool) {
	t.Helper()
	v := "0"
	if on {
		v = "1"
	}
	t.Setenv("ZV_MAINTENANCE_MODE", v)
	settings.LoadMaintenanceMode(context.Background(), nil)
	t.Cleanup(func() {
		t.Setenv("ZV_MAINTENANCE_MODE", "")
		settings.LoadMaintenanceMode(context.Background(), nil)
	})
}

// TestMaintenance_MutationsBlockedReadsPass locks the contract:
// when maintenance mode is on, GET / HEAD / OPTIONS pass through
// untouched, and POST / PUT / PATCH / DELETE on non-allowlisted
// paths get a 503 with Retry-After. The allowlist (migrate
// toggles, auth, /api/health, /api/version) stays callable so the
// operator can run the export and then turn the flag back off.
func TestMaintenance_MutationsBlockedReadsPass(t *testing.T) {
	pinMaintenanceMode(t, true)

	cases := []struct {
		name           string
		method         string
		path           string
		wantStatus     int
		wantInner      bool
		wantRetryAfter bool
	}{
		// Reads always pass through, even on mutation-shaped paths.
		{"GET passes through", http.MethodGet, "/api/admin/events", http.StatusOK, true, false},
		{"HEAD passes through", http.MethodHead, "/team/abc", http.StatusOK, true, false},
		{"OPTIONS passes through", http.MethodOptions, "/api/admin/events", http.StatusOK, true, false},

		// Non-allowlisted writes are blocked with 503 + Retry-After.
		{"POST on /api/admin/events", http.MethodPost, "/api/admin/events", http.StatusServiceUnavailable, false, true},
		{"PATCH on /api/admin/events/1", http.MethodPatch, "/api/admin/events/1", http.StatusServiceUnavailable, false, true},
		{"DELETE on /api/admin/events/1", http.MethodDelete, "/api/admin/events/1", http.StatusServiceUnavailable, false, true},
		{"POST on /api/auth/login (player)", http.MethodPost, "/api/auth/login", http.StatusOK, true, false},

		// Allowlisted operator-toggle paths stay open so the freeze
		// is reversible without a restart.
		{"POST migrate redirect-mode", http.MethodPost, "/api/admin/migrate/redirect-mode", http.StatusOK, true, false},
		{"POST migrate maintenance", http.MethodPost, "/api/admin/migrate/maintenance", http.StatusOK, true, false},
		{"GET migrate export", http.MethodGet, "/api/admin/migrate/export", http.StatusOK, true, false},
		{"POST migrate import", http.MethodPost, "/api/admin/migrate/import", http.StatusOK, true, false},

		// The platform admin's own settings stay savable.
		{"PUT legal", http.MethodPut, "/api/admin/legal", http.StatusOK, true, false},
		{"POST legal preview", http.MethodPost, "/api/admin/legal/preview", http.StatusOK, true, false},
		{"PUT pricing", http.MethodPut, "/api/admin/pricing", http.StatusOK, true, false},
		{"PUT signup", http.MethodPut, "/api/admin/settings/signup", http.StatusOK, true, false},
		{"PUT admin 2FA", http.MethodPut, "/api/admin/settings/admin-totp", http.StatusOK, true, false},
		{"PUT log level", http.MethodPut, "/api/admin/logs/level", http.StatusOK, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &reachedHandler{}
			handler := maintenanceFreeze(inner)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if inner.called != tc.wantInner {
				t.Errorf("inner reached: got %v, want %v", inner.called, tc.wantInner)
			}
			if tc.wantRetryAfter && rec.Header().Get("Retry-After") == "" {
				t.Errorf("expected Retry-After header on 503")
			}
			if tc.wantStatus == http.StatusServiceUnavailable &&
				!strings.Contains(rec.Body.String(), `"code":"maintenance"`) {
				t.Errorf("503 body should mention maintenance, got %s", rec.Body.String())
			}
		})
	}
}

// TestMaintenance_OffPassesThrough confirms the middleware is a
// no-op when the flag is off — every method on every path reaches
// the inner handler.
func TestMaintenance_OffPassesThrough(t *testing.T) {
	pinMaintenanceMode(t, false)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		inner := &reachedHandler{}
		handler := maintenanceFreeze(inner)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, "/api/admin/events", nil))
		if !inner.called {
			t.Errorf("%s: inner not reached with maintenance off", method)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status=%d, want 200", method, rec.Code)
		}
	}
}

// TestMaintenance_HotFlip exercises the in-memory atomic: flipping
// the flag on/off via the env-pin path takes effect immediately on
// the same handler instance — no restart, same wire posture as the
// in-process settings setter would produce in production.
func TestMaintenance_HotFlip(t *testing.T) {
	inner := &reachedHandler{}
	handler := maintenanceFreeze(inner)

	// Off: POST passes through.
	t.Setenv("ZV_MAINTENANCE_MODE", "")
	settings.LoadMaintenanceMode(context.Background(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/events", nil))
	if rec.Code != http.StatusOK || !inner.called {
		t.Fatalf("expected pass-through with maintenance off, got status=%d inner=%v",
			rec.Code, inner.called)
	}

	// Flip on: same handler now blocks POST.
	inner.called = false
	t.Setenv("ZV_MAINTENANCE_MODE", "1")
	settings.LoadMaintenanceMode(context.Background(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/events", nil))
	if rec.Code != http.StatusServiceUnavailable || inner.called {
		t.Fatalf("expected 503 with maintenance on, got status=%d inner=%v",
			rec.Code, inner.called)
	}

	// Flip off: pass-through again.
	inner.called = false
	t.Setenv("ZV_MAINTENANCE_MODE", "0")
	settings.LoadMaintenanceMode(context.Background(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/events", nil))
	if rec.Code != http.StatusOK || !inner.called {
		t.Fatalf("expected pass-through after flip off, got status=%d inner=%v",
			rec.Code, inner.called)
	}
}
