package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/klahr/zonventure/internal/auth"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/valid"
)

type graphDTO struct {
	Version  int            `json:"version"`
	Envelope graph.Envelope `json:"envelope"`
	Report   valid.Report   `json:"report"`
}

type graphStateDTO struct {
	Envelope  graph.Envelope `json:"envelope"`
	Report    valid.Report   `json:"report"`
	Dirty     bool           `json:"dirty"`
	Published *struct {
		Version int    `json:"version"`
		Label   string `json:"label"`
	} `json:"published"`
}

type refusalDTO struct {
	Error  string        `json:"error"`
	Code   string        `json:"code"`
	Report *valid.Report `json:"report"`
}

// doAny is client.do, but it decodes the body whatever the status. The
// shared helper skips error bodies on purpose; these tests need them,
// because a refusal's code and report are the contract being tested.
func (c *client) doAny(method, path string, body any, into any) int {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		if tok := c.csrfCookie(); tok != "" {
			req.Header.Set(auth.CSRFHeaderName, tok)
		}
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil && err != io.EOF {
			c.t.Fatalf("decode %s %s (%d): %v", method, path, resp.StatusCode, err)
		}
	}
	return resp.StatusCode
}

// The whole authoring loop over HTTP: an adventure with no graph, a
// conversion from its lanes, a save, a reload, a refused broken save.
func TestGraphAuthoringLoop(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	// No graph yet.
	var refused refusalDTO
	if s := pub.doAny("GET", "/api/admin/adventures/demo/graph", nil, &refused); s != 404 || refused.Code != "graph_not_found" {
		t.Fatalf("GET before any save = %d %+v; want 404 graph_not_found", s, refused)
	}

	// Convert the seeded three-page linear adventure. Not saved.
	var converted graphDTO
	if s := pub.do("POST", "/api/admin/adventures/demo/graph/convert", nil, &converted); s != 200 {
		t.Fatalf("convert = %d", s)
	}
	if !converted.Report.OK() {
		t.Fatalf("the converted graph should validate: %v", converted.Report.Errors())
	}
	g := converted.Envelope.Graph
	// start + three pages + end.
	if len(g.Nodes) != 5 {
		t.Errorf("converted graph has %d nodes, want 5", len(g.Nodes))
	}
	// The seed's second page is gated at 50 m; that must become a zone.
	if len(g.Zones) != 1 || g.Zones[0].RadiusM != 50 {
		t.Errorf("zones = %+v; want the landmark's 50 m gate", g.Zones)
	}
	if s := pub.do("GET", "/api/admin/adventures/demo/graph", nil, nil); s != 404 {
		t.Errorf("convert must not save; GET after convert = %d, want 404", s)
	}

	// Autosave it as the draft. Nothing is published yet.
	var state graphStateDTO
	state = graphStateDTO{}
	if s := pub.do("PUT", "/api/admin/adventures/demo/graph", converted.Envelope, &state); s != 200 || state.Published != nil || !state.Dirty {
		t.Fatalf("draft save = %d %+v", s, state)
	}

	// A broken draft still saves — autosave must never refuse an edit
	// in progress — and reports its errors.
	broken := converted.Envelope
	broken.Graph.Edges = nil
	state = graphStateDTO{}
	if s := pub.do("PUT", "/api/admin/adventures/demo/graph", broken, &state); s != 200 || state.Report.OK() {
		t.Fatalf("broken draft save = %d, report ok=%v", s, state.Report.OK())
	}
	// Publishing it is refused with the full report.
	refused = refusalDTO{}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, &refused); s != 422 || refused.Code != "graph_invalid" {
		t.Fatalf("publishing a broken draft = %d %+v; want 422 graph_invalid", s, refused)
	}
	if refused.Report == nil || len(refused.Report.Errors()) == 0 {
		t.Error("the refusal should carry every error, so the editor can mark them all")
	}

	// Fix the draft and publish it, named.
	pub.do("PUT", "/api/admin/adventures/demo/graph", converted.Envelope, nil)
	state = graphStateDTO{}
	if s := pub.do("POST", "/api/admin/adventures/demo/graph/publish", map[string]string{"label": "Launch"}, &state); s != 200 ||
		state.Published == nil || state.Published.Version != 1 || state.Published.Label != "Launch" || state.Dirty {
		t.Fatalf("publish = %d %+v", s, state)
	}

	// Reload: the draft, clean against what is published.
	var loaded graphStateDTO
	if s := pub.do("GET", "/api/admin/adventures/demo/graph", nil, &loaded); s != 200 {
		t.Fatalf("reload = %d", s)
	}
	if loaded.Dirty || len(loaded.Envelope.Graph.Nodes) != 5 || loaded.Envelope.Format != graph.FormatV2 {
		t.Errorf("reloaded %+v", loaded)
	}

	// An edit makes the draft dirty; publishing makes version 2.
	edited := converted.Envelope
	edited.Graph.Nodes = append([]graph.Node(nil), edited.Graph.Nodes...)
	edited.Graph.Nodes[1].Title = "Hello there"
	state = graphStateDTO{}
	pub.do("PUT", "/api/admin/adventures/demo/graph", edited, &state)
	if !state.Dirty {
		t.Error("a changed title is an unpublished change")
	}
	state = graphStateDTO{}
	pub.do("POST", "/api/admin/adventures/demo/graph/publish", nil, &state)

	// History, preview and rollback.
	var hist struct {
		Versions []struct {
			Version   int    `json:"version"`
			Label     string `json:"label"`
			Published bool   `json:"published"`
		} `json:"versions"`
	}
	pub.do("GET", "/api/admin/adventures/demo/graph/versions", nil, &hist)
	if len(hist.Versions) != 2 || !hist.Versions[0].Published || hist.Versions[1].Label != "Launch" {
		t.Fatalf("history = %+v", hist.Versions)
	}
	var v1 graphDTO
	if s := pub.do("GET", "/api/admin/adventures/demo/graph/versions/1", nil, &v1); s != 200 || v1.Envelope.Graph.Nodes[1].Title == "Hello there" {
		t.Errorf("version 1 preview = %d %+v", s, v1.Envelope.Graph.Nodes[1])
	}
	state = graphStateDTO{}
	if s := pub.do("POST", "/api/admin/adventures/demo/graph/versions/1/publish", nil, &state); s != 200 || state.Published.Version != 1 || !state.Dirty {
		t.Errorf("rollback = %d %+v; the draft keeps the newer edit, so it is dirty", s, state)
	}
	if s := pub.do("PATCH", "/api/admin/adventures/demo/graph/versions/2", map[string]string{"label": "Friendlier"}, nil); s != 204 {
		t.Errorf("label = %d", s)
	}
}

func TestGraphRejectsMalformedEnvelopes(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	pub := newClient(t, ts)
	pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil)

	for name, body := range map[string]any{
		"v1 format":     map[string]any{"format": "zonventure-adventure-v1", "graph": map[string]any{"nodes": []any{}, "edges": []any{}}},
		"unknown field": map[string]any{"format": graph.FormatV2, "graph": map[string]any{"nodes": []any{}, "edges": []any{}}, "extra": 1},
		"not an object": []int{1, 2, 3},
	} {
		var refused refusalDTO
		if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", body, &refused); s != 400 || refused.Code != "graph_malformed" {
			t.Errorf("%s: got %d %+v; want 400 graph_malformed", name, s, refused)
		}
	}
}

// The graph carries every answer, so only someone who can edit the
// adventure may see it. The seeded demo is in the global catalog, which
// an org admin cannot edit.
func TestGraphRequiresEditRights(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/admin/adventures/demo/graph"},
		{"POST", "/api/admin/adventures/demo/graph/convert"},
		{"POST", "/api/admin/adventures/demo/graph/validate"},
		{"PUT", "/api/admin/adventures/demo/graph"},
		{"POST", "/api/admin/adventures/demo/graph/publish"},
		{"GET", "/api/admin/adventures/demo/graph/versions"},
	} {
		if s := boss.do(c.method, c.path, map[string]any{}, nil); s != 403 {
			t.Errorf("%s %s as an org admin on a global adventure = %d, want 403", c.method, c.path, s)
		}
	}

	anon := newClient(t, ts)
	if s := anon.do("GET", "/api/admin/adventures/demo/graph", nil, nil); s != 401 && s != 403 {
		t.Errorf("anonymous GET = %d, want 401 or 403", s)
	}

	pub := newClient(t, ts)
	pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil)
	if s := pub.do("GET", "/api/admin/adventures/no-such-adventure/graph", nil, nil); s != 404 {
		t.Errorf("unknown adventure = %d, want 404", s)
	}
}

// A new adventure is listed with nothing to play, so the console offers
// to design it rather than schedule it; its puzzle count follows its
// designer draft, and publishing makes it playable.
func TestAdventureListFollowsTheDesigner(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := arr.do("POST", "/api/admin/adventures", map[string]string{"slug": "fresh", "title": "Fresh"}, nil); s != 201 {
		t.Fatalf("create = %d", s)
	}
	type row struct {
		Slug      string `json:"slug"`
		PageCount int    `json:"page_count"`
		Playable  bool   `json:"playable"`
	}
	listed := func(slug string) row {
		t.Helper()
		var list struct {
			Adventures []row `json:"adventures"`
		}
		if s := arr.do("GET", "/api/admin/adventures", nil, &list); s != 200 {
			t.Fatalf("list = %d", s)
		}
		for _, a := range list.Adventures {
			if a.Slug == slug {
				return a
			}
		}
		t.Fatalf("%s is not listed", slug)
		return row{}
	}
	if a := listed("fresh"); a.Playable || a.PageCount != 0 {
		t.Errorf("a new adventure has nothing to play: %+v", a)
	}
	if a := listed("demo"); !a.Playable || a.PageCount != 3 {
		t.Errorf("the lane demo plays its three pages: %+v", a)
	}

	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Blocks: graph.LegacyBlocks("?", nil), Outputs: []graph.Output{
				{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: graph.PortOut, To: "q"}, {From: "q", FromPort: "solved", To: "end"}},
	}
	if s := arr.do("PUT", "/api/admin/adventures/fresh/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save draft = %d", s)
	}
	if a := listed("fresh"); a.Playable || a.PageCount != 1 {
		t.Errorf("a draft counts its puzzles but is not playable until published: %+v", a)
	}
	if s := arr.do("POST", "/api/admin/adventures/fresh/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	if a := listed("fresh"); !a.Playable || a.PageCount != 1 {
		t.Errorf("published, it plays: %+v", a)
	}
}
