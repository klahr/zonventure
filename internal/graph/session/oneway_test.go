package session

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// fork is a one-way choice: two drawers, each carrying on to the same
// archive, neither coming back to the choice.
func fork() *graph.Graph {
	story := func(slug string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks(slug, nil)}}
	}
	return &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "drawers", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, OneWay: true, Branches: []graph.Branch{
				{Port: "left", Label: "Rattling"}, {Port: "right", Label: "Blinking"},
			}}},
			story("rattle"), story("blink"), story("archive"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "drawers"},
			{From: "drawers", FromPort: "left", To: "rattle"},
			{From: "drawers", FromPort: "right", To: "blink"},
			{From: "rattle", FromPort: "out", To: "archive"},
			{From: "blink", FromPort: "out", To: "archive"},
			{From: "archive", FromPort: "out", To: "end"},
		},
	}
}

func TestOneWayChoice(t *testing.T) {
	g := fork()
	if r := valid.Check(g); !r.OK() {
		t.Fatalf("a one-way choice needs no done wire: %+v", r.Issues)
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); tm.Current != "drawers" || len(v.Offer) != 2 || v.CanFinish {
		t.Fatalf("the choice offers both drawers and no way past them: %+v", v)
	}
	res, st, err := tm.Choose("drawers", "right")
	act(t, tm, "blink", res, st, err)
	res, st, err = tm.Continue("blink")
	act(t, tm, "blink done", res, st, err)
	if tm.Current != "archive" {
		t.Fatalf("the team carries on down its branch, not back to the choice: at %s", tm.Current)
	}
}
