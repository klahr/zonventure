package valid

import (
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
)

// --- builders -------------------------------------------------------

func puzzle(slug string, ports ...string) graph.Node {
	cfg := &graph.PuzzleConfig{}
	if len(ports) == 0 {
		ports = []string{"solved"}
	}
	for _, p := range ports {
		cfg.Outputs = append(cfg.Outputs, graph.Output{
			Port: p, Trigger: graph.OnAnswer,
			Matchers: []match.Matcher{{Kind: match.Exact, Value: p}},
		})
	}
	return graph.Node{Slug: slug, Type: graph.Puzzle, PuzzleCfg: cfg}
}

func edge(from, port, to string) graph.Edge {
	return graph.Edge{From: from, FromPort: port, To: to}
}

// valid3 is a well-formed Start → p1 → p2 → End.
func valid3() *graph.Graph {
	return &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			puzzle("p1"), puzzle("p2"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "p1"),
			edge("p1", "solved", "p2"),
			edge("p2", "solved", "end"),
		},
	}
}

// --- helpers --------------------------------------------------------

func codes(r Report) []string {
	out := make([]string, 0, len(r.Issues))
	for _, i := range r.Issues {
		out = append(out, i.Code)
	}
	return out
}

func assertCode(t *testing.T, r Report, code string) {
	t.Helper()
	for _, i := range r.Issues {
		if i.Code == code {
			return
		}
	}
	t.Errorf("expected issue %q; got %v", code, codes(r))
}

func assertNoCode(t *testing.T, r Report, code string) {
	t.Helper()
	for _, i := range r.Issues {
		if i.Code == code {
			t.Errorf("did not expect issue %q: %s", code, i)
		}
	}
}

// --- tests ----------------------------------------------------------

func TestValidGraphPasses(t *testing.T) {
	r := Check(valid3())
	if !r.OK() {
		t.Errorf("a well-formed graph should validate; got %v", r.Errors())
	}
}

func TestStartAndEnd(t *testing.T) {
	t.Run("missing start", func(t *testing.T) {
		g := valid3()
		g.Nodes = g.Nodes[1:]
		assertCode(t, Check(g), "graph.start.missing")
	})

	// Several starts are allowed; an unwired one is still an unconnected
	// output, like any other.
	t.Run("an unwired second start", func(t *testing.T) {
		g := valid3()
		g.Nodes = append(g.Nodes, graph.Node{Slug: "start2", Type: graph.Start})
		assertCode(t, Check(g), "port.unconnected")
	})

	// An adventure nobody can finish is the worst possible silent bug.
	t.Run("missing end", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, puzzle("p1")},
			Edges: []graph.Edge{edge("start", graph.PortOut, "p1")},
		}
		assertCode(t, Check(g), "graph.end.missing")
	})
}

func TestDuplicateAndEmptySlugs(t *testing.T) {
	g := valid3()
	g.Nodes = append(g.Nodes, puzzle("p1"))
	assertCode(t, Check(g), "node.slug.duplicate")

	g2 := valid3()
	g2.Nodes = append(g2.Nodes, graph.Node{Type: graph.Story})
	assertCode(t, Check(g2), "node.slug.empty")
}

// The check that most directly stops a team being stranded.
func TestUnconnectedPort(t *testing.T) {
	g := valid3()
	g.Edges = g.Edges[:len(g.Edges)-1] // drop p2 → end
	r := Check(g)
	assertCode(t, r, "port.unconnected")
	if r.OK() {
		t.Error("an unconnected port must block the save")
	}
}

func TestEdgeIntegrity(t *testing.T) {
	t.Run("edge to nowhere", func(t *testing.T) {
		g := valid3()
		g.Edges = append(g.Edges, edge("p1", "ghost-port", "p2"))
		assertCode(t, Check(g), "edge.port.unknown")
	})

	t.Run("edge to a missing node", func(t *testing.T) {
		g := valid3()
		g.Edges[2] = edge("p2", "solved", "ghost")
		assertCode(t, Check(g), "edge.to.unknown")
	})

	// Two edges from one port would make the destination ambiguous and
	// the interpreter would silently take whichever came first.
	t.Run("two edges from one port", func(t *testing.T) {
		g := valid3()
		g.Edges = append(g.Edges, edge("p1", "solved", "end"))
		assertCode(t, Check(g), "edge.port.duplicate")
	})

	t.Run("edge back to start", func(t *testing.T) {
		g := valid3()
		g.Edges[2] = edge("p2", "solved", "start")
		assertCode(t, Check(g), "edge.to.start")
	})
}

func TestReachabilityAndTermination(t *testing.T) {
	t.Run("orphan node", func(t *testing.T) {
		g := valid3()
		g.Nodes = append(g.Nodes, puzzle("orphan"))
		g.Edges = append(g.Edges, edge("orphan", "solved", "end"))
		r := Check(g)
		assertCode(t, r, "node.unreachable")
		// Unreachable is a warning: mid-edit, a node not yet wired up is
		// normal and should not block saving.
		if r.HasErrors() {
			t.Errorf("an unreachable node should warn, not block: %v", r.Errors())
		}
	})

	// Reachable but with no route to an end. Nothing on the canvas makes
	// this obvious, which is exactly why it needs checking.
	t.Run("dead end", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{
				{Slug: "start", Type: graph.Start},
				puzzle("fork", "left", "right"),
				puzzle("trap"), puzzle("good"),
				{Slug: "end", Type: graph.End},
			},
			Edges: []graph.Edge{
				edge("start", graph.PortOut, "fork"),
				edge("fork", "left", "good"),
				edge("fork", "right", "trap"),
				edge("good", "solved", "end"),
				// The trap loops to itself, so nothing downstream of it
				// ever reaches an end. A loop back to the fork would be
				// fine — the fork can still reach "good".
				edge("trap", "solved", "trap"),
			},
		}
		r := Check(g)
		assertCode(t, r, "node.nodeadend")
		if r.OK() {
			t.Error("a reachable dead end must block the save")
		}
	})
}

// A hub loop is the whole point of a hub and must not be flagged.
func TestHubLoopIsNotACycleError(t *testing.T) {
	hub := graph.Node{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{
		Policy:   graph.PolicyChoice,
		Branches: []graph.Branch{{Port: "a"}, {Port: "b"}},
	}}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start}, hub,
			puzzle("task-a"), puzzle("task-b"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "hub"),
			edge("hub", "a", "task-a"),
			edge("hub", "b", "task-b"),
			edge("task-a", "solved", "hub"),
			edge("task-b", "solved", "hub"),
			edge("hub", graph.PortDone, "end"),
		},
	}
	r := Check(g)
	if !r.OK() {
		t.Errorf("a hub with looping branches is valid; got %v", r.Errors())
	}
	assertNoCode(t, r, "graph.cycle.noprogress")
}

// A ring of gates and effects has nothing for a player to do, so a team
// entering it would spin until the request timed out.
func TestProgressFreeCycle(t *testing.T) {
	eff := func(slug string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Effect,
			EffectCfg: &graph.EffectConfig{Kind: graph.AdjustScore, Points: 1}}
	}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			eff("a"), eff("b"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "a"),
			edge("a", graph.PortOut, "b"),
			edge("b", graph.PortOut, "a"),
		},
	}
	assertCode(t, Check(g), "graph.cycle.noprogress")
}

func TestPuzzleOutputRules(t *testing.T) {
	t.Run("no outputs", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs = nil
		assertCode(t, Check(g), "puzzle.outputs.none")
	})

	t.Run("duplicate ports", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs = append(g.Nodes[1].PuzzleCfg.Outputs,
			graph.Output{Port: "solved", Trigger: graph.OnAnswer,
				Matchers: []match.Matcher{{Kind: match.Anything}}})
		assertCode(t, Check(g), "output.port.duplicate")
	})

	// An answer output with no matchers can never fire, so the team is
	// stuck typing into a box that rejects everything.
	t.Run("answer output with no matchers", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs[0].Matchers = nil
		assertCode(t, Check(g), "output.matchers.none")
	})

	t.Run("timeout output without a time limit", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs = append(g.Nodes[1].PuzzleCfg.Outputs,
			graph.Output{Port: "late", Trigger: graph.OnTimeout})
		g.Edges = append(g.Edges, edge("p1", "late", "end"))
		assertCode(t, Check(g), "output.timeout.nolimit")
	})

	t.Run("attempts output without a threshold", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs = append(g.Nodes[1].PuzzleCfg.Outputs,
			graph.Output{Port: "pity", Trigger: graph.OnAttempts})
		g.Edges = append(g.Edges, edge("p1", "pity", "end"))
		assertCode(t, Check(g), "output.attempts.threshold")
	})

	t.Run("skip output without thresholds", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs = append(g.Nodes[1].PuzzleCfg.Outputs,
			graph.Output{Port: "skip", Trigger: graph.OnSkip})
		g.Edges = append(g.Edges, edge("p1", "skip", "end"))
		assertCode(t, Check(g), "output.skip.nothreshold")
	})

	t.Run("unknown trigger", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs[0].Trigger = "invented"
		assertCode(t, Check(g), "output.trigger.unknown")
	})

	// Legal but almost always a mistake, so it warns rather than blocks.
	t.Run("no answer output warns", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.TimeLimitSeconds = 60
		g.Nodes[1].PuzzleCfg.Outputs = []graph.Output{{Port: "solved", Trigger: graph.OnTimeout}}
		r := Check(g)
		assertCode(t, r, "puzzle.noanswer")
		assertNoCode(t, r, "output.timeout.nolimit")
	})
}

func TestHubRules(t *testing.T) {
	newHub := func(cfg *graph.HubConfig) *graph.Graph {
		g := &graph.Graph{
			Nodes: []graph.Node{
				{Slug: "start", Type: graph.Start},
				{Slug: "hub", Type: graph.Hub, HubCfg: cfg},
				{Slug: "end", Type: graph.End},
			},
			Edges: []graph.Edge{
				edge("start", graph.PortOut, "hub"),
				edge("hub", graph.PortDone, "end"),
			},
		}
		for _, b := range cfg.Branches {
			slug := "task-" + b.Port
			g.Nodes = append(g.Nodes, puzzle(slug))
			g.Edges = append(g.Edges, edge("hub", b.Port, slug), edge(slug, "solved", "hub"))
		}
		return g
	}

	t.Run("no branches", func(t *testing.T) {
		assertCode(t, Check(newHub(&graph.HubConfig{Policy: graph.PolicyChoice})), "hub.branches.none")
	})

	// A hub requiring more branches than it has can never complete, and
	// the team is parked there forever.
	t.Run("required exceeds branches", func(t *testing.T) {
		g := newHub(&graph.HubConfig{
			Policy: graph.PolicyChoice, Required: 5,
			Branches: []graph.Branch{{Port: "a"}, {Port: "b"}},
		})
		assertCode(t, Check(g), "hub.required.exceeds")
	})

	t.Run("duplicate branch ports", func(t *testing.T) {
		g := newHub(&graph.HubConfig{
			Policy:   graph.PolicyChoice,
			Branches: []graph.Branch{{Port: "a"}, {Port: "a"}},
		})
		assertCode(t, Check(g), "branch.port.duplicate")
	})

	t.Run("branch named done", func(t *testing.T) {
		g := newHub(&graph.HubConfig{
			Policy:   graph.PolicyChoice,
			Branches: []graph.Branch{{Port: graph.PortDone}},
		})
		assertCode(t, Check(g), "branch.port.reserved")
	})

	t.Run("capacity policy with no capacities warns", func(t *testing.T) {
		g := newHub(&graph.HubConfig{
			Policy:   graph.PolicyCapacity,
			Branches: []graph.Branch{{Port: "a"}},
		})
		r := Check(g)
		assertCode(t, r, "hub.capacity.unset")
		if r.HasErrors() {
			t.Errorf("this should warn, not block: %v", r.Errors())
		}
	})
}

// A gate pointing at a deleted puzzle is the classic way to build an
// adventure that cannot be finished, and it is invisible on the canvas.
func TestDanglingReferences(t *testing.T) {
	withGate := func(p cond.Predicate) *graph.Graph {
		g := valid3()
		g.Nodes = append(g.Nodes, graph.Node{
			Slug: "gate", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: p},
		})
		g.Edges[1] = edge("p1", "solved", "gate")
		g.Edges = append(g.Edges,
			edge("gate", graph.PortPass, "p2"),
			edge("gate", graph.PortFail, "p2"),
		)
		return g
	}

	assertCode(t, Check(withGate(cond.Predicate{Op: cond.OpSolved, Node: "ghost"})), "ref.node.unknown")
	assertCode(t, Check(withGate(cond.Predicate{Op: cond.OpItem, Item: "ghost"})), "ref.item.unknown")
	assertCode(t, Check(withGate(cond.Predicate{Op: cond.OpCounter, Counter: "ghost"})), "ref.counter.unknown")
	assertCode(t, Check(withGate(cond.Predicate{Op: cond.OpInZone, Zone: "ghost"})), "ref.zone.unknown")

	// And the same references resolve once declared.
	g := withGate(cond.Predicate{Op: cond.OpItem, Item: "crowbar"})
	g.Items = []graph.Item{{Slug: "crowbar"}}
	assertNoCode(t, Check(g), "ref.item.unknown")
}

func TestZoneReferences(t *testing.T) {
	g := valid3()
	g.Nodes[1].PuzzleCfg.Zone = "ghost"
	assertCode(t, Check(g), "ref.zone.unknown")

	g2 := valid3()
	g2.Nodes[1].PuzzleCfg.Outputs[0].Zone = "ghost"
	assertCode(t, Check(g2), "ref.zone.unknown")

	g3 := valid3()
	g3.Nodes[1].PuzzleCfg.Zone = "square"
	g3.Zones = []graph.Zone{{Slug: "square", Lat: 59.3, Lng: 18.1, RadiusM: 30}}
	assertNoCode(t, Check(g3), "ref.zone.unknown")
}

// Today's importer never re-checks a regex, so a broken pattern is
// accepted and then rejects every answer at match time with nothing in
// any log. This is the single clearest win of validating properly.
func TestBrokenRegexIsCaughtAtSaveTime(t *testing.T) {
	g := valid3()
	g.Nodes[1].PuzzleCfg.Outputs[0].Matchers = []match.Matcher{
		{Kind: match.Regex, Value: "(unclosed"},
	}
	r := Check(g)
	assertCode(t, r, "matcher.regex.invalid")
	if r.OK() {
		t.Error("a pattern that cannot compile must block the save")
	}
}

func TestMatcherRules(t *testing.T) {
	withMatcher := func(m match.Matcher) Report {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Outputs[0].Matchers = []match.Matcher{m}
		return Check(g)
	}

	assertCode(t, withMatcher(match.Matcher{Kind: match.Near, Target: 10}), "matcher.near.tolerance")
	assertCode(t, withMatcher(match.Matcher{Kind: match.Set}), "matcher.set.empty")
	assertCode(t, withMatcher(match.Matcher{Kind: match.Set, Members: []string{"a"}, Required: 3}), "matcher.set.required")
	assertCode(t, withMatcher(match.Matcher{Kind: match.Exact}), "matcher.value.empty")
	assertCode(t, withMatcher(match.Matcher{Kind: "invented"}), "matcher.kind.unknown")

	// A reversed interval is tolerated at runtime, so it only warns.
	r := withMatcher(match.Matcher{Kind: match.Interval, Low: 20, High: 10})
	assertCode(t, r, "matcher.interval.reversed")
	if r.HasErrors() {
		t.Errorf("a reversed interval should warn, not block: %v", r.Errors())
	}

	// A catch-all is legal: match.Any evaluates it last so it cannot
	// shadow the specific matchers.
	assertNoCode(t, withMatcher(match.Matcher{Kind: match.Anything}), "matcher.kind.unknown")
}

func TestEffectRules(t *testing.T) {
	withEffect := func(cfg *graph.EffectConfig) Report {
		g := valid3()
		g.Nodes = append(g.Nodes, graph.Node{Slug: "fx", Type: graph.Effect, EffectCfg: cfg})
		g.Edges[1] = edge("p1", "solved", "fx")
		g.Edges = append(g.Edges, edge("fx", graph.PortOut, "p2"))
		return Check(g)
	}

	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.GrantItem}), "effect.item.missing")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.SetCounter}), "effect.counter.missing")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.UnlockJournal}), "effect.journal.missing")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.SendEmail}), "effect.email.missing")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.CallWebhook}), "effect.webhook.missing")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.SetTheme}), "effect.theme.missing")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: "invented"}), "effect.kind.unknown")
	assertCode(t, withEffect(&graph.EffectConfig{Kind: graph.AdjustScore}), "effect.score.zero")
}

func TestMissingConfigs(t *testing.T) {
	for _, tc := range []struct {
		name string
		node graph.Node
	}{
		{"puzzle", graph.Node{Slug: "x", Type: graph.Puzzle}},
		{"hub", graph.Node{Slug: "x", Type: graph.Hub}},
		{"gate", graph.Node{Slug: "x", Type: graph.Gate}},
		{"effect", graph.Node{Slug: "x", Type: graph.Effect}},
	} {
		g := valid3()
		g.Nodes = append(g.Nodes, tc.node)
		assertCode(t, Check(g), "node.config.missing")
	}

	g := valid3()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "x", Type: "invented"})
	assertCode(t, Check(g), "node.type.unknown")
}

func TestResourceRules(t *testing.T) {
	g := valid3()
	g.Zones = []graph.Zone{{Slug: "a", RadiusM: 10}, {Slug: "a", RadiusM: 10}}
	assertCode(t, Check(g), "resource.slug.duplicate")

	g2 := valid3()
	g2.Items = []graph.Item{{}}
	assertCode(t, Check(g2), "resource.slug.empty")

	// A zero radius means nobody can ever be inside it.
	g3 := valid3()
	g3.Zones = []graph.Zone{{Slug: "a", RadiusM: 0}}
	assertCode(t, Check(g3), "zone.radius")

	g4 := valid3()
	g4.Zones = []graph.Zone{{Slug: "a", Lat: 999, RadiusM: 10}}
	assertCode(t, Check(g4), "zone.coords")

	lo, hi := 10.0, 5.0
	g5 := valid3()
	g5.Counters = []graph.Counter{{Slug: "c", Min: &lo, Max: &hi}}
	assertCode(t, Check(g5), "counter.bounds")
}

// Validation runs everything rather than stopping at the first problem:
// fixing an adventure one round trip at a time is a miserable way to work.
func TestReportsEveryProblemAtOnce(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			puzzle("p1"),
			{Slug: "p1", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{}},
		},
	}
	r := Check(g)
	for _, want := range []string{
		"graph.start.missing", "graph.end.missing",
		"node.slug.duplicate", "puzzle.outputs.none", "port.unconnected",
	} {
		assertCode(t, r, want)
	}
}

// Errors sort before warnings so the editor shows what blocks the save
// first.
func TestErrorsSortFirst(t *testing.T) {
	g := valid3()
	g.Nodes = append(g.Nodes, puzzle("orphan")) // warning
	g.Edges = g.Edges[:1]                       // errors
	r := Check(g)
	seenWarning := false
	for _, i := range r.Issues {
		if i.Severity == Warning {
			seenWarning = true
		} else if seenWarning {
			t.Fatalf("an error sorted after a warning: %v", codes(r))
		}
	}
}

func TestIssueString(t *testing.T) {
	if got := (Issue{Code: "c", Message: "m"}).String(); got != "[c] m" {
		t.Errorf("String() = %q", got)
	}
	got := (Issue{Code: "c", Node: "n", Port: "p", Message: "m"}).String()
	if !strings.Contains(got, "n.p") {
		t.Errorf("String() = %q; should name the node and port", got)
	}
}

func TestCrossroadsAnswerReferences(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Outputs: []graph.Output{{
				Port: "ok", Trigger: graph.OnAnswer, Matchers: []match.Matcher{
					{ID: "a", Kind: match.Exact, Value: "x"}, {ID: "a", Kind: match.Exact, Value: "y"},
				},
			}}}},
			{Slug: "x", Type: graph.Switch, SwitchCfg: &graph.SwitchConfig{Cases: []graph.Case{
				{Port: "p", Answers: []string{"q/gone"}},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "q"},
			{From: "q", FromPort: "ok", To: "x"},
			{From: "x", FromPort: "p", To: "end"},
			{From: "x", FromPort: "else", To: "end"},
		},
	}
	got := strings.Join(codes(Check(g)), " ")
	for _, want := range []string{"case.answer.unknown", "matcher.id.duplicate"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in %s", want, got)
		}
	}
}

// Several starts are allowed, and reachability counts from all of them.
func TestSeveralStarts(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "s1", Type: graph.Start},
			{Slug: "s2", Type: graph.Start, StartCfg: &graph.StartConfig{Zone: "nowhere"}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "s1", FromPort: "out", To: "end"}, {From: "s2", FromPort: "out", To: "end"}},
	}
	got := strings.Join(codes(Check(g)), " ")
	if strings.Contains(got, "start.duplicate") || strings.Contains(got, "unreachable") {
		t.Errorf("two starts should be fine: %s", got)
	}
	if !strings.Contains(got, "ref.zone.unknown") {
		t.Errorf("a start at an undeclared zone should be reported: %s", got)
	}
}
