package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/trail"
)

type trailView struct {
	Slug        string          `json:"slug"`
	Title       string          `json:"title"`
	Cards       []trail.Card    `json:"cards"`
	Revision    int             `json:"revision"`
	Problems    []trail.Problem `json:"problems"`
	Editable    bool            `json:"editable"`
	Published   bool            `json:"published"`
	Unpublished bool            `json:"unpublished"`
	Printed     bool            `json:"printed"`
	Reprint     bool            `json:"reprint"`
	Theme       string          `json:"theme"`
}

func (f *partyFixture) trailRoutes() {
	f.consoleRoute("POST", "/api/admin/trails", f.h.CreateTrail)
	f.consoleRoute("GET", "/api/admin/adventures/{slug}/cards", f.h.GetTrail)
	f.consoleRoute("PUT", "/api/admin/adventures/{slug}/cards", f.h.PutTrail)
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/cards/publish", f.h.PublishTrail)
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/cards/designer", f.h.OpenTrailInDesigner)
	f.consoleRoute("GET", "/api/admin/adventures/{slug}/cards.pdf", f.h.TrailPDF)
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/host", f.h.HostAdventure)
	f.consoleRoute("PUT", "/api/admin/adventures/{slug}/cards/theme", f.h.PutTrailTheme)
}

// TestTrail_Theme: a starter theme's look becomes the adventure's own,
// validated as any look is; its id is kept to show it chosen.
func TestTrail_Theme(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.trailRoutes()
	slug := f.newTrail(t, "birthday")
	path := "/api/admin/adventures/" + slug + "/cards/theme"
	look := map[string]any{"accent": "#e91e63", "accent_secondary": "#ff9800", "background": "#fff8f0", "text": "#3b1d2a",
		"heading_font": "chewy", "backdrop": map[string]any{"kind": "confetti", "opacity": 0.5, "scale": 100}}
	if w := call(t, f.console, "PUT", path, map[string]any{"id": "party", "look": look}); w.Code != http.StatusNoContent {
		t.Fatalf("theme: %d %s", w.Code, w.Body)
	}
	var accent, font string
	_ = f.h.DB.QueryRow(`SELECT accent_color, COALESCE(heading_font, '') FROM adventures WHERE slug = $1`, slug).Scan(&accent, &font)
	if accent != "#e91e63" || font != "chewy" {
		t.Errorf("look saved: accent %q heading font %q", accent, font)
	}
	if v := f.trail(t, slug); v.Theme != "party" {
		t.Errorf("theme = %q", v.Theme)
	}
	for name, body := range map[string]map[string]any{
		"bad id":     {"id": "Party!", "look": look},
		"bad colour": {"id": "x", "look": map[string]any{"accent": "red"}},
		"picture":    {"id": "x", "look": map[string]any{"backdrop": map[string]any{"kind": "picture", "media": 1, "opacity": 1, "scale": 100}}},
	} {
		if w := call(t, f.console, "PUT", path, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}

func (f *partyFixture) newTrail(t *testing.T, template string) string {
	t.Helper()
	w := call(t, f.console, "POST", "/api/admin/trails", map[string]any{"template": template, "language": "sv"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create trail: %d %s", w.Code, w.Body)
	}
	var out struct{ Slug string }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out.Slug
}

func (f *partyFixture) trail(t *testing.T, slug string) trailView {
	t.Helper()
	w := call(t, f.console, "GET", "/api/admin/adventures/"+slug+"/cards", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get trail: %d %s", w.Code, w.Body)
	}
	var v trailView
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return v
}

// TestTrail_Edit: a trail starts from a template, saves unfinished, is
// published only when it can be played, refuses a stale save, and once
// changed in the designer is the designer's.
func TestTrail_Edit(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.trailRoutes()
	slug := f.newTrail(t, "kids")
	v := f.trail(t, slug)
	if v.Title != "Skattjakt" || len(v.Cards) != 3 || !v.Editable || len(v.Problems) != 0 || v.Published || !v.Unpublished {
		t.Fatalf("new trail: %+v", v)
	}

	cards := append(v.Cards, trail.Card{ID: trail.NewID(), Kind: trail.Quiz, Options: []string{"", "", ""}})
	put := func(body map[string]any) *trailView {
		w := call(t, f.console, "PUT", "/api/admin/adventures/"+slug+"/cards", body)
		if w.Code != http.StatusOK {
			t.Fatalf("save: %d %s", w.Code, w.Body)
		}
		var out trailView
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return &out
	}
	saved := put(map[string]any{"cards": cards, "revision": v.Revision, "title": "Elsas skattjakt"})
	if saved.Revision != v.Revision+1 || len(saved.Problems) == 0 || saved.Title != "Elsas skattjakt" {
		t.Fatalf("unfinished save: %+v", saved)
	}
	if w := call(t, f.console, "PUT", "/api/admin/adventures/"+slug+"/cards", map[string]any{"cards": cards, "revision": v.Revision}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"changed"`) {
		t.Errorf("stale save: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.console, "POST", "/api/admin/adventures/"+slug+"/cards/publish", nil); w.Code != http.StatusBadRequest {
		t.Errorf("publishing an unfinished trail: %d", w.Code)
	}
	saved = put(map[string]any{"cards": cards[:3], "revision": saved.Revision})
	if w := call(t, f.console, "POST", "/api/admin/adventures/"+slug+"/cards/publish", nil); w.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	if v := f.trail(t, slug); !v.Published || v.Unpublished {
		t.Errorf("after publishing: %+v", v)
	}

	// Someone changes the draft in the designer: the creator leaves it be.
	var advID int64
	_ = f.h.DB.QueryRow(`SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&advID)
	g := trail.Compile(cards[:2], "sv")
	if _, _, err := graphstore.SaveDraft(context.Background(), f.h.DB, advID, 0, g); err != nil {
		t.Fatal(err)
	}
	if v := f.trail(t, slug); v.Editable {
		t.Error("still editable after a designer change")
	}
	if w := call(t, f.console, "PUT", "/api/admin/adventures/"+slug+"/cards", map[string]any{"cards": cards[:3], "revision": saved.Revision}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "designer") {
		t.Errorf("save after a designer change: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.console, "POST", "/api/admin/adventures/"+slug+"/cards/designer", nil); w.Code != http.StatusNoContent {
		t.Fatalf("handover: %d", w.Code)
	}
	if w := call(t, f.console, "GET", "/api/admin/adventures/"+slug+"/cards", nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not_trail") {
		t.Errorf("after handover: %d %s", w.Code, w.Body)
	}
}

// TestTrail_HostedGame: a trail is hosted and played — no location, the
// quiz answered with 1/X/2 and scored at the end, the typed answer given
// once — and cards changed between games play from the next one.
func TestTrail_HostedGame(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.trailRoutes()
	slug := f.newTrail(t, "kids")
	if w := call(t, f.console, "POST", "/api/admin/adventures/"+slug+"/host", map[string]any{"blocks": 1}); w.Code != http.StatusCreated {
		t.Fatalf("host it: %d %s", w.Code, w.Body)
	}
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil {
		t.Fatal("no host link mailed")
	}
	host := m[1]
	var hv struct {
		Trail     bool   `json:"trail"`
		PlayerURL string `json:"player_url"`
	}
	_ = json.Unmarshal(call(t, f.public, "GET", "/api/party/"+host, nil).Body.Bytes(), &hv)
	if !hv.Trail {
		t.Error("host page does not know it is a trail")
	}
	player := hv.PlayerURL[strings.LastIndex(hv.PlayerURL, "/")+1:]
	if w := f.join(t, player, "Ugglor", 3); w.Code != http.StatusCreated {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	var teamID int64
	if err := f.h.DB.QueryRow(`SELECT id FROM teams WHERE name = 'Ugglor'`).Scan(&teamID); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	e := play.New(f.h.DB)
	v := f.trail(t, slug)
	quiz1, quiz2, answer := "card-"+v.Cards[0].ID, "card-"+v.Cards[1].ID, "card-"+v.Cards[2].ID
	view, err := e.Snapshot(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Node != quiz1 || view.WatchPosition || view.Zone != nil {
		t.Fatalf("first card: node %q watch %v zone %v", view.Node, view.WatchPosition, view.Zone)
	}
	if _, err := e.Pick(ctx, teamID, quiz1, "q", "X"); err != nil { // right
		t.Fatal(err)
	}
	if _, err := e.Continue(ctx, teamID, quiz1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pick(ctx, teamID, quiz2, "q", "2"); err != nil { // wrong
		t.Fatal(err)
	}
	if _, err := e.Continue(ctx, teamID, quiz2); err != nil {
		t.Fatal(err)
	}
	res, err := e.Answer(ctx, teamID, answer, "blå") // wrong, and only one try
	if err != nil {
		t.Fatal(err)
	}
	if !res.View.Finished {
		t.Fatalf("a wrong answer did not move on: %+v", res.View)
	}
	ranked, err := e.EventRanking(ctx, mustEventID(t, f))
	if err != nil || len(ranked) != 1 || ranked[0].Score != 1 {
		t.Fatalf("score: %+v %v", ranked, err)
	}

	// Between games the cards change; the next game plays them.
	if w := call(t, f.public, "POST", "/api/party/"+host+"/finish-game", nil); w.Code != http.StatusNoContent {
		t.Fatalf("finish: %d", w.Code)
	}
	v.Cards[0].Text = "Hur många ben har en bläckfisk?"
	v.Cards[0].Options = []string{"6", "8", "10"}
	if w := call(t, f.console, "PUT", "/api/admin/adventures/"+slug+"/cards", map[string]any{"cards": v.Cards[:1], "revision": v.Revision}); w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	if w := f.join(t, player, "Rävar", 2); w.Code != http.StatusCreated {
		t.Fatalf("join game 2: %d %s", w.Code, w.Body)
	}
	var nodes int
	if err := f.h.DB.QueryRow(`
		SELECT jsonb_array_length(g.graph->'nodes') FROM events e JOIN adventure_graphs g ON g.id = e.graph_id WHERE e.party`).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if nodes != 3 { // start, one card, end
		t.Errorf("game 2 plays %d nodes, want the edited trail's 3", nodes)
	}
}

func mustEventID(t *testing.T, f *partyFixture) int64 {
	t.Helper()
	var id int64
	if err := f.h.DB.QueryRow(`SELECT id FROM events WHERE party`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestTrail_PDF: the cards and the answer key print, and printing the
// cards is remembered until they change.
func TestTrail_PDF(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.trailRoutes()
	slug := f.newTrail(t, "birthday")
	for _, q := range []string{"", "?answers=1"} {
		w := call(t, f.console, "GET", "/api/admin/adventures/"+slug+"/cards.pdf"+q, nil)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") || w.Header().Get("Content-Type") != "application/pdf" {
			t.Fatalf("pdf %q: %d %.40s", q, w.Code, w.Body)
		}
	}
	v := f.trail(t, slug)
	if !v.Printed || v.Reprint {
		t.Fatalf("after printing: %+v", v)
	}
	if w := call(t, f.console, "PUT", "/api/admin/adventures/"+slug+"/cards", map[string]any{"cards": v.Cards[1:], "revision": v.Revision}); w.Code != http.StatusOK {
		t.Fatal(w.Body)
	}
	if v := f.trail(t, slug); !v.Reprint {
		t.Error("no reprint after the cards changed")
	}
}
