package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

func asUser(method, path string, me auth.User) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(auth.WithUser(req.Context(), me))
}

// TestEmailChangePending: an email change waiting for confirmation shows
// in the status with its address, and cancelling it kills the link.
func TestEmailChangePending(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	me := seedUsernameUser(t, db, "pend@acme.com", "pend@acme.com", placeholderHash)
	h := &admin.Handlers{DB: db}
	tok, err := auth.IssueEmailChangeToken(ctx, db, me.ID, "new@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	status := func() (st struct {
		PendingEmail     string `json:"pending_email"`
		PendingExpiresAt string `json:"pending_expires_at"`
	}) {
		w := httptest.NewRecorder()
		h.MyEmailChangeStatus(w, asUser(http.MethodGet, "/api/admin/me/email/revert-status", me))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d %s", w.Code, w.Body.String())
		}
		_ = json.Unmarshal(w.Body.Bytes(), &st)
		return st
	}
	if st := status(); st.PendingEmail != "new@acme.com" || st.PendingExpiresAt == "" {
		t.Fatalf("pending = %+v, want new@acme.com with an expiry", st)
	}
	w := httptest.NewRecorder()
	h.CancelMyEmailChange(w, asUser(http.MethodDelete, "/api/admin/me/email/pending", me))
	if w.Code != http.StatusNoContent {
		t.Fatalf("cancel = %d %s", w.Code, w.Body.String())
	}
	if st := status(); st.PendingEmail != "" {
		t.Fatalf("pending after cancel = %+v, want none", st)
	}
	if _, err := auth.ConsumeEmailChangeToken(ctx, db, tok); err == nil {
		t.Fatal("cancelled link still changed the email")
	}
}

// TestDeletionMails: scheduling deletion mails the date and the way
// back; the sweep sends the reminder once, within a day of it.
func TestDeletionMails(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	hash, err := testPasswordHash()
	if err != nil {
		t.Fatal(err)
	}
	me := seedUsernameUser(t, db, "bye@acme.com", "bye@acme.com", hash)
	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Mailer: mail, PublicURL: "https://zv.example.com"}

	req := asUser(http.MethodDelete, "/api/admin/me", me)
	req.Header.Set("X-Confirm-Password", testPassword)
	w := httptest.NewRecorder()
	h.DeleteMyAccount(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	if len(mail.messages) != 1 || mail.messages[0].To[0] != "bye@acme.com" || !strings.Contains(mail.messages[0].Text, "https://zv.example.com/arrange#/settings/account") {
		t.Fatalf("scheduled mail = %+v", mail.messages)
	}

	// Not yet within a day: no reminder.
	h.SweepDeletionReminders(ctx)
	if len(mail.messages) != 1 {
		t.Fatalf("reminder a week early: %d mails", len(mail.messages))
	}
	if _, err := db.Exec(`UPDATE users SET scheduled_deletion_at = now() + interval '12 hours' WHERE id = $1`, me.ID); err != nil {
		t.Fatal(err)
	}
	h.SweepDeletionReminders(ctx)
	h.SweepDeletionReminders(ctx)
	if len(mail.messages) != 2 || !strings.Contains(mail.messages[1].Subject, "tomorrow") {
		t.Fatalf("reminders = %d, want one more saying tomorrow", len(mail.messages))
	}
}
