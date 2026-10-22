package graph

import (
	"maps"
	"math/rand/v2"
	"sync"
	"testing"
)

// ownerHubsNoSplits is OwnerHubs as it was before splits bounded it: for
// a graph without splits, it must still say the same.
func ownerHubsNoSplits(g *Graph) map[string]string {
	next := map[string][]string{}
	for _, e := range g.Edges {
		next[e.From] = append(next[e.From], e.To)
	}
	isHub := map[string]bool{}
	for _, n := range g.Nodes {
		isHub[n.Slug] = n.Type == Hub
	}
	owner, shared := map[string]string{}, map[string]bool{}
	for _, h := range g.Nodes {
		if h.Type != Hub || h.HubCfg == nil {
			continue
		}
		seen := map[string]bool{h.Slug: true}
		var queue []string
		for _, b := range h.HubCfg.Branches {
			if to, ok := g.Target(h.Slug, b.Port); ok {
				queue = append(queue, to)
			}
		}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			if seen[n] {
				continue
			}
			seen[n] = true
			if o, ok := owner[n]; ok && o != h.Slug {
				shared[n] = true
			}
			owner[n] = h.Slug
			if !isHub[n] {
				queue = append(queue, next[n]...)
			}
		}
	}
	for n := range shared {
		delete(owner, n)
	}
	return owner
}

func ownHub(slug string, ports ...string) Node {
	cfg := &HubConfig{Policy: PolicyChoice, Home: true}
	for _, p := range ports {
		cfg.Branches = append(cfg.Branches, Branch{Port: p})
	}
	return Node{Slug: slug, Type: Hub, HubCfg: cfg}
}

func ownStep(slug string) Node { return Node{Slug: slug, Type: Story} }

func ownEdge(from, port, to string) Edge { return Edge{From: from, FromPort: port, To: to} }

// Without splits, who owns what is as it always was.
func TestOwnerHubsWithoutSplits(t *testing.T) {
	shapes := map[string]*Graph{
		// start → home{a → a1 → home, b → b1 → inner{x → x1 → inner,
		// done → b2 → home}, done → end}
		"nested": {
			Nodes: []Node{{Slug: "start", Type: Start}, ownHub("home", "a", "b"), ownStep("a1"), ownStep("b1"),
				ownHub("inner", "x"), ownStep("x1"), ownStep("b2"), {Slug: "end", Type: End}},
			Edges: []Edge{ownEdge("start", PortOut, "home"), ownEdge("home", "a", "a1"), ownEdge("a1", PortOut, "home"),
				ownEdge("home", "b", "b1"), ownEdge("b1", PortOut, "inner"), ownEdge("inner", "x", "x1"),
				ownEdge("x1", PortOut, "inner"), ownEdge("inner", PortDone, "b2"), ownEdge("b2", PortOut, "home"),
				ownEdge("home", PortDone, "end")},
		},
		// Two screens reaching one step: it has no owner.
		"shared": {
			Nodes: []Node{{Slug: "start", Type: Start}, ownHub("h1", "a"), ownHub("h2", "b"), ownStep("s"), ownStep("t"), {Slug: "end", Type: End}},
			Edges: []Edge{ownEdge("start", PortOut, "h1"), ownEdge("h1", "a", "s"), ownEdge("h1", PortDone, "h2"),
				ownEdge("h2", "b", "s"), ownEdge("s", PortOut, "t"), ownEdge("t", PortOut, "end"), ownEdge("h2", PortDone, "end")},
		},
		// A home screen with no done port: everything after it is its.
		"open": {
			Nodes: []Node{{Slug: "start", Type: Start}, ownHub("home", "go", "stay"), ownStep("g1"), ownStep("g2"), ownStep("s1"), {Slug: "end", Type: End}},
			Edges: []Edge{ownEdge("start", PortOut, "home"), ownEdge("home", "go", "g1"), ownEdge("g1", PortOut, "g2"),
				ownEdge("g2", PortOut, "end"), ownEdge("home", "stay", "s1"), ownEdge("s1", PortOut, "home")},
		},
	}
	for name, g := range shapes {
		if got, want := g.Splits().OwnerHubs(), ownerHubsNoSplits(g); !maps.Equal(got, want) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}

	// And on any shape at all.
	rng := rand.New(rand.NewPCG(3, 4))
	for range 300 {
		g := &Graph{}
		n := 3 + rng.IntN(10)
		slug := func(i int) string { return string(rune('a' + i)) }
		for i := range n {
			if rng.IntN(3) == 0 {
				g.Nodes = append(g.Nodes, ownHub(slug(i), "p", "q"))
			} else {
				g.Nodes = append(g.Nodes, ownStep(slug(i)))
			}
		}
		for _, nd := range g.Nodes {
			for _, p := range nd.Ports() {
				if rng.IntN(4) > 0 {
					g.Edges = append(g.Edges, ownEdge(nd.Slug, p, slug(rng.IntN(n))))
				}
			}
		}
		if got, want := g.Splits().OwnerHubs(), ownerHubsNoSplits(g); !maps.Equal(got, want) {
			t.Fatalf("%v: %v, want %v", g.Edges, got, want)
		}
	}
}

// A hub owns only steps walked by whoever stands on it: a home screen on
// a path stops at its join, and one before a split owns none of its
// paths' steps, but what the team walks after the join.
func TestOwnerHubsAcrossASplit(t *testing.T) {
	split := Node{Slug: "split", Type: Split, SplitCfg: &SplitConfig{Paths: []SplitPath{{Port: "a"}, {Port: "b"}}}}
	g := &Graph{
		// start → pre{go → split, done → end}; split.a → home{look → l →
		// home, leave → lv → join}; split.b → pb → join → post → pre.
		Nodes: []Node{{Slug: "start", Type: Start}, ownHub("pre", "go"), split, ownHub("home", "look", "leave"),
			ownStep("l"), ownStep("lv"), ownStep("pb"), {Slug: "join", Type: Join}, ownStep("post"), {Slug: "end", Type: End}},
		Edges: []Edge{ownEdge("start", PortOut, "pre"), ownEdge("pre", "go", "split"), ownEdge("pre", PortDone, "end"),
			ownEdge("split", "a", "home"), ownEdge("home", "look", "l"), ownEdge("l", PortOut, "home"),
			ownEdge("home", "leave", "lv"), ownEdge("lv", PortOut, "join"), ownEdge("split", "b", "pb"),
			ownEdge("pb", PortOut, "join"), ownEdge("join", PortOut, "post"), ownEdge("post", PortOut, "pre")},
	}
	want := map[string]string{"l": "home", "lv": "home", "split": "pre", "join": "pre", "post": "pre"}
	if got := g.Splits().OwnerHubs(); !maps.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if s, p, ok := g.Splits().OnPath("lv"); !ok || s != "split" || p != "a" {
		t.Errorf("lv on %s.%s %v", s, p, ok)
	}
	if _, _, ok := g.Splits().OnPath("post"); ok {
		t.Error("post on a path")
	}

	// Every path holding a home hub of its own: pre still owns what the
	// team walks after the join, through them, and its branch still
	// comes back to it.
	g = &Graph{
		Nodes: []Node{{Slug: "start", Type: Start}, ownHub("pre", "go"), split, ownHub("ha", "x", "leave"),
			ownStep("ax"), ownStep("al"), ownHub("hb", "y", "leave"), ownStep("bx"), ownStep("bl"),
			{Slug: "join", Type: Join}, ownStep("post"), {Slug: "end", Type: End}},
		Edges: []Edge{ownEdge("start", PortOut, "pre"), ownEdge("pre", "go", "split"), ownEdge("pre", PortDone, "end"),
			ownEdge("split", "a", "ha"), ownEdge("ha", "x", "ax"), ownEdge("ax", PortOut, "ha"),
			ownEdge("ha", "leave", "al"), ownEdge("al", PortOut, "join"),
			ownEdge("split", "b", "hb"), ownEdge("hb", "y", "bx"), ownEdge("bx", PortOut, "hb"),
			ownEdge("hb", "leave", "bl"), ownEdge("bl", PortOut, "join"),
			ownEdge("join", PortOut, "post"), ownEdge("post", PortOut, "pre")},
	}
	want = map[string]string{"split": "pre", "ax": "ha", "al": "ha", "bx": "hb", "bl": "hb", "join": "pre", "post": "pre"}
	if got := g.Splits().OwnerHubs(); !maps.Equal(got, want) {
		t.Errorf("hubs on every path: %v, want %v", got, want)
	}
	if back := g.Splits().BranchReturns("pre", "go"); len(back) != 1 || back[0].From != "post" {
		t.Errorf("pre's branch comes back by %v, want post", back)
	}
	// A hub on a path looks no further than its path: past the join, the
	// way round to the split again is not ha's branch coming back.
	if back := g.Splits().BranchReturns("ha", "leave"); len(back) != 0 {
		t.Errorf("ha's leave comes back by %v, want none", back)
	}
	if back := g.Splits().BranchReturns("ha", "x"); len(back) != 1 || back[0].From != "ax" {
		t.Errorf("ha's x comes back by %v, want ax", back)
	}
}

// A warmed Splits only reads, so one serves many readers at once (run
// with -race).
func TestWarmSplitsIsShared(t *testing.T) {
	split := Node{Slug: "split", Type: Split, SplitCfg: &SplitConfig{Paths: []SplitPath{{Port: "a"}, {Port: "b"}}}}
	g := &Graph{
		Nodes: []Node{{Slug: "start", Type: Start}, ownHub("pre", "go"), split, ownHub("home", "look", "leave"),
			ownStep("l"), ownStep("lv"), ownStep("pb"), {Slug: "join", Type: Join}, ownStep("post"), {Slug: "end", Type: End}},
		Edges: []Edge{ownEdge("start", PortOut, "pre"), ownEdge("pre", "go", "split"), ownEdge("pre", PortDone, "end"),
			ownEdge("split", "a", "home"), ownEdge("home", "look", "l"), ownEdge("l", PortOut, "home"),
			ownEdge("home", "leave", "lv"), ownEdge("lv", PortOut, "join"), ownEdge("split", "b", "pb"),
			ownEdge("pb", PortOut, "join"), ownEdge("join", PortOut, "post"), ownEdge("post", PortOut, "pre")},
	}
	s := g.Splits().Warm()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				s.Paths("split")
				s.JoinOf("split")
				s.OnPath("lv")
				s.OwnerHubs()
				s.BranchReturns("home", "look")
				s.Reach([]string{"pre"}, func(string) bool { return true })
			}
		}()
	}
	wg.Wait()
}
