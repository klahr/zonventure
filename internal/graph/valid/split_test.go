package valid

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

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

// A group does not split again: a split on a path is refused, and the
// path stops there.
func TestNestedSplitRefused(t *testing.T) {
	r := Check(nested())
	if !issueAt(r, "split.nested", "split", "a") {
		t.Errorf("issues: %v", r.Issues)
	}
	paths := nested().Splits().Paths("split")
	if !slices.Equal(paths.Nested["a"], []string{"inner"}) || len(paths.Steps["a"]) != 0 {
		t.Errorf("steps %v nested %v", paths.Steps["a"], paths.Nested)
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

// A chain of splits, each path of one leading into the next, is checked
// in seconds: each split once, its paths stopping at the next.
func TestSplitChainChecksFast(t *testing.T) {
	if raceOn {
		t.Skip("timed without the race detector")
	}
	const k = 20
	g := &graph.Graph{Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}}}
	g.Edges = append(g.Edges, edge("start", graph.PortOut, "s0"))
	for i := range k {
		sp, next := fmt.Sprintf("s%d", i), fmt.Sprintf("s%d", i+1)
		if i == k-1 {
			next = "end"
		}
		g.Nodes = append(g.Nodes, graph.Node{Slug: sp, Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}})
		g.Edges = append(g.Edges, edge(sp, "a", next), edge(sp, "b", next))
	}
	began := time.Now()
	Check(g)
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("checking a chain of %d splits took %v", k, took)
	}
	if nested := g.Splits().Paths("s0").Nested["a"]; !slices.Equal(nested, []string{"s1"}) {
		t.Errorf("s0 nests %v", nested)
	}
}

// Onto a path only from its split: not from after the join, nor from a
// sibling path.
func TestSplitEntryFromOutside(t *testing.T) {
	g := splitValid()
	g.Nodes = append(g.Nodes, puzzle("k", "solved", "on"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "k"), edge("k", "solved", "pa"), edge("k", "on", "end"))
	r := Check(g)
	assertCode(t, r, "split.entry.outside")
	assertNoCode(t, r, "split.path.skip")

	g = splitValid()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i] = puzzle("pb", "solved", "over")
		}
	}
	g.Edges = append(g.Edges, edge("pb", "over", "pa"))
	assertCode(t, Check(g), "split.entry.outside")
}

// A path leading past its join without passing it leaves the other
// groups waiting there for good, whether or not it reaches the join too.
func TestSplitPathSkipsItsJoin(t *testing.T) {
	// beyond is join → d1 → split2{c, d} → pc / pd → join2 → end: the
	// adventure goes on after the join, so d1 is the team's, together.
	beyond := func(g *graph.Graph) {
		g.Nodes = append(g.Nodes, puzzle("d1"), puzzle("pc"), puzzle("pd"), graph.Node{Slug: "join2", Type: graph.Join},
			graph.Node{Slug: "split2", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "c"}, {Port: "d"}}}})
		g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
		g.Edges = append(g.Edges, edge("join", graph.PortOut, "d1"), edge("d1", "solved", "split2"),
			edge("split2", "c", "pc"), edge("split2", "d", "pd"), edge("pc", "solved", "join2"), edge("pd", "solved", "join2"),
			edge("join2", graph.PortOut, "end"))
	}
	g := splitValid()
	beyond(g)
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i] = puzzle("pa", "solved", "skip")
		}
	}
	g.Edges = append(g.Edges, edge("pa", "skip", "d1"))
	r := Check(g)
	assertCode(t, r, "split.path.skip")
	assertNoCode(t, r, "split.entry.outside")
	for _, i := range r.Issues {
		if i.Code == "split.path.skip" && (i.Node != "pa" || i.Port != "skip") {
			t.Errorf("the skip is pa's: %v", i)
		}
	}

	// Straight past it, never reaching it.
	g = splitValid()
	beyond(g)
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "pb" })
	g.Edges = append(g.Edges, edge("pb", "solved", "d1"))
	r = Check(g)
	assertCode(t, r, "split.path.skip")
	assertNoCode(t, r, "split.entry.outside")
}

// lose is a step leading only to an ending: lose → lend. A group
// reaching it ends the adventure for every group, as at an ending, so
// it may be reached from outside a path, and from past its join.
func withLose(g *graph.Graph, from ...string) {
	g.Nodes = append(g.Nodes, puzzle("lose"), graph.Node{Slug: "lend", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Failed}})
	g.Edges = append(g.Edges, edge("lose", "solved", "lend"))
	for _, f := range from {
		found := false
		for i := range g.Nodes {
			if g.Nodes[i].Slug == f && g.Nodes[i].PuzzleCfg != nil {
				g.Nodes[i].PuzzleCfg.Outputs = append(g.Nodes[i].PuzzleCfg.Outputs, graph.Output{Port: "fail", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "fail"}}})
				found = true
			}
		}
		if !found {
			panic(f)
		}
		g.Edges = append(g.Edges, edge(f, "fail", "lose"))
	}
}

func TestSplitSharedLosingStep(t *testing.T) {
	noErrors := func(name string, g *graph.Graph) {
		t.Helper()
		if r := Check(g); r.HasErrors() {
			t.Errorf("%s: %v", name, r.Errors())
		}
	}
	// From a path and from after the join: the join leads there, so it
	// is the team's, and a group falling into it plays it alone while
	// the others wait — until it ends the adventure, so only a warning.
	g := splitValid()
	g.Nodes = append(g.Nodes, puzzle("post"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "post"), edge("post", "solved", "end"))
	withLose(g, "pa", "post")
	alone(t, Check(g), "pa", "fail")

	// From before the split.
	g = splitValid()
	g.Nodes = append(g.Nodes, puzzle("pre"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "start" })
	g.Edges = append(g.Edges, edge("start", graph.PortOut, "pre"), edge("pre", "solved", "split"))
	withLose(g, "pre", "pa")
	noErrors("before the split and a path", g)

	// From the paths of two splits, one after the other.
	g = splitValid()
	g.Nodes = append(g.Nodes, puzzle("pc"), puzzle("pd"), graph.Node{Slug: "join2", Type: graph.Join},
		graph.Node{Slug: "split2", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "c"}, {Port: "d"}}}})
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "split2"),
		edge("split2", "c", "pc"), edge("split2", "d", "pd"), edge("pc", "solved", "join2"), edge("pd", "solved", "join2"),
		edge("join2", graph.PortOut, "end"))
	withLose(g, "pa", "pc")
	noErrors("two splits' paths", g)

	// From a path leading back onto it: not only to endings, so still
	// onto the path from outside it.
	g = splitValid()
	g.Nodes = append(g.Nodes, puzzle("pre"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "start" })
	g.Edges = append(g.Edges, edge("start", graph.PortOut, "pre"), edge("pre", "solved", "split"))
	withLose(g, "pre", "pa")
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "lose" {
			g.Nodes[i] = puzzle("lose", "solved", "retry")
		}
	}
	g.Edges = append(g.Edges, edge("lose", "retry", "pa"))
	assertCode(t, Check(g), "split.entry.outside")

	// Two paths of one split may not share it: until it ends the
	// adventure, its attempts, lockout and hints are the team's, and the
	// groups on it at once would answer it for each other.
	g = splitValid()
	withLose(g, "pa", "pb")
	assertCode(t, Check(g), "split.paths.cross")
}

// Skipping the join for an ending ends the adventure for every group, as
// an ending on the path would; skipping it for the team's finale, which
// leads only to the ending, has the group play the finale alone.
func TestSplitPathSkipsToTheEnd(t *testing.T) {
	g := splitValid()
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "pb" })
	g.Edges = append(g.Edges, edge("pb", "solved", "end"))
	r := Check(g)
	if r.HasErrors() {
		t.Fatalf("errors: %v", r.Errors())
	}
	assertCode(t, r, "split.path.nojoin")

	g = splitValid()
	g.Nodes = append(g.Nodes, puzzle("d1"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" || e.From == "pb" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "d1"), edge("d1", "solved", "end"), edge("pb", "solved", "d1"))
	alone(t, Check(g), "pb", "solved")

	// One way to the join, another past it to the finale.
	g = splitValid()
	g.Nodes = append(g.Nodes, puzzle("fin"))
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i] = puzzle("pa", "solved", "skip")
		}
	}
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "fin"), edge("fin", "solved", "end"), edge("pa", "skip", "fin"))
	alone(t, Check(g), "pa", "skip")

	// A finale leading on to more than an ending is the team's to play.
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "fin" {
			g.Nodes[i] = puzzle("fin", "solved", "again")
		}
	}
	g.Edges = append(g.Edges, edge("fin", "again", "split"))
	if r := Check(g); !issueAt(r, "split.path.skip", "pa", "skip") {
		t.Errorf("pa.skip skips the join into more than the finale: %v", r.Issues)
	}
}

// alone asserts the one issue about the group playing alone into the
// team's steps is the warning on node's port, with no error.
func alone(t *testing.T, r Report, node, port string) {
	t.Helper()
	if r.HasErrors() {
		t.Errorf("errors: %v", r.Errors())
	}
	n := 0
	for _, i := range r.Issues {
		if i.Code != "split.path.alone" {
			continue
		}
		n++
		if i.Node != node || i.Port != port || i.Severity != Warning {
			t.Errorf("want a warning on %s.%s: %v", node, port, i)
		}
	}
	if n != 1 {
		t.Errorf("want one split.path.alone, got %d: %v", n, r.Issues)
	}
}

// Splits sharing joins are told so once a join, and each way into one
// from outside once, not once for every split and join they share.
func TestSplitsSharingJoinsReportOnce(t *testing.T) {
	const splits, joins = 30, 6
	g := &graph.Graph{Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}}}
	for j := range joins {
		g.Nodes = append(g.Nodes, graph.Node{Slug: fmt.Sprintf("j%d", j), Type: graph.Join})
		g.Edges = append(g.Edges, edge(fmt.Sprintf("j%d", j), graph.PortOut, "end"))
	}
	g.Nodes = append(g.Nodes, puzzle("hub", func() []string {
		var ports []string
		for i := range splits {
			ports = append(ports, fmt.Sprintf("to%d", i))
		}
		return ports
	}()...))
	g.Edges = append(g.Edges, edge("start", graph.PortOut, "hub"))
	for i := range splits {
		sp, pa, pb, j := fmt.Sprintf("s%02d", i), fmt.Sprintf("a%02d", i), fmt.Sprintf("b%02d", i), fmt.Sprintf("j%d", i%joins)
		g.Nodes = append(g.Nodes, graph.Node{Slug: sp, Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}}, puzzle(pa), puzzle(pb))
		g.Edges = append(g.Edges, edge("hub", fmt.Sprintf("to%d", i), sp), edge(sp, "a", pa), edge(sp, "b", pb), edge(pa, "solved", j), edge(pb, "solved", j))
	}
	r := Check(g)
	seen, count := map[string]bool{}, map[string]int{}
	for _, i := range r.Issues {
		k := i.Code + " " + i.Node + " " + i.Port
		if seen[k] {
			t.Errorf("reported twice: %v", i)
		}
		seen[k] = true
		count[i.Code]++
	}
	if count["join.splits.many"] != joins || count["join.entry.outside"] != 2*splits {
		t.Errorf("issues %v", count)
	}
}

// A long chain of splits, one after another, and a long run of them each
// on the last's paths, check in seconds at most, not the ages an exponential walk would take.
func TestManySplitsCheckFast(t *testing.T) {
	if raceOn {
		t.Skip("timed without the race detector")
	}
	const k = 300
	ends := []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}}
	chain := &graph.Graph{Nodes: slices.Clone(ends)}
	prev, port := "start", graph.PortOut
	for i := range k {
		sp, j, pa, pb := fmt.Sprintf("s%d", i), fmt.Sprintf("j%d", i), fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i)
		chain.Nodes = append(chain.Nodes, graph.Node{Slug: sp, Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}},
			graph.Node{Slug: j, Type: graph.Join}, puzzle(pa), puzzle(pb))
		chain.Edges = append(chain.Edges, edge(prev, port, sp), edge(sp, "a", pa), edge(sp, "b", pb), edge(pa, "solved", j), edge(pb, "solved", j))
		prev, port = j, graph.PortOut
	}
	chain.Edges = append(chain.Edges, edge(prev, port, "end"))
	nest := &graph.Graph{Nodes: slices.Clone(ends)}
	nest.Edges = append(nest.Edges, edge("start", graph.PortOut, "s0"))
	for i := range k {
		sp, next := fmt.Sprintf("s%d", i), fmt.Sprintf("s%d", i+1)
		if i == k-1 {
			next = "end"
		}
		nest.Nodes = append(nest.Nodes, graph.Node{Slug: sp, Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}})
		nest.Edges = append(nest.Edges, edge(sp, "a", next), edge(sp, "b", next))
	}
	for name, g := range map[string]*graph.Graph{"chain": chain, "nest": nest} {
		began := time.Now()
		r := Check(g)
		if took := time.Since(began); took > 5*time.Second {
			t.Errorf("checking a %s of %d splits took %v", name, k, took)
		}
		if name == "chain" && len(r.Issues) != 0 {
			t.Errorf("chain: %v", r.Issues)
		}
	}
}

// A split on a path is refused the same however its nodes and edges are
// ordered: s.a → t, t.y → ty → back to s.
func TestSplitNestedVerdictSteady(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}},
			{Slug: "t", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "x"}, {Port: "y"}}}},
			puzzle("tx"), puzzle("ty", "solved", "back"), puzzle("a2"), puzzle("pb"),
			{Slug: "tj", Type: graph.Join}, {Slug: "sj", Type: graph.Join},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "s"),
			edge("s", "a", "t"), edge("s", "b", "pb"),
			edge("t", "x", "tx"), edge("t", "y", "ty"),
			edge("tx", "solved", "tj"), edge("ty", "solved", "tj"), edge("ty", "back", "s"),
			edge("tj", graph.PortOut, "a2"), edge("a2", "solved", "sj"), edge("pb", "solved", "sj"),
			edge("sj", graph.PortOut, "end"),
		},
	}
	verdict := func(g *graph.Graph) []string {
		var out []string
		for _, i := range Check(g).Issues {
			out = append(out, fmt.Sprintf("%s %s %s %s", i.Code, i.Node, i.Port, i.Message))
		}
		slices.Sort(out)
		return out
	}
	want := verdict(g)
	if !slices.ContainsFunc(want, func(s string) bool { return strings.HasPrefix(s, "split.nested ") }) {
		t.Fatalf("no nesting found: %v", want)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		rng.Shuffle(len(g.Nodes), func(i, j int) { g.Nodes[i], g.Nodes[j] = g.Nodes[j], g.Nodes[i] })
		rng.Shuffle(len(g.Edges), func(i, j int) { g.Edges[i], g.Edges[j] = g.Edges[j], g.Edges[i] })
		if got := verdict(g); !slices.Equal(got, want) {
			t.Fatalf("verdict changed with order:\n%v\nwas\n%v", got, want)
		}
	}
}

// homeHub is a home screen with the given branches.
func homeHub(slug string, ports ...string) graph.Node {
	cfg := &graph.HubConfig{Policy: graph.PolicyChoice, Home: true}
	for _, p := range ports {
		cfg.Branches = append(cfg.Branches, graph.Branch{Port: p})
	}
	return graph.Node{Slug: slug, Type: graph.Hub, HubCfg: cfg}
}

func issueAt(r Report, code, node, port string) bool {
	return slices.ContainsFunc(r.Issues, func(i Issue) bool { return i.Code == code && i.Node == node && i.Port == port })
}

// Back leads from a home screen's branch to the screen with no edge
// drawn: a step leading only to an ending that way leads onto the path
// the screen is on, too.
func TestSplitBackOntoAPath(t *testing.T) {
	// start → pre{x → split, y → look1}; split.a → home{look → look1 →
	// end, leave → leave1 → join}; split.b → pb → join → end. A team
	// reaching look1 together goes back to home, leaves for the join and
	// finishes without path b.
	g := splitValid()
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "pa" })
	g.Nodes = append(g.Nodes, puzzle("pre", "x", "y"), homeHub("home", "look", "leave"), puzzle("look1"), puzzle("leave1"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "start" || e.From == "pa" || e.To == "pa" })
	g.Edges = append(g.Edges, edge("start", graph.PortOut, "pre"), edge("pre", "x", "split"), edge("pre", "y", "look1"),
		edge("split", "a", "home"), edge("home", "look", "look1"), edge("look1", "solved", "end"),
		edge("home", "leave", "leave1"), edge("leave1", "solved", "join"))
	if r := Check(g); !issueAt(r, "split.entry.outside", "pre", "y") {
		t.Errorf("pre.y leads onto path a: %v", r.Issues)
	}

	// After the join, post is the team's, not home's: home owns no step
	// past its path's join, so Back is not offered there and post has
	// its own way on.
	g = splitValid()
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "pa" })
	g.Nodes = append(g.Nodes, homeHub("home", "look", "leave"), puzzle("look1"), puzzle("leave1"), puzzle("post"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" || e.From == "pa" || e.To == "pa" })
	g.Edges = append(g.Edges, edge("split", "a", "home"), edge("home", "look", "look1"), edge("look1", "solved", "end"),
		edge("home", "leave", "leave1"), edge("leave1", "solved", "join"), edge("join", graph.PortOut, "post"), edge("post", "solved", "end"))
	if r := Check(g); r.HasErrors() {
		t.Errorf("errors: %v", r.Errors())
	}
	if h, ok := ownerHub(g, "post"); ok {
		t.Errorf("post is owned by %s", h)
	}
	if _, ok := backs(g, g.Splits())["post"]; ok {
		t.Error("Back from post")
	}
	if h, _ := ownerHub(g, "look1"); h != "home" {
		t.Errorf("look1 owned by %q, want home", h)
	}

	// Back within the path is the group's own.
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" || e.From == "post" })
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "post" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "end"))
	if r := Check(g); r.HasErrors() {
		t.Errorf("errors: %v", r.Errors())
	}
}

// A group goes back only to a home screen on its own path: a step whose
// only way on is Back to one off it holds the group for good.
func TestSplitBackOffThePath(t *testing.T) {
	// The screen after the join: a.skip → t, in its branch.
	g := splitValid()
	g.Nodes = append(g.Nodes, homeHub("home", "x", "quit"), puzzle("t"))
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i] = puzzle("pa", "solved", "skip")
		}
	}
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("pa", "skip", "t"), edge("join", graph.PortOut, "home"),
		edge("home", "x", "t"), edge("home", "quit", "end"))
	r := Check(g)
	if !issueAt(r, "split.path.skip", "pa", "skip") {
		t.Errorf("pa.skip skips the join: %v", r.Issues)
	}
	if !issueAt(r, "node.nodeadend", "t", "") {
		t.Errorf("a group on t is stuck: %v", r.Issues)
	}

	// The screen before the split.
	g = splitValid()
	g.Nodes = append(g.Nodes, homeHub("home", "go", "quit"), puzzle("t"))
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i] = puzzle("pa", "solved", "stuck")
		}
	}
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "start" })
	g.Edges = append(g.Edges, edge("start", graph.PortOut, "home"), edge("home", "go", "split"), edge("home", "quit", "end"),
		edge("pa", "stuck", "t"))
	if r := Check(g); !issueAt(r, "node.nodeadend", "t", "") {
		t.Errorf("a group on t is stuck: %v", r.Issues)
	}
}

// Time up brings a split team together where the limit sends it: onto
// one group's path, or to a join, the whole team walks what is a
// group's.
func TestTimeLimitIntoASplit(t *testing.T) {
	for to, bad := range map[string]bool{"pa": true, "join": true, "end": false, "late": false, "split": false, "lend": false} {
		g := splitValid()
		g.Nodes = append(g.Nodes, puzzle("late"))
		g.Edges = append(g.Edges, edge("late", "solved", "end"))
		// An ending a path's step also fails into: the whole team's too.
		withLose(g, "pa")
		g.TimeLimit = &graph.TimeLimit{Seconds: 60, To: to}
		if got := issueAt(Check(g), "time_limit.to", to, ""); got != bad {
			t.Errorf("to %s: reported %v, want %v", to, got, bad)
		}
	}
}

// A gate, an effect or a crossroads holds no group: two paths falling
// through one to an ending end the adventure as at the ending itself.
func TestSplitPathsSharePassingStep(t *testing.T) {
	build := func() *graph.Graph {
		g := splitValid()
		g.Items = []graph.Item{{Slug: "scar", Name: "Scar"}}
		g.Nodes = append(g.Nodes, graph.Node{Slug: "fx", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "scar", Qty: 1}},
			graph.Node{Slug: "lend", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Failed}})
		for i := range g.Nodes {
			if s := g.Nodes[i].Slug; s == "pa" || s == "pb" {
				g.Nodes[i] = puzzle(s, "solved", "fail")
			}
		}
		g.Edges = append(g.Edges, edge("pa", "fail", "fx"), edge("pb", "fail", "fx"), edge("fx", graph.PortOut, "lend"))
		return g
	}
	if r := Check(build()); r.HasErrors() {
		t.Errorf("errors: %v", r.Errors())
	}

	// The join leading there too: still no one stops on it, and whoever
	// reaches it ends the adventure.
	g := build()
	g.Nodes = append(g.Nodes, puzzle("post", "solved", "fail"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "post"), edge("post", "solved", "end"), edge("post", "fail", "fx"))
	r := Check(g)
	if r.HasErrors() {
		t.Errorf("errors: %v", r.Errors())
	}
	assertNoCode(t, r, "split.path.alone")

	// One path only, the join leading there too.
	g = build()
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "pb" && e.FromPort == "fail" })
	g.Nodes = append(g.Nodes, puzzle("post", "solved", "fail"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" })
	g.Edges = append(g.Edges, edge("pb", "fail", "end"), edge("join", graph.PortOut, "post"), edge("post", "solved", "end"), edge("post", "fail", "fx"))
	r = Check(g)
	if r.HasErrors() {
		t.Errorf("errors: %v", r.Errors())
	}
	assertNoCode(t, r, "split.path.alone")

	// One path, through it to a step a group stops on that the join
	// leads to as well: the group plays that alone.
	g = build()
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool {
		return e.From == "pb" && e.FromPort == "fail" || e.From == "fx" || e.From == "join"
	})
	g.Nodes = append(g.Nodes, puzzle("last"), puzzle("post", "solved", "fail"))
	g.Edges = append(g.Edges, edge("pb", "fail", "end"), edge("fx", graph.PortOut, "last"), edge("last", "solved", "lend"),
		edge("join", graph.PortOut, "post"), edge("post", "solved", "end"), edge("post", "fail", "last"))
	alone(t, Check(g), "fx", graph.PortOut)

	// Past it, a step a group stops on: the two paths cross there.
	g = build()
	g.Nodes = append(g.Nodes, puzzle("last"))
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "fx" })
	g.Edges = append(g.Edges, edge("fx", graph.PortOut, "last"), edge("last", "solved", "lend"))
	assertCode(t, Check(g), "split.paths.cross")
}

// ownerHub is g's owner hub of slug.
func ownerHub(g *graph.Graph, slug string) (string, bool) {
	h, ok := g.Splits().OwnerHubs()[slug]
	return h, ok
}
