package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

func boundedGraph(lo, hi int) graph.Graph {
	return graph.Graph{
		MinPlayers: lo,
		MaxPlayers: hi,
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "intro", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Welcome.", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "intro"}, {From: "intro", FromPort: "out", To: "end"}},
	}
}

// A team of three on an adventure for at most two: the third cannot
// join, so its phone cannot play.
func TestMaxPlayersOverHTTP(t *testing.T) {
	event, view, act, _ := playServer(t, boundedGraph(0, 2))
	crew := event(true)
	if v := view(crew[0]); v.Node != "intro" {
		t.Fatalf("first player is at %q", v.Node)
	}
	if s := crew[2].doAny("GET", "/api/player/play", nil, nil); s == 200 {
		t.Error("a third player joined a team for at most two")
	}
	if s := act(crew[0], "continue", map[string]string{"node": "intro"}); s != 200 {
		t.Errorf("the two who joined could not play: %d", s)
	}
}

// A team of three on an adventure for at least four waits, and its
// phones say so.
func TestMinPlayersOverHTTP(t *testing.T) {
	event, view, act, _ := playServer(t, boundedGraph(4, 0))
	crew := event(true)
	if s := act(crew[0], "continue", map[string]string{"node": "intro"}); s != 403 {
		t.Errorf("three of four moved on: %d, want 403", s)
	}
	if v := view(crew[1]); v.PlayersIn != 3 || v.PlayersMin != 4 {
		t.Errorf("the phone says %d of %d", v.PlayersIn, v.PlayersMin)
	}
}
