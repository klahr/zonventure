package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/trail"
)

type publicTrailView struct {
	trailView
	ID          int64  `json:"id"`
	State       string `json:"state"`
	Email       string `json:"email"`
	FirstGameAt string `json:"first_game_at"`
}

type draftCreated struct {
	ID     int64           `json:"id"`
	Secret string          `json:"secret"`
	Trail  publicTrailView `json:"trail"`
}

func (f *partyFixture) publicTrailRoutes() {
	_, _ = f.h.DB.Exec(`UPDATE organizations SET quiz_trail_enabled = TRUE WHERE id = $1`, f.orgID)
	p := f.public.(*chi.Mux)
	p.Get("/api/o/{slug}/trail", f.h.GetTrailStart)
	p.Post("/api/o/{slug}/trail/drafts", f.h.CreateTrailDraft)
	p.Get("/api/trail/drafts/{id}", f.h.GetTrailDraft)
	p.Put("/api/trail/drafts/{id}", f.h.PutTrailDraft)
	p.Delete("/api/trail/drafts/{id}", f.h.DeleteTrailDraft)
	p.Post("/api/trail/drafts/{id}/duplicate", f.h.DuplicateTrailDraft)
	p.Post("/api/trail/drafts/{id}/checkout", f.h.CheckoutTrailDraft)
	p.Post("/api/trail/drafts/{id}/host-link", f.h.SendTrailHostLink)
	p.Post("/api/trail/email/save", f.h.SaveTrailsToEmail)
	p.Post("/api/trail/email/signin", f.h.SignInToTrails)
	p.Post("/api/trail/email/verify", f.h.VerifyTrailEmail)
	p.Post("/api/trail/mine", f.h.ListMyTrails)
	p.Get("/api/trail/keep/{token}", f.h.GetKeepTrail)
	p.Get("/api/party/{token}/cards", f.h.GetHostTrail)
	p.Put("/api/party/{token}/cards", f.h.PutHostTrail)
	p.Post("/api/party/{token}/keep", f.h.HostTrailKeep)
	f.consoleRoute("GET", "/api/admin/adventures", f.h.ListAdventures)
}

// callH is call with request headers.
func callH(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var s string
	if body != nil {
		b, _ := json.Marshal(body)
		s = string(b)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(s))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func (f *partyFixture) newDraft(t *testing.T, template string) draftCreated {
	t.Helper()
	w := call(t, f.public, "POST", "/api/o/acme/trail/drafts", map[string]any{"template": template, "language": "sv"})
	if w.Code != http.StatusCreated {
		t.Fatalf("new draft: %d %s", w.Code, w.Body)
	}
	var d draftCreated
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func draftPath(d draftCreated) string { return "/api/trail/drafts/" + strconv.FormatInt(d.ID, 10) }

func secretOf(d draftCreated) map[string]string { return map[string]string{"X-Trail-Secret": d.Secret} }

func (f *partyFixture) lastMail(t *testing.T) string {
	t.Helper()
	f.mail.mu.Lock()
	defer f.mail.mu.Unlock()
	if len(f.mail.messages) == 0 {
		t.Fatal("no mail sent")
	}
	m := f.mail.messages[len(f.mail.messages)-1]
	return m.To[0] + "\n" + m.Subject + "\n" + m.Text
}

func (f *partyFixture) mails() int {
	f.mail.mu.Lock()
	defer f.mail.mu.Unlock()
	return len(f.mail.messages)
}

// TestPublicTrail_Draft: a draft is made only where the org sells quiz
// trails, opens with its secret and nothing else, saves, stays out of
// the org's adventures, duplicates and deletes.
func TestPublicTrail_Draft(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.publicTrailRoutes()
	_, _ = f.h.DB.Exec(`UPDATE organizations SET quiz_trail_enabled = FALSE WHERE id = $1`, f.orgID)
	if w := call(t, f.public, "POST", "/api/o/acme/trail/drafts", map[string]any{"template": "kids"}); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "not_available") {
		t.Fatalf("org without quiz trails: %d %s", w.Code, w.Body)
	}
	w := call(t, f.public, "GET", "/api/o/acme/trail?lang=sv", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Errorf("start page: %d %s", w.Code, w.Body)
	}
	_, _ = f.h.DB.Exec(`UPDATE organizations SET quiz_trail_enabled = TRUE WHERE id = $1`, f.orgID)

	d := f.newDraft(t, "kids")
	if d.Secret == "" || d.Trail.State != "draft" || len(d.Trail.Cards) != 3 || !d.Trail.Editable {
		t.Fatalf("new draft: %+v", d)
	}
	if w := call(t, f.public, "GET", draftPath(d), nil); w.Code != http.StatusNotFound {
		t.Errorf("no secret: %d", w.Code)
	}
	if w := callH(t, f.public, "GET", draftPath(d), nil, map[string]string{"X-Trail-Secret": "wrong"}); w.Code != http.StatusNotFound {
		t.Errorf("wrong secret: %d", w.Code)
	}
	cards := append(d.Trail.Cards, trail.Card{ID: trail.NewID(), Kind: trail.Info, Text: "Slut!"})
	w = callH(t, f.public, "PUT", draftPath(d), map[string]any{"cards": cards, "revision": d.Trail.Revision, "title": "Elsas runda"}, secretOf(d))
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	var saved publicTrailView
	_ = json.Unmarshal(w.Body.Bytes(), &saved)
	if saved.Title != "Elsas runda" || len(saved.Cards) != 4 || saved.State != "draft" {
		t.Errorf("saved: %+v", saved)
	}

	w = call(t, f.console, "GET", "/api/admin/adventures", nil)
	if strings.Contains(w.Body.String(), saved.Slug) || !strings.Contains(w.Body.String(), "treasure") {
		t.Errorf("the org's adventures show the hidden trail: %s", w.Body)
	}

	w = callH(t, f.public, "POST", draftPath(d)+"/duplicate", nil, secretOf(d))
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body)
	}
	var dup draftCreated
	_ = json.Unmarshal(w.Body.Bytes(), &dup)
	if dup.ID == d.ID || dup.Trail.Title != "Elsas runda (kopia)" || len(dup.Trail.Cards) != 4 {
		t.Errorf("duplicate: %+v", dup.Trail)
	}

	if w := callH(t, f.public, "DELETE", draftPath(d), nil, secretOf(d)); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	var n int
	_ = f.h.DB.QueryRow(`SELECT COUNT(*) FROM adventures WHERE slug = $1`, saved.Slug).Scan(&n)
	if n != 0 {
		t.Error("the deleted draft's adventure is still there")
	}
	w = call(t, f.public, "POST", "/api/trail/mine", map[string]any{"trails": []map[string]any{
		{"id": d.ID, "secret": d.Secret}, {"id": dup.ID, "secret": dup.Secret}}})
	if !strings.Contains(w.Body.String(), `"gone":[`+strconv.FormatInt(d.ID, 10)+`]`) || !strings.Contains(w.Body.String(), "Elsas runda (kopia)") {
		t.Errorf("mine: %s", w.Body)
	}
}

// TestPublicTrail_PayHostKeep: a finished draft is paid for, becomes a
// hosted event whose host link edits the cards, plays, and a week in is
// kept: it moves into the keeper's org and the event goes.
func TestPublicTrail_PayHostKeep(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.publicTrailRoutes()
	f.h.Hosted = payments.HostedPass{BlockCents: 14900, Currency: "sek"}
	d := f.newDraft(t, "kids")
	path := draftPath(d)

	if w := callH(t, f.public, "POST", path+"/checkout", map[string]any{"email": "anna@example.com", "blocks": 1}, secretOf(d)); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "consent") {
		t.Errorf("kids' trail without consent: %d %s", w.Code, w.Body)
	}
	if w := callH(t, f.public, "POST", path+"/checkout", map[string]any{"email": "nope", "blocks": 1, "consent": true}, secretOf(d)); w.Code != http.StatusBadRequest {
		t.Errorf("bad email: %d", w.Code)
	}
	w := callH(t, f.public, "POST", path+"/checkout", map[string]any{"email": "Anna@Example.com", "blocks": 2, "consent": true}, secretOf(d))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("checkout: %d %s", w.Code, w.Body)
	}
	var amount int64
	_ = f.h.DB.QueryRow(`SELECT amount_cents FROM ticket_purchases WHERE kind = 'hosted'`).Scan(&amount)
	if amount != 29800 {
		t.Errorf("amount = %d", amount)
	}
	f.pay(t, "hosted")
	mail := f.lastMail(t)
	if !strings.HasPrefix(mail, "anna@example.com") || !strings.Contains(mail, "redo att spelas") {
		t.Errorf("paid mail: %s", mail)
	}
	host := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(mail)[1]
	// The browser that paid was given the same admin page at checkout,
	// so it can go there without waiting for the mail.
	var co struct {
		HostURL string `json:"host_url"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &co)
	if !strings.HasSuffix(co.HostURL, "/party/"+host) {
		t.Errorf("checkout's admin page %q, mailed %q", co.HostURL, host)
	}
	var kept sql.NullString
	_ = f.h.DB.QueryRow(`SELECT host_token FROM ticket_purchases WHERE kind = 'hosted'`).Scan(&kept)
	if kept.Valid {
		t.Error("the purchase kept the host link's secret after the payment")
	}

	var v publicTrailView
	w = callH(t, f.public, "GET", path, nil, secretOf(d))
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v.State != "paid" || v.Email != "anna@…" || !v.Published {
		t.Errorf("paid trail: %+v", v)
	}
	if w := callH(t, f.public, "POST", path+"/checkout", map[string]any{"email": "anna@example.com", "blocks": 1, "consent": true}, secretOf(d)); w.Code != http.StatusConflict {
		t.Errorf("paying twice: %d", w.Code)
	}

	hv := f.host(t, host)
	if !strings.Contains(call(t, f.public, "GET", "/api/party/"+host, nil).Body.String(), `"public_trail":true`) {
		t.Error("host page doesn't know it's a public trail")
	}
	w = call(t, f.public, "PUT", "/api/party/"+host+"/cards", map[string]any{"cards": v.Cards[:2], "revision": v.Revision})
	if w.Code != http.StatusOK {
		t.Fatalf("host edits the cards: %d %s", w.Code, w.Body)
	}
	if w := f.join(t, hv.playerToken(), "Ugglorna", 3); w.Code != http.StatusCreated {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "POST", "/api/party/"+host+"/keep", nil); w.Code != http.StatusConflict {
		t.Errorf("keep on day one: %d", w.Code)
	}
	_, _ = f.h.DB.Exec(`UPDATE events SET first_game_at = now() - interval '8 days' WHERE party`)
	w = call(t, f.public, "POST", "/api/party/"+host+"/keep", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("keep: %d %s", w.Code, w.Body)
	}
	var keep struct{ URL string }
	_ = json.Unmarshal(w.Body.Bytes(), &keep)
	token := keep.URL[strings.LastIndex(keep.URL, "/")+1:]
	if w := call(t, f.public, "GET", "/api/trail/keep/"+token, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "anna@example.com") {
		t.Errorf("keep page: %d %s", w.Code, w.Body)
	}

	// Someone signs up and keeps it.
	var otherOrg, otherUser int64
	_ = f.h.DB.QueryRow(`INSERT INTO organizations(name) VALUES ('Anna') RETURNING id`).Scan(&otherOrg)
	_ = f.h.DB.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('anna', 'anna@example.com', 1) RETURNING id`).Scan(&otherUser)
	other := chi.NewRouter()
	other.Use(injectUser(auth.User{ID: otherUser, Role: auth.RoleOrgAdmin, OrgID: &otherOrg, Email: "anna@example.com", EmailVerified: true}))
	other.Post("/api/admin/trail-keep", f.h.KeepTrail)
	if w := call(t, other, "POST", "/api/admin/trail-keep", map[string]any{"token": "nope"}); w.Code != http.StatusGone {
		t.Errorf("bad keep token: %d", w.Code)
	}
	before := f.mails()
	w = call(t, other, "POST", "/api/admin/trail-keep", map[string]any{"token": token})
	if w.Code != http.StatusOK {
		t.Fatalf("keep it: %d %s", w.Code, w.Body)
	}
	var org int64
	var hidden bool
	_ = f.h.DB.QueryRow(`SELECT org_id, trail_hidden FROM adventures WHERE slug = $1`, v.Slug).Scan(&org, &hidden)
	if org != otherOrg || hidden {
		t.Errorf("kept adventure: org %d hidden %v", org, hidden)
	}
	var events, trails int
	_ = f.h.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM events WHERE party), (SELECT COUNT(*) FROM trails)`).Scan(&events, &trails)
	if events != 0 || trails != 0 {
		t.Errorf("after keeping: %d events, %d trails", events, trails)
	}
	if f.mails() < before+2 || !strings.Contains(f.lastMail(t), "finns nu i ditt konto") {
		t.Errorf("no summary and goodbye mail: %s", f.lastMail(t))
	}
	if w := call(t, other, "POST", "/api/admin/trail-keep", map[string]any{"token": token}); w.Code != http.StatusGone {
		t.Errorf("keeping twice: %d", w.Code)
	}
}

// TestPublicTrail_Email: a confirm link saves the browser's trails to an
// address and signs in; a sign-in link opens them from elsewhere; an
// address with no trails gets nothing, and the answer is the same.
func TestPublicTrail_Email(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.publicTrailRoutes()
	d := f.newDraft(t, "blank")

	w := call(t, f.public, "POST", "/api/trail/email/save", map[string]any{"email": "Bo@Example.com",
		"trails": []map[string]any{{"id": d.ID, "secret": d.Secret}}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("save to email: %d %s", w.Code, w.Body)
	}
	tok := regexp.MustCompile(`verify#([A-Za-z0-9_-]+)`).FindStringSubmatch(f.lastMail(t))
	if tok == nil {
		t.Fatalf("confirm mail: %s", f.lastMail(t))
	}
	w = call(t, f.public, "POST", "/api/trail/email/verify", map[string]any{"token": tok[1]})
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	var s struct{ Session, Email string }
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	if s.Email != "bo@example.com" || s.Session == "" {
		t.Errorf("session: %+v", s)
	}
	if w := call(t, f.public, "POST", "/api/trail/email/verify", map[string]any{"token": tok[1]}); w.Code != http.StatusGone {
		t.Errorf("link used twice: %d", w.Code)
	}
	session := map[string]string{"X-Trail-Session": s.Session}
	if w := callH(t, f.public, "GET", draftPath(d), nil, session); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"email":"bo@…"`) {
		t.Errorf("draft by session: %d %s", w.Code, w.Body)
	}
	w = callH(t, f.public, "POST", "/api/trail/mine", map[string]any{"trails": []any{}}, session)
	if !strings.Contains(w.Body.String(), `"id":`+strconv.FormatInt(d.ID, 10)) {
		t.Errorf("mine by session: %s", w.Body)
	}

	before := f.mails()
	if w := call(t, f.public, "POST", "/api/trail/email/signin", map[string]any{"email": "nobody@example.com"}); w.Code != http.StatusNoContent || f.mails() != before {
		t.Errorf("sign-in to no trails: %d, %d mails", w.Code, f.mails()-before)
	}
	if w := call(t, f.public, "POST", "/api/trail/email/signin", map[string]any{"email": "bo@example.com"}); w.Code != http.StatusNoContent || f.mails() != before+1 {
		t.Fatalf("sign-in: %d", w.Code)
	}
	tok = regexp.MustCompile(`verify#([A-Za-z0-9_-]+)`).FindStringSubmatch(f.lastMail(t))
	if w := call(t, f.public, "POST", "/api/trail/email/verify", map[string]any{"token": tok[1]}); w.Code != http.StatusOK {
		t.Errorf("sign-in link: %d %s", w.Code, w.Body)
	}
}

// TestPublicTrail_Sweep: an old draft goes, a paid trail whose event has
// ended goes with a goodbye, and a week into play the buyer is offered
// to keep it, once.
func TestPublicTrail_Sweep(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.publicTrailRoutes()
	ctx := context.Background()

	old := f.newDraft(t, "blank")
	_, _ = f.h.DB.Exec(`UPDATE trails SET updated_at = now() - interval '31 days' WHERE id = $1`, old.ID)
	keep := f.newDraft(t, "blank")

	// Free hosting: paid at once.
	playing := f.newDraft(t, "kids")
	if w := callH(t, f.public, "POST", draftPath(playing)+"/checkout", map[string]any{"email": "c@example.com", "blocks": 1, "consent": true}, secretOf(playing)); w.Code != http.StatusCreated {
		t.Fatalf("free checkout: %d %s", w.Code, w.Body)
	}
	ended := f.newDraft(t, "kids")
	if w := callH(t, f.public, "POST", draftPath(ended)+"/checkout", map[string]any{"email": "e@example.com", "blocks": 1, "consent": true}, secretOf(ended)); w.Code != http.StatusCreated {
		t.Fatalf("free checkout: %d %s", w.Code, w.Body)
	}
	_, _ = f.h.DB.Exec(`UPDATE events SET state = 'running', first_game_at = now() - interval '8 days', valid_until = now() + interval '6 days'
		WHERE id = (SELECT event_id FROM trails WHERE id = $1)`, playing.ID)
	_, _ = f.h.DB.Exec(`UPDATE events SET state = 'completed', completed_at = now_text() WHERE id = (SELECT event_id FROM trails WHERE id = $1)`, ended.ID)

	before := f.mails()
	f.h.SweepTrails(ctx)
	f.h.SweepTrails(ctx)
	var ids []int64
	rows, _ := f.h.DB.Query(`SELECT id FROM trails ORDER BY id`)
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 2 || ids[0] != keep.ID || ids[1] != playing.ID {
		t.Errorf("trails left: %v (want %d, %d)", ids, keep.ID, playing.ID)
	}
	var sent []string
	f.mail.mu.Lock()
	for _, m := range f.mail.messages[before:] {
		sent = append(sent, m.To[0]+": "+m.Subject)
	}
	f.mail.mu.Unlock()
	if len(sent) != 2 || !strings.Contains(strings.Join(sent, "|"), "e@example.com") || !strings.Contains(strings.Join(sent, "|"), "c@example.com: Vill du behålla") {
		t.Errorf("mails: %v", sent)
	}
}

// TestPublicTrail_Free: with no blocks a draft is hosted at once, free,
// for FreeTeams teams a game. Nothing paid vouches for the email, so the
// link isn't mailed and the address isn't confirmed; the answer opens
// the admin page.
func TestPublicTrail_Free(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.publicTrailRoutes()
	f.h.Hosted = payments.HostedPass{BlockCents: 14900, Currency: "sek"}
	d := f.newDraft(t, "birthday")
	mailed := len(f.mail.messages)

	w := callH(t, f.public, "POST", draftPath(d)+"/checkout", map[string]any{"email": "someone@example.com", "blocks": 0, "consent": true}, secretOf(d))
	if w.Code != http.StatusCreated {
		t.Fatalf("free: %d %s", w.Code, w.Body)
	}
	var co struct {
		HostURL string `json:"host_url"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &co)
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)$`).FindStringSubmatch(co.HostURL)
	if m == nil {
		t.Fatalf("no admin page: %s", w.Body)
	}
	if got := f.teamsLimit(t, m[1]); got != admin.FreeTeams {
		t.Errorf("teams limit = %d, want %d", got, admin.FreeTeams)
	}
	if len(f.mail.messages) != mailed {
		t.Error("mailed an address nothing vouches for")
	}
	var confirmed bool
	var purchases int
	_ = f.h.DB.QueryRow(`SELECT email_confirmed_at IS NOT NULL FROM trails WHERE id = $1`, d.ID).Scan(&confirmed)
	_ = f.h.DB.QueryRow(`SELECT COUNT(*) FROM ticket_purchases`).Scan(&purchases)
	if confirmed || purchases != 0 {
		t.Errorf("confirmed %v, purchases %d", confirmed, purchases)
	}
}
