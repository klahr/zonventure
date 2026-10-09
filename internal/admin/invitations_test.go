package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/eventfile"
	"github.com/klahr/zonventure/internal/mailer"
)

// fakeMailer records every Send call. Lets the invitation tests
// assert on what would have been mailed without dialling out.
type fakeMailer struct {
	mu       sync.Mutex
	messages []mailer.Message
}

func (f *fakeMailer) Send(_ context.Context, m mailer.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, m)
	return nil
}

// TestSendEventInvitations_HappyPath sets up an event with three
// teams — two with emails, one without — and confirms each team
// with an email gets exactly one message containing its slot links
// and the .ics attachment. The team without an email shows up in
// `skipped_no_email`.
func TestSendEventInvitations_HappyPath(t *testing.T) {
	t.Parallel()
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

	// Adventure + event with a scheduled time so .ics generation
	// has something to render.
	var advID int64
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1) RETURNING id
	`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, org_id)
		VALUES ($1, 0, 'P1', '', '[]', 59.32931, 18.06869, $2)
	`, advID, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, $2, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Test Event')
		RETURNING id
	`, orgID, userID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	// Three teams. Alpha has an email + 2 slots; Beta has an email
	// + 1 slot; Gamma has NO email + 1 slot.
	mintTeam := func(name, email string, slots []string) {
		t.Helper()
		var teamID int64
		if err := db.QueryRow(`
			INSERT INTO teams(event_id, name, email, token, recap_token) VALUES ($1, $2, $3, gen_random_uuid()::text, gen_random_uuid()::text) RETURNING id
		`, eventID, name, email).Scan(&teamID); err != nil {
			t.Fatal(err)
		}
		for _, slotName := range slots {
			tok := "tok-" + name + "-" + slotName
			if _, err := db.Exec(`
				INSERT INTO player_slots(team_id, name, token) VALUES ($1, $2, $3)
			`, teamID, slotName, tok); err != nil {
				t.Fatal(err)
			}
		}
	}
	mintTeam("Alpha", "alpha@acme.com", []string{"alice", "bob"})
	mintTeam("Beta", "beta@acme.com", []string{"carol"})
	mintTeam("Gamma", "", []string{"dave"})

	mailerStub := &fakeMailer{}
	h := &admin.Handlers{
		DB:        db,
		Lib:       adventure.New(db),
		Mailer:    mailerStub,
		PublicURL: "https://zv.example.com",
	}
	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/events/{eventID}/invitations/send", h.SendEventInvitations)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/admin/events/"+itoa64(eventID)+"/invitations/send",
		strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Sent           int `json:"sent"`
		SkippedNoEmail int `json:"skipped_no_email"`
		Failed         int `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Sent != 2 || resp.SkippedNoEmail != 1 || resp.Failed != 0 {
		t.Errorf("response = %+v, want sent=2 skipped_no_email=1 failed=0", resp)
	}

	// Two messages, one per team-with-email. Recipients must match
	// the team emails — no cross-team leakage.
	if len(mailerStub.messages) != 2 {
		t.Fatalf("messages sent = %d, want 2", len(mailerStub.messages))
	}
	got := map[string]mailer.Message{}
	for _, m := range mailerStub.messages {
		if len(m.To) != 1 {
			t.Errorf("expected one recipient per message, got %v", m.To)
			continue
		}
		got[m.To[0]] = m
	}
	if _, ok := got["alpha@acme.com"]; !ok {
		t.Errorf("alpha@acme.com did not receive a message; got %v", keys(got))
	}
	if _, ok := got["beta@acme.com"]; !ok {
		t.Errorf("beta@acme.com did not receive a message; got %v", keys(got))
	}
	// Alpha's message contains BOTH slot links, not just one.
	alpha := got["alpha@acme.com"]
	if !strings.Contains(alpha.Text, "alice") || !strings.Contains(alpha.Text, "bob") {
		t.Errorf("Alpha's email missing one of the player names; body =\n%s", alpha.Text)
	}
	if !strings.Contains(alpha.Text, "tok-Alpha-alice") || !strings.Contains(alpha.Text, "tok-Alpha-bob") {
		t.Errorf("Alpha's email missing one of the magic-link tokens; body =\n%s", alpha.Text)
	}
	// Cross-team leakage check: Alpha's email must NOT contain any
	// of Beta's or Gamma's tokens.
	if strings.Contains(alpha.Text, "tok-Beta-") || strings.Contains(alpha.Text, "tok-Gamma-") {
		t.Errorf("cross-team token leakage in Alpha's email:\n%s", alpha.Text)
	}
	// .ics attachment present and well-formed.
	if len(alpha.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(alpha.Attachments))
	}
	att := alpha.Attachments[0]
	if !strings.HasPrefix(att.ContentType, "text/calendar") {
		t.Errorf("attachment content-type = %q, want text/calendar prefix", att.ContentType)
	}
	if !strings.HasPrefix(string(att.Bytes), "BEGIN:VCALENDAR\r\n") {
		t.Errorf("attachment bytes don't look like an .ics: prefix=%q", att.Bytes[:min(40, len(att.Bytes))])
	}
}

// TestSendEventInvitations_NoMailer falls back to 503 when the
// admin Handlers wasn't wired with a Mailer at boot. Important so
// a self-host without ZV_SMTP_HOST gets a clear "configure SMTP"
// signal instead of silently swallowing the click.
func TestSendEventInvitations_NoMailer(t *testing.T) {
	t.Parallel()
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
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, $2, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Test')
		RETURNING id
	`, orgID, userID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)} // Mailer intentionally nil
	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/events/{eventID}/invitations/send", h.SendEventInvitations)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/admin/events/"+itoa64(eventID)+"/invitations/send",
		strings.NewReader("{}"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
}

// TestSendTeamInvitation_HappyPath confirms the per-team variant
// reaches exactly one team's email with all of its slot tokens
// and the .ics. Used when the arranger needs to resend to a single
// team that lost its original message.
func TestSendTeamInvitation_HappyPath(t *testing.T) {
	t.Parallel()
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
	var advID int64
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1) RETURNING id
	`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, org_id)
		VALUES ($1, 0, 'P1', '', '[]', 59, 18, $2)
	`, advID, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, $2, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Test')
		RETURNING id
	`, orgID, userID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	var (
		teamID    int64
		teamToken string
	)
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, email, token, recap_token) VALUES ($1, 'Alpha', 'alpha@acme.com', gen_random_uuid()::text, gen_random_uuid()::text)
		RETURNING id, token
	`, eventID).Scan(&teamID, &teamToken); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'alice', 'tok-alice')
	`, teamID); err != nil {
		t.Fatal(err)
	}

	// A file the organisers left for the players: the email links it
	// by the team's token.
	file, err := eventfile.Add(context.Background(), db, eventID, userID, "Regler.pdf", []byte("%PDF-1.4\n"))
	if err != nil {
		t.Fatal(err)
	}

	mailerStub := &fakeMailer{}
	h := &admin.Handlers{
		DB: db, Lib: adventure.New(db),
		Mailer: mailerStub, PublicURL: "https://zv.example.com",
	}
	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/teams/{teamID}/invitations/send", h.SendTeamInvitation)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/admin/teams/"+itoa64(teamID)+"/invitations/send",
		strings.NewReader("{}"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if len(mailerStub.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(mailerStub.messages))
	}
	got := mailerStub.messages[0]
	if got.To[0] != "alpha@acme.com" {
		t.Errorf("recipient = %q, want alpha@acme.com", got.To[0])
	}
	if !strings.Contains(got.Text, "tok-alice") {
		t.Errorf("body missing slot token; body =\n%s", got.Text)
	}
	if want := "https://zv.example.com/team/" + teamToken + "/files/" + itoa64(file.ID); !strings.Contains(got.Text, want) || !strings.Contains(got.Text, "Regler.pdf") {
		t.Errorf("body missing the file link %s; body =\n%s", want, got.Text)
	}
	if len(got.Attachments) != 1 || !strings.HasPrefix(got.Attachments[0].ContentType, "text/calendar") {
		t.Errorf("expected one text/calendar attachment, got %+v", got.Attachments)
	}
}

// TestSendTeamInvitation_NoEmail rejects with 400 when the team's
// contact email is unset — there's nothing to send to. Better as
// an explicit error than silently swallowing the click.
func TestSendTeamInvitation_NoEmail(t *testing.T) {
	t.Parallel()
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
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, $2, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Test')
		RETURNING id
	`, orgID, userID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	// Team with empty email + a slot.
	var teamID int64
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, token, recap_token) VALUES ($1, 'Alpha', gen_random_uuid()::text, gen_random_uuid()::text) RETURNING id
	`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'alice', 'tok-alice')
	`, teamID); err != nil {
		t.Fatal(err)
	}

	mailerStub := &fakeMailer{}
	h := &admin.Handlers{
		DB: db, Lib: adventure.New(db),
		Mailer: mailerStub,
	}
	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/teams/{teamID}/invitations/send", h.SendTeamInvitation)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/admin/teams/"+itoa64(teamID)+"/invitations/send",
		strings.NewReader("{}"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if len(mailerStub.messages) != 0 {
		t.Errorf("expected no messages sent on missing-email rejection, got %d", len(mailerStub.messages))
	}
}

// TestSendTeamInvitation_CrossOrgRejected — a user in org A
// can't resend to a team belonging to an event in org B, even
// when they know the team id. Same authorize-event check that
// gates other event-scoped endpoints.
func TestSendTeamInvitation_CrossOrgRejected(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgA, orgB int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('A') RETURNING id`).Scan(&orgA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('B') RETURNING id`).Scan(&orgB); err != nil {
		t.Fatal(err)
	}
	var aBoss int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified) VALUES ('a-boss', 'boss@a.com', 1)
		RETURNING id
	`).Scan(&aBoss); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		aBoss, orgA); err != nil {
		t.Fatal(err)
	}
	// B's adventure + event + team — invisible to A.
	var bAdv int64
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, org_id) VALUES ('b-adv', 'B', $1) RETURNING id
	`, orgB).Scan(&bAdv); err != nil {
		t.Fatal(err)
	}
	var bEvent int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, scheduled_at, live_token, name)
		VALUES ('b-adv', $1, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'B')
		RETURNING id
	`, orgB).Scan(&bEvent); err != nil {
		t.Fatal(err)
	}
	var bTeam int64
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, email, token, recap_token) VALUES ($1, 'BeeTeam', 'b@b.com', gen_random_uuid()::text, gen_random_uuid()::text) RETURNING id
	`, bEvent).Scan(&bTeam); err != nil {
		t.Fatal(err)
	}

	mailerStub := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: mailerStub}
	user := auth.User{ID: aBoss, Role: auth.RoleOrgAdmin, OrgID: &orgA}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/teams/{teamID}/invitations/send", h.SendTeamInvitation)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/admin/teams/"+itoa64(bTeam)+"/invitations/send",
		strings.NewReader("{}"))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if len(mailerStub.messages) != 0 {
		t.Errorf("expected no messages on cross-org rejection, got %d", len(mailerStub.messages))
	}
}

func keys(m map[string]mailer.Message) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// itoa64 stringifies an id for URL composition.
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
