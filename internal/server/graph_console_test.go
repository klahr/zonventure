package server_test

import (
	"io"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// The classic console for an adventure that plays a graph: its lane
// pages can no longer be edited, a lane skip is refused, and the event
// and adventure pages carry the graph's progress and steps.
func TestConsoleOnGraphAdventure(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	// Before the graph is published, the lane pages are still the game.
	if s := pub.do("POST", "/api/admin/adventures/demo/pages", map[string]any{"title": "Lane page", "prompt": "?", "lat": 59.3, "lng": 18.0}, nil); s == 409 {
		t.Fatalf("a lane edit before any graph was refused")
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, Title: "The door", PuzzleCfg: &graph.PuzzleConfig{
				Zone:    "square",
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}},
			}},
			{Slug: "s", Type: graph.Story, Title: "On the way"},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "q"}, {From: "q", FromPort: "solved", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
		Zones: []graph.Zone{{Slug: "square", Name: "The square", Lat: 59.33, Lng: 18.06, RadiusM: 5000}},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	var refused struct {
		Code string `json:"code"`
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/pages", map[string]any{"title": "Lane page", "prompt": "?", "lat": 59.3, "lng": 18.0}, &refused); s != 409 || refused.Code != "graph_adventure" {
		t.Errorf("a lane edit to a graph adventure = %d %q, want 409 graph_adventure", s, refused.Code)
	}
	var detail struct {
		Graph *struct {
			Steps []struct {
				Slug, Type, Title, Zone string
			} `json:"steps"`
			PuzzleCount int `json:"puzzle_count"`
			Zones       []struct {
				Slug string `json:"slug"`
			} `json:"zones"`
		} `json:"graph"`
	}
	if s := pub.do("GET", "/api/admin/adventures/demo", nil, &detail); s != 200 || detail.Graph == nil {
		t.Fatalf("adventure detail = %d %+v", s, detail)
	}
	if gr := detail.Graph; len(gr.Steps) != 3 || gr.Steps[0].Title != "The door" || gr.Steps[0].Zone != "square" || gr.PuzzleCount != 1 || len(gr.Zones) != 1 {
		t.Errorf("graph summary = %+v", gr)
	}

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	teamIDs := map[string]int64{}
	players := map[string]*client{}
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		var team struct {
			ID      int64 `json:"id"`
			Members []struct {
				Token string `json:"token"`
			} `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": name, "member_names": []string{name}}, &team); s != 201 {
			t.Fatalf("create team = %d", s)
		}
		teamIDs[name] = team.ID
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + team.Members[0].Token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		players[name] = c
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	if s := players["Alpha"].doAny("POST", "/api/player/location", map[string]any{"lat": 59.33, "lng": 18.06, "accuracy": 5}, nil); s != 200 && s != 204 {
		t.Fatalf("location = %d", s)
	}
	if s := players["Alpha"].doAny("POST", "/api/player/play/answer", map[string]string{"node": "q", "guess": "x"}, nil); s != 200 {
		t.Fatalf("answer = %d", s)
	}

	if s := arr.do("POST", "/api/admin/teams/"+itoa(teamIDs["Beta"])+"/unlock", nil, nil); s != 409 {
		t.Errorf("a lane skip on a graph event = %d, want 409", s)
	}
	var ed struct {
		Event struct {
			Engine string `json:"engine"`
		} `json:"event"`
		Teams []struct {
			Name  string   `json:"name"`
			Score *float64 `json:"score"`
			Rank  int      `json:"rank"`
			Node  string   `json:"node"`
		} `json:"teams"`
		Graph *struct {
			PuzzleCount int               `json:"puzzle_count"`
			Titles      map[string]string `json:"titles"`
		} `json:"graph"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &ed); s != 200 || ed.Event.Engine != "graph" || ed.Graph == nil || ed.Graph.Titles["q"] != "The door" {
		t.Fatalf("event detail = %d %+v", s, ed)
	}
	for _, tm := range ed.Teams {
		want := map[string]struct {
			score float64
			rank  int
			node  string
		}{"Alpha": {10, 1, "s"}, "Beta": {0, 2, "q"}, "Gamma": {0, 2, "q"}}[tm.Name]
		if tm.Score == nil || *tm.Score != want.score || tm.Rank != want.rank || tm.Node != want.node {
			t.Errorf("%s = score %v rank %d node %q, want %+v", tm.Name, tm.Score, tm.Rank, tm.Node, want)
		}
	}
	resp, err := arr.c.Get(ts.URL + "/api/admin/events/" + itoa(ev.ID) + "/recap.pdf")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(string(body), "%PDF") {
		t.Errorf("PDF report = %d, %d bytes", resp.StatusCode, len(body))
	}
}
