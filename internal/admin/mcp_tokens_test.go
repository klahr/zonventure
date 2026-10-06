package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// seedMCPUser installs a fresh org + user + admin membership and
// returns the auth.User the handler would see if this person had a
// session. Cheap fixture: each test gets a clean DB so we don't
// bother with idempotent inserts.
func seedMCPUser(t *testing.T, db *sql.DB) auth.User {
	t.Helper()
	ctx := context.Background()
	var orgID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var uid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(username, email, email_verified) VALUES ('alice','alice@acme.com',1)
		RETURNING id
	`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1,$2,'org_admin')`,
		uid, orgID); err != nil {
		t.Fatal(err)
	}
	return auth.User{
		ID: uid, Username: "alice", Role: "org_admin",
		OrgID: &orgID, Email: "alice@acme.com", EmailVerified: true,
	}
}

func postCreateToken(t *testing.T, h *admin.Handlers, me auth.User, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/me/mcp-tokens", bytes.NewReader(raw))
	req = req.WithContext(auth.WithUser(req.Context(), me))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreateMCPToken(w, req)
	return w
}

// TestCreateMCPToken_DefaultScopes covers the back-compat path:
// posting {name} with no scopes field yields a token with the full
// ['read','write'] set, matching the column DEFAULT. Old clients
// keep working without a code change.
func TestCreateMCPToken_DefaultScopes(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	me := seedMCPUser(t, db)
	h := &admin.Handlers{DB: db}

	w := postCreateToken(t, h, me, map[string]any{"name": "claude-desktop"})
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Token  string   `json:"token"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	sort.Strings(resp.Scopes)
	if want := []string{"read", "write"}; len(resp.Scopes) != 2 || resp.Scopes[0] != want[0] || resp.Scopes[1] != want[1] {
		t.Errorf("create scopes=%v, want %v", resp.Scopes, want)
	}

	_, scopes, err := admin.LookupMCPToken(context.Background(), db, resp.Token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	sort.Strings(scopes)
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("lookup scopes=%v, want [read write]", scopes)
	}
}

// TestCreateMCPToken_ReadOnly proves a caller can mint a narrowed
// token by passing scopes:["read"]. The roundtrip preserves the
// narrowed set, so the MCP gate will refuse write tools when this
// token is presented.
func TestCreateMCPToken_ReadOnly(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	me := seedMCPUser(t, db)
	h := &admin.Handlers{DB: db}

	w := postCreateToken(t, h, me, map[string]any{"name": "dashboard", "scopes": []string{"read"}})
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Token  string   `json:"token"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Scopes) != 1 || resp.Scopes[0] != "read" {
		t.Fatalf("create scopes=%v, want [read]", resp.Scopes)
	}

	_, scopes, err := admin.LookupMCPToken(context.Background(), db, resp.Token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(scopes) != 1 || scopes[0] != "read" {
		t.Errorf("lookup scopes=%v, want [read]", scopes)
	}
}

// TestCreateMCPToken_RejectsUnknownScope locks in the validator: a
// caller fishing for future scope names gets a 400 with a clear
// message rather than silently storing the unknown string.
func TestCreateMCPToken_RejectsUnknownScope(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	me := seedMCPUser(t, db)
	h := &admin.Handlers{DB: db}

	w := postCreateToken(t, h, me, map[string]any{"name": "weird", "scopes": []string{"admin"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
	}
}
