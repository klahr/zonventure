package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// TestCancelEvent_EmailsTeams: cancelling with email_teams tells each
// team with a contact email that the event is cancelled; without it no
// one is emailed.
func TestCancelEvent_EmailsTeams(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified, password_hash) VALUES ('boss', 'boss@acme.com', 1, $1) RETURNING id`, hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	newEvent := func() int64 {
		var id int64
		if err := db.QueryRow(`
			INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, live_token, name)
			VALUES ('demo', $1, $2, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Harbour day') RETURNING id
		`, orgID, userID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		for _, tm := range [][2]string{{"Alpha", "alpha@acme.com"}, {"Gamma", ""}} {
			if _, err := db.Exec(`INSERT INTO teams(event_id, name, email, token, recap_token) VALUES ($1, $2, $3, gen_random_uuid()::text, gen_random_uuid()::text)`, id, tm[0], tm[1]); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: mail, PublicURL: "https://zv.example.com"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Post("/api/admin/events/{eventID}/cancel", h.CancelEvent)
	cancel := func(id int64, body string) {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/admin/events/"+itoa64(id)+"/cancel", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Confirm-Password", "s3cret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("cancel = %d %s", w.Code, w.Body.String())
		}
	}

	cancel(newEvent(), "")
	if len(mail.messages) != 0 {
		t.Fatalf("a cancel without email_teams sent %d emails", len(mail.messages))
	}
	cancel(newEvent(), `{"email_teams":true}`)
	if len(mail.messages) != 1 || mail.messages[0].To[0] != "alpha@acme.com" || !strings.Contains(mail.messages[0].Subject, "cancelled") {
		t.Fatalf("emails = %+v, want one cancel notice to alpha@acme.com", mail.messages)
	}
	// It says whom to ask, replies go there, and the calendar entry the
	// invitation made is cancelled.
	got := mail.messages[0]
	if got.ReplyTo != "boss@acme.com" || !strings.Contains(got.Text, "Write to the organisers at boss@acme.com") {
		t.Errorf("reply-to %q, body\n%s", got.ReplyTo, got.Text)
	}
	if len(got.Attachments) != 1 || !strings.Contains(got.Attachments[0].ContentType, "method=CANCEL") || !strings.Contains(string(got.Attachments[0].Bytes), "METHOD:CANCEL\r\n") {
		t.Errorf("attachments = %+v, want a calendar cancellation", got.Attachments)
	}

	// The contact address the organisation sets wins over its admin's.
	if _, err := db.Exec(`UPDATE organizations SET contact_email = 'hello@acme.com' WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	cancel(newEvent(), `{"email_teams":true}`)
	if got := mail.messages[1]; got.ReplyTo != "hello@acme.com" || !strings.Contains(got.Text, "hello@acme.com") {
		t.Errorf("with a contact set: reply-to %q, body\n%s", got.ReplyTo, got.Text)
	}
}
