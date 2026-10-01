package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

type playStateDTO struct {
	View *session.View `json:"view"`
}

type playRefusalDTO struct {
	Code   string          `json:"code"`
	Result *session.Result `json:"result"`
}

// The whole graph player loop over HTTP: an author saves a graph, an
// arranger starts the event, and two teammates play it — including the
// two guarantees the offline outbox depends on: a retried action is
// replayed, not re-run, and a teammate's stale submit is refused.
func TestGraphPlayerLoop(t *testing.T) {
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
				Blocks:    graph.LegacyBlocks("Say hello.", nil),
				Outputs:   []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "hello"}}}},
				Responses: []graph.Response{{Text: "Not quite."}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Well done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "hello"},
			{From: "hello", FromPort: "solved", To: "end"},
		},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}

	// The designer's preview of the saved puzzle, through the simulator.
	var sim struct {
		Result session.Result `json:"result"`
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/simulate", map[string]any{
		"envelope": graph.Envelope{Format: graph.FormatV2, Graph: g},
		"action":   map[string]string{"kind": "place", "node": "hello"},
	}, &sim); s != 200 || sim.Result.View.Text != "Say hello." {
		t.Fatalf("simulate place = %d %+v", s, sim.Result.View)
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
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams",
		map[string]any{"name": "Alpha", "member_names": []string{"alice", "bob"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	// A second team keeps the event running after Alpha finishes: the
	// last team finishing completes the event, which ends every player
	// session, and the retries below need a live one.
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams",
		map[string]any{"name": "Beta", "member_names": []string{"carol"}}, nil); s != 201 {
		t.Fatalf("create team = %d", s)
	}

	// Before the start, a player's magic link already lands in the app.
	alice := newClient(t, ts)
	resp, err := alice.c.Get(ts.URL + "/play?t=" + team.Members[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.HasPrefix(resp.Request.URL.Path, "/app/play") {
		t.Fatalf("a magic link should land in /app/play, got %s", resp.Request.URL.Path)
	}
	var st playStateDTO
	if s := alice.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View != nil {
		t.Fatalf("before start: %d %+v", s, st)
	}

	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	if s := alice.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil || st.View.Node != "hello" {
		t.Fatalf("after start: %d %+v", s, st.View)
	}

	var res session.Result
	if s := alice.doAny("POST", "/api/player/play/answer", map[string]string{"node": "hello", "guess": "hi"}, &res); s != 200 || res.Accepted || res.Response != "Not quite." {
		t.Fatalf("wrong answer: %d %+v", s, res)
	}

	body := map[string]string{"node": "hello", "guess": "Hello", "client_action_id": "a-1"}
	if s := alice.doAny("POST", "/api/player/play/answer", body, &res); s != 200 || !res.Accepted || !res.View.Finished {
		t.Fatalf("right answer: %d %+v", s, res)
	}
	var replay session.Result
	if s := alice.doAny("POST", "/api/player/play/answer", body, &replay); s != 200 || !replay.Accepted || replay.Points != res.Points {
		t.Fatalf("a retried action should replay the first response: %d %+v", s, replay)
	}

	bob := newClient(t, ts)
	if s := bob.do("GET", "/play?t="+team.Members[1].Token, nil, nil); s != http.StatusOK {
		t.Fatalf("bob login = %d", s)
	}
	var refused playRefusalDTO
	if s := bob.doAny("POST", "/api/player/play/answer", map[string]string{"node": "hello", "guess": "hello"}, &refused); s != 409 || refused.Code != "finished" {
		t.Fatalf("a teammate answering after the team finished: %d %+v", s, refused)
	}
	if refused.Result == nil || !refused.Result.View.Finished {
		t.Error("a refusal should carry the current view so the client can redraw")
	}
}
