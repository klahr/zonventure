package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/eventfile"
	"github.com/klahr/zonventure/internal/eventmsg"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/mailer"
)

// An event played in Swedish and English: each team gets its
// invitation, calendar entry, files and the organiser's words in the
// language it plays in; a team in a language the adventure is not
// played in gets the event as written.
func TestEventLanguages(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()

	var orgID, userID, advID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, org_id, language) VALUES ('skatt', 'Skatten', $1, 'sv') RETURNING id`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges:        []graph.Edge{{From: "start", FromPort: "out", To: "end"}},
		Translations: map[string]map[string]string{"en": {graph.MetaKey("title"): "The Treasure"}},
	}
	v, rep, err := graphstore.Publish(ctx, db, advID, userID, g, "v1")
	if err != nil {
		t.Fatalf("publish: %v %+v", err, rep)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, live_token, name, meeting_place, graph_id, translations)
		VALUES ('skatt', $1, $2, 'scheduled', '2026-05-01 10:00:00', 'lt-' || md5(random()::text), 'Skattjakten', 'Vid fontänen', $3,
		        '{"en": {"name": "The Treasure Hunt", "meeting_place": "By the fountain"}}')
		RETURNING id`, orgID, userID, v.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	teams := map[string]int64{}
	for name, lang := range map[string]string{"alpha": "", "bravo": "en", "charlie": "de"} {
		var id int64
		if err := db.QueryRow(`
			INSERT INTO teams(event_id, name, email, token, recap_token, lang) VALUES ($1, $2, $2 || '@acme.com', $2 || '-token', gen_random_uuid()::text, $3)
			RETURNING id`, eventID, name, lang).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'p', $2 || '-slot')`, id, name); err != nil {
			t.Fatal(err)
		}
		teams[name] = id
	}
	for _, f := range []struct{ name, lang string }{{"Regler.pdf", "sv"}, {"Rules.pdf", "en"}, {"Karta.pdf", ""}} {
		if _, err := eventfile.Add(ctx, db, eventID, userID, f.name, f.lang, []byte("%PDF-1.4\n")); err != nil {
			t.Fatal(err)
		}
	}

	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: mail, PublicURL: "https://zv.example.com"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Post("/api/admin/events/{eventID}/invitations/send", h.SendEventInvitations)
	r.Post("/api/admin/events/{eventID}/emails", h.EmailTeams)
	r.Patch("/api/admin/teams/{teamID}", h.UpdateTeam)
	post := func(path, body string) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	to := func(team string) mailer.Message {
		t.Helper()
		for _, m := range mail.messages {
			if m.To[0] == team+"@acme.com" {
				return m
			}
		}
		t.Fatalf("no mail to %s", team)
		return mailer.Message{}
	}

	post("/api/admin/events/"+itoa64(eventID)+"/invitations/send", "{}")
	for team, want := range map[string]struct{ name, place, file, notFile string }{
		"alpha":   {"Skattjakten", "Vid fontänen", "Regler.pdf", "Rules.pdf"},
		"bravo":   {"The Treasure Hunt", "By the fountain", "Rules.pdf", "Regler.pdf"},
		"charlie": {"Skattjakten", "Vid fontänen", "Regler.pdf", "Rules.pdf"},
	} {
		m := to(team)
		if !strings.Contains(m.Subject+m.Text, want.name) || !strings.Contains(m.Text, want.place) {
			t.Errorf("%s's invitation = %q\n%s", team, m.Subject, m.Text)
		}
		if !strings.Contains(m.Text, want.file) || !strings.Contains(m.Text, "Karta.pdf") || strings.Contains(m.Text, want.notFile) {
			t.Errorf("%s's invitation links the wrong files:\n%s", team, m.Text)
		}
		if len(m.Attachments) != 1 || !strings.Contains(string(m.Attachments[0].Bytes), "SUMMARY:"+want.name) || !strings.Contains(string(m.Attachments[0].Bytes), "LOCATION:"+want.place) {
			t.Errorf("%s's calendar entry: %+v", team, m.Attachments)
		}
	}
	if m := to("bravo"); !strings.Contains(m.Text, "It starts") {
		t.Errorf("bravo's invitation is not in English:\n%s", m.Text)
	}

	// The organiser's email: in English for the team playing in it, the
	// email around it too; as written for the others.
	mail.messages = nil
	post("/api/admin/events/"+itoa64(eventID)+"/emails",
		`{"subject": "Lunch klockan 12", "body": "Hej {{team}}!", "translations": {"en": {"subject": "Lunch at 12", "body": "Hi {{team}}!"}, "de": {"subject": "Nur halb"}}}`)
	if m := to("bravo"); m.Subject != "Lunch at 12" || !strings.Contains(m.Text, "Hi bravo!") {
		t.Errorf("bravo's email = %q\n%s", m.Subject, m.Text)
	}
	for _, team := range []string{"alpha", "charlie"} {
		if m := to(team); m.Subject != "Lunch klockan 12" || !strings.Contains(m.Text, "Hej "+team+"!") {
			t.Errorf("%s's email = %q\n%s", team, m.Subject, m.Text)
		}
	}

	// A message, read in the team's language.
	if _, err := eventmsg.Send(ctx, db, eventID, userID, "Lunch vid torget", map[string]string{"en": "Lunch at the square", "de": " "}, nil); err != nil {
		t.Fatal(err)
	}
	msgs, err := eventmsg.ForTeam(ctx, db, eventID, teams["bravo"])
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(msgs[0].Translations) != 1 {
		t.Fatalf("messages = %+v, want one with only its English", msgs)
	}
	if en, sv := eventmsg.In(msgs, "en"), eventmsg.In(msgs, "sv"); en[0].Text != "Lunch at the square" || sv[0].Text != "Lunch vid torget" || en[0].Translations != nil {
		t.Errorf("in English %+v, in Swedish %+v", en, sv)
	}

	// Files for a team by its language.
	names := func(lang string) []string {
		files, err := eventfile.ListForPlayersIn(ctx, db, eventID, lang)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, f := range files {
			out = append(out, f.Name)
		}
		return out
	}
	for lang, want := range map[string][]string{"": {"Regler.pdf", "Karta.pdf"}, "sv": {"Regler.pdf", "Karta.pdf"}, "en": {"Rules.pdf", "Karta.pdf"}, "de": {"Regler.pdf", "Karta.pdf"}} {
		if got := names(lang); !slices.Equal(got, want) {
			t.Errorf("files in %q = %v, want %v", lang, got, want)
		}
	}

	// A team's language is set like its name.
	patch := func(body string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PATCH", "/api/admin/teams/"+itoa64(teams["alpha"]), strings.NewReader(body)))
		return w.Code
	}
	if code := patch(`{"lang": "e n"}`); code != http.StatusBadRequest {
		t.Errorf("a bad language = %d, want 400", code)
	}
	if code := patch(`{"lang": "en"}`); code != http.StatusNoContent {
		t.Errorf("setting a language = %d, want 204", code)
	}
	var lang string
	if err := db.QueryRow(`SELECT lang FROM teams WHERE id = $1`, teams["alpha"]).Scan(&lang); err != nil || lang != "en" {
		t.Errorf("alpha's language = %q (%v), want en", lang, err)
	}
}
