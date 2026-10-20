package walk

import (
	"errors"
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
	s.divided = true
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
	s.divided = true
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
	s.divided = true
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
	s.waits = map[string]bool{} // not divided, so never asked
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
	s.divided = true
	step, err := Advance(g, s, "pb", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.To != "after" || len(step.Effects) != 1 || s.score != 5 {
		t.Fatalf("step = %+v, score %v: the effect behind the join applies once, as the team goes on", step, s.score)
	}
}

func TestAGroupCannotSplitAgain(t *testing.T) {
	g := splitGraph()
	s := newState()
	s.divided = true
	if _, err := Jump(g, s, "pa", "split"); !errors.Is(err, ErrNested) {
		t.Fatalf("err = %v, want ErrNested", err)
	}
}
