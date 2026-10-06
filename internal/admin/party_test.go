package admin_test

import (
	"context"
	"encoding/json"
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
	console http.Handler
	public  http.Handler
	orgID   int64
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
		PublicURL: "https://zv.example.com", Provider: &fakeProvider{}}

	user := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &f.orgID, Email: "boss@acme.com", EmailVerified: true}
	console := chi.NewRouter()
	console.Use(injectUser(user))
	console.Get("/api/admin/party-offers", f.h.ListPartyOffers)
	console.Put("/api/admin/party-offers/{slug}", f.h.PutPartyOffer)
	f.console = console

	public := chi.NewRouter()
	public.Get("/api/o/{slug}/parties", f.h.ListPartiesByOrg)
	public.Post("/api/o/{slug}/parties/{offerID}/book", f.h.BookParty)
	public.Post("/api/party/resend", f.h.ResendPartyLinks)
	public.Get("/api/party/{token}", f.h.GetHostParty)
	public.Post("/api/party/{token}/teams", f.h.AddHostTeam)
	public.Delete("/api/party/{token}/teams/{teamID}", f.h.DeleteHostTeam)
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
	Name     string `json:"name"`
	State    string `json:"state"`
	Kids     int    `json:"kids"`
	Stations []struct {
		Zone, Name, Code, URL string
	} `json:"stations"`
	Teams []struct {
		ID   int64 `json:"id"`
		Size int   `json:"size"`
	} `json:"teams"`
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

	// Starting with no team is refused; so is a team past the booking.
	if w := call(t, f.public, "POST", "/api/party/"+token+"/start", nil); w.Code != http.StatusConflict {
		t.Errorf("start with no team: %d, want 409", w.Code)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"name": "Owls", "size": 3}); w.Code != http.StatusCreated {
		t.Fatalf("add team: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"name": "Foxes", "size": 3}); w.Code != http.StatusConflict {
		t.Errorf("team past the booking: %d %s, want 409", w.Code, w.Body)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"name": "Foxes", "size": 2}); w.Code != http.StatusCreated {
		t.Fatalf("second team: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/start", nil); w.Code != http.StatusNoContent {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	v = f.host(t, token)
	if v.State != "running" || len(v.Teams) != 2 {
		t.Fatalf("after start: %+v", v)
	}

	// The party plays its own copy of the graph: stations, without the
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

	// Ending the party keeps it for its results page; the sweeper's
	// test below covers its deletion.
	if w := call(t, f.public, "POST", "/api/party/"+token+"/end", nil); w.Code != http.StatusNoContent {
		t.Fatalf("end: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"name": "Late", "size": 1}); w.Code != http.StatusConflict {
		t.Errorf("team after the end: %d, want 409", w.Code)
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
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"name": "Owls", "size": 4}); w.Code != http.StatusCreated {
		t.Fatalf("team: %d", w.Code)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/start", nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "places_missing") {
		t.Fatalf("start before placing: %d %s", w.Code, w.Body)
	}
	// The host's park, far from where the author drew the oak.
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/places/oak", map[string]any{"lat": 57.70, "lng": 11.97}); w.Code != http.StatusNoContent {
		t.Fatalf("place: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/start", nil); w.Code != http.StatusNoContent {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	// Changing anything now is refused: the party plays as it was set up.
	if w := call(t, f.public, "PUT", "/api/party/"+token+"/mode", map[string]any{"mode": "qr"}); w.Code != http.StatusConflict {
		t.Errorf("mode after start: %d", w.Code)
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
}
