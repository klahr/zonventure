package server

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLandingOverride verifies the ZV_WEB_OVERRIDE_DIR seam end to end:
// a landing.html and landing-locales.json dropped in the override dir
// shadow the embedded template + struct defaults, and the rendered
// page reflects the operator-supplied copy. Catches regressions in
// readWebFile (file shadowing), applyLandingLocaleOverrides (per-field
// JSON merge), and the wiring between loadOverrides and the rest of
// the package.
func TestLandingOverride(t *testing.T) {
	dir := t.TempDir()
	// NAV_STORE is included to lock in the bug fix where
	// renderLandingPage was substituting its own duplicated chrome
	// list and silently dropping any field added to topbarSubs
	// (NavStore, future additions). Now it delegates to topbarSubs
	// so this assertion proves the chrome subs reach the landing.
	tpl := `<!doctype html><html lang="{{LANG_CODE}}"><body>HERO={{HERO_HEADING}};STORE={{NAV_STORE}};FOOTER={{FOOTER_COPY}}</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "landing.html"), []byte(tpl), 0o644); err != nil {
		t.Fatalf("write landing: %v", err)
	}
	locales := `{
		"en": {"HeroHeading": "Override hero EN", "FooterCopy": "© Override EN"},
		"sv": {"HeroHeading": "Override hero SV"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "landing-locales.json"), []byte(locales), 0o644); err != nil {
		t.Fatalf("write locales: %v", err)
	}

	// Snapshot package state so other tests in this package — which
	// re-use the same landingLocales map — see the unmodified defaults
	// regardless of execution order.
	restore := snapshotLandingState()
	t.Cleanup(restore)

	if err := loadOverrides(dir); err != nil {
		t.Fatalf("loadOverrides: %v", err)
	}
	if webOverrideDir != dir {
		t.Fatalf("webOverrideDir = %q, want %q", webOverrideDir, dir)
	}
	if got := landingLocales["en"].HeroHeading; got != "Override hero EN" {
		t.Errorf("en HeroHeading = %q, want override", got)
	}
	if got := landingLocales["en"].FooterCopy; got != "© Override EN" {
		t.Errorf("en FooterCopy = %q, want override", got)
	}
	// sv override only set HeroHeading — FooterCopy must keep the
	// embedded default rather than getting blanked.
	if got := landingLocales["sv"].FooterCopy; got == "" {
		t.Errorf("sv FooterCopy was blanked by partial override")
	}
	if got := landingLocales["sv"].HeroHeading; got != "Override hero SV" {
		t.Errorf("sv HeroHeading = %q, want override", got)
	}

	req := httptest.NewRequest("GET", "/?lang=en", nil)
	rec := httptest.NewRecorder()
	renderLandingPage(rec, req, "https://zv.example.com", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "HERO=Override hero EN") {
		t.Errorf("rendered body missing override hero: %s", body)
	}
	if !strings.Contains(body, "FOOTER=© Override EN") {
		t.Errorf("rendered body missing override footer: %s", body)
	}
	// The Store nav link was the original bug: NAV_STORE existed
	// in topbarSubs but renderLandingPage rolled its own duplicated
	// list and never picked it up, so the literal placeholder
	// leaked through to users on the override-rendered landing.
	if !strings.Contains(body, "STORE=Store") {
		t.Errorf("rendered body missing NAV_STORE substitution (chrome subs not delegated to topbarSubs?): %s", body)
	}
	if strings.Contains(body, "{{NAV_STORE}}") {
		t.Errorf("rendered body still contains {{NAV_STORE}} placeholder: %s", body)
	}
}

// TestLandingOverride_MissingDir is the operator-typo case: pointing
// the env var at a nonexistent path must NOT take the service down —
// it should log and fall through to embedded defaults.
func TestLandingOverride_MissingDir(t *testing.T) {
	restore := snapshotLandingState()
	t.Cleanup(restore)

	if err := loadOverrides(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("missing dir should be tolerated, got %v", err)
	}
	if webOverrideDir != "" {
		t.Errorf("webOverrideDir set to %q despite missing dir", webOverrideDir)
	}
}

// snapshotLandingState captures the package-level state mutated by
// loadOverrides so a test can reset it after running. Package tests
// share the same landingLocales map and webOverrideDir var; without
// this, an override-set test contaminates every later render test
// in the same `go test` invocation.
func snapshotLandingState() func() {
	prevDir := webOverrideDir
	prev := maps.Clone(landingLocales)
	return func() {
		webOverrideDir = prevDir
		clear(landingLocales)
		maps.Copy(landingLocales, prev)
	}
}

// TestServeOverridePageOr404 pins the extra-page fallback: an unmatched
// /name serves name.html from the override dir with the topbar chrome
// filled in, while reserved template names, nested paths and missing
// files fall through to the 404.
func TestServeOverridePageOr404(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"create.html": `<html lang="{{LANG_CODE}}">CREATE</html>`,
		"store.html":  `STORE TEMPLATE`,
		"Upper.html":  `UPPER`,
		"notes.txt":   `TXT`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prev := webOverrideDir
	webOverrideDir = dir
	t.Cleanup(func() { webOverrideDir = prev })

	for _, tc := range []struct {
		method, path string
		want         int
		body         string
	}{
		{"GET", "/create?lang=sv", http.StatusOK, `<html lang="sv">CREATE</html>`},
		{"HEAD", "/create", http.StatusOK, ""},
		{"POST", "/create", http.StatusNotFound, ""},
		{"GET", "/store", http.StatusNotFound, ""},
		{"GET", "/Upper", http.StatusNotFound, ""},
		{"GET", "/notes", http.StatusNotFound, ""},
		{"GET", "/a/create", http.StatusNotFound, ""},
		{"GET", "/missing", http.StatusNotFound, ""},
	} {
		rec := httptest.NewRecorder()
		serveOverridePageOr404(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
		if tc.body != "" && rec.Body.String() != tc.body {
			t.Errorf("%s %s body = %q, want %q", tc.method, tc.path, rec.Body.String(), tc.body)
		}
	}
}

// TestStoreListingOrgPlaceholders pins the org data /o/{slug}/store
// hands its template, and that the global /store fills the same
// placeholders with nothing.
func TestStoreListingOrgPlaceholders(t *testing.T) {
	dir := t.TempDir()
	tpl := `SCOPE={{STORE_SCOPE}};NAME={{ORG_NAME}};TAG={{ORG_TAGLINE}};PATH={{ORG_PATH}};LOGO={{LOGO_BLOCK}};BG={{HERO_BG_STYLE}};BRAND={{ORG_BRAND_OVERRIDES}};API={{STORE_API_BASE}}`
	if err := os.WriteFile(filepath.Join(dir, "store.html"), []byte(tpl), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := webOverrideDir
	webOverrideDir = dir
	t.Cleanup(func() { webOverrideDir = prev })

	org := &orgProfile{ID: 7, Slug: "acme", Name: `Acme & "Co"`, HasLogo: true}
	org.Tagline.String, org.Tagline.Valid = "Hunts by the sea", true
	org.Accent.String, org.Accent.Valid = "#123456", true

	rec := httptest.NewRecorder()
	renderStoreListingPageScoped(rec, httptest.NewRequest("GET", "/o/acme/store?lang=en", nil), org)
	body := rec.Body.String()
	for _, want := range []string{
		"SCOPE=org;",
		"NAME=Acme &amp; &#34;Co&#34;;",
		"TAG=Hunts by the sea;",
		"PATH=/o/acme;",
		`<img src="/org-logo/7"`,
		"BG=;",
		"--accent:#123456;",
		"API=/api/o/acme/store",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("org store body missing %q: %s", want, body)
		}
	}

	rec = httptest.NewRecorder()
	renderStoreListingPage(rec, httptest.NewRequest("GET", "/store?lang=en", nil))
	if got, want := rec.Body.String(), "SCOPE=all;NAME=;TAG=;PATH=;LOGO=;BG=;BRAND=;API=/api/store"; got != want {
		t.Errorf("global store body = %q, want %q", got, want)
	}
}
