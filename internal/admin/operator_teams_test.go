package admin_test

import (
	"encoding/json"
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

// Support lists an org's event's teams and mails one its links again;
// the send lands in admin_actions, and a team of another org's event
// is not found through this org's URL.
func TestOperatorSendTeamInvitation(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var orgID, otherOrg, supportID, eventID, teamID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Other') RETURNING id`).Scan(&otherOrg))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin, is_support) VALUES ('anna', 1, TRUE, TRUE) RETURNING id`).Scan(&supportID))
	_, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID)
	must(err)
	must(db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Test')
		RETURNING id`, orgID).Scan(&eventID))
	must(db.QueryRow(`
		INSERT INTO teams(event_id, name, email, token, recap_token)
		VALUES ($1, 'Alpha', 'alpha@acme.com', gen_random_uuid()::text, gen_random_uuid()::text) RETURNING id`, eventID).Scan(&teamID))
	_, err = db.Exec(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'alice', 'tok-alice')`, teamID)
	must(err)

	mailerStub := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: mailerStub, PublicURL: "https://zv.example.com"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: supportID, Username: "anna", IsAdmin: true, Support: true, Role: auth.RoleSupport}))
	r.Use(admin.LogAdminActions(db))
	r.Get("/api/admin/organizations/{orgID}/events/{eventID}/teams", h.ListOrgEventTeams)
	r.Post("/api/admin/organizations/{orgID}/teams/{teamID}/invitations/send", h.OperatorSendTeamInvitation)
	call := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
		return w
	}

	w := call("GET", "/api/admin/organizations/"+itoa64(orgID)+"/events/"+itoa64(eventID)+"/teams")
	var list struct {
		Teams []struct {
			ID      int64  `json:"id"`
			Email   string `json:"email"`
			Players int    `json:"players"`
		} `json:"teams"`
	}
	must(json.Unmarshal(w.Body.Bytes(), &list))
	if w.Code != http.StatusOK || len(list.Teams) != 1 || list.Teams[0].ID != teamID || list.Teams[0].Email != "alpha@acme.com" || list.Teams[0].Players != 1 {
		t.Fatalf("teams = %d %s", w.Code, w.Body)
	}
	if w := call("GET", "/api/admin/organizations/"+itoa64(otherOrg)+"/events/"+itoa64(eventID)+"/teams"); w.Code != http.StatusNotFound {
		t.Fatalf("teams via other org = %d, want 404", w.Code)
	}
	if w := call("POST", "/api/admin/organizations/"+itoa64(otherOrg)+"/teams/"+itoa64(teamID)+"/invitations/send"); w.Code != http.StatusNotFound {
		t.Fatalf("send via other org = %d, want 404", w.Code)
	}
	if w := call("POST", "/api/admin/organizations/"+itoa64(orgID)+"/teams/"+itoa64(teamID)+"/invitations/send"); w.Code != http.StatusOK {
		t.Fatalf("send = %d %s", w.Code, w.Body)
	}
	if len(mailerStub.messages) != 1 || mailerStub.messages[0].To[0] != "alpha@acme.com" || !strings.Contains(mailerStub.messages[0].Text, "tok-alice") {
		t.Fatalf("messages = %+v", mailerStub.messages)
	}
	var logged int
	must(db.QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE username = 'anna' AND action = 'POST /api/admin/organizations/{orgID}/teams/{teamID}/invitations/send'`).Scan(&logged))
	if logged != 1 {
		t.Errorf("admin_actions rows = %d, want 1", logged)
	}
}
