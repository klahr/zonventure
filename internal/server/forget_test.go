package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// A player erases their data from the phone's menu after the event has
// ended, which signed everyone out of the game: the request succeeds,
// and afterwards the session is gone for erasing too.
func TestPlayerForgetAfterPlaying(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hello", Type: graph.Puzzle, Title: "Greeting", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("Say hello.", nil),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "hello"}}}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "hello"}, {From: "hello", FromPort: "solved", To: "end"}},
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
	var team struct {
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Alpha", "member_names": []string{"alice"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := newClient(t, ts)
	if s := alice.do("GET", "/play?t="+team.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("alice login = %d", s)
	}
	if s := alice.doAny("POST", "/api/player/play/answer", map[string]string{"node": "hello", "guess": "nope", "client_action_id": "a-1"}, nil); s != 200 {
		t.Fatalf("answer = %d", s)
	}
	if s := alice.doAny("POST", "/api/player/play/answer", map[string]string{"node": "hello", "guess": "hello", "client_action_id": "a-2"}, nil); s != 200 {
		t.Fatalf("finishing answer = %d", s)
	}
	// The event is over, so the game refuses the session…
	if s := alice.doAny("GET", "/api/player/play", nil, nil); s != 401 {
		t.Fatalf("after the event ended, play should refuse the session: %d", s)
	}
	// …but the player can still erase their data from the phone.
	if s := alice.doAny("POST", "/api/player/forget", map[string]any{}, nil); s/100 != 2 {
		t.Fatalf("forget = %d", s)
	}
	if s := alice.doAny("POST", "/api/player/forget", map[string]any{}, nil); s != 401 {
		t.Errorf("after forgetting, the session should be gone: %d", s)
	}
}
