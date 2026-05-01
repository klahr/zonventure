package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/dbtest"
	"github.com/go-chi/chi/v5"
)

// TestDuplicateAdventure_HappyPath inserts an admin-owned template,
// then verifies an org_admin in another org can clone it into their
// own org with all pages, hints, and accept-list values intact.
//
// Uses the dbtest harness directly + a hand-built chi router rather
// than the full server.New stack so the test stays focused on
// duplicate-handler behaviour and doesn't depend on the wider
// auth/cookie/CSRF pipeline.
func TestDuplicateAdventure_HappyPath(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	// Org + org_admin user.
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1)
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		userID, orgID); err != nil {
		t.Fatal(err)
	}

	// Admin-owned template with one page + one hint to verify the
	// cascade is wired.
	var advID int64
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, author, is_template, published)
		VALUES ('starter', 'Starter Tour', 'Zonventure', 1, 0)
		RETURNING id
	`).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	var pageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng)
		VALUES ($1, 0, 'Welcome', 'Find the year', '[{"text":"1900"}]', 0, 0)
		RETURNING id
	`, advID).Scan(&pageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
		VALUES ($1, 0, 'It is a four-digit number', 30)
	`, pageID); err != nil {
		t.Fatal(err)
	}

	// Wire a Handlers + chi router with auth middleware that injects
	// the boss user. Mirrors the production wiring but skips the
	// signed-cookie session lookup in favour of a fake middleware.
	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	user := auth.User{
		ID:    userID,
		Role:  auth.RoleOrgAdmin,
		OrgID: &orgID,
	}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/adventures/{slug}/duplicate", h.DuplicateAdventure)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/adventures/starter/duplicate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.Slug, "starter-") {
		t.Errorf("expected slug prefix starter-, got %q", resp.Slug)
	}

	// New adventure exists in the caller's org, is unpublished, is
	// not itself a template, and carries the source's title with
	// "(copy)" appended.
	var (
		gotOrg     int64
		gotPublished int
		gotTemplate  int
		gotTitle    string
	)
	if err := db.QueryRow(`
		SELECT org_id, published, is_template, title FROM adventures WHERE slug = $1
	`, resp.Slug).Scan(&gotOrg, &gotPublished, &gotTemplate, &gotTitle); err != nil {
		t.Fatalf("loaded clone: %v", err)
	}
	if gotOrg != orgID {
		t.Errorf("clone org = %d, want %d", gotOrg, orgID)
	}
	if gotPublished != 0 {
		t.Errorf("clone published = %d, want 0", gotPublished)
	}
	if gotTemplate != 0 {
		t.Errorf("clone is_template = %d, want 0 (clones are never templates)", gotTemplate)
	}
	if gotTitle != "Starter Tour (copy)" {
		t.Errorf("clone title = %q, want %q", gotTitle, "Starter Tour (copy)")
	}

	// Pages cascade.
	var pageCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM adventure_pages p
		JOIN adventures a ON a.id = p.adventure_id
		WHERE a.slug = $1
	`, resp.Slug).Scan(&pageCount); err != nil {
		t.Fatal(err)
	}
	if pageCount != 1 {
		t.Errorf("clone page count = %d, want 1", pageCount)
	}

	// Hints cascade.
	var hintCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM adventure_page_hints h
		JOIN adventure_pages p ON p.id = h.page_id
		JOIN adventures a ON a.id = p.adventure_id
		WHERE a.slug = $1
	`, resp.Slug).Scan(&hintCount); err != nil {
		t.Fatal(err)
	}
	if hintCount != 1 {
		t.Errorf("clone hint count = %d, want 1", hintCount)
	}

	// Source untouched.
	var sourceTitle string
	if err := db.QueryRow(`SELECT title FROM adventures WHERE slug = 'starter'`).Scan(&sourceTitle); err != nil {
		t.Fatal(err)
	}
	if sourceTitle != "Starter Tour" {
		t.Errorf("source title mutated to %q", sourceTitle)
	}
}

// TestDuplicateAdventure_OrgMemberRejected confirms canAuthor gates
// the destination side: a Facilitator (org_member) can run events
// but cannot create authored rows by duplication. If this test
// regresses, the role-tightening shipped earlier has been quietly
// undone.
func TestDuplicateAdventure_OrgMemberRejected(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified) VALUES ('intern', 'intern@acme.com', 1)
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`,
		userID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, title, author, is_template, published)
		VALUES ('starter', 'Starter Tour', 'Zonventure', 1, 0)
	`); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	user := auth.User{ID: userID, Role: auth.RoleOrgMember, OrgID: &orgID}

	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/adventures/{slug}/duplicate", h.DuplicateAdventure)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/adventures/starter/duplicate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

// TestDuplicateAdventure_CrossOrgPrivateRejected confirms the
// canSeeOwned source check: a user can't clone a private adventure
// belonging to a different org just because they happen to know the
// slug. Stops cross-org content theft via the duplicate endpoint.
func TestDuplicateAdventure_CrossOrgPrivateRejected(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	// Two orgs. Caller belongs to A. Adventure belongs to B.
	var orgA, orgB int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('A') RETURNING id`).Scan(&orgA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('B') RETURNING id`).Scan(&orgB); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified) VALUES ('a-boss', 'boss@a.com', 1)
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		userID, orgA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, title, author, is_template, published, org_id)
		VALUES ('private-b', 'B Private', '', 0, 0, $1)
	`, orgB); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgA}

	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/adventures/{slug}/duplicate", h.DuplicateAdventure)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/adventures/private-b/duplicate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

// injectUser is a test middleware that places a fake auth.User into
// the request context, sidestepping cookie + DB lookup. Mirrors what
// the production middleware injects after a real session lookup.
func injectUser(u auth.User) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
		})
	}
}
