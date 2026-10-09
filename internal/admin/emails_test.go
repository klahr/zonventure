package admin_test

import (
	"bytes"
	"image"
	pngenc "image/png"
	"mime/multipart"
	"net/textproto"

	"context"
	"encoding/json"
	"github.com/klahr/zonventure/internal/storage/dbblob"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/mailer"
)

type sentMail struct {
	mu   sync.Mutex
	msgs []mailer.Message
}

func (s *sentMail) Send(_ context.Context, m mailer.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
	return nil
}

func do(t *testing.T, r http.Handler, method, path string, body any, into any) int {
	t.Helper()
	var rdr *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if into != nil && w.Code < 300 {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("decode %s %s: %v (%s)", method, path, err, w.Body.String())
		}
	}
	return w.Code
}

// An organisation picks a theme; an event overrides it; previews show
// both; an arranger writes to the event's teams, each getting the
// message filled in for them in the event's look.
func TestEmails_ThemesPreviewsAndTeamMessages(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID, eventID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.QueryRow(`INSERT INTO organizations(name, accent_color) VALUES ('Acme Quests', '#0f766e') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&userID))
	_, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID)
	must(err)
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('demo', $1, 'scheduled', 'lt-1', 'Harbour Night') RETURNING id`, orgID).Scan(&eventID))
	for _, t := range [][2]string{{"Seagulls", "gulls@example.org"}, {"Crabs", "crabs@example.org"}, {"Nomail", ""}} {
		_, err := db.Exec(`INSERT INTO teams(event_id, name, email, token) VALUES ($1, $2, $3, $4)`, eventID, t[0], t[1], "tok-"+t[0])
		must(err)
	}

	mail := &sentMail{}
	h := &admin.Handlers{DB: db, Mailer: mail, PublicURL: "https://zv.example"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Get("/api/admin/email-themes", h.EmailThemes)
	r.Get("/api/admin/email-preview", h.EmailPreview)
	r.Post("/api/admin/email-preview", h.DraftPreview)
	r.Put("/api/admin/orgs/me/email-theme", h.SetOrgEmailTheme)
	r.Put("/api/admin/events/{eventID}/email-theme", h.SetEventEmailTheme)
	r.Get("/api/admin/events/{eventID}/emails", h.ListEventEmails)
	r.Post("/api/admin/events/{eventID}/emails", h.EmailTeams)
	ev := "/api/admin/events/" + strconv.FormatInt(eventID, 10)

	var list struct {
		Themes []struct{ ID string } `json:"themes"`
		Kinds  []struct{ ID string } `json:"kinds"`
	}
	if s := do(t, r, "GET", "/api/admin/email-themes", nil, &list); s != 200 || len(list.Themes) != 4 || len(list.Kinds) == 0 {
		t.Fatalf("themes = %d %+v", s, list)
	}
	if s := do(t, r, "PUT", "/api/admin/orgs/me/email-theme", map[string]string{"theme": "neon"}, nil); s != 400 {
		t.Errorf("unknown theme = %d, want 400", s)
	}
	if s := do(t, r, "PUT", "/api/admin/orgs/me/email-theme", map[string]string{"theme": "bold"}, nil); s != 204 {
		t.Fatalf("set org theme = %d", s)
	}

	var p struct {
		Subject, HTML, Text, Theme string
	}
	if s := do(t, r, "GET", "/api/admin/email-preview?kind=team_invitation", nil, &p); s != 200 || p.Theme != "bold" || !strings.Contains(p.HTML, "#0f766e") || !strings.Contains(p.HTML, "Acme Quests") {
		t.Errorf("org preview = %d theme %q", s, p.Theme)
	}
	if s := do(t, r, "GET", "/api/admin/email-preview?kind=nope", nil, nil); s != 400 {
		t.Errorf("unknown kind = %d, want 400", s)
	}
	// The event goes its own way, and its preview says its name.
	if s := do(t, r, "PUT", ev+"/email-theme", map[string]string{"theme": "adventure"}, nil); s != 204 {
		t.Fatalf("set event theme = %d", s)
	}
	if s := do(t, r, "GET", "/api/admin/email-preview?kind=team_invitation&event_id="+strconv.FormatInt(eventID, 10), nil, &p); s != 200 || p.Theme != "adventure" || !strings.Contains(p.HTML, "Harbour Night") {
		t.Errorf("event preview = %d theme %q", s, p.Theme)
	}
	if s := do(t, r, "POST", "/api/admin/email-preview", map[string]any{"subject": "Hi {{team}}", "body": "See you at **{{event}}**", "event_id": eventID}, &p); s != 200 || p.Subject != "Hi Seagulls" || !strings.Contains(p.HTML, "<strong>Harbour Night</strong>") {
		t.Errorf("draft preview = %d %q", s, p.Subject)
	}

	if s := do(t, r, "POST", ev+"/emails", map[string]string{"subject": " ", "body": "x"}, nil); s != 400 {
		t.Errorf("empty subject = %d, want 400", s)
	}
	var res struct{ Sent, Skipped int }
	if s := do(t, r, "POST", ev+"/emails", map[string]string{"subject": "Meet at 14:00, {{team}}", "body": "At the lighthouse.\nBring shoes."}, &res); s != 200 || res.Sent != 2 || res.Skipped != 1 {
		t.Fatalf("send = %d %+v", s, res)
	}
	if len(mail.msgs) != 2 {
		t.Fatalf("mailed %d", len(mail.msgs))
	}
	m := mail.msgs[0]
	if m.To[0] != "gulls@example.org" || m.Subject != "Meet at 14:00, Seagulls" || !strings.Contains(m.HTML, "Georgia") || !strings.Contains(m.Text, "Bring shoes.") {
		t.Errorf("message = %v %q", m.To, m.Subject)
	}
	var hist struct {
		Emails []struct {
			Subject       string
			Sent, Skipped int
		} `json:"emails"`
		ThemeUsed string `json:"theme_used"`
		WithEmail int    `json:"with_email"`
	}
	if s := do(t, r, "GET", ev+"/emails", nil, &hist); s != 200 || len(hist.Emails) != 1 || hist.Emails[0].Sent != 2 || hist.ThemeUsed != "adventure" || hist.WithEmail != 2 {
		t.Errorf("history = %d %+v", s, hist)
	}
	// Choosing teams sends to those alone.
	var firstID int64
	must(db.QueryRow(`SELECT id FROM teams WHERE event_id = $1 AND name = 'Crabs'`, eventID).Scan(&firstID))
	if s := do(t, r, "POST", ev+"/emails", map[string]any{"subject": "Just you", "body": "Hi", "team_ids": []int64{firstID}}, &res); s != 200 || res.Sent != 1 || mail.msgs[2].To[0] != "crabs@example.org" {
		t.Errorf("chosen = %d %+v", s, res)
	}
}

// Designers are told when a step is given to them, when a suggestion is
// made to their step and when one of theirs is decided, and when their
// step is put up for review — never about what they did themselves, nor
// what they turned off.
func TestEmails_DesignerNotifications(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, bossID, annaID, advID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&bossID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('anna', 'anna@acme.com', 1) RETURNING id`).Scan(&annaID))
	for _, id := range []int64{bossID, annaID} {
		_, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, id, orgID)
		must(err)
	}
	must(db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('harbour', 'Harbour', '', '', $1) RETURNING id`, orgID).Scan(&advID))
	_, err := db.Exec(`INSERT INTO adventure_graph_drafts (adventure_id, graph) VALUES ($1, '{"nodes":[{"slug":"riddle","type":"story","title":"The riddle"}],"edges":[]}')`, advID)
	must(err)

	mail := &sentMail{}
	h := &admin.Handlers{DB: db, Mailer: mail, PublicURL: "https://zv.example"}
	router := func(u auth.User) http.Handler {
		r := chi.NewRouter()
		r.Use(injectUser(u))
		r.Put("/api/admin/adventures/{slug}/steps/{node}/status", h.SetStepStatus)
		r.Post("/api/admin/adventures/{slug}/threads", h.CreateThread)
		r.Patch("/api/admin/adventures/{slug}/threads/{threadID}", h.UpdateThread)
		r.Put("/api/admin/me/email-prefs", h.SetEmailPrefs)
		return r
	}
	boss := router(auth.User{ID: bossID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})
	anna := router(auth.User{ID: annaID, Username: "anna", Role: auth.RoleOrgAdmin, OrgID: &orgID})
	status := "/api/admin/adventures/harbour/steps/riddle/status"
	last := func() mailer.Message { return mail.msgs[len(mail.msgs)-1] }

	// Boss gives the riddle to Anna: she is told, in a themed email naming the step.
	if s := do(t, boss, "PUT", status, map[string]any{"owner_id": annaID}, nil); s != 204 {
		t.Fatalf("assign = %d", s)
	}
	if len(mail.msgs) != 1 || last().To[0] != "anna@acme.com" || !strings.Contains(last().Subject, "The riddle") || !strings.Contains(last().HTML, "/adventures/harbour/design") {
		t.Fatalf("assigned mail = %+v", mail.msgs)
	}
	// Anna putting her own step up for review tells nobody: it is hers.
	if s := do(t, anna, "PUT", status, map[string]any{"status": "review"}, nil); s != 204 || len(mail.msgs) != 1 {
		t.Fatalf("own review = %d, %d mails", s, len(mail.msgs))
	}
	// Boss asking for review of Anna's step tells her.
	do(t, boss, "PUT", status, map[string]any{"status": "draft"}, nil)
	if s := do(t, boss, "PUT", status, map[string]any{"status": "review"}, nil); s != 204 || len(mail.msgs) != 2 || !strings.Contains(last().Subject, "ready for review") {
		t.Fatalf("review = %d, %+v", s, last().Subject)
	}
	// Boss suggests a change to Anna's step: she is told; deciding tells him.
	var made struct {
		ThreadID int64 `json:"thread_id"`
	}
	sg := map[string]any{"base": map[string]any{"slug": "riddle", "type": "story", "title": "The riddle"}, "proposed": map[string]any{"slug": "riddle", "type": "story", "title": "A riddle"}}
	if s := do(t, boss, "POST", "/api/admin/adventures/harbour/threads", map[string]any{"node": "riddle", "body": "Shorter?", "suggestion": sg}, &made); s != 201 || len(mail.msgs) != 3 || last().To[0] != "anna@acme.com" || !strings.Contains(last().Text, "Shorter?") {
		t.Fatalf("suggestion = %d, %d mails", s, len(mail.msgs))
	}
	if s := do(t, anna, "PATCH", "/api/admin/adventures/harbour/threads/"+strconv.FormatInt(made.ThreadID, 10), map[string]string{"suggestion_state": "accepted"}, nil); s != 204 || len(mail.msgs) != 4 || last().To[0] != "boss@acme.com" || !strings.Contains(last().Subject, "accepted") {
		t.Fatalf("decided = %d, %d mails", s, len(mail.msgs))
	}
	// Anna turns review emails off: asking for review now tells nobody.
	if s := do(t, anna, "PUT", "/api/admin/me/email-prefs", map[string]bool{"review_requested": false}, nil); s != 200 {
		t.Fatalf("prefs = %d", s)
	}
	if s := do(t, anna, "PUT", "/api/admin/me/email-prefs", map[string]bool{"newsletter": false}, nil); s != 400 {
		t.Errorf("unknown pref = %d, want 400", s)
	}
	do(t, boss, "PUT", status, map[string]any{"status": "draft"}, nil)
	do(t, boss, "PUT", status, map[string]any{"status": "review"}, nil)
	if len(mail.msgs) != 4 {
		t.Errorf("review mail despite the preference: %d mails", len(mail.msgs))
	}
}

// Emails speak the players' language: an event's follow its adventure's,
// a designer's notification the language they use the console in.
func TestEmails_InTheirLanguage(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var orgID, bossID, annaID, eventID, advID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Äventyr AB') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@a.se', 1) RETURNING id`).Scan(&bossID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified, language) VALUES ('anna', 'anna@a.se', 1, 'sv') RETURNING id`).Scan(&annaID))
	for _, id := range []int64{bossID, annaID} {
		_, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, id, orgID)
		must(err)
	}
	must(db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id, language) VALUES ('hamn', 'Hamnen', '', '', $1, 'sv') RETURNING id`, orgID).Scan(&advID))
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('hamn', $1, 'scheduled', 'lt-sv', 'Hamnnatten') RETURNING id`, orgID).Scan(&eventID))
	_, err := db.Exec(`INSERT INTO teams(event_id, name, email, token) VALUES ($1, 'Måsarna', 'masar@a.se', 'tk-m')`, eventID)
	must(err)

	mail := &sentMail{}
	h := &admin.Handlers{DB: db, Mailer: mail, PublicURL: "https://zv.example"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: bossID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Post("/api/admin/events/{eventID}/emails", h.EmailTeams)
	r.Get("/api/admin/email-preview", h.EmailPreview)
	r.Put("/api/admin/adventures/{slug}/steps/{node}/status", h.SetStepStatus)
	r.Put("/api/admin/orgs/me/email-theme", h.SetOrgEmailTheme)

	if s := do(t, r, "POST", "/api/admin/events/"+strconv.FormatInt(eventID, 10)+"/emails", map[string]string{"subject": "Hej {{team}}", "body": "Vi ses vid fyren."}, nil); s != 200 {
		t.Fatalf("send = %d", s)
	}
	if m := mail.msgs[0]; !strings.Contains(m.Text, "Du får det här eftersom ditt lag spelar Hamnnatten med Äventyr AB.") || !strings.Contains(m.HTML, `lang="sv"`) {
		t.Errorf("team email not in Swedish: %q", m.Text)
	}
	// Anna uses the console in Swedish: her notification is too.
	if s := do(t, r, "PUT", "/api/admin/adventures/hamn/steps/riddle/status", map[string]any{"owner_id": annaID}, nil); s != 204 {
		t.Fatalf("assign = %d", s)
	}
	if m := mail.msgs[1]; m.To[0] != "anna@a.se" || !strings.Contains(m.Subject, "boss gav dig") {
		t.Errorf("notification = %q", m.Subject)
	}
	// A preview can be asked for in either language.
	var p struct{ Subject string }
	if s := do(t, r, "GET", "/api/admin/email-preview?kind=team_link&lang=sv", nil, &p); s != 200 || !strings.Contains(p.Subject, "Din biljett") {
		t.Errorf("Swedish preview = %d %q", s, p.Subject)
	}
	if s := do(t, r, "PUT", "/api/admin/orgs/me/email-theme", map[string]string{"language": "de"}, nil); s != 400 {
		t.Errorf("unknown language = %d, want 400", s)
	}
	if s := do(t, r, "PUT", "/api/admin/orgs/me/email-theme", map[string]string{"language": "sv"}, nil); s != 204 {
		t.Errorf("set language = %d", s)
	}
}

// An arranger can send themselves exactly what a preview shows, marked
// as a test: a sample of a kind, or their draft.
func TestEmails_SendMeATest(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('tester', 'tester@acme.com', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	mail := &sentMail{}
	h := &admin.Handlers{DB: db, Mailer: mail}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Username: "tester", Email: "tester@acme.com", Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Post("/api/admin/email-test", h.SendMeTest)

	if s := do(t, r, "POST", "/api/admin/email-test", map[string]string{"sample": "team_invitation", "lang": "sv"}, nil); s != 200 {
		t.Fatalf("sample test = %d", s)
	}
	if m := mail.msgs[0]; m.To[0] != "tester@acme.com" || m.Subject != "[Test] Dina laglänkar till Zonventure" || m.HTML == "" {
		t.Errorf("sample = %v %q", m.To, m.Subject)
	}
	if s := do(t, r, "POST", "/api/admin/email-test", map[string]string{"subject": "Hi {{team}}", "body": "See you"}, nil); s != 200 || mail.msgs[1].Subject != "[Test] Hi The Seagulls" {
		t.Errorf("draft test = %d %q", s, mail.msgs[len(mail.msgs)-1].Subject)
	}
	if s := do(t, r, "POST", "/api/admin/email-test", map[string]string{}, nil); s != 400 {
		t.Errorf("empty test = %d, want 400", s)
	}
}

// A person sets their own picture; it is served at a versioned address,
// named beside them in the work, and can be cleared again.
func TestUserAvatar(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID, advID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('pic', 'pic@acme.com', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('pics', 'P', '', '', $1) RETURNING id`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	h := &admin.Handlers{DB: db, Storage: dbblob.New(db)}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Username: "pic", Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Post("/api/admin/me/avatar", h.SetMyAvatar)
	r.Delete("/api/admin/me/avatar", h.DeleteMyAvatar)
	r.Get("/api/admin/users/{userID}/avatar", h.ServeUserAvatar)
	r.Get("/api/admin/adventures/{slug}/collab", h.GetCollab)

	// A 300×4 PNG: it is kept at most 256 wide. Wider than the cap so
	// the downscale runs, but a thin strip keeps the resize cheap.
	img := image.NewRGBA(image.Rect(0, 0, 300, 4))
	var png bytes.Buffer
	if err := pngenc.Encode(&png, img); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="me.png"`}, "Content-Type": {"image/png"}})
	part.Write(png.Bytes())
	mw.Close()
	req := httptest.NewRequest("POST", "/api/admin/me/avatar", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var set struct {
		AvatarURL string `json:"avatar_url"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &set) != nil || !strings.Contains(set.AvatarURL, "/users/"+strconv.FormatInt(userID, 10)+"/avatar?v=") {
		t.Fatalf("set = %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", set.AvatarURL, nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
		t.Fatalf("serve = %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if got, _, err := image.Decode(bytes.NewReader(w.Body.Bytes())); err != nil || got.Bounds().Dx() > 256 {
		t.Errorf("stored picture: %v, err %v", got.Bounds(), err)
	}
	var c struct {
		People []struct{ Avatar string } `json:"people"`
	}
	if do(t, r, "GET", "/api/admin/adventures/pics/collab", nil, &c); len(c.People) != 1 || c.People[0].Avatar != set.AvatarURL {
		t.Errorf("collab people = %+v", c.People)
	}
	if s := do(t, r, "DELETE", "/api/admin/me/avatar", nil, nil); s != 204 {
		t.Fatalf("delete = %d", s)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", set.AvatarURL, nil))
	if w.Code != 404 {
		t.Errorf("after delete = %d, want 404", w.Code)
	}
}
