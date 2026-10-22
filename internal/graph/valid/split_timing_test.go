//go:build !race

package valid

// Timing tests, left out under the race detector, which slows checking
// too much for them to say anything.

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
)

// A chain of splits, each path of one leading into the next, is checked
// in seconds: each split once, its paths stopping at the next.
func TestSplitChainChecksFast(t *testing.T) {
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

// A long chain of splits, one after another, and a long run of them each
// on the last's paths, check in seconds at most, not the ages an exponential walk would take.
func TestManySplitsCheckFast(t *testing.T) {
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
