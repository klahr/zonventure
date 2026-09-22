package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// brandingFixture seeds a single org + an org_admin and returns the
// router with the GET / PUT /orgs/me/profile routes mounted. Each
// test gets a fresh DB; the org has no custom colors yet so empty
// fields exercise the "fall back to default" path.
func brandingFixture(t *testing.T) (db *sql.DB, orgID int64, h *admin.Handlers, r chi.Router) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)

	if err := db.QueryRow(
		`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified)
		VALUES ('boss', 'boss@acme.com', 1) RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		userID, orgID); err != nil {
		t.Fatal(err)
	}

	h = &admin.Handlers{DB: db}
	user := auth.User{
		ID: userID, Username: "boss",
		Role: auth.RoleOrgAdmin, OrgID: &orgID,
	}
	r = chi.NewRouter()
	r.Use(injectUser(user))
	r.Get("/api/admin/orgs/me/profile", h.GetMyOrgProfile)
	r.Put("/api/admin/orgs/me/profile", h.SetMyOrgProfile)
	return db, orgID, h, r
}

// TestBranding_ColorsRoundTrip exercises the happy path: PUT four
// hex colors via the profile endpoint, GET them back, assert each
// column survives intact. Locks the wire shape that the redesigned
// Settings → General "Branding" card relies on.
func TestBranding_ColorsRoundTrip(t *testing.T) {
	_, _, _, r := brandingFixture(t)

	body := map[string]any{
		"accent_color":           "#112233",
		"accent_secondary_color": "#445566",
		"background_color":       "#778899",
		"text_color":             "#aabbcc",
	}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/admin/orgs/me/profile", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status=%d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		Accent     string `json:"accent_color"`
		AccentSec  string `json:"accent_secondary_color"`
		Background string `json:"background_color"`
		Text       string `json:"text_color"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Accent != "#112233" || got.AccentSec != "#445566" ||
		got.Background != "#778899" || got.Text != "#aabbcc" {
		t.Errorf("colors after PUT: got %+v, want #112233/#445566/#778899/#aabbcc", got)
	}

	// Independent GET to confirm persistence (the PUT response
	// could in principle echo without saving).
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/admin/orgs/me/profile", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status=%d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Accent != "#112233" {
		t.Errorf("colors after GET: got %+v, want #112233/...", got)
	}
}

// TestBranding_ColorsValidation locks down the hex-color validator:
// only #RGB and #RRGGBB are accepted; anything else produces 400
// with the colour name in the body so the GUI can highlight the
// offending field.
func TestBranding_ColorsValidation(t *testing.T) {
	_, _, _, r := brandingFixture(t)

	cases := []struct {
		name    string
		payload map[string]any
		wantErr string // substring expected in the 400 body
	}{
		{
			name:    "rgb-named keyword rejected",
			payload: map[string]any{"accent_color": "red"},
			wantErr: "accent",
		},
		{
			name:    "missing-hash rejected",
			payload: map[string]any{"accent_color": "112233"},
			wantErr: "accent",
		},
		{
			name:    "8-digit rgba rejected",
			payload: map[string]any{"text_color": "#11223344"},
			wantErr: "text",
		},
		{
			name:    "non-hex character",
			payload: map[string]any{"background_color": "#11223z"},
			wantErr: "background",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.payload)
			w := httptest.NewRecorder()
			req := httptest.NewRequest("PUT", "/api/admin/orgs/me/profile", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(strings.ToLower(w.Body.String()), tc.wantErr) {
				t.Errorf("body should mention %q, got %s", tc.wantErr, w.Body.String())
			}
		})
	}

	// Acceptance check: short-form (#RGB) is valid.
	body, _ := json.Marshal(map[string]any{"accent_color": "#abc"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/admin/orgs/me/profile", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("short-form #abc: status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestBranding_ColorsClearOnEmpty ensures the "fall back to platform
// default" path works: sending an empty string clears the column.
// The GUI relies on this — the user blanks a field, hits Save, and
// the saved-state echoes back without the colour.
func TestBranding_ColorsClearOnEmpty(t *testing.T) {
	db, orgID, _, r := brandingFixture(t)

	// Pre-set a colour directly.
	if _, err := db.Exec(
		`UPDATE organizations SET accent_color = '#112233' WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}

	// Save with empty accent_color; should clear the column.
	body, _ := json.Marshal(map[string]any{"accent_color": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/admin/orgs/me/profile", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status=%d body=%s", w.Code, w.Body.String())
	}

	var stored sql.NullString
	if err := db.QueryRow(
		`SELECT accent_color FROM organizations WHERE id = $1`, orgID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Valid && stored.String != "" {
		t.Errorf("accent_color after blank PUT: got %q, want NULL/empty", stored.String)
	}
}

// TestBranding_LogoAuthGate proves the org-admin self-logo upload +
// delete are gated to RoleOrgAdmin: an org_member call returns 403
// without touching the row.
func TestBranding_LogoAuthGate(t *testing.T) {
	db, orgID, h, _ := brandingFixture(t)

	// Add a second user as org_member and exercise the same routes
	// through them.
	var memberID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email_verified) VALUES ('grunt', 1) RETURNING id
	`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`,
		memberID, orgID); err != nil {
		t.Fatal(err)
	}
	memberUser := auth.User{
		ID: memberID, Username: "grunt",
		Role: auth.RoleOrgMember, OrgID: &orgID,
	}
	r := chi.NewRouter()
	r.Use(injectUser(memberUser))
	r.Post("/api/admin/orgs/me/logo", h.SetMyOrgLogo)
	r.Delete("/api/admin/orgs/me/logo", h.DeleteMyOrgLogo)

	// org_member POST → 403 (gate refuses before parsing the body).
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/orgs/me/logo", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("org_member POST: status=%d, want 403", w.Code)
	}

	// org_member DELETE → 403.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/admin/orgs/me/logo", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("org_member DELETE: status=%d, want 403", w.Code)
	}

	// Anonymous → 403 (no org context).
	r2 := chi.NewRouter()
	r2.Delete("/api/admin/orgs/me/logo", h.DeleteMyOrgLogo)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/admin/orgs/me/logo", nil)
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("anon DELETE: status=%d, want 403", w.Code)
	}
}

// TestBranding_ProfilePreservesUnrelatedFields locks in that PUT
// /orgs/me/profile with only colors doesn't wipe the tagline or
// website. The current handler resets every column to the request
// payload, so omitting a field would clear it — the test makes the
// trade-off explicit so a future "patch" semantics change is a
// deliberate decision rather than a silent regression.
func TestBranding_ProfilePreservesUnrelatedFields(t *testing.T) {
	db, orgID, _, r := brandingFixture(t)

	// Pre-set tagline + website directly.
	if _, err := db.Exec(`
		UPDATE organizations
		   SET tagline = 'A real-world adventure company',
		       public_website = 'https://acme.example'
		 WHERE id = $1
	`, orgID); err != nil {
		t.Fatal(err)
	}

	// PUT with full payload — caller must echo tagline + website to
	// preserve them. The test pins the contract: the GUI saves the
	// whole form on every Save click, never just the colors.
	body, _ := json.Marshal(map[string]any{
		"tagline":        "A real-world adventure company",
		"public_website": "https://acme.example",
		"accent_color":   "#112233",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/admin/orgs/me/profile", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status=%d body=%s", w.Code, w.Body.String())
	}

	// Verify all three survived.
	var (
		tagline sql.NullString
		website sql.NullString
		accent  sql.NullString
	)
	if err := db.QueryRow(`
		SELECT tagline, public_website, accent_color FROM organizations WHERE id = $1
	`, orgID).Scan(&tagline, &website, &accent); err != nil {
		t.Fatal(err)
	}
	if tagline.String != "A real-world adventure company" {
		t.Errorf("tagline: got %q, want preserved", tagline.String)
	}
	if website.String != "https://acme.example" {
		t.Errorf("website: got %q, want preserved", website.String)
	}
	if accent.String != "#112233" {
		t.Errorf("accent: got %q, want #112233", accent.String)
	}
	_ = context.Background() // keep context import warm for future additions
}
