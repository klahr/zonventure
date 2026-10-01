package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

func seedUsernameUser(t *testing.T, db *sql.DB, username, email, password string) auth.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var uid int64
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO users(username, email, email_verified, password_hash) VALUES ($1,$2,1,$3)
		RETURNING id
	`, username, email, hash).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	return auth.User{ID: uid, Username: username, Email: email, EmailVerified: true}
}

func postChangeUsername(t *testing.T, h *admin.Handlers, me auth.User, username string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"username": username})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/me/username", bytes.NewReader(raw))
	req = req.WithContext(auth.WithUser(req.Context(), me))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ChangeMyUsername(w, req)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

// TestChangeMyUsername_Success: a signup-style user (username ==
// email) picks a name, can log in with it and with the email, and the
// change lands in their auth_events.
func TestChangeMyUsername_Success(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	me := seedUsernameUser(t, db, "alice@acme.com", "alice@acme.com", "correct horse battery")
	h := &admin.Handlers{DB: db}

	w := postChangeUsername(t, h, me, "  alice.k_1-  ")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != me.ID || resp.Username != "alice.k_1-" {
		t.Errorf("resp=%+v, want id=%d username=alice.k_1-", resp, me.ID)
	}

	for _, ident := range []string{"alice.k_1-", "ALICE.K_1-", "alice@acme.com"} {
		u, err := auth.VerifyPassword(ctx, db, ident, "correct horse battery")
		if err != nil {
			t.Errorf("login as %q: %v", ident, err)
			continue
		}
		if u.ID != me.ID || u.Username != "alice.k_1-" {
			t.Errorf("login as %q: got id=%d username=%q", ident, u.ID, u.Username)
		}
	}

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM auth_events WHERE user_id = $1 AND event = $2 AND ok <> 0`,
		me.ID, auth.EventSetUsername).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("set_username events=%d, want 1", n)
	}
}

func TestChangeMyUsername_Invalid(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	me := seedUsernameUser(t, db, "alice", "alice@acme.com", "correct horse battery")
	h := &admin.Handlers{DB: db}

	for _, name := range []string{
		"", "ab", "   ab   ", "bob@acme.com", "has space", "åsa",
		"abcdefghijklmnopqrstuvwxyz0123456", // 33
	} {
		w := postChangeUsername(t, h, me, name)
		if w.Code != http.StatusBadRequest || errorCode(t, w) != "username_invalid" {
			t.Errorf("%q: status=%d body=%s, want 400 username_invalid", name, w.Code, w.Body.String())
		}
	}
	var got string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = $1`, me.ID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "alice" {
		t.Errorf("username=%q after rejected changes, want alice", got)
	}
}

// TestChangeMyUsername_Taken: another user's name is a 409 whatever
// the case, while a case-only change of your own name goes through.
func TestChangeMyUsername_Taken(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	me := seedUsernameUser(t, db, "alice", "alice@acme.com", "correct horse battery")
	seedUsernameUser(t, db, "bob", "bob@acme.com", "correct horse battery")
	h := &admin.Handlers{DB: db}

	w := postChangeUsername(t, h, me, "BOB")
	if w.Code != http.StatusConflict || errorCode(t, w) != "username_taken" {
		t.Fatalf("status=%d body=%s, want 409 username_taken", w.Code, w.Body.String())
	}

	w = postChangeUsername(t, h, me, "Alice")
	if w.Code != http.StatusOK {
		t.Fatalf("own case change: status=%d body=%s", w.Code, w.Body.String())
	}
}
