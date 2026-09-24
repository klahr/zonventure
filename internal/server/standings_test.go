package server_test

import (
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A standings block ranks the team among the event's others; the
// organisers' messages reach every team or only those chosen, beside the
// game rather than in it.
func TestStandingsAndMessages(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  []graph.Block{{ID: "rank", Kind: graph.BlockStandings}},
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "q"}, {From: "q", FromPort: "solved", To: "end"}},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
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
	tokens := map[string]string{}
	teamIDs := map[string]int64{}
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		var team struct {
			ID      int64 `json:"id"`
			Members []struct {
				Token string `json:"token"`
			} `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": name, "member_names": []string{strings.ToLower(name)}}, &team); s != 201 {
			t.Fatalf("create team = %d", s)
		}
		tokens[name] = team.Members[0].Token
		teamIDs[name] = team.ID
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	players := map[string]*client{}
	for name, tok := range tokens {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + tok)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		players[name] = c
	}
	// Beta scores; Alpha and Gamma share second place.
	if s := players["Beta"].doAny("POST", "/api/player/play/answer", map[string]string{"node": "q", "guess": "x"}, nil); s != 200 {
		t.Fatalf("answer = %d", s)
	}

	var st playStateDTO
	if s := players["Alpha"].doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
		t.Fatalf("play = %d %+v", s, st.View)
	}
	// The saved puzzle has its title and points blocks too.
	var rank session.BlockView
	for _, b := range st.View.Blocks {
		if b.Kind == graph.BlockStandings {
			rank = b
		}
	}
	if r := rank.Standings; r == nil || r.Rank != 2 || r.Of != 3 || r.Ahead == nil || r.Ahead.Name != "Beta" || r.Ahead.Gap != 10 || r.Behind != nil {
		t.Errorf("standings = %+v", r)
	}

	msgs := "/api/admin/events/" + itoa(ev.ID) + "/messages"
	var all, toBeta struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", msgs, map[string]any{"text": "Lunch at the square at 12."}, &all); s != 201 || all.ID == 0 {
		t.Fatalf("send to all = %d", s)
	}
	if s := arr.do("POST", msgs, map[string]any{"text": "Beta, you missed the turn.", "team_ids": []int64{teamIDs["Beta"]}}, &toBeta); s != 201 {
		t.Fatalf("send to Beta = %d", s)
	}
	for _, bad := range []map[string]any{{"text": "  "}, {"text": "x", "team_ids": []int64{999999}}} {
		if s := arr.do("POST", msgs, bad, nil); s != 400 {
			t.Errorf("send %v = %d, want 400", bad, s)
		}
	}
	inbox := func(name string) []string {
		var out struct {
			Messages []struct {
				Text string `json:"text"`
			} `json:"messages"`
		}
		if s := players[name].doAny("GET", "/api/player/play", nil, &out); s != 200 {
			t.Fatalf("%s play = %d", name, s)
		}
		var texts []string
		for _, m := range out.Messages {
			texts = append(texts, m.Text)
		}
		return texts
	}
	if got := inbox("Beta"); len(got) != 2 || got[0] != "Beta, you missed the turn." {
		t.Errorf("Beta's inbox, newest first = %q", got)
	}
	if got := inbox("Alpha"); len(got) != 1 || got[0] != "Lunch at the square at 12." {
		t.Errorf("Alpha sees only the message to everyone: %q", got)
	}
	if s := arr.do("DELETE", msgs+"/"+itoa(toBeta.ID), nil, nil); s != 204 {
		t.Fatalf("delete = %d", s)
	}
	if got := inbox("Beta"); len(got) != 1 {
		t.Errorf("a deleted message is gone: %q", got)
	}
	var list struct {
		Messages []struct {
			TeamIDs []int64 `json:"team_ids"`
		} `json:"messages"`
	}
	if s := arr.do("GET", msgs, nil, &list); s != 200 || len(list.Messages) != 1 || len(list.Messages[0].TeamIDs) != 0 {
		t.Errorf("organiser's list = %d %+v", s, list)
	}
}
