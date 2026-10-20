package valid

import (
	"fmt"
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
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

// nested is start → split{a, b}; on a: inner{x, y} → x1 / y1 → ij →
// a2 → join; b: pb → join; join → end.
func nested() *graph.Graph {
	g := splitValid()
	g.Nodes = append(g.Nodes,
		graph.Node{Slug: "inner", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "x"}, {Port: "y"}}}},
		puzzle("x1"), puzzle("y1"), puzzle("a2"),
		graph.Node{Slug: "ij", Type: graph.Join})
	g.Edges = []graph.Edge{
		edge("start", graph.PortOut, "split"),
		edge("split", "a", "inner"), edge("split", "b", "pb"),
		edge("inner", "x", "x1"), edge("inner", "y", "y1"),
		edge("x1", "solved", "ij"), edge("y1", "solved", "ij"),
		edge("ij", graph.PortOut, "a2"), edge("a2", "solved", "join"),
		edge("pb", "solved", "join"),
		edge("join", graph.PortOut, "end"),
	}
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "pa" })
	return g
}

// A group may split again, meeting at a join of its own before the outer
// groups meet.
func TestNestedSplitValid(t *testing.T) {
	if r := Check(nested()); len(r.Issues) != 0 {
		t.Fatalf("issues: %v", r.Issues)
	}
	paths := nested().SplitPaths("split")
	for _, s := range []string{"inner", "x1", "y1", "ij", "a2"} {
		if !slices.Contains(paths.Steps["a"], s) {
			t.Errorf("%s should be a step of the outer path a: %v", s, paths.Steps["a"])
		}
	}
	if !slices.Equal(paths.Joins["a"], []string{"join"}) || !slices.Equal(paths.Inner["a"], []string{"inner"}) {
		t.Errorf("joins %v inner %v", paths.Joins, paths.Inner)
	}
	if d := nested().SplitDepth("split"); d != 2 {
		t.Errorf("depth %d, want 2", d)
	}
}

// An inner group may not skip its own join for the outer one: its
// siblings would wait for it at theirs.
func TestNestedSplitMeetsAtItsOwnJoin(t *testing.T) {
	g := nested()
	for i, e := range g.Edges {
		if e.From == "y1" {
			g.Edges[i].To = "join"
		}
	}
	assertCode(t, Check(g), "split.joins.many")
}

// The inner join may not be the outer one.
func TestNestedSplitSharesNoJoin(t *testing.T) {
	g := nested()
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "ij" || e.From == "a2" })
	for i, e := range g.Edges {
		if e.From == "x1" || e.From == "y1" {
			g.Edges[i].To = "join"
		}
	}
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "ij" || n.Slug == "a2" })
	assertCode(t, Check(g), "join.splits.many")
}

// Back into an enclosing split is a group splitting into itself.
func TestNestedLoopToTheOuterSplit(t *testing.T) {
	g := nested()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "x1" {
			g.Nodes[i].PuzzleCfg.Outputs = append(g.Nodes[i].PuzzleCfg.Outputs, graph.Output{Port: "again", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "again"}}})
		}
	}
	g.Edges = append(g.Edges, edge("x1", "again", "split"))
	assertCode(t, Check(g), "split.loop")
}

// Five deep is too deep; four is fine.
func TestSplitDepth(t *testing.T) {
	g := &graph.Graph{Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}}}
	prev, port := "start", graph.PortOut
	const levels = graph.MaxSplitDepth + 1
	for l := range levels {
		sp, jn, side := fmt.Sprintf("s%d", l), fmt.Sprintf("j%d", l), fmt.Sprintf("side%d", l)
		g.Nodes = append(g.Nodes, graph.Node{Slug: sp, Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "in"}, {Port: "out"}}}},
			graph.Node{Slug: jn, Type: graph.Join}, puzzle(side))
		g.Edges = append(g.Edges, edge(prev, port, sp), edge(sp, "out", side), edge(side, "solved", jn))
		prev, port = sp, "in"
	}
	g.Nodes = append(g.Nodes, puzzle("core"))
	g.Edges = append(g.Edges, edge(prev, port, "core"))
	last, lastPort := "core", "solved"
	for l := levels - 1; l >= 0; l-- {
		jn := fmt.Sprintf("j%d", l)
		g.Edges = append(g.Edges, edge(last, lastPort, jn))
		last, lastPort = jn, graph.PortOut
	}
	g.Edges = append(g.Edges, edge(last, lastPort, "end"))
	r := Check(g)
	assertCode(t, r, "split.depth")
	for _, i := range r.Issues {
		if i.Code == "split.depth" && i.Node != "s0" {
			t.Errorf("only the five-deep split is too deep, not %s", i.Node)
		}
	}
	if d := g.SplitDepth("s1"); d != graph.MaxSplitDepth {
		t.Errorf("s1 depth %d", d)
	}
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
	assertCode(t, Check(g), "split.loop")
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

// Two paths may share an ending: whichever group reaches it ends the
// adventure for all. Found by random play: a failure route on each path
// to one "game over" was refused as crossing paths.
func TestSplitPathsShareAnEnding(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "over", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Failed}})
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" || g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.Outputs = append(g.Nodes[i].PuzzleCfg.Outputs, graph.Output{Port: "fall", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "fall"}}})
		}
	}
	g.Edges = append(g.Edges, edge("pa", "fall", "over"), edge("pb", "fall", "over"))
	if r := Check(g); r.HasErrors() {
		t.Fatalf("errors: %v", r.Errors())
	}
}
