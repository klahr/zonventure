package admin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/settings"
)

// migrateRouter wires the three migrate handlers behind a fake auth
// gate that mirrors the production posture: route-level
// RequireRole(RoleAdmin), with the caller's auth.User injected via
// the same middleware quests_test.go uses for every other admin
// surface. Returns the chi router so the test can drive HTTP calls.
func migrateRouter(t *testing.T, h *admin.Handlers, user auth.User) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	r.Use(injectUser(user))
	// Mimic the production gate: only RoleAdmin can reach the
	// migration surface. We run this check inline rather than
	// through auth.RequireRole because that helper expects the
	// auth middleware chain we've sidestepped.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			me, _ := auth.FromContext(req.Context())
			if me.Role != auth.RoleAdmin {
				http.Error(w, "admin only", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/api/admin/migrate/export", h.MigrateExport)
	r.Post("/api/admin/migrate/import", h.MigrateImport)
	r.Get("/api/admin/migrate/redirect-mode", h.MigrateRedirectMode)
	r.Post("/api/admin/migrate/redirect-mode", h.MigrateRedirectMode)
	r.Get("/api/admin/migrate/maintenance", h.MigrateMaintenance)
	r.Post("/api/admin/migrate/maintenance", h.MigrateMaintenance)
	return r
}

// withMaintenanceOn flips the instance into maintenance mode for the
// duration of the test, then turns it back off in cleanup. The
// operator-side MigrateExport refuses unless maintenance is on, so
// every test that drives the export needs this in setup.
func withMaintenanceOn(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := settings.SetMaintenanceMode(context.Background(), db, true); err != nil {
		t.Fatalf("enable maintenance: %v", err)
	}
	t.Cleanup(func() {
		if err := settings.SetMaintenanceMode(context.Background(), db, false); err != nil {
			t.Logf("disable maintenance (cleanup): %v", err)
		}
	})
}

// TestMigrate_ExportRequiresMaintenance proves the operator-side
// export refuses unless maintenance mode is on. This is the
// snapshot-consistency guarantee — concurrent player progression
// during an active export would otherwise tear the bundle, and
// catching that with a clear 409 is far better than producing a
// bundle the operator only later notices is internally
// inconsistent.
//
// Once maintenance is on, it also checks the export response is a
// readable zip whose org.json declares the right format string and
// org name. Locks the on-wire shape — a future format bump would
// fail this until the constant + manifest both move.
func TestMigrate_ExportRequiresMaintenance(t *testing.T) {
	db, orgID, _, _, h := orgFixture(t)

	// Belt-and-braces: explicitly clear the flag in case a leaking
	// prior test left it on. (Each test should clean up via
	// withMaintenanceOn's t.Cleanup, but the cost of being defensive
	// here is one extra setting write.)
	if err := settings.SetMaintenanceMode(context.Background(), db, false); err != nil {
		t.Fatalf("clear maintenance: %v", err)
	}

	admUser := auth.User{ID: 0, Role: auth.RoleAdmin}
	r := migrateRouter(t, h, admUser)

	// Off → 409.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/api/admin/migrate/export?org_id="+strconv.FormatInt(orgID, 10), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("export with maintenance off: status=%d body=%s, want 409",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "maintenance") {
		t.Errorf("error body should mention maintenance, got %s", w.Body.String())
	}

	// Flip on → 200.
	withMaintenanceOn(t, db)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET",
		"/api/admin/migrate/export?org_id="+strconv.FormatInt(orgID, 10), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export with maintenance on: status=%d body=%s, want 200",
			w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type: got %q, want application/zip", got)
	}
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	var (
		gotManifest bool
		gotAdvDir   bool
	)
	for _, zf := range zr.File {
		if zf.Name == "org.json" {
			gotManifest = true
			rc, _ := zf.Open()
			defer rc.Close()
			var m struct {
				Format string `json:"format"`
				Org    struct {
					Name string `json:"name"`
				} `json:"org"`
			}
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				t.Errorf("decode org.json: %v", err)
			}
			if m.Format != "zonventure-org-v1" {
				t.Errorf("manifest format: got %q, want zonventure-org-v1", m.Format)
			}
			if m.Org.Name != "Acme" {
				t.Errorf("org name: got %q, want Acme", m.Org.Name)
			}
		}
		if strings.HasPrefix(zf.Name, "adventures/treasure/") {
			gotAdvDir = true
		}
	}
	if !gotManifest {
		t.Errorf("zip missing org.json")
	}
	if !gotAdvDir {
		t.Errorf("zip missing adventures/treasure/ dir")
	}

	// MigrateSelfExport (per-org, on hosted) must NOT require
	// maintenance — flipping the whole platform freeze for one
	// tenant's portability request would be wrong. With maintenance
	// off again, self-export still works.
	if err := settings.SetMaintenanceMode(context.Background(), db, false); err != nil {
		t.Fatalf("clear maintenance: %v", err)
	}
	orgUser := auth.User{ID: 1, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	selfR := chi.NewRouter()
	selfR.Use(injectUser(orgUser))
	selfR.Get("/api/admin/migrate/self-export", h.MigrateSelfExport)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/admin/migrate/self-export", nil)
	selfR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("self-export with maintenance off: status=%d body=%s, want 200 (self-export must not require maintenance)",
			w.Code, w.Body.String())
	}
}

// TestMigrate_MaintenanceToggle drives the toggle endpoint end-to-
// end the same way TestMigrate_RedirectModeToggle does for the
// redirect setting: GET reports current state, POST flips it on,
// GET confirms, POST clears it, GET confirms cleared. Verifies
// settings.MaintenanceMode() reflects the change in-process.
func TestMigrate_MaintenanceToggle(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	h := &admin.Handlers{DB: db}
	t.Setenv("ZV_MAINTENANCE_MODE", "")
	settings.LoadMaintenanceMode(context.Background(), db)
	t.Cleanup(func() {
		// Defensive: clear in case a write-then-error path left the
		// flag on. The settings table is per-test (dbtest gives us
		// a fresh DB) but the in-memory atomic is package-level.
		_ = settings.SetMaintenanceMode(context.Background(), db, false)
	})

	admUser := auth.User{ID: 0, Role: auth.RoleAdmin}
	r := migrateRouter(t, h, admUser)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/migrate/maintenance", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: status=%d body=%s", w.Code, w.Body.String())
	}
	var status admin.MaintenanceModeStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if status.On || status.PinnedByEnv {
		t.Errorf("initial: got %+v, want off + not pinned", status)
	}

	// Flip on.
	w = httptest.NewRecorder()
	body := strings.NewReader(`{"on": true}`)
	req := httptest.NewRequest("POST", "/api/admin/migrate/maintenance", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post enable: status=%d body=%s", w.Code, w.Body.String())
	}
	if !settings.MaintenanceMode() {
		t.Errorf("settings.MaintenanceMode() after enable: got false, want true")
	}

	// GET confirms enabled.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/migrate/maintenance", nil))
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode get-after-enable: %v", err)
	}
	if !status.On {
		t.Errorf("after enable: got %+v, want On=true", status)
	}

	// Flip off.
	w = httptest.NewRecorder()
	body = strings.NewReader(`{"on": false}`)
	req = httptest.NewRequest("POST", "/api/admin/migrate/maintenance", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post disable: status=%d body=%s", w.Code, w.Body.String())
	}
	if settings.MaintenanceMode() {
		t.Errorf("settings.MaintenanceMode() after disable: got true, want false")
	}
}

// TestMigrate_SelfExportAuthGate locks the data-portability surface:
//   - RoleOrgAdmin → 200 with their own org's bundle.
//   - RoleOrgMember → 403 (bundle has answer keys + buyer emails).
//   - RoleAdmin → 403 (admin must use the operator endpoint).
//   - Anonymous (no user in context) → 403.
//   - Crucially: the bundle returned to org A's admin must NOT carry
//     any of org B's data, even when B exists in the same DB.
func TestMigrate_SelfExportAuthGate(t *testing.T) {
	t.Parallel()
	db, h, ids := richFixture(t)

	// org_admin of Acme — should succeed.
	acmeAdmin := auth.User{
		ID: ids.bossUserID, Username: "boss",
		Role: auth.RoleOrgAdmin, OrgID: &ids.acme,
	}
	r := chi.NewRouter()
	r.Use(injectUser(acmeAdmin))
	r.Get("/api/admin/migrate/self-export", h.MigrateSelfExport)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/admin/migrate/self-export", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("org_admin self-export: status=%d body=%s, want 200",
			w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type: got %q, want application/zip", got)
	}
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	// Pull the manifest out and assert it carries Acme's data and not
	// Bystander's. The fixture seeds Bystander alongside Acme; an
	// over-broad self-export query (forgot the org_id filter) would
	// surface here as a leak.
	var manifest struct {
		Org struct {
			Name string `json:"name"`
		} `json:"org"`
		Adventures []struct {
			Slug string `json:"slug"`
		} `json:"adventures"`
		Events []struct {
			LiveToken string `json:"live_token"`
		} `json:"events"`
	}
	for _, zf := range zr.File {
		if zf.Name != "org.json" {
			continue
		}
		rc, _ := zf.Open()
		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			t.Fatalf("decode org.json: %v", err)
		}
		rc.Close()
	}
	if manifest.Org.Name != "Acme" {
		t.Errorf("manifest.org.name: got %q, want Acme", manifest.Org.Name)
	}
	for _, a := range manifest.Adventures {
		if a.Slug == ids.advBystander {
			t.Errorf("self-export leaked bystander adventure %q", a.Slug)
		}
	}
	for _, e := range manifest.Events {
		if e.LiveToken == "live-tok-bystander" {
			t.Errorf("self-export leaked bystander event")
		}
	}

	// org_member of Acme — should 403. Set up a member user to exercise
	// this; the fixture already created the membership row.
	memberUser := auth.User{
		ID: ids.minionUserID, Username: "minion",
		Role: auth.RoleOrgMember, OrgID: &ids.acme,
	}
	r = chi.NewRouter()
	r.Use(injectUser(memberUser))
	r.Get("/api/admin/migrate/self-export", h.MigrateSelfExport)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/admin/migrate/self-export", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("org_member self-export: status=%d, want 403", w.Code)
	}

	// Global admin — should 403 (admin uses MigrateExport, which takes
	// org_id explicitly). Admins have no OrgID, so even if the gate
	// missed they'd hit the OrgID nil check.
	adminUser := auth.User{ID: 0, Role: auth.RoleAdmin}
	r = chi.NewRouter()
	r.Use(injectUser(adminUser))
	r.Get("/api/admin/migrate/self-export", h.MigrateSelfExport)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/admin/migrate/self-export", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("admin self-export: status=%d, want 403", w.Code)
	}

	// Anonymous (no user in context) — should also 403. We sidestep
	// injectUser to leave the context empty.
	r = chi.NewRouter()
	r.Get("/api/admin/migrate/self-export", h.MigrateSelfExport)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/admin/migrate/self-export", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("anon self-export: status=%d, want 403", w.Code)
	}

	_ = db // silence linter when fixture vars are unused later
}

// TestMigrate_RedirectModeToggle drives the toggle endpoint end-to-
// end: GET reports current state, POST flips it on, GET confirms,
// POST clears it, GET confirms cleared. Verifies the in-memory
// atomic pointer (`settings.RedirectTarget()`) reflects the change
// at every step — the redirect middleware reads from there, so
// this is the wiring that the GUI relies on.
func TestMigrate_RedirectModeToggle(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	h := &admin.Handlers{DB: db}

	// Make sure the env-pin path doesn't leak across tests.
	t.Setenv("ZV_REDIRECT_TARGET", "")
	settings.LoadRedirectTarget(context.Background(), h.DB)

	admUser := auth.User{ID: 0, Role: auth.RoleAdmin}
	r := migrateRouter(t, h, admUser)

	// 1. GET — should report empty target, not pinned.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/migrate/redirect-mode", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: status=%d body=%s", w.Code, w.Body.String())
	}
	var status admin.RedirectModeStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if status.Target != "" || status.PinnedByEnv {
		t.Errorf("initial state: got %+v, want empty + not pinned", status)
	}

	// 2. POST — enable.
	w = httptest.NewRecorder()
	body := strings.NewReader(`{"target": "https://hosted.example"}`)
	req := httptest.NewRequest("POST", "/api/admin/migrate/redirect-mode", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post enable: status=%d body=%s", w.Code, w.Body.String())
	}
	if got := settings.RedirectTarget(); got != "https://hosted.example" {
		t.Errorf("settings.RedirectTarget after POST: got %q, want https://hosted.example", got)
	}

	// 3. GET — should report enabled state.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/migrate/redirect-mode", nil))
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode get-after-enable: %v", err)
	}
	if status.Target != "https://hosted.example" {
		t.Errorf("get-after-enable: got %+v, want target=https://hosted.example", status)
	}

	// 4. POST — disable.
	w = httptest.NewRecorder()
	body = strings.NewReader(`{"target": ""}`)
	req = httptest.NewRequest("POST", "/api/admin/migrate/redirect-mode", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post disable: status=%d body=%s", w.Code, w.Body.String())
	}
	if got := settings.RedirectTarget(); got != "" {
		t.Errorf("settings.RedirectTarget after disable: got %q, want empty", got)
	}
}

// An env pin turns the stored toggle off: after unsetting it and
// starting again, maintenance is off, as the UI says.
func TestMigrate_MaintenancePinClearsToggle(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	t.Setenv("ZV_MAINTENANCE_MODE", "")
	if err := settings.SetMaintenanceMode(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.SetMaintenanceMode(ctx, db, false) })
	t.Setenv("ZV_MAINTENANCE_MODE", "1")
	settings.LoadMaintenanceMode(ctx, db)
	if !settings.MaintenanceMode() {
		t.Fatal("pinned on: maintenance off")
	}
	t.Setenv("ZV_MAINTENANCE_MODE", "")
	settings.LoadMaintenanceMode(ctx, db)
	if settings.MaintenanceMode() {
		t.Error("pin removed and started again: maintenance still on")
	}
}
