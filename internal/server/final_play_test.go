package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// The last team finishing ends the event and signs its players out at
// once: a teammate whose phone still showed the question reads the end
// screen from /api/player/final, and can do nothing from it.
func TestFinalAfterEventEnds(t *testing.T) {
	right := []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}}
	event, view, act, _ := playServer(t, graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, Title: "Q", PuzzleCfg: &graph.PuzzleConfig{Outputs: right}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "q"},
			{From: "q", FromPort: "ok", To: "end"},
		},
	})
	crew := event(false)
	ann, bo := crew[0], crew[1]

	if s := bo.doAny("GET", "/api/player/final", nil, nil); s != 404 {
		t.Fatalf("final while the event runs = %d, want 404", s)
	}
	if v := view(bo); v.Node != "q" {
		t.Fatalf("Bo at %q", v.Node)
	}
	if s := act(ann, "answer", map[string]string{"node": "q", "guess": "x"}); s != 200 {
		t.Fatalf("Ann answers: %d", s)
	}

	var ref playRefusalDTO
	if s := bo.doAny("GET", "/api/player/play", nil, &ref); s != 401 || ref.Code != "event_ended" {
		t.Fatalf("Bo's play after the end = %d %q", s, ref.Code)
	}
	var st playStateDTO
	if s := bo.doAny("GET", "/api/player/final", nil, &st); s != 200 || st.View == nil || !st.View.Finished {
		t.Fatalf("Bo's final = %d %+v", s, st.View)
	}
	if s := act(bo, "continue", map[string]string{"node": "end"}); s != 401 {
		t.Fatalf("Bo acting after the end = %d, want 401", s)
	}
}
