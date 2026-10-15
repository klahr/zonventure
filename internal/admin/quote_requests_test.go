package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/settings"
)

type quoteFixture struct {
	h      *admin.Handlers
	mail   *fakeMailer
	public http.Handler
	orgID  int64
	// console is the org admin's; member's an event host's in the same org.
	console, member http.Handler
	// ip is this test's address, so the per-IP limit is its own.
	ip string
}

func newQuoteFixture(t *testing.T, ip string) (*quoteFixture, func()) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	f := &quoteFixture{mail: &fakeMailer{}, ip: ip}
	if err := db.QueryRow(`INSERT INTO organizations(name, slug, contact_email) VALUES ('Acme Events', 'acme', 'hello@acme.com') RETURNING id`).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	var adminID, memberID int64
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('host', 'host@acme.com', 1) RETURNING id`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(org_id, user_id, role) VALUES ($1, $2, 'org_admin'), ($1, $3, 'org_member')`, f.orgID, adminID, memberID); err != nil {
		t.Fatal(err)
	}
	f.h = &admin.Handlers{DB: db, Mailer: f.mail, PublicURL: "https://zv.example.com",
		Pricing: func() settings.Pricing { return settings.Pricing{} }}

	public := chi.NewRouter()
	public.Get("/api/o/{slug}/request", f.h.GetQuoteForm)
	public.Post("/api/o/{slug}/request", f.h.SendQuoteRequest)
	f.public = public

	routes := func(u auth.User) http.Handler {
		r := chi.NewRouter()
		r.Use(injectUser(u))
		r.Get("/api/admin/requests", f.h.ListQuoteRequests)
		r.Patch("/api/admin/requests/{id}", f.h.UpdateQuoteRequest)
		r.Delete("/api/admin/requests/{id}", f.h.DeleteQuoteRequest)
		r.Get("/api/admin/orgs/me/requests", f.h.GetMyQuoteSettings)
		r.Put("/api/admin/orgs/me/requests", f.h.SetMyQuoteSettings)
		return r
	}
	f.console = routes(auth.User{ID: adminID, Role: auth.RoleOrgAdmin, OrgID: &f.orgID, Email: "boss@acme.com", EmailVerified: true})
	f.member = routes(auth.User{ID: memberID, Role: auth.RoleOrgMember, OrgID: &f.orgID, Email: "host@acme.com", EmailVerified: true})
	return f, cleanup
}

// send posts a request from the fixture's address.
func (f *quoteFixture) send(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/o/acme/request", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "sv-SE,sv;q=0.9")
	req.RemoteAddr = f.ip + ":4711"
	w := httptest.NewRecorder()
	f.public.ServeHTTP(w, req)
	return w
}

// loadedAgo is the stamp of a form that loaded d ago.
func (f *quoteFixture) loadedAgo(t *testing.T, d time.Duration) string {
	t.Helper()
	key, err := settings.FormKey(context.Background(), f.h.DB)
	if err != nil {
		t.Fatal(err)
	}
	return admin.ExportedQuoteStamp(key, time.Now().Add(-d))
}

func (f *quoteFixture) request(t *testing.T) map[string]any {
	return map[string]any{
		"answers": map[string]any{
			"occasion": "kickoff", "date": "2027-05-12", "time_of_day": "afternoon", "duration": "3h", "people": 100,
			"city": "Stockholm", "venue": "Hotel Rival", "scope": "adapted", "game_master": true,
			"extras": []string{"logo", "prize_ceremony"}, "languages": []string{"sv", "en"}, "budget": "15k_40k",
			"message": "We'd like *our* CEO in it.",
		},
		"name": "Kim Berg", "company": "Bolaget AB", "email": "Kim@Bolaget.se", "phone": "+46 70 123 45 67",
		"started": f.loadedAgo(t, time.Minute),
	}
}

func (f *quoteFixture) open(t *testing.T) {
	t.Helper()
	if w := call(t, f.console, http.MethodPut, "/api/admin/orgs/me/requests", map[string]any{"open": true, "intro": "Kickoffs in Stockholm."}); w.Code != http.StatusOK {
		t.Fatalf("open: %d %s", w.Code, w.Body)
	}
}

func (f *quoteFixture) stored(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.h.DB.QueryRow(`SELECT COUNT(*) FROM org_requests WHERE org_id = $1`, f.orgID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestQuoteRequestFormIsOffOrOn(t *testing.T) {
	t.Parallel()
	f, cleanup := newQuoteFixture(t, "198.51.100.1")
	defer cleanup()

	if w := call(t, f.public, http.MethodGet, "/api/o/acme/request", nil); w.Code != http.StatusNotFound {
		t.Fatalf("closed form: %d", w.Code)
	}
	if w := f.send(t, f.request(t)); w.Code != http.StatusNotFound || f.stored(t) != 0 {
		t.Fatalf("sent to a closed form: %d, %d stored", w.Code, f.stored(t))
	}
	f.open(t)
	w := call(t, f.public, http.MethodGet, "/api/o/acme/request", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("open form: %d %s", w.Code, w.Body)
	}
	var form struct{ OrgName, Intro, Currency, Started string }
	_ = json.Unmarshal(w.Body.Bytes(), &struct {
		OrgName  *string `json:"org_name"`
		Intro    *string `json:"intro"`
		Currency *string `json:"currency"`
		Started  *string `json:"started"`
	}{&form.OrgName, &form.Intro, &form.Currency, &form.Started})
	if form.OrgName != "Acme Events" || form.Intro != "Kickoffs in Stockholm." || form.Currency == "" || form.Started == "" {
		t.Errorf("form = %+v", form)
	}
	// Its own stamp is too fresh to send at once.
	body := f.request(t)
	body["started"] = form.Started
	if w := f.send(t, body); w.Code != http.StatusCreated || f.stored(t) != 0 {
		t.Errorf("sent at once: %d, %d stored", w.Code, f.stored(t))
	}

	var settingsOut struct {
		Open    bool   `json:"open"`
		URL     string `json:"url"`
		Contact string `json:"contact"`
	}
	w = call(t, f.console, http.MethodGet, "/api/admin/orgs/me/requests", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &settingsOut)
	if !settingsOut.Open || settingsOut.URL != "https://zv.example.com/o/acme/request" || settingsOut.Contact != "hello@acme.com" {
		t.Errorf("settings = %+v", settingsOut)
	}
	if w := call(t, f.console, http.MethodPut, "/api/admin/orgs/me/requests", map[string]any{"open": true, "intro": strings.Repeat("x", 601)}); w.Code != http.StatusBadRequest {
		t.Errorf("long intro: %d", w.Code)
	}
	if w := call(t, f.member, http.MethodPut, "/api/admin/orgs/me/requests", map[string]any{"open": false}); w.Code != http.StatusForbidden {
		t.Errorf("a member changed the settings: %d", w.Code)
	}
}

func TestQuoteRequestSpamTrap(t *testing.T) {
	t.Parallel()
	f, cleanup := newQuoteFixture(t, "198.51.100.2")
	defer cleanup()
	f.open(t)

	honeypot := f.request(t)
	honeypot["website"] = "https://spam.example"
	tooFast := f.request(t)
	tooFast["started"] = f.loadedAgo(t, time.Second)
	forged := f.request(t)
	forged["started"] = strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10) + ".0123456789abcdef0123456789abcdef"
	missing := f.request(t)
	delete(missing, "started")
	for name, body := range map[string]map[string]any{"honeypot": honeypot, "too fast": tooFast, "forged stamp": forged, "no stamp": missing} {
		if w := f.send(t, body); w.Code != http.StatusCreated {
			t.Errorf("%s: %d, want the same answer as a good request", name, w.Code)
		}
	}
	if n := f.stored(t); n != 0 {
		t.Errorf("%d trapped requests stored", n)
	}
	if n := len(f.mail.messages); n != 0 {
		t.Errorf("%d mails sent for trapped requests", n)
	}
}

func TestQuoteRequestStoredAndMailed(t *testing.T) {
	t.Parallel()
	f, cleanup := newQuoteFixture(t, "198.51.100.3")
	defer cleanup()
	f.open(t)

	if w := f.send(t, f.request(t)); w.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	var email, lang, status string
	if err := f.h.DB.QueryRow(`SELECT email, lang, status FROM org_requests WHERE org_id = $1`, f.orgID).Scan(&email, &lang, &status); err != nil {
		t.Fatal(err)
	}
	if email != "kim@bolaget.se" || lang != "sv" || status != "new" {
		t.Errorf("stored email %q lang %q status %q", email, lang, status)
	}
	if len(f.mail.messages) != 2 {
		t.Fatalf("%d mails, want the organisation's and the requester's", len(f.mail.messages))
	}
	toOrg, toKim := f.mail.messages[0], f.mail.messages[1]
	if toOrg.To[0] != "hello@acme.com" || toOrg.ReplyTo != "kim@bolaget.se" {
		t.Errorf("organisation's mail to %v, reply to %q", toOrg.To, toOrg.ReplyTo)
	}
	for _, want := range []string{"Kickoff", "100", "Hotel Rival", "Bolaget AB", "tel:+46701234567", "*our*", "/arrange#/requests"} {
		if !strings.Contains(toOrg.Text+toOrg.HTML, want) {
			t.Errorf("organisation's mail lacks %q:\n%s", want, toOrg.Text)
		}
	}
	if strings.Contains(toOrg.HTML, "<em>our</em>") {
		t.Error("the requester's message was read as Markdown")
	}
	if toKim.To[0] != "kim@bolaget.se" || toKim.ReplyTo != "hello@acme.com" {
		t.Errorf("requester's mail to %v, reply to %q", toKim.To, toKim.ReplyTo)
	}
	if !strings.Contains(toKim.Subject, "Din förfrågan till Acme Events") {
		t.Errorf("requester's mail isn't in Swedish: %q", toKim.Subject)
	}

	for name, spoil := range map[string]func(map[string]any){
		"no company":  func(b map[string]any) { b["company"] = " " },
		"bad email":   func(b map[string]any) { b["email"] = "kim at bolaget" },
		"no occasion": func(b map[string]any) { b["answers"].(map[string]any)["occasion"] = "wedding" },
	} {
		b := f.request(t)
		spoil(b)
		if w := f.send(t, b); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

func TestQuoteRequestRateLimit(t *testing.T) {
	t.Parallel()
	f, cleanup := newQuoteFixture(t, "198.51.100.4")
	defer cleanup()
	f.open(t)
	for i := range 5 {
		if w := f.send(t, f.request(t)); w.Code != http.StatusCreated {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := f.send(t, f.request(t))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Errorf("6th request: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestQuoteRequestsInTheConsole(t *testing.T) {
	t.Parallel()
	f, cleanup := newQuoteFixture(t, "198.51.100.5")
	defer cleanup()
	f.open(t)
	if w := f.send(t, f.request(t)); w.Code != http.StatusCreated {
		t.Fatalf("send: %d", w.Code)
	}
	// Another organisation's request is out of reach.
	var otherOrg, otherID int64
	if err := f.h.DB.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Other', 'other') RETURNING id`).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if err := f.h.DB.QueryRow(`INSERT INTO org_requests(org_id, answers, name, email) VALUES ($1, '{}', 'X', 'x@other.com') RETURNING id`, otherOrg).Scan(&otherID); err != nil {
		t.Fatal(err)
	}

	type list struct {
		Requests []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			Notes   string `json:"notes"`
			Answers struct {
				People int `json:"people"`
			} `json:"answers"`
		} `json:"requests"`
		Counts map[string]int `json:"counts"`
	}
	var l list
	w := call(t, f.console, http.MethodGet, "/api/admin/requests", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil || len(l.Requests) != 1 || l.Requests[0].Answers.People != 100 || l.Counts["new"] != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	id := strconv.FormatInt(l.Requests[0].ID, 10)

	if w := call(t, f.member, http.MethodGet, "/api/admin/requests", nil); w.Code != http.StatusForbidden {
		t.Errorf("a member listed requests: %d", w.Code)
	}
	if w := call(t, f.console, http.MethodPatch, "/api/admin/requests/"+strconv.FormatInt(otherID, 10), map[string]any{"status": "won"}); w.Code != http.StatusNotFound {
		t.Errorf("changed another organisation's request: %d", w.Code)
	}
	if w := call(t, f.console, http.MethodPatch, "/api/admin/requests/"+id, map[string]any{"status": "maybe"}); w.Code != http.StatusBadRequest {
		t.Errorf("unknown status: %d", w.Code)
	}
	if w := call(t, f.console, http.MethodPatch, "/api/admin/requests/"+id, map[string]any{"status": "answered"}); w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.console, http.MethodPatch, "/api/admin/requests/"+id, map[string]any{"notes": "Sent quote 18 000 kr"}); w.Code != http.StatusOK {
		t.Fatalf("notes: %d %s", w.Code, w.Body)
	}
	w = call(t, f.console, http.MethodGet, "/api/admin/requests?status=answered", nil)
	l = list{}
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil || len(l.Requests) != 1 || l.Requests[0].Status != "answered" || l.Requests[0].Notes != "Sent quote 18 000 kr" {
		t.Fatalf("after update: %s", w.Body)
	}
	if w := call(t, f.console, http.MethodDelete, "/api/admin/requests/"+id, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if n := f.stored(t); n != 0 {
		t.Errorf("%d left after delete", n)
	}
}

func TestQuoteRequestOnlyWhatIsOffered(t *testing.T) {
	t.Parallel()
	f, cleanup := newQuoteFixture(t, "198.51.100.6")
	defer cleanup()
	f.open(t)

	put := func(off []string) *httptest.ResponseRecorder {
		return call(t, f.console, http.MethodPut, "/api/admin/orgs/me/requests", map[string]any{"open": true, "intro": "", "off": off})
	}
	if w := put([]string{"ready_made", "adapted", "new"}); w.Code != http.StatusBadRequest {
		t.Errorf("no kind of adventure left: %d", w.Code)
	}
	if w := put([]string{"helicopter"}); w.Code != http.StatusBadRequest {
		t.Errorf("unknown option: %d", w.Code)
	}
	if w := put([]string{"game_master", "new", "logo", "en"}); w.Code != http.StatusOK {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	// Leaving off out keeps what is saved.
	if w := call(t, f.console, http.MethodPut, "/api/admin/orgs/me/requests", map[string]any{"open": true, "intro": "Hej"}); w.Code != http.StatusOK {
		t.Fatalf("intro only: %d", w.Code)
	}
	var form struct {
		Off []string `json:"off"`
	}
	w := call(t, f.public, http.MethodGet, "/api/o/acme/request", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &form); err != nil || strings.Join(form.Off, ",") != "new,game_master,logo,en" {
		t.Fatalf("form's off = %v (%s)", form.Off, w.Body)
	}

	for name, spoil := range map[string]func(map[string]any){
		"a game master": func(b map[string]any) { b["answers"].(map[string]any)["game_master"] = true },
		"a new one":     func(b map[string]any) { b["answers"].(map[string]any)["scope"] = "new" },
		"their logo":    func(b map[string]any) { b["answers"].(map[string]any)["extras"] = []string{"logo"} },
		"in English":    func(b map[string]any) { b["answers"].(map[string]any)["languages"] = []string{"en"} },
	} {
		b := f.request(t)
		b["answers"].(map[string]any)["game_master"] = false
		b["answers"].(map[string]any)["extras"] = []string{"prize_ceremony"}
		b["answers"].(map[string]any)["languages"] = []string{"sv"}
		spoil(b)
		if w := f.send(t, b); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not_offered") {
			t.Errorf("asked for %s: %d %s", name, w.Code, w.Body)
		}
	}
	b := f.request(t)
	b["answers"].(map[string]any)["game_master"] = false
	b["answers"].(map[string]any)["extras"] = []string{"prize_ceremony"}
	b["answers"].(map[string]any)["languages"] = []string{"sv"}
	if w := f.send(t, b); w.Code != http.StatusCreated || f.stored(t) != 1 {
		t.Errorf("only what is offered: %d, %d stored", w.Code, f.stored(t))
	}

	if w := put([]string{}); w.Code != http.StatusOK {
		t.Fatalf("all back on: %d", w.Code)
	}
	w = call(t, f.public, http.MethodGet, "/api/o/acme/request", nil)
	form.Off = nil
	if err := json.Unmarshal(w.Body.Bytes(), &form); err != nil || len(form.Off) != 0 {
		t.Errorf("off after clearing = %v", form.Off)
	}
}
