package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// "Send reset link" mails another admin the forgot-password link and
// leaves their password as it was; an admin without an email gets 409.
func TestSendAdminResetLink(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	hash, err := auth.HashPassword("old-secret-pw") // gitleaks:allow (test fixture)
	must(err)
	var rootID, otherID, bareID int64
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin) VALUES ('root', 1, TRUE) RETURNING id`).Scan(&rootID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified, is_admin, password_hash) VALUES ('ops', 'ops@example.com', 1, TRUE, $1) RETURNING id`, hash).Scan(&otherID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin) VALUES ('bare', 1, TRUE) RETURNING id`).Scan(&bareID))

	m := &fakeMailer{}
	h := &admin.Handlers{DB: db, Mailer: m, PublicURL: "https://zv.example"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: rootID, Username: "root", Role: auth.RoleAdmin}))
	r.Post("/api/admin/admins/{userID}/reset-link", h.SendAdminResetLink)
	call := func(id int64) int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/admins/"+strconv.FormatInt(id, 10)+"/reset-link", nil))
		return rec.Code
	}

	if code := call(otherID); code != http.StatusNoContent {
		t.Fatalf("send: %d", code)
	}
	if len(m.messages) != 1 || m.messages[0].To[0] != "ops@example.com" || !strings.Contains(m.messages[0].Text, "https://zv.example/arrange#reset?token=") {
		t.Fatalf("messages = %+v", m.messages)
	}
	var after string
	must(db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, otherID).Scan(&after))
	if after != hash {
		t.Error("the password changed")
	}
	if code := call(bareID); code != http.StatusConflict {
		t.Errorf("no email: %d, want 409", code)
	}
	if code := call(999999); code != http.StatusNotFound {
		t.Errorf("unknown: %d, want 404", code)
	}
}
