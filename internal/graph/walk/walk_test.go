package walk

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
)

// fakeState is a hand-built team. No database, which is the whole point
// of keeping this package pure.
type fakeState struct {
	solved    map[string]bool
	visited   map[string]bool
	outcomes  map[string]string
	items     map[string]int
	branches  map[string]map[string]bool // hub -> port -> done
	occupancy map[string]int             // "hub/port" -> teams inside
	slot      int
	size      int
	score     float64
	now       time.Time
}

func newState() *fakeState {
	return &fakeState{
		solved: map[string]bool{}, visited: map[string]bool{},
		outcomes: map[string]string{}, items: map[string]int{},
		branches: map[string]map[string]bool{}, occupancy: map[string]int{},
		now:  time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
		size: 2,
	}
}

func (f *fakeState) Solved(n string) bool  { return f.solved[n] }
func (f *fakeState) Visited(n string) bool { return f.visited[n] }
func (f *fakeState) Outcome(n string) (string, bool) {
	v, ok := f.outcomes[n]
	return v, ok
}
func (f *fakeState) Attempts(string) int          { return 0 }
func (f *fakeState) ItemQty(i string) int         { return f.items[i] }
func (f *fakeState) CounterValue(string) float64  { return 0 }
func (f *fakeState) Score() float64               { return f.score }
func (f *fakeState) ElapsedSeconds() float64      { return 0 }
func (f *fakeState) NodeElapsedSeconds() float64  { return 0 }
func (f *fakeState) TeamSize() int                { return f.size }
func (f *fakeState) InZone(string) bool           { return false }
func (f *fakeState) PlayersInZone(string) int     { return 0 }
func (f *fakeState) ZonesHeld([]string) bool      { return false }
func (f *fakeState) Here(string) bool             { return false }
func (f *fakeState) HasRole(string) bool          { return false }
func (f *fakeState) Walked() float64              { return 0 }
func (f *fakeState) Picked(string, string) string { return "" }
func (f *fakeState) LocalTime() time.Time         { return f.now }
func (f *fakeState) Slot() int                    { return f.slot }
func (f *fakeState) BranchDone(h, p string) bool  { return f.branches[h][p] }
func (f *fakeState) Occupancy(h, p string) int    { return f.occupancy[h+"/"+p] }
func (f *fakeState) CompletedBranches(h string) int {
	n := 0
	for _, done := range f.branches[h] {
		if done {
			n++
		}
	}
	return n
}

// ApplyEffect folds score and item effects in, which is enough to test
// that a gate sees an effect applied earlier in the same step.
func (f *fakeState) ApplyEffect(_ string, e graph.EffectConfig) {
	switch e.Kind {
	case graph.AdjustScore:
		f.score += float64(e.Points)
	case graph.GrantItem:
		q := e.Qty
		if q == 0 {
			q = 1
		}
		f.items[e.Item] += q
	}
}

func (f *fakeState) finishBranch(hub, port string) {
	if f.branches[hub] == nil {
		f.branches[hub] = map[string]bool{}
	}
	f.branches[hub][port] = true
}

// --- graph builders -------------------------------------------------

func node(slug string, t graph.Type) graph.Node { return graph.Node{Slug: slug, Type: t} }

func puzzle(slug string, ports ...string) graph.Node {
	n := node(slug, graph.Puzzle)
	cfg := &graph.PuzzleConfig{}
	if len(ports) == 0 {
		ports = []string{"solved"}
	}
	for _, p := range ports {
		cfg.Outputs = append(cfg.Outputs, graph.Output{Port: p, Trigger: graph.OnAnswer})
	}
	n.PuzzleCfg = cfg
	return n
}

func edge(from, port, to string) graph.Edge {
	return graph.Edge{From: from, FromPort: port, To: to}
}

// linear builds Start → p1 → p2 → … → End, the shape of the overwhelming
// majority of real adventures.
func linear(n int) *graph.Graph {
	g := &graph.Graph{Nodes: []graph.Node{node("start", graph.Start), node("end", graph.End)}}
	prev, prevPort := "start", graph.PortOut
	for i := 1; i <= n; i++ {
		slug := "p" + string(rune('0'+i))
		g.Nodes = append(g.Nodes, puzzle(slug))
		g.Edges = append(g.Edges, edge(prev, prevPort, slug))
		prev, prevPort = slug, "solved"
	}
	g.Edges = append(g.Edges, edge(prev, prevPort, "end"))
	return g
}

// --- tests ----------------------------------------------------------

func TestLinearWalk(t *testing.T) {
	g := linear(3)
	s := newState()

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if step.To != "p1" {
		t.Fatalf("First landed on %q, want p1", step.To)
	}

	for _, want := range []string{"p2", "p3"} {
		step, err = Advance(g, s, step.To, "solved")
		if err != nil {
			t.Fatalf("Advance: %v", err)
		}
		if step.To != want {
			t.Fatalf("landed on %q, want %q", step.To, want)
		}
	}

	step, err = Advance(g, s, "p3", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if !step.Finished || step.Outcome != graph.Complete {
		t.Fatalf("expected a completed run, got finished=%v outcome=%q", step.Finished, step.Outcome)
	}
}

// The branching mechanism: one puzzle, two answers, two destinations —
// with no key granted anywhere.
func TestPuzzleOutputsRouteByAnswer(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			node("start", graph.Start),
			puzzle("door", "left", "right"),
			puzzle("church"), puzzle("market"),
			node("end", graph.End),
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "door"),
			edge("door", "left", "church"),
			edge("door", "right", "market"),
			edge("church", "solved", "end"),
			edge("market", "solved", "end"),
		},
	}
	s := newState()

	left, err := Advance(g, s, "door", "left")
	if err != nil {
		t.Fatalf("left: %v", err)
	}
	if left.To != "church" {
		t.Errorf("left output went to %q, want church", left.To)
	}

	right, err := Advance(g, s, "door", "right")
	if err != nil {
		t.Fatalf("right: %v", err)
	}
	if right.To != "market" {
		t.Errorf("right output went to %q, want market", right.To)
	}
}

// Gates, Effects and Hubs resolve inside one Advance — a player is never
// parked on machinery, only on something they act upon.
func TestGatesAndEffectsAreInvisibleToThePlayer(t *testing.T) {
	grant := node("grant", graph.Effect)
	grant.EffectCfg = &graph.EffectConfig{Kind: graph.GrantItem, Item: "crowbar", Qty: 1}

	gate := node("check", graph.Gate)
	gate.GateCfg = &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpSolved, Node: "bakery"}}

	g := &graph.Graph{
		Nodes: []graph.Node{
			node("start", graph.Start), puzzle("bakery"), grant, gate,
			puzzle("vault"), puzzle("consolation"), node("end", graph.End),
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "bakery"),
			edge("bakery", "solved", "grant"),
			edge("grant", graph.PortOut, "check"),
			edge("check", graph.PortPass, "vault"),
			edge("check", graph.PortFail, "consolation"),
			edge("vault", "solved", "end"),
			edge("consolation", "solved", "end"),
		},
	}

	s := newState()
	s.solved["bakery"] = true

	step, err := Advance(g, s, "bakery", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if step.To != "vault" {
		t.Errorf("landed on %q, want vault (the gate should have passed)", step.To)
	}
	if len(step.Effects) != 1 || step.Effects[0].Slug != "grant" {
		t.Errorf("expected to collect the grant effect, got %v", step.Effects)
	}

	// The same graph with the condition unmet routes the other way.
	s2 := newState()
	step2, err := Advance(g, s2, "bakery", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if step2.To != "consolation" {
		t.Errorf("landed on %q, want consolation (the gate should have failed)", step2.To)
	}
}

// hubGraph: Start → hub{a,b,c} with branches looping back, done → End.
func hubGraph(policy graph.Policy, required int, branches ...graph.Branch) *graph.Graph {
	hub := node("hub", graph.Hub)
	hub.HubCfg = &graph.HubConfig{Policy: policy, Required: required, Branches: branches}

	g := &graph.Graph{
		Nodes: []graph.Node{node("start", graph.Start), hub, node("end", graph.End)},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "hub"),
			edge("hub", graph.PortDone, "end"),
		},
	}
	for _, b := range branches {
		slug := "task-" + b.Port
		g.Nodes = append(g.Nodes, puzzle(slug))
		g.Edges = append(g.Edges,
			edge("hub", b.Port, slug),
			edge(slug, "solved", "hub"), // the loop back
		)
	}
	return g
}

func branchSet(ports ...string) []graph.Branch {
	out := make([]graph.Branch, 0, len(ports))
	for _, p := range ports {
		out = append(out, graph.Branch{Port: p})
	}
	return out
}

func TestHubChoiceOffersOpenBranches(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 0, branchSet("a", "b", "c")...)
	s := newState()

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if step.To != "hub" || len(step.Offer) != 3 {
		t.Fatalf("expected an offer of 3 branches at the hub, got To=%q offer=%d", step.To, len(step.Offer))
	}

	entered, err := EnterBranch(g, s, "hub", "b")
	if err != nil {
		t.Fatalf("EnterBranch: %v", err)
	}
	if entered.To != "task-b" {
		t.Errorf("entered %q, want task-b", entered.To)
	}

	// A finished branch is not offered again.
	s.finishBranch("hub", "b")
	step, _ = Advance(g, s, "task-b", "solved")
	if len(step.Offer) != 2 {
		t.Errorf("expected 2 remaining branches, got %d", len(step.Offer))
	}
}

// "Do any 2 of 3, then move on" — legacy's routes_required.
func TestHubRequiredCount(t *testing.T) {
	g := hubGraph(graph.PolicySequence, 2, branchSet("a", "b", "c")...)
	s := newState()

	s.finishBranch("hub", "a")
	step, err := Advance(g, s, "task-a", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if step.To != "task-b" {
		t.Fatalf("after 1 of 2 the team should get another branch, got %q", step.To)
	}

	s.finishBranch("hub", "b")
	step, err = Advance(g, s, "task-b", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if !step.Finished {
		t.Errorf("after 2 of 2 the hub should be done and the run finished, landed on %q", step.To)
	}
}

func TestHubRequiredZeroMeansAll(t *testing.T) {
	g := hubGraph(graph.PolicySequence, 0, branchSet("a", "b")...)
	s := newState()
	s.finishBranch("hub", "a")

	step, _ := Advance(g, s, "task-a", "solved")
	if step.Finished {
		t.Error("Required=0 should mean all branches, so one is not enough")
	}

	s.finishBranch("hub", "b")
	step, _ = Advance(g, s, "task-b", "solved")
	if !step.Finished {
		t.Error("both branches done should complete the hub")
	}
}

// The shuffle exists so eight teams do not all walk to the same station
// at the same minute. With N teams and N branches, every branch should
// be someone's first.
func TestShuffleGivesEachSlotADifferentFirstBranch(t *testing.T) {
	ports := []string{"a", "b", "c"}
	g := hubGraph(graph.PolicyShuffle, 0, branchSet(ports...)...)

	firsts := map[string]int{}
	for slot := range 3 {
		s := newState()
		s.slot = slot
		step, err := First(g, s)
		if err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
		firsts[step.To]++
	}
	if len(firsts) != 3 {
		t.Errorf("3 slots over 3 branches should each start somewhere different, got %v", firsts)
	}
}

func TestSpreadPicksTheLeastOccupiedBranch(t *testing.T) {
	g := hubGraph(graph.PolicySpread, 0, branchSet("a", "b", "c")...)
	s := newState()
	s.occupancy["hub/a"] = 5
	s.occupancy["hub/b"] = 1
	s.occupancy["hub/c"] = 3

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if step.To != "task-b" {
		t.Errorf("spread sent the team to %q, want task-b (the emptiest)", step.To)
	}
}

// Crowd control at a physical station. This is the one policy whose
// failure is visible in the field — two teams at a one-team location.
func TestCapacitySkipsFullBranches(t *testing.T) {
	g := hubGraph(graph.PolicyCapacity, 0,
		graph.Branch{Port: "a", Capacity: 1},
		graph.Branch{Port: "b", Capacity: 2},
	)
	s := newState()
	s.occupancy["hub/a"] = 1 // full

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if step.To != "task-b" {
		t.Errorf("capacity sent the team to %q, want task-b (a is full)", step.To)
	}
}

// When every branch is full a team must still be sent somewhere. A
// player standing in a park with no instruction is worse than a queue,
// which is the call legacy made too.
func TestCapacityOverflowsRatherThanStranding(t *testing.T) {
	g := hubGraph(graph.PolicyCapacity, 0,
		graph.Branch{Port: "a", Capacity: 1},
		graph.Branch{Port: "b", Capacity: 1},
	)
	s := newState()
	s.occupancy["hub/a"] = 9
	s.occupancy["hub/b"] = 9

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("every branch full should still route, got error: %v", err)
	}
	if step.To == "" {
		t.Error("expected a destination even when every branch is over capacity")
	}
}

// Random must be reproducible, or the simulator and the recap disagree
// with what the team actually played.
func TestRandomIsSeededAndReproducible(t *testing.T) {
	g := hubGraph(graph.PolicyRandom, 0, branchSet("a", "b", "c")...)

	s := newState()
	s.slot = 7
	first, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	for range 10 {
		again, _ := First(g, newStateWithSlot(7))
		if again.To != first.To {
			t.Fatalf("random policy is not reproducible: %q then %q", first.To, again.To)
		}
	}
}

func newStateWithSlot(slot int) *fakeState {
	s := newState()
	s.slot = slot
	return s
}

// A branch condition decides whether the branch is *offered*. This is
// the hub-level counterpart to a Gate, and it is what HideLocked hides.
func TestBranchConditionsGateTheOffer(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 0,
		graph.Branch{Port: "open"},
		graph.Branch{Port: "locked", Condition: cond.Predicate{Op: cond.OpSolved, Node: "key-puzzle"}},
	)

	s := newState()
	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if len(step.Offer) != 1 || step.Offer[0].Port != "open" {
		t.Errorf("only the open branch should be offered, got %v", step.Offer)
	}

	// Entering a shut branch directly must be refused, not just hidden.
	if _, err := EnterBranch(g, s, "hub", "locked"); err == nil {
		t.Error("entering a locked branch should fail")
	}

	s.solved["key-puzzle"] = true
	step, _ = First(g, s)
	if len(step.Offer) != 2 {
		t.Errorf("both branches should be offered once the condition holds, got %d", len(step.Offer))
	}
}

// If every remaining branch is shut but the team has done all it ever
// could, finish rather than strand them.
func TestHubCompletesWhenNothingRemainsReachable(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 2,
		graph.Branch{Port: "a"},
		graph.Branch{Port: "b", Condition: cond.Predicate{Op: cond.OpSolved, Node: "never"}},
	)
	s := newState()
	s.finishBranch("hub", "a")

	step, err := Advance(g, s, "task-a", "solved")
	if err != nil {
		t.Fatalf("expected a graceful completion, got %v", err)
	}
	if !step.Finished {
		t.Errorf("expected the run to finish; landed on %q", step.To)
	}
}

// A branch shut only until the players get in place — one at the gate,
// one at the bridge — keeps the team at the hub with nothing to choose
// yet. A team too small to do it moves on, as from any shut branch.
func TestHubWaitsForPlayersToSpreadOut(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 2,
		graph.Branch{Port: "a"},
		graph.Branch{Port: "b", Condition: cond.Predicate{Op: cond.OpZonesHeld, Members: []string{"gate", "bridge"}}},
	)
	s := newState()
	s.finishBranch("hub", "a")

	step, err := Advance(g, s, "task-a", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.Finished || step.To != "hub" || step.Offer == nil || len(step.Offer) != 0 {
		t.Fatalf("a team of two should wait at the hub: to %q, offer %v, finished %v", step.To, step.Offer, step.Finished)
	}

	s.size = 1
	step, err = Advance(g, s, "task-a", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if !step.Finished {
		t.Errorf("a team of one cannot spread out and should move on; landed on %q", step.To)
	}
}

// A branch locked until the team is somewhere is waiting, not shut:
// the team stays to walk there.
func TestHubWaitsForTheTeamToGetThere(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 2,
		graph.Branch{Port: "a"},
		graph.Branch{Port: "b", Condition: cond.Predicate{Op: cond.OpInZone, Zone: "harbour"}},
	)
	s := newState()
	s.finishBranch("hub", "a")
	s.size = 1
	step, err := Advance(g, s, "task-a", "solved")
	if err != nil {
		t.Fatal(err)
	}
	if step.Finished || step.To != "hub" {
		t.Errorf("the team should wait at the hub; landed on %q, finished %v", step.To, step.Finished)
	}
}

func TestErrors(t *testing.T) {
	t.Run("no start", func(t *testing.T) {
		_, err := First(&graph.Graph{}, newState())
		if !errors.Is(err, ErrNoStart) {
			t.Errorf("got %v, want ErrNoStart", err)
		}
	})

	t.Run("dangling port", func(t *testing.T) {
		g := &graph.Graph{Nodes: []graph.Node{node("start", graph.Start)}}
		_, err := First(g, newState())
		if !errors.Is(err, ErrDanglingPort) {
			t.Errorf("got %v, want ErrDanglingPort", err)
		}
	})

	t.Run("edge to nowhere", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{node("start", graph.Start)},
			Edges: []graph.Edge{edge("start", graph.PortOut, "ghost")},
		}
		if _, err := First(g, newState()); err == nil {
			t.Error("expected an error for an edge pointing at a missing node")
		}
	})

	// A ring of effect nodes makes no progress and would otherwise spin
	// the request until it timed out.
	t.Run("progress-free cycle", func(t *testing.T) {
		a := node("a", graph.Effect)
		a.EffectCfg = &graph.EffectConfig{Kind: graph.AdjustScore}
		b := node("b", graph.Effect)
		b.EffectCfg = &graph.EffectConfig{Kind: graph.AdjustScore}

		g := &graph.Graph{
			Nodes: []graph.Node{node("start", graph.Start), a, b},
			Edges: []graph.Edge{
				edge("start", graph.PortOut, "a"),
				edge("a", graph.PortOut, "b"),
				edge("b", graph.PortOut, "a"),
			},
		}
		if _, err := First(g, newState()); !errors.Is(err, ErrLoop) {
			t.Errorf("got %v, want ErrLoop", err)
		}
	})

	t.Run("edge back to start", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{node("start", graph.Start), puzzle("p")},
			Edges: []graph.Edge{
				edge("start", graph.PortOut, "p"),
				edge("p", "solved", "start"),
			},
		}
		if _, err := Advance(g, newState(), "p", "solved"); !errors.Is(err, ErrLoop) {
			t.Errorf("got %v, want ErrLoop", err)
		}
	})

	t.Run("enter branch on a non-hub", func(t *testing.T) {
		g := linear(1)
		if _, err := EnterBranch(g, newState(), "p1", "x"); err == nil {
			t.Error("expected an error entering a branch on a puzzle")
		}
	})

	t.Run("unknown branch", func(t *testing.T) {
		g := hubGraph(graph.PolicyChoice, 0, branchSet("a")...)
		if _, err := EnterBranch(g, newState(), "hub", "nope"); err == nil {
			t.Error("expected an error for an unknown branch port")
		}
	})

	t.Run("unknown policy", func(t *testing.T) {
		g := hubGraph("invented", 0, branchSet("a")...)
		if _, err := First(g, newState()); err == nil {
			t.Error("expected an error for an unknown hub policy")
		}
	})
}

func TestEndOutcomeFail(t *testing.T) {
	end := node("end", graph.End)
	end.EndCfg = &graph.EndConfig{Outcome: graph.Failed}
	g := &graph.Graph{
		Nodes: []graph.Node{node("start", graph.Start), end},
		Edges: []graph.Edge{edge("start", graph.PortOut, "end")},
	}
	step, err := First(g, newState())
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if !step.Finished || step.Outcome != graph.Failed {
		t.Errorf("expected a failed outcome, got finished=%v outcome=%q", step.Finished, step.Outcome)
	}
}

// The scenario from the plan's verification section, end to end: a hub
// with a capacity-limited branch, a gate on history, and two puzzle
// outcomes that diverge and later reconverge.
func TestBranchingAdventureEndToEnd(t *testing.T) {
	gate := node("has-code", graph.Gate)
	gate.GateCfg = &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpSolved, Node: "task-church"}}

	hub := node("hub", graph.Hub)
	hub.HubCfg = &graph.HubConfig{
		Policy:   graph.PolicyChoice,
		Required: 1,
		Branches: []graph.Branch{
			{Port: "church", Capacity: 2},
			{Port: "market"},
		},
	}

	g := &graph.Graph{
		Nodes: []graph.Node{
			node("start", graph.Start),
			puzzle("statue"),
			hub,
			puzzle("task-church"), puzzle("task-market"),
			gate,
			puzzle("vault"),
			node("hint", graph.Story),
			node("end", graph.End),
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "statue"),
			edge("statue", "solved", "hub"),
			edge("hub", "church", "task-church"),
			edge("hub", "market", "task-market"),
			edge("task-church", "solved", "hub"),
			edge("task-market", "solved", "hub"),
			edge("hub", graph.PortDone, "has-code"),
			edge("has-code", graph.PortPass, "vault"),
			edge("has-code", graph.PortFail, "hint"),
			edge("hint", graph.PortOut, "hub"),
			edge("vault", "solved", "end"),
		},
	}

	s := newState()

	// Start, solve the statue, arrive at the hub and be offered a choice.
	step, err := First(g, s)
	if err != nil || step.To != "statue" {
		t.Fatalf("First = %q, %v", step.To, err)
	}
	step, err = Advance(g, s, "statue", "solved")
	if err != nil || len(step.Offer) != 2 {
		t.Fatalf("expected a 2-way offer at the hub, got %v %v", step.Offer, err)
	}

	// Take the market branch — which does NOT grant the code.
	step, err = EnterBranch(g, s, "hub", "market")
	if err != nil || step.To != "task-market" {
		t.Fatalf("EnterBranch(market) = %q, %v", step.To, err)
	}
	s.solved["task-market"] = true
	s.finishBranch("hub", "market")

	// Required is 1, so the team may now move on — but the church branch
	// is still there, so they are offered both. The choice is theirs.
	step, err = Advance(g, s, "task-market", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if !step.CanFinish {
		t.Fatalf("with Required met the team should be allowed to move on, got %+v", step)
	}
	if len(step.Offer) != 1 || step.Offer[0].Port != "church" {
		t.Fatalf("the unfinished church branch should still be offered, got %v", step.Offer)
	}

	// They choose to move on — and the gate sends them back for the code
	// they never collected.
	step, err = EnterBranch(g, s, "hub", graph.PortDone)
	if err != nil {
		t.Fatalf("EnterBranch(done): %v", err)
	}
	if step.To != "hint" {
		t.Fatalf("without the code the gate should route to the hint, got %q", step.To)
	}

	// From the hint, back to the hub; the church branch is still open.
	step, err = Advance(g, s, "hint", graph.PortOut)
	if err != nil {
		t.Fatalf("Advance from hint: %v", err)
	}
	if len(step.Offer) != 1 || step.Offer[0].Port != "church" {
		t.Fatalf("expected only the church branch to remain, got %v", step.Offer)
	}

	// Do the church, and now the gate passes and the vault opens.
	step, err = EnterBranch(g, s, "hub", "church")
	if err != nil || step.To != "task-church" {
		t.Fatalf("EnterBranch(church) = %q, %v", step.To, err)
	}
	s.solved["task-church"] = true
	s.finishBranch("hub", "church")

	step, err = Advance(g, s, "task-church", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if step.To != "vault" {
		t.Fatalf("with the code the gate should open the vault, got %q", step.To)
	}

	step, err = Advance(g, s, "vault", "solved")
	if err != nil || !step.Finished {
		t.Fatalf("expected the run to finish, got %q finished=%v err=%v", step.To, step.Finished, err)
	}
}

// The DB layer tracks occupancy and branch completion from Passed and
// Entered, so both must report exactly what the walk did — including
// under an automatic policy, where the walk picks the branch itself and
// the caller has no other way to find out which one.
func TestStepReportsPassedAndEntered(t *testing.T) {
	g := hubGraph(graph.PolicySequence, 0, branchSet("a", "b")...)
	s := newState()

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if want := []string{"hub", "task-a"}; !equal(step.Passed, want) {
		t.Errorf("Passed = %v, want %v", step.Passed, want)
	}
	if len(step.Entered) != 1 || step.Entered[0] != (BranchRef{Hub: "hub", Port: "a"}) {
		t.Errorf("Entered = %v, want [hub.a]", step.Entered)
	}

	// Coming back through the hub into the next branch.
	s.finishBranch("hub", "a")
	step, err = Advance(g, s, "task-a", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if want := []string{"hub", "task-b"}; !equal(step.Passed, want) {
		t.Errorf("Passed = %v, want %v", step.Passed, want)
	}
	if len(step.Entered) != 1 || step.Entered[0].Port != "b" {
		t.Errorf("Entered = %v, want [hub.b]", step.Entered)
	}
}

func TestEnterBranchReportsTheChosenBranch(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 0, branchSet("a", "b")...)
	step, err := EnterBranch(g, newState(), "hub", "b")
	if err != nil {
		t.Fatalf("EnterBranch: %v", err)
	}
	if len(step.Entered) != 1 || step.Entered[0] != (BranchRef{Hub: "hub", Port: "b"}) {
		t.Errorf("Entered = %v, want [hub.b]", step.Entered)
	}
}

// Choosing to move on is refused until the requirement is actually met,
// and a branch already done cannot be entered twice.
func TestEnterBranchGuards(t *testing.T) {
	g := hubGraph(graph.PolicyChoice, 1, branchSet("a", "b")...)
	s := newState()
	if _, err := EnterBranch(g, s, "hub", graph.PortDone); err == nil {
		t.Error("moving on before any branch is done should fail")
	}
	s.finishBranch("hub", "a")
	if _, err := EnterBranch(g, s, "hub", "a"); err == nil {
		t.Error("re-entering a finished branch should fail")
	}
	step, err := EnterBranch(g, s, "hub", graph.PortDone)
	if err != nil || !step.Finished {
		t.Errorf("moving on once satisfied should finish the run; got %+v, %v", step, err)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A converted shuffled lane must play in exactly the order zv gives it
// today, or converting an adventure silently changes where teams go.
// LanePermutation is canonical[(slot+i) % n]; the hub must reproduce
// that whole sequence as branches complete, not just the first pick.
func TestShuffleMatchesLanePermutationOrder(t *testing.T) {
	ports := []string{"a", "b", "c", "d"}
	g := hubGraph(graph.PolicyShuffle, 0, branchSet(ports...)...)

	for slot := range 6 {
		s := newStateWithSlot(slot)
		var got []string
		step, err := First(g, s)
		for err == nil && !step.Finished {
			port := strings.TrimPrefix(step.To, "task-")
			got = append(got, port)
			s.finishBranch("hub", port)
			step, err = Advance(g, s, step.To, "solved")
		}
		if err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}

		var want []string
		for i := range len(ports) {
			want = append(want, ports[(slot+i)%len(ports)])
		}
		if !equal(got, want) {
			t.Errorf("slot %d visited %v, want LanePermutation order %v", slot, got, want)
		}
	}
}

// An effect must be visible to a gate later in the same step, or
// "award 50 points, then open the vault at 100" tests the old score.
func TestGateSeesEffectAppliedEarlierInTheSameStep(t *testing.T) {
	bonus := node("bonus", graph.Effect)
	bonus.EffectCfg = &graph.EffectConfig{Kind: graph.AdjustScore, Points: 50}
	gate := node("rich", graph.Gate)
	gate.GateCfg = &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpScore, N: 100}}

	g := &graph.Graph{
		Nodes: []graph.Node{
			node("start", graph.Start), puzzle("p"), bonus, gate,
			puzzle("vault"), puzzle("poor"), node("end", graph.End),
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "p"),
			edge("p", "solved", "bonus"),
			edge("bonus", graph.PortOut, "rich"),
			edge("rich", graph.PortPass, "vault"),
			edge("rich", graph.PortFail, "poor"),
			edge("vault", "solved", "end"),
			edge("poor", "solved", "end"),
		},
	}

	s := newState()
	s.score = 60
	step, err := Advance(g, s, "p", "solved")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if step.To != "vault" {
		t.Errorf("60 + 50 should clear the 100 gate; landed on %q", step.To)
	}
}

// Arriving at a puzzle you already left takes you on the way you left
// it, rather than presenting the riddle again.
func TestRevisitedPuzzleIsPassedThrough(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			node("start", graph.Start), puzzle("riddle", "left", "right"),
			puzzle("a"), puzzle("b"), node("end", graph.End),
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "riddle"),
			edge("riddle", "left", "a"),
			edge("riddle", "right", "b"),
			edge("a", "solved", "end"),
			edge("b", "solved", "end"),
		},
	}
	s := newState()
	s.outcomes["riddle"] = "right"

	step, err := First(g, s)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if step.To != "b" {
		t.Errorf("an already-answered puzzle should pass through by its last outcome; landed on %q", step.To)
	}
}

func TestRepeatablePuzzleIsPresentedAgain(t *testing.T) {
	p := puzzle("riddle")
	p.PuzzleCfg.Repeatable = true
	g := &graph.Graph{
		Nodes: []graph.Node{node("start", graph.Start), p, node("end", graph.End)},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "riddle"),
			edge("riddle", "solved", "end"),
		},
	}
	s := newState()
	s.outcomes["riddle"] = "solved"
	step, err := First(g, s)
	if err != nil || step.To != "riddle" {
		t.Errorf("a repeatable puzzle should be presented again; got %q, %v", step.To, err)
	}
}

func (f *fakeState) AnswerTo(string) (string, bool) { return "", false }
func (f *fakeState) LastAnswer() (string, bool)     { return "", false }
func (f *fakeState) MatchedAnswer(string) string    { return "" }
