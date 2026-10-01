package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

// Steps done together, over HTTP with two phones: a unanimous answer
// that waits for both players, then a two-key launch both must turn.
func TestTogetherOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "vow", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Unanimous: true,
				Blocks:    graph.LegacyBlocks("Say it together.", nil),
				Outputs:   []graph.Output{{Port: "said", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "friends"}}}},
			}},
			{Slug: "launch", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer:  &graph.Answer{Kind: graph.AnswerKeys},
				Outputs: []graph.Output{{Port: "launched", Trigger: graph.OnAnswer, Points: 10}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "vow"},
			{From: "vow", FromPort: "said", To: "launch"},
			{From: "launch", FromPort: "launched", To: "end"},
		},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	arr := newClient(t, ts)
	arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil)
	var ev struct {
		ID int64 `json:"id"`
	}
	arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev)
	var team struct {
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Alpha", "member_names": []string{"Ann", "Bo"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	login := func(token string) *client {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return c
	}
	ann, bo := login(team.Members[0].Token), login(team.Members[1].Token)
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	act := func(c *client, kind string, body map[string]string) session.Result {
		t.Helper()
		var res session.Result
		if s := c.doAny("POST", "/api/player/play/"+kind, body, &res); s != 200 {
			t.Fatalf("%s = %d", kind, s)
		}
		return res
	}
	view := func(c *client) *session.View {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		return st.View
	}

	// Ann has it; the team waits for Bo, and Bo's phone says one agrees.
	if res := act(ann, "answer", map[string]string{"node": "vow", "guess": "friends"}); res.Accepted || res.Waiting == nil || res.Waiting.Done != 1 || res.Waiting.Of != 2 {
		t.Fatalf("Ann alone: %+v", res)
	}
	if v := view(bo); v.Node != "vow" || v.Together == nil || v.Together.Done != 1 || v.Together.Mine {
		t.Fatalf("Bo's view: node %q, %+v", v.Node, v.Together)
	}
	if res := act(bo, "answer", map[string]string{"node": "vow", "guess": "Friends"}); !res.Accepted {
		t.Fatalf("both agree: %+v", res)
	}

	// The launch: Ann's key alone waits, Bo's opens it.
	if res := act(ann, "turn", map[string]string{"node": "launch"}); res.Accepted || res.Waiting == nil || res.Waiting.Done != 1 {
		t.Fatalf("Ann's key: %+v", res)
	}
	if v := view(bo); v.Input == nil || v.Input.Kind != "keys" || v.Together == nil || v.Together.Done != 1 {
		t.Fatalf("Bo's launch view: %+v %+v", v.Input, v.Together)
	}
	if res := act(bo, "turn", map[string]string{"node": "launch"}); !res.Accepted || res.Points != 10 || !res.View.Finished {
		t.Fatalf("Bo's key: %+v", res)
	}
}
