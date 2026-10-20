package valid

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// splitValid is start → split{a: scout, b} → pa / pb → join → end.
func splitValid() *graph.Graph {
	return &graph.Graph{
		Roles: []graph.Role{{Slug: "scout"}, {Slug: "mole", Secret: true}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "split", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a", Role: "scout"}, {Port: "b"}}}},
			puzzle("pa"), puzzle("pb"),
			{Slug: "join", Type: graph.Join},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "split"),
			edge("split", "a", "pa"), edge("split", "b", "pb"),
			edge("pa", "solved", "join"), edge("pb", "solved", "join"),
			edge("join", graph.PortOut, "end"),
		},
	}
}

func splitNode(g *graph.Graph) *graph.SplitConfig {
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "split" {
			return g.Nodes[i].SplitCfg
		}
	}
	return nil
}

func TestSplitValid(t *testing.T) {
	r := Check(splitValid())
	if len(r.Issues) != 0 {
		t.Fatalf("issues: %v", r.Issues)
	}
}

func TestSplitPathRules(t *testing.T) {
	g := splitValid()
	splitNode(g).Paths = splitNode(g).Paths[:1]
	assertCode(t, Check(g), "split.paths.few")

	g = splitValid()
	splitNode(g).Paths[1].Port = "a"
	assertCode(t, Check(g), "split.port.duplicate")

	g = splitValid()
	splitNode(g).Paths[1].Role = "nobody"
	assertCode(t, Check(g), "split.role.unknown")

	g = splitValid()
	splitNode(g).Paths[1].Role = "mole"
	assertCode(t, Check(g), "split.role.secret")

	g = splitValid()
	splitNode(g).Paths[1].Role = "scout"
	assertCode(t, Check(g), "split.role.duplicate")

	g = splitValid()
	splitNode(g).Paths = append(splitNode(g).Paths, graph.SplitPath{Port: "c"})
	assertCode(t, Check(g), "port.unconnected")
}

// Two groups on one step would answer it for each other.
func TestSplitPathsMayNotCross(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes, puzzle("shared"))
	g.Edges = []graph.Edge{
		edge("start", graph.PortOut, "split"),
		edge("split", "a", "pa"), edge("split", "b", "pb"),
		edge("pa", "solved", "shared"), edge("pb", "solved", "shared"),
		edge("shared", "solved", "join"),
		edge("join", graph.PortOut, "end"),
	}
	assertCode(t, Check(g), "split.paths.cross")
}

func TestSplitInsideASplit(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes,
		graph.Node{Slug: "inner", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "x"}, {Port: "y"}}}},
		graph.Node{Slug: "inner-join", Type: graph.Join})
	g.Edges = []graph.Edge{
		edge("start", graph.PortOut, "split"),
		edge("split", "a", "inner"), edge("split", "b", "pb"),
		edge("inner", "x", "inner-join"), edge("inner", "y", "inner-join"),
		edge("inner-join", graph.PortOut, "join"),
		edge("pb", "solved", "join"),
		edge("join", graph.PortOut, "end"),
	}
	assertCode(t, Check(g), "split.nested")
}

func TestSplitPathBackToItsSplit(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes, puzzle("loop", "back", "on"))
	g.Edges = []graph.Edge{
		edge("start", graph.PortOut, "split"),
		edge("split", "a", "loop"), edge("split", "b", "pb"),
		edge("loop", "back", "split"), edge("loop", "on", "join"),
		edge("pb", "solved", "join"),
		edge("join", graph.PortOut, "end"),
	}
	assertCode(t, Check(g), "split.nested")
}

func TestSplitPathsMeetAtOneJoin(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "join2", Type: graph.Join})
	g.Edges = []graph.Edge{
		edge("start", graph.PortOut, "split"),
		edge("split", "a", "pa"), edge("split", "b", "pb"),
		edge("pa", "solved", "join"), edge("pb", "solved", "join2"),
		edge("join", graph.PortOut, "end"), edge("join2", graph.PortOut, "end"),
	}
	assertCode(t, Check(g), "split.joins.many")
}

// A way into the join from anywhere but the split's paths would arrive
// unwaited-for.
func TestJoinEntryFromOutside(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes, puzzle("side"))
	g.Edges = append(g.Edges, edge("side", "solved", "join"))
	assertCode(t, Check(g), "join.entry.outside")
}

// A path may end the adventure — it is the whole team's end — but the
// author is told.
func TestSplitPathToAnEnd(t *testing.T) {
	g := splitValid()
	for i, e := range g.Edges {
		if e.From == "pb" {
			g.Edges[i].To = "end"
		}
	}
	r := Check(g)
	assertCode(t, r, "split.path.nojoin")
	if r.HasErrors() {
		t.Fatalf("a path ending the adventure is allowed: %v", r.Errors())
	}
}

func TestJoinWithNoSplit(t *testing.T) {
	g := valid3()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "j", Type: graph.Join})
	g.Edges = []graph.Edge{
		edge("start", graph.PortOut, "p1"),
		edge("p1", "solved", "j"),
		edge("j", graph.PortOut, "p2"),
		edge("p2", "solved", "end"),
	}
	r := Check(g)
	assertCode(t, r, "join.split.none")
	if r.HasErrors() {
		t.Fatalf("a lone join only warns: %v", r.Errors())
	}
}

func TestJoinIsPassiveInACycle(t *testing.T) {
	g := valid3()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "j", Type: graph.Join},
		graph.Node{Slug: "fx", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.AdjustScore, Points: 1}})
	g.Edges = append(g.Edges, edge("j", graph.PortOut, "fx"), edge("fx", graph.PortOut, "j"))
	assertCode(t, Check(g), "graph.cycle.noprogress")
}
