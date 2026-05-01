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

// TestDuplicatePage_HappyPath confirms the Google-Slides-style
// duplicate inserts at sourceIdx + 1, shifts later pages out by 1
// to make room (without violating the UNIQUE(adventure_id, idx)
// constraint), and copies hints + media.
func TestDuplicatePage_HappyPath(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

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

	// Adventure with 3 puzzles. Duplicate the middle one (idx 1) and
	// confirm we end up with 4 pages: idx 0 (original A), 1 (B),
	// 2 (B-clone), 3 (original C).
	var advID int64
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)
		RETURNING id
	`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	pageIDs := make([]int64, 3)
	for i, title := range []string{"A", "B", "C"} {
		if err := db.QueryRow(`
			INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, org_id)
			VALUES ($1, $2, $3, '', '[]', 0, 0, $4)
			RETURNING id
		`, advID, i, title, orgID).Scan(&pageIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Add a hint to the middle page so the cascade is exercised.
	if _, err := db.Exec(`
		INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
		VALUES ($1, 0, 'B-hint', 30)
	`, pageIDs[1]); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/pages/{pageID}/duplicate", h.DuplicatePage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/pages/"+itoa64(pageIDs[1])+"/duplicate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID  int64 `json:"id"`
		Idx int   `json:"idx"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Idx != 2 {
		t.Errorf("clone idx = %d, want 2 (sourceIdx + 1)", resp.Idx)
	}

	// Re-load the adventure: 4 pages, ordered A, B, B-clone, C.
	rows, err := db.Query(`SELECT idx, title FROM adventure_pages WHERE adventure_id = $1 ORDER BY idx`, advID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []struct {
		Idx   int
		Title string
	}
	for rows.Next() {
		var entry struct {
			Idx   int
			Title string
		}
		if err := rows.Scan(&entry.Idx, &entry.Title); err != nil {
			t.Fatal(err)
		}
		got = append(got, entry)
	}
	want := []struct {
		Idx   int
		Title string
	}{
		{0, "A"},
		{1, "B"},
		{2, "B"}, // duplicate carries the source title verbatim — author renames in the editor
		{3, "C"},
	}
	if len(got) != len(want) {
		t.Fatalf("page count = %d, want %d (rows: %+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// Hint cascade: the clone should carry one hint with text "B-hint".
	var hintCount int
	var hintText string
	if err := db.QueryRow(`
		SELECT COUNT(*), COALESCE(MAX(text), '') FROM adventure_page_hints WHERE page_id = $1
	`, resp.ID).Scan(&hintCount, &hintText); err != nil {
		t.Fatal(err)
	}
	if hintCount != 1 || hintText != "B-hint" {
		t.Errorf("hints on clone: count=%d text=%q (expected 1, %q)", hintCount, hintText, "B-hint")
	}

	// Source untouched: pageIDs[1] still has its single hint.
	var srcHints int
	if err := db.QueryRow(`SELECT COUNT(*) FROM adventure_page_hints WHERE page_id = $1`, pageIDs[1]).Scan(&srcHints); err != nil {
		t.Fatal(err)
	}
	if srcHints != 1 {
		t.Errorf("source hint count = %d, want 1 (cascade should copy, not move)", srcHints)
	}
}

// TestDuplicatePage_OrgMemberRejected — the role-tightening from
// the canAuthor split must reject Facilitators here too. If this
// regresses, the page-edit gate has been quietly loosened.
func TestDuplicatePage_OrgMemberRejected(t *testing.T) {
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
	var advID int64
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1) RETURNING id
	`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	var pageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, org_id)
		VALUES ($1, 0, 'A', '', '[]', 0, 0, $2)
		RETURNING id
	`, advID, orgID).Scan(&pageID); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	user := auth.User{ID: userID, Role: auth.RoleOrgMember, OrgID: &orgID}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/pages/{pageID}/duplicate", h.DuplicatePage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/pages/"+itoa64(pageID)+"/duplicate", strings.NewReader("{}"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

// itoa64 — int64 stringifier used in URL composition. Pulled out
// so the test files don't depend on strconv directly in many
// places; trivial helper.
func itoa64(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}
