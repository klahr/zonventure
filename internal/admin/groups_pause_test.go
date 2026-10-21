package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A group's since leaves out the event's pauses since it reached its
// step: one that set off with 60 s of pauses behind it, and 300 s now,
// has 240 s of pause to skip.
func TestGroupsSinceLeavesPausesOut(t *testing.T) {
	t.Parallel()
	g := &graph.Graph{Nodes: []graph.Node{
		{Slug: "split", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a", Label: "North"}, {Port: "b"}}}},
		{Slug: "pa", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{}},
	}}
	entered := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal([]session.Strand{{Port: "a", Current: "pa", EnteredAt: entered, Paused: 60}})
	gs := groupsOf(g, "split", raw, 300)
	if len(gs) != 1 || gs[0].Since != "2026-05-01T12:04:00Z" {
		t.Fatalf("groups %+v, want since 12:04", gs)
	}
	if len(gs[0].Labels) != 1 || gs[0].Labels[0] != "North" {
		t.Errorf("labels %v", gs[0].Labels)
	}
}
