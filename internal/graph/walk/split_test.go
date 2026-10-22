package walk

import (
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// splitGraph is start → split{a, b}; a: pa → join, b: pb → join;
// join → after → end. The walk itself never forks: it parks at the
// split, and each group's walk is a separate Advance out of it.
func splitGraph() *graph.Graph {
	sp := node("split", graph.Split)
	sp.SplitCfg = &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}
	return &graph.Graph{
		Nodes: []graph.Node{node("start", graph.Start), sp, puzzle("pa"), puzzle("pb"), node("join", graph.Join), puzzle("after"), node("end", graph.End)},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "split"),
			edge("split", "a", "pa"), edge("split", "b", "pb"),
			edge("pa", "solved", "join"), edge("pb", "solved", "join"),
			edge("join", graph.PortOut, "after"), edge("after", "solved", "end"),
		},
	}
}

func TestSplitParksTheTeam(t *testing.T) {
	step, err := First(splitGraph(), newState())
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "split" || step.Finished || step.Held {
		t.Fatalf("step = %+v, want parked at the split", step)
	}
}

func TestEachPathIsItsOwnAdvance(t *testing.T) {
	s := newState()
	for port, want := range map[string]string{"a": "pa", "b": "pb"} {
		step, err := Advance(splitGraph(), s, "split", port)
		if err != nil {
			t.Fatal(err)
		}
		if step.To != want {
			t.Errorf("path %s: To = %q, want %q", port, step.To, want)
		}
	}
}

func TestJoinHoldsAGroupWhileAnotherWalks(t *testing.T) {
	s := newState()
	s.waits = map[string]bool{"join": true}
	step, err := Advance(splitGraph(), s, "pa", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "join" || !step.Held {
		t.Fatalf("step = %+v, want held at the join", step)
	}
}

func TestLastGroupTakesTheTeamOn(t *testing.T) {
	s := newState()
	step, err := Advance(splitGraph(), s, "pb", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "after" || step.Held {
		t.Fatalf("step = %+v, want on to after", step)
	}
	if !slices.Contains(step.Passed, "join") {
		t.Errorf("Passed = %v, want the join in it: that is how the team learns it is one again", step.Passed)
	}
}

// A team that is not split — sent past the split by a time limit, say —
// walks through a join as if it were not there.
func TestJoinPassesATeamThatIsNotSplit(t *testing.T) {
	s := newState()
	s.waits = map[string]bool{} // none waits
	step, err := Jump(splitGraph(), s, "pa", "join")
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "after" || step.Held {
		t.Fatalf("step = %+v, want through to after", step)
	}
}

func TestJoinWithEffectsBehindIt(t *testing.T) {
	g := splitGraph()
	fx := node("fx", graph.Effect)
	fx.EffectCfg = &graph.EffectConfig{Kind: graph.AdjustScore, Points: 5}
	g.Nodes = append(g.Nodes, fx)
	for i, e := range g.Edges {
		if e.From == "join" {
			g.Edges[i].To = "fx"
		}
	}
	g.Edges = append(g.Edges, edge("fx", graph.PortOut, "after"))
	s := newState()
	step, err := Advance(g, s, "pb", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "after" || len(step.Effects) != 1 || s.score != 5 {
		t.Fatalf("step = %+v, score %v: the effect behind the join applies once, as the team goes on", step, s.score)
	}
}

func TestAGroupSplitsAgain(t *testing.T) {
	g := splitGraph()
	s := newState()
	step, err := Jump(g, s, "pa", "split")
	if err != nil || step.To != "split" || step.Held {
		t.Fatalf("a group reaching a split parks there to split again: %+v %v", step, err)
	}
}

// The last group through a join takes the team on, and the team may
// split again at once.
func TestThroughAJoinIntoAnotherSplit(t *testing.T) {
	g := splitGraph()
	sp2 := node("split2", graph.Split)
	sp2.SplitCfg = &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "c"}, {Port: "d"}}}
	g.Nodes = append(g.Nodes, sp2)
	for i, e := range g.Edges {
		if e.From == "join" {
			g.Edges[i].To = "split2"
		}
	}
	s := newState()
	step, err := Advance(g, s, "pb", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "split2" || !slices.Contains(step.Passed, "join") || step.Held {
		t.Fatalf("step = %+v, want through the join and parked at split2", step)
	}
}

// A group held at its join parks there.
func TestHeldAtTheJoin(t *testing.T) {
	s := newState()
	s.waits = map[string]bool{"join": true}
	step, err := Advance(splitGraph(), s, "pa", "solved")
	if err != nil || !step.Held || step.To != "join" {
		t.Fatalf("step = %+v, err %v", step, err)
	}
}
