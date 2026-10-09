package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/klahr/zonventure/internal/settings"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// partyGraph is a one-step treasure hunt whose step waits at a zone: in
// a party that zone is a card to scan.
func partyGraph() *graph.Graph {
	return &graph.Graph{
		Zones: []graph.Zone{{Slug: "oak", Name: "The old oak", Lat: 59.33, Lng: 18.06, RadiusM: 30}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Zone:    "oak",
				Blocks:  append(graph.LegacyBlocks("What grows on the oak?", nil), graph.Block{ID: "m", Kind: graph.BlockMap, Zone: "oak"}),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "acorns"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: graph.PortOut, To: "q"}, {From: "q", FromPort: "solved", To: "end"}},
	}
}

type partyFixture struct {
	h       *admin.Handlers
	mail    *fakeMailer
	console chi.Router
	public  http.Handler
	orgID   int64
}

// consoleRoute adds a route to the console router.
func (f *partyFixture) consoleRoute(method, pattern string, h http.HandlerFunc) {
	f.console.Method(method, pattern, h)
}

func newPartyFixture(t *testing.T) (*partyFixture, func()) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	f := &partyFixture{mail: &fakeMailer{}}
	if err := db.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Acme', 'acme') RETURNING id`).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	var userID, advID int64
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, author, org_id, published) VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1)
		RETURNING id`, f.orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := graphstore.Publish(context.Background(), db, advID, userID, partyGraph(), ""); err != nil {
		t.Fatal(err)
	}
	f.h = &admin.Handlers{DB: db, Lib: adventure.New(db), Hub: ws.NewHub(), Mailer: f.mail,
		PublicURL: "https://zv.example.com", Provider: &fakeProvider{},
		// Free hosting and no platform fee unless a test sets its prices.
		Pricing: func() settings.Pricing { return settings.Pricing{} }}

	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &f.orgID, Email: "boss@acme.com", EmailVerified: true}
	console := chi.NewRouter()
	console.Use(injectUser(user))
	console.Get("/api/admin/party-offers", f.h.ListPartyOffers)
	console.Put("/api/admin/party-offers/{slug}", f.h.PutPartyOffer)
	console.Get("/api/admin/events/{eventID}/hosted", f.h.GetHostParty)
	console.Post("/api/admin/events/{eventID}/hosted/finish-game", f.h.FinishHostedGame)
	f.console = console

	public := chi.NewRouter()
	public.Get("/api/o/{slug}/parties", f.h.ListPartiesByOrg)
	public.Post("/api/o/{slug}/parties/{offerID}/book", f.h.BookParty)
	public.Post("/api/party/resend", f.h.ResendPartyLinks)
	public.Get("/api/party/{token}", f.h.GetHostParty)
	public.Delete("/api/party/{token}/teams/{teamID}", f.h.RemoveHostTeam)
	public.Post("/api/party/{token}/finish-game", f.h.FinishHostedGame)
	public.Put("/api/party/{token}/hold-results", f.h.PutHoldResults)
	public.Post("/api/party/{token}/send-player-link", f.h.SendHostedPlayerLink)
	public.Post("/api/party/{token}/blocks", f.h.AddHostedBlocks)
	public.Get("/api/join/{token}", f.h.GetJoin)
	public.Post("/api/join/{token}", f.h.Join)
	public.Put("/api/party/{token}/personalise", f.h.PutHostPersonalise)
	public.Put("/api/party/{token}/mode", f.h.PutHostMode)
	public.Put("/api/party/{token}/places/{slug}", f.h.PutHostPlace)
	public.Post("/api/party/{token}/places/shift", f.h.PostHostShift)
	public.Post("/api/party/{token}/{action}", f.h.HostPartyAction)
	f.public = public
	return f, cleanup
}

func call(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var s string
	if body != nil {
		b, _ := json.Marshal(body)
		s = string(b)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(s))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// book offers the treasure hunt as a party, books it for kids children
// and lands the payment, returning the purchase id and the host link's
// secret from the booker's mail.
func (f *partyFixture) book(t *testing.T, kids int) (int64, string) {
	t.Helper()
	w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", map[string]any{
		"enabled": true, "base_cents": 19900, "per_kid_cents": 2900, "currency": "sek", "max_kids": 12, "blurb": "Find the treasure",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("offer: %d %s", w.Code, w.Body)
	}
	var offer struct {
		ID int64 `json:"offer_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &offer)

	w = call(t, f.public, "POST", "/api/o/acme/parties/"+strconv.FormatInt(offer.ID, 10)+"/book", map[string]any{
		"email": "Parent@Example.com", "kids": kids, "party_name": "Elsa turns 8", "consent": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("book: %d %s", w.Code, w.Body)
	}
	var purchaseID, amount int64
	if err := f.h.DB.QueryRow(`SELECT id, amount_cents FROM ticket_purchases WHERE kind = 'party' ORDER BY id DESC LIMIT 1`).Scan(&purchaseID, &amount); err != nil {
		t.Fatal(err)
	}
	if want := admin.PartyPriceCents(19900, 2900, kids); amount != want {
		t.Errorf("amount = %d, want %d", amount, want)
	}
	ev := buildSessionCompletedEvent(t, purchaseID, "cs_party_"+strconv.FormatInt(purchaseID, 10))
	if err := admin.ExportedHandleTicketCheckoutCompleted(f.h, context.Background(), ev); err != nil {
		t.Fatalf("fulfil: %v", err)
	}
	if len(f.mail.messages) == 0 {
		t.Fatal("no host link mailed")
	}
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil {
		t.Fatalf("mail has no host link: %s", f.mail.messages[len(f.mail.messages)-1].Text)
	}
	return purchaseID, m[1]
}

type hostView struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Kids        int    `json:"kids"`
	PlayerURL   string `json:"player_url"`
	Round       int    `json:"round"`
	RoundOpen   bool   `json:"round_open"`
	HoldResults bool   `json:"hold_results"`
	Stations    []struct {
		Zone, Name, Code, URL string
	} `json:"stations"`
	Teams []struct {
		ID        int64 `json:"id"`
		Headcount int   `json:"headcount"`
	} `json:"teams"`
	Rounds []struct {
		Round   int `json:"round"`
		Teams   int `json:"teams"`
		Players int `json:"players"`
	} `json:"rounds"`
}

// playerToken is the player link's secret, from the host page.
func (v hostView) playerToken() string {
	return v.PlayerURL[strings.LastIndex(v.PlayerURL, "/")+1:]
}

// join makes a team from the player link, returning the response.
func (f *partyFixture) join(t *testing.T, player, team string, headcount int) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, f.public, "POST", "/api/join/"+player, map[string]any{"team": team, "headcount": headcount})
}

func (f *partyFixture) host(t *testing.T, token string) hostView {
	t.Helper()
	w := call(t, f.public, "GET", "/api/party/"+token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("host page: %d %s", w.Code, w.Body)
	}
	var v hostView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestParty_BookHostAndPlay walks a party end to end: the seller offers
// an adventure, a parent books and pays with no account, the host page
// shows a card per zone, the host makes teams within the children
// booked and starts, and a team at the zone's step is let in by
// scanning that card — not by where it stands.
func TestParty_BookHostAndPlay(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()

	w := call(t, f.public, "GET", "/api/o/acme/parties", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Treasure Hunt"`) {
		// Not offered yet: the list is empty until the offer is saved.
		if !strings.Contains(w.Body.String(), `"parties":[]`) {
			t.Fatalf("parties before offer: %d %s", w.Code, w.Body)
		}
	}

	_, token := f.book(t, 5)
	v := f.host(t, token)
	if v.Name != "Elsa turns 8" || v.State != "scheduled" || v.Kids != 5 {
		t.Errorf("host view = %+v", v)
	}
	if len(v.Stations) != 1 || v.Stations[0].Zone != "oak" || v.Stations[0].Code == "" {
		t.Fatalf("stations = %+v, want one card for the oak", v.Stations)
	}
	if !strings.Contains(v.Stations[0].URL, "/play?station=") {
		t.Errorf("card URL = %q", v.Stations[0].URL)
	}
	// The booker's email is the purchase's, lower-cased.
	var email string
	_ = f.h.DB.QueryRow(`SELECT buyer_email FROM ticket_purchases WHERE kind = 'party'`).Scan(&email)
	if email != "parent@example.com" {
		t.Errorf("buyer_email = %q", email)
	}

	// The first team to join opens the first game and starts the party.
	player := v.playerToken()
	if !strings.Contains(v.PlayerURL, "/j/") || player == "" {
		t.Fatalf("player link = %q", v.PlayerURL)
	}
	w = f.join(t, player, "Owls", 3)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), "/play?t=") {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	if w := f.join(t, player, "owls", 2); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "name_taken") {
		t.Errorf("same name again: %d %s", w.Code, w.Body)
	}
	// Without a headcount, as the join page sends it, a team is one.
	if w := f.join(t, player, "Foxes", 0); w.Code != http.StatusCreated {
		t.Fatalf("second team: %d %s", w.Code, w.Body)
	}
	v = f.host(t, token)
	if v.State != "running" || v.Round != 1 || !v.RoundOpen || len(v.Teams) != 2 || v.Teams[0].Headcount != 3 || v.Teams[1].Headcount != 1 {
		t.Fatalf("after joining: %+v", v)
	}

	// The game plays its own copy of the graph: stations, without the
	// map block, never among the adventure's versions.
	var version int
	var doc string
	if err := f.h.DB.QueryRow(`
		SELECT g.version, g.graph::text FROM events e JOIN adventure_graphs g ON g.id = e.graph_id WHERE e.party`).Scan(&version, &doc); err != nil {
		t.Fatal(err)
	}
	var pg graph.Graph
	if err := json.Unmarshal([]byte(doc), &pg); err != nil {
		t.Fatal(err)
	}
	if version >= 0 || !pg.Stations || strings.Contains(doc, `"map"`) {
		t.Errorf("party graph: version %d, %s", version, doc)
	}

	e := play.New(f.h.DB)
	team := v.Teams[0].ID
	view, err := e.Snapshot(context.Background(), team)
	if err != nil {
		t.Fatal(err)
	}
	if view.Node != "q" || view.Here || !view.Stations || view.WatchPosition {
		t.Fatalf("at the step before the card: node %q here %v stations %v watch %v", view.Node, view.Here, view.Stations, view.WatchPosition)
	}
	if _, err := e.Answer(context.Background(), team, "q", "acorns"); err == nil {
		t.Error("answered away from the card")
	}
	if _, err := e.Station(context.Background(), team, "NOT-A-CARD"); err != play.ErrUnknownStation {
		t.Errorf("unknown card: %v", err)
	}
	// Typed with a dash and in lower case, as a child might.
	code := strings.ToLower(v.Stations[0].Code)
	res, err := e.Station(context.Background(), team, code[:3]+"-"+code[3:])
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !res.View.Here {
		t.Fatal("scanning the card did not bring the team to the oak")
	}
	if res, err := e.Answer(context.Background(), team, "q", "acorns"); err != nil || !res.Accepted {
		t.Fatalf("answer at the card: %+v %v", res, err)
	}
	// Every team finishing does not end a hosted party: it plays on.
	if v := f.host(t, token); v.State != "running" {
		t.Errorf("after a team finished: state %q", v.State)
	}

	// Finishing the game mails the host its summary; the next team to
	// join opens game 2, playing a copy of its own.
	mails := len(f.mail.messages)
	if w := call(t, f.public, "POST", "/api/party/"+token+"/finish-game", nil); w.Code != http.StatusNoContent {
		t.Fatalf("finish game: %d %s", w.Code, w.Body)
	}
	if len(f.mail.messages) != mails+1 || !strings.Contains(f.mail.messages[mails].Text, "Owls") ||
		!strings.Contains(f.mail.messages[mails].Text, "2 teams, 4 players") {
		t.Fatalf("summary mail: %+v", f.mail.messages[mails:])
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/finish-game", nil); w.Code != http.StatusConflict {
		t.Errorf("finish with no game: %d", w.Code)
	}
	if w := f.join(t, player, "Owls", 4); w.Code != http.StatusCreated {
		t.Fatalf("join game 2: %d %s", w.Code, w.Body)
	}
	v = f.host(t, token)
	if v.Round != 2 || len(v.Teams) != 1 || len(v.Rounds) != 2 || v.Rounds[1].Teams != 2 || v.Rounds[1].Players != 4 {
		t.Fatalf("game 2: %+v", v)
	}
	var graphs int
	_ = f.h.DB.QueryRow(`SELECT COUNT(DISTINCT s.graph_id) FROM team_graph_state s JOIN teams t ON t.id = s.team_id JOIN events e ON e.id = t.event_id WHERE e.party`).Scan(&graphs)
	if graphs != 2 {
		t.Errorf("games played %d graphs, want one each", graphs)
	}

	// Once its days are up the sweeper ends it, and nobody joins.
	if _, err := f.h.DB.Exec(`UPDATE events SET valid_until = now() - interval '1 minute' WHERE party`); err != nil {
		t.Fatal(err)
	}
	f.h.SweepHosted(context.Background())
	if v := f.host(t, token); v.State != "completed" || v.RoundOpen {
		t.Errorf("after its days: %+v", v)
	}
	if w := f.join(t, player, "Late", 1); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "ended") {
		t.Errorf("join after the end: %d %s", w.Code, w.Body)
	}
}

// TestParty_Console: the org's arrangers see a hosted event's host page
// in the console, and finish its game; another org's do not.
func TestParty_Console(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	_, token := f.book(t, 5)
	var eventID int64
	if err := f.h.DB.QueryRow(`SELECT id FROM events WHERE party`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/events/" + strconv.FormatInt(eventID, 10) + "/hosted"
	// Its teams join from the player link and its games start as they do:
	// the console makes no teams for it and does not start it.
	f.consoleRoute("POST", "/api/admin/events/{eventID}/teams", f.h.CreateTeam)
	f.consoleRoute("POST", "/api/admin/events/{eventID}/start", f.h.StartEvent)
	for _, p := range []string{"/teams", "/start"} {
		w := call(t, f.console, "POST", "/api/admin/events/"+strconv.FormatInt(eventID, 10)+p, map[string]any{"name": "Owls"})
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "event_hosted") {
			t.Errorf("console %s: %d %s", p, w.Code, w.Body)
		}
	}
	w := call(t, f.console, "GET", path, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/j/") {
		t.Fatalf("console host page: %d %s", w.Code, w.Body)
	}
	if w := f.join(t, f.host(t, token).playerToken(), "Owls", 2); w.Code != http.StatusCreated {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.console, "POST", path+"/finish-game", nil); w.Code != http.StatusNoContent {
		t.Fatalf("console finish: %d %s", w.Code, w.Body)
	}

	var otherOrg int64
	if err := f.h.DB.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Other', 'other') RETURNING id`).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	other := chi.NewRouter()
	other.Use(injectUser(auth.User{ID: 999, Role: auth.RoleOrgAdmin, OrgID: &otherOrg}))
	other.Get("/api/admin/events/{eventID}/hosted", f.h.GetHostParty)
	if w := call(t, other, "GET", path, nil); w.Code != http.StatusForbidden {
		t.Errorf("another org's console: %d, want 403", w.Code)
	}
}

// TestParty_GameIsFull: a game takes 15 teams per block bought.
func TestParty_GameIsFull(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	_, token := f.book(t, 5)
	player := f.host(t, token).playerToken()
	for i := range admin.TeamsPerBlock {
		if w := f.join(t, player, "Team "+strconv.Itoa(i), 1); w.Code != http.StatusCreated {
			t.Fatalf("team %d: %d %s", i, w.Code, w.Body)
		}
	}
	if w := f.join(t, player, "One too many", 1); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "full") {
		t.Fatalf("team past the limit: %d %s", w.Code, w.Body)
	}
	// A removed team frees its place.
	v := f.host(t, token)
	if w := call(t, f.public, "DELETE", "/api/party/"+token+"/teams/"+strconv.FormatInt(v.Teams[0].ID, 10), nil); w.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", w.Code, w.Body)
	}
	if w := f.join(t, player, "One too many", 1); w.Code != http.StatusCreated {
		t.Fatalf("after a removal: %d %s", w.Code, w.Body)
	}
}

// TestParty_OfferRefusesUnplayable keeps an adventure that asks where
// each player stands out of parties: cards put a team in one place.
func TestParty_OfferRefusesUnplayable(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	var advID int64
	if err := f.h.DB.QueryRow(`SELECT id FROM adventures WHERE slug = 'treasure'`).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	g := partyGraph()
	g.Nodes[1].PuzzleCfg.Answer = &graph.Answer{Kind: graph.AnswerSearch}
	if _, _, err := graphstore.Publish(context.Background(), f.h.DB, advID, 0, g, ""); err != nil {
		t.Fatal(err)
	}
	w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", map[string]any{
		"enabled": true, "base_cents": 100, "currency": "sek", "max_kids": 10,
	})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not_playable_as_party") {
		t.Fatalf("offer of a search adventure: %d %s", w.Code, w.Body)
	}
	w = call(t, f.console, "GET", "/api/admin/party-offers", nil)
	if !strings.Contains(w.Body.String(), "searches its zones") {
		t.Errorf("offers list does not say why: %s", w.Body)
	}
}

// TestParty_BookingValidation covers the refusals a public form meets.
func TestParty_BookingValidation(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", map[string]any{
		"enabled": true, "base_cents": 100, "per_kid_cents": 10, "currency": "sek", "max_kids": 4,
	})
	var offer struct {
		ID int64 `json:"offer_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &offer)
	path := "/api/o/acme/parties/" + strconv.FormatInt(offer.ID, 10) + "/book"
	for name, body := range map[string]map[string]any{
		"no consent":    {"email": "p@example.com", "kids": 3},
		"too many kids": {"email": "p@example.com", "kids": 5, "consent": true},
		"no kids":       {"email": "p@example.com", "kids": 0, "consent": true},
		"bad email":     {"email": "nope", "kids": 3, "consent": true},
	} {
		if w := call(t, f.public, "POST", path, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, w.Code, w.Body)
		}
	}
	if w := call(t, f.public, "POST", "/api/o/other/parties/"+strconv.FormatInt(offer.ID, 10)+"/book",
		map[string]any{"email": "p@example.com", "kids": 3, "consent": true}); w.Code != http.StatusNotFound {
		t.Errorf("booking through another org's page: %d, want 404", w.Code)
	}
}

// TestParty_RefundCancelsAndResendRotates: a refunded party is called
// off, and a resent host link replaces the old one.
func TestParty_RefundCancelsAndResendRotates(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	purchaseID, token := f.book(t, 3)

	if w := call(t, f.public, "POST", "/api/party/resend", map[string]any{"email": "parent@example.com"}); w.Code != http.StatusNoContent {
		t.Fatalf("resend: %d", w.Code)
	}
	if w := call(t, f.public, "GET", "/api/party/"+token, nil); w.Code != http.StatusNotFound {
		t.Errorf("old link after resend: %d, want 404", w.Code)
	}
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil || m[1] == token {
		t.Fatal("resend mailed no new link")
	}
	if v := f.host(t, m[1]); v.State != "scheduled" {
		t.Errorf("new link: %+v", v)
	}

	if _, err := f.h.DB.Exec(`UPDATE ticket_purchases SET stripe_charge_id = 'ch_party' WHERE id = $1`, purchaseID); err != nil {
		t.Fatal(err)
	}
	if err := admin.ExportedHandleChargeRefunded(f.h, context.Background(), buildChargeRefundedEvent(t, "ch_party")); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if v := f.host(t, m[1]); v.State != "cancelled" {
		t.Errorf("after refund: state %q, want cancelled", v.State)
	}
}

// fixedLocator puts every team at one spot, as a phone's GPS would.
type fixedLocator struct{ lat, lng float64 }

func (l fixedLocator) Position(context.Context, play.Querier, int64) (float64, float64, bool, error) {
	return l.lat, l.lng, true, nil
}

// TestParty_PersonaliseAndPark: the host rewrites what the author let
// them — the birthday child's name, the answer — and moves the
// adventure to their park; the party plays exactly that, by GPS.
func TestParty_PersonaliseAndPark(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	var advID int64
	if err := f.h.DB.QueryRow(`SELECT id FROM adventures WHERE slug = 'treasure'`).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	g := partyGraph()
	g.HostValues = []graph.HostValue{{Slug: "name", Label: "Birthday child", Default: "the hero"}}
	g.Zones[0].Movable = true
	q := g.Nodes[1].PuzzleCfg
	q.HostAnswer = true
	q.Blocks = []graph.Block{{ID: "t", Kind: graph.BlockText, Text: "What is {{host:name}}'s favourite animal?", HostEdit: true}}
	if _, _, err := graphstore.Publish(context.Background(), f.h.DB, advID, 0, g, ""); err != nil {
		t.Fatal(err)
	}
	_, token := f.book(t, 4)

	var v struct {
		Mode      string `json:"mode"`
		ParkOK    bool   `json:"park_ok"`
		Values    []struct{ Slug, Label, Default string }
		Editables []struct{ Key string }
		Places    []struct {
			Slug    string
			Movable bool
			Placed  bool
		}
	}
	w := call(t, f.public, "GET", "/api/party/"+token, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v.Mode != "qr" || !v.ParkOK || len(v.Values) != 1 || len(v.Editables) != 2 || len(v.Places) != 1 || !v.Places[0].Movable {
		t.Fatalf("adaptation = %+v (%s)", v, w.Body)
	}

	if w := call(t, f.public, "PUT", "/api/party/"+token+"/personalise", map[string]any{
		"values": map[string]string{"nope": "x"},
	}); w.Code != http.StatusBadRequest {
		t.Errorf("unknown value: %d", w.Code)
	}
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/personalise", map[string]any{
		"values":  map[string]string{"name": "Elsa"},
		"answers": map[string][]string{"q": {"horse"}},
	}); w.Code != http.StatusNoContent {
		t.Fatalf("personalise: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/mode", map[string]any{"mode": "park"}); w.Code != http.StatusNoContent {
		t.Fatalf("park mode: %d %s", w.Code, w.Body)
	}
	player := f.host(t, token).playerToken()
	if w := f.join(t, player, "Owls", 4); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not_ready") {
		t.Fatalf("join before placing: %d %s", w.Code, w.Body)
	}
	// The host's park, far from where the author drew the oak.
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/places/oak", map[string]any{"lat": 57.70, "lng": 11.97}); w.Code != http.StatusNoContent {
		t.Fatalf("place: %d %s", w.Code, w.Body)
	}
	if w := f.join(t, player, "Owls", 4); w.Code != http.StatusCreated {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	// Changing anything during a game is refused: it plays as it was set
	// up. Between games it is allowed again.
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/mode", map[string]any{"mode": "qr"}); w.Code != http.StatusConflict {
		t.Errorf("mode during a game: %d", w.Code)
	}

	host := f.host(t, token)
	e := play.New(f.h.DB)
	e.Locator = fixedLocator{57.70, 11.97}
	view, err := e.Snapshot(context.Background(), host.Teams[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	named := false
	for _, b := range view.Blocks {
		named = named || strings.Contains(b.Text, "What is Elsa's favourite animal?")
	}
	if view.Stations || !view.Here || !named {
		t.Fatalf("in the park: stations %v here %v blocks %+v", view.Stations, view.Here, view.Blocks)
	}
	if res, err := e.Answer(context.Background(), host.Teams[0].ID, "q", "acorns"); err != nil || res.Accepted {
		t.Errorf("the author's answer still counts: %+v %v", res, err)
	}
	if res, err := e.Answer(context.Background(), host.Teams[0].ID, "q", "horse"); err != nil || !res.Accepted {
		t.Errorf("the host's answer: %+v %v", res, err)
	}
	// Where the author drew the oak is nowhere now.
	away := play.New(f.h.DB)
	away.Locator = fixedLocator{59.33, 18.06}
	if v, _ := away.Snapshot(context.Background(), host.Teams[0].ID); v.Here {
		t.Error("the author's oak still counts")
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/finish-game", nil); w.Code != http.StatusNoContent {
		t.Fatalf("finish game: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/personalise", map[string]any{
		"values": map[string]string{"name": "Alva"}, "answers": map[string][]string{"q": {"horse"}},
	}); w.Code != http.StatusNoContent {
		t.Errorf("personalise between games: %d %s", w.Code, w.Body)
	}
}

// TestParty_HoldResults: a new party keeps each game's results off the
// phones until its host finishes the game, and the host can let them
// show as the teams play instead.
func TestParty_HoldResults(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	ctx := context.Background()

	_, token := f.book(t, 5)
	v := f.host(t, token)
	if !v.HoldResults {
		t.Fatal("a new party should hold its results")
	}
	if w := f.join(t, v.playerToken(), "Owls", 3); w.Code != http.StatusCreated {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	v = f.host(t, token)
	team := v.Teams[0].ID
	e := play.New(f.h.DB)
	if _, err := e.Station(ctx, team, v.Stations[0].Code); err != nil {
		t.Fatal(err)
	}
	res, err := e.Answer(ctx, team, "q", "acorns")
	if err != nil || !res.Accepted {
		t.Fatalf("answer: %+v %v", res, err)
	}
	if !res.View.ResultsHeld || !res.View.HideScore {
		t.Errorf("while the game is on: held %v, score hidden %v", res.View.ResultsHeld, res.View.HideScore)
	}

	if w := call(t, f.public, "POST", "/api/party/"+token+"/finish-game", nil); w.Code != http.StatusNoContent {
		t.Fatalf("finish game: %d %s", w.Code, w.Body)
	}
	view, err := e.Snapshot(ctx, team)
	if err != nil {
		t.Fatal(err)
	}
	if view.ResultsHeld || view.HideScore {
		t.Errorf("after the game: held %v, score hidden %v", view.ResultsHeld, view.HideScore)
	}
	// The game keeps what it spent paused, for the live board's clock.
	var paused sql.NullInt64
	if err := f.h.DB.QueryRow(`SELECT paused_seconds FROM event_rounds WHERE round = 1`).Scan(&paused); err != nil || !paused.Valid {
		t.Errorf("game 1 paused_seconds = %+v, %v", paused, err)
	}

	// Turned off, the next game shows its results as it goes.
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/hold-results", map[string]bool{"hold": false}); w.Code != http.StatusNoContent {
		t.Fatalf("hold off: %d %s", w.Code, w.Body)
	}
	if f.host(t, token).HoldResults {
		t.Error("still holding")
	}
	if w := f.join(t, v.playerToken(), "Foxes", 1); w.Code != http.StatusCreated {
		t.Fatalf("join game 2: %d %s", w.Code, w.Body)
	}
	fox := f.host(t, token).Teams[0].ID
	if view, err := e.Snapshot(ctx, fox); err != nil || view.ResultsHeld {
		t.Errorf("game 2 with results shown: held %v, %v", view.ResultsHeld, err)
	}
}
