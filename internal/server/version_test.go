package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestVersionEndpoint asserts that /api/version returns the configured
// version string plus the Go runtime info. VCS fields (commit / time)
// are populated by debug.ReadBuildInfo, which only fills them when the
// test binary itself was built inside a VCS checkout — so we don't
// require them, only that the response shape is stable.
//
// The route is mounted on a bare router, as New mounts it, so the test
// needs no database. MCP is pinned off: its routes, registered alongside,
// would want the handlers New builds.
func TestVersionEndpoint(t *testing.T) {
	t.Setenv("ZV_MCP_ENABLED", "")
	r := chi.NewRouter()
	cfg := Config{Version: "v9.9.9-test"}
	registerMetaRoutes(r, &routesEnv{cfg: cfg, verResp: buildVersion(cfg.Version)})

	req := httptest.NewRequest("GET", "/api/version", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache-control = %q, want no-store", got)
	}

	var resp versionResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != "v9.9.9-test" {
		t.Errorf("version = %q, want v9.9.9-test", resp.Version)
	}
	if resp.Go != runtime.Version() {
		t.Errorf("go = %q, want %q", resp.Go, runtime.Version())
	}
	if resp.OS != runtime.GOOS {
		t.Errorf("os = %q, want %q", resp.OS, runtime.GOOS)
	}
	if resp.Arch != runtime.GOARCH {
		t.Errorf("arch = %q, want %q", resp.Arch, runtime.GOARCH)
	}
}

// TestBuildVersion_DefaultsToDev confirms an empty Config.Version
// surfaces as "dev" rather than the empty string.
func TestBuildVersion_DefaultsToDev(t *testing.T) {
	if got := buildVersion("").Version; got != "dev" {
		t.Errorf("buildVersion(``).Version = %q, want %q", got, "dev")
	}
	if got := buildVersion("v1.2.3").Version; got != "v1.2.3" {
		t.Errorf("buildVersion(`v1.2.3`).Version = %q, want %q", got, "v1.2.3")
	}
}
