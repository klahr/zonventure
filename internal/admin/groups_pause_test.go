package admin

import (
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
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "split", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a", Label: "North"}, {Port: "b"}}}},
			{Slug: "pa", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer}}}},
			{Slug: "join", Type: graph.Join},
		},
		Edges: []graph.Edge{{From: "split", FromPort: "a", To: "pa"}, {From: "split", FromPort: "b", To: "join"}, {From: "pa", FromPort: "solved", To: "join"}},
	}
	entered := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	gs := groupsOf(g.Splits(), "split", []session.Strand{
		{Port: "a", Cursor: session.Cursor{Current: "pa", NodeEnteredAt: entered, NodePaused: 60}},
		{Port: "b", Cursor: session.Cursor{Current: "join", NodeEnteredAt: entered, NodePaused: 60}},
	}, []bool{true, false}, 300)
	if len(gs) != 2 || gs[0].Since != "2026-05-01T12:04:00Z" || gs[0].Arrived || !gs[1].Arrived {
		t.Fatalf("groups %+v, want a since 12:04, b waiting at the join", gs)
	}
	if gs[0].Label != "North" || !gs[0].Rescuable || gs[1].Rescuable {
		t.Errorf("label %q, rescuable %v %v: as told", gs[0].Label, gs[0].Rescuable, gs[1].Rescuable)
	}
}
