package convert

import (
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
)

func radius(r float64) *float64 { return &r }

func accept(texts ...string) []Accept {
	out := make([]Accept, 0, len(texts))
	for _, t := range texts {
		out = append(out, Accept{Text: t})
	}
	return out
}

// Every converted adventure must validate, or conversion hands the
// author a graph they cannot save.
func mustValidate(t *testing.T, g *graph.Graph) {
	t.Helper()
	if r := valid.Check(g); !r.OK() {
		t.Fatalf("converted graph does not validate: %v", r.Errors())
	}
}

func TestLinearLaneBecomesAChain(t *testing.T) {
	g := ToGraph(Adventure{
		Lanes: []Lane{{Idx: 0, Mode: "linear"}},
		Pages: []Page{
			{Idx: 1, LaneIdx: 0, Title: "Second", Accept: accept("b")},
			{Idx: 0, LaneIdx: 0, Title: "First", Accept: accept("a")},
		},
	})
	mustValidate(t, g)

	// Pages play in index order regardless of their order in the file.
	got := playLinear(t, g)
	want := []string{PageSlug(0), PageSlug(1)}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("visited %v, want %v", got, want)
	}
}

func TestAcceptEntriesBecomeMatchers(t *testing.T) {
	g := ToGraph(Adventure{Pages: []Page{{
		Idx: 0, Title: "PIN",
		Accept: []Accept{{Text: "Blue Door"}, {Text: `\d{4}`, Regex: true}},
	}}})
	mustValidate(t, g)

	n, _ := g.Node(PageSlug(0))
	out := n.PuzzleCfg.Outputs[0]

	// Same semantics as today: literal and anchored regex.
	for _, guess := range []string{"blue door", "1234"} {
		if !match.Any(out.Matchers, guess).OK {
			t.Errorf("converted matchers should accept %q", guess)
		}
	}
	if match.Any(out.Matchers, "my pin is 1234").OK {
		t.Error("the regex must stay anchored after conversion")
	}
}

// zv shows a page with no accepted answers as a Continue button, which
// is exactly a Story node.
func TestNoAnswerPageBecomesStory(t *testing.T) {
	g := ToGraph(Adventure{Pages: []Page{{Idx: 0, Title: "Intro", Prompt: "Welcome"}}})
	mustValidate(t, g)

	n, ok := g.Node(PageSlug(0))
	if !ok || n.Type != graph.Story {
		t.Fatalf("expected a story node, got %+v", n)
	}
	if n.StoryCfg.Text != "Welcome" {
		t.Errorf("story text = %q, want the page prompt", n.StoryCfg.Text)
	}
}

func TestRadiusBecomesAZone(t *testing.T) {
	g := ToGraph(Adventure{Pages: []Page{
		{Idx: 0, Title: "Statue", Accept: accept("x"), Lat: 59.33, Lng: 18.07, RadiusM: radius(25)},
		{Idx: 1, Title: "Anywhere", Accept: accept("y")}, // ungated
	}})
	mustValidate(t, g)

	if len(g.Zones) != 1 {
		t.Fatalf("expected exactly one zone, got %d", len(g.Zones))
	}
	z := g.Zones[0]
	if z.Lat != 59.33 || z.Lng != 18.07 || z.RadiusM != 25 {
		t.Errorf("zone = %+v", z)
	}
	gated, _ := g.Node(PageSlug(0))
	if gated.PuzzleCfg.Zone != z.Slug {
		t.Error("the gated page should reference its zone")
	}
	free, _ := g.Node(PageSlug(1))
	if free.PuzzleCfg.Zone != "" {
		t.Error("an ungated page must not get a zone")
	}
}

func TestHintsCarryOverInOrder(t *testing.T) {
	g := ToGraph(Adventure{Pages: []Page{{
		Idx: 0, Accept: accept("x"),
		Hints: []Hint{{Idx: 1, Text: "second", DelaySeconds: 120}, {Idx: 0, Text: "first", DelaySeconds: 60}},
	}}})
	n, _ := g.Node(PageSlug(0))
	h := n.PuzzleCfg.Hints
	if len(h) != 2 || h[0].Text != "first" || h[1].DelaySeconds != 120 {
		t.Errorf("hints = %+v", h)
	}
}

func TestDisabledPagesAreDropped(t *testing.T) {
	g := ToGraph(Adventure{Pages: []Page{
		{Idx: 0, Accept: accept("a")},
		{Idx: 1, Accept: accept("b"), Disabled: true},
	}})
	mustValidate(t, g)
	if _, ok := g.Node(PageSlug(1)); ok {
		t.Error("a disabled page must not appear in the graph")
	}
}

func TestLanesRunInOrder(t *testing.T) {
	g := ToGraph(Adventure{
		Lanes: []Lane{{Idx: 1, Mode: "linear"}, {Idx: 0, Mode: "linear"}},
		Pages: []Page{
			{Idx: 0, LaneIdx: 1, Accept: accept("later")},
			{Idx: 1, LaneIdx: 0, Accept: accept("sooner")},
		},
	})
	mustValidate(t, g)
	got := playLinear(t, g)
	if got[0] != PageSlug(1) || got[1] != PageSlug(0) {
		t.Errorf("lane 0 should run before lane 1; visited %v", got)
	}
}

// The v1 importer sends a page with an unknown lane to lane 0; the
// conversion must place it the same way.
func TestUnknownLaneFallsBackToLaneZero(t *testing.T) {
	g := ToGraph(Adventure{
		Lanes: []Lane{{Idx: 0, Mode: "linear"}},
		Pages: []Page{{Idx: 0, LaneIdx: 7, Accept: accept("x")}},
	})
	mustValidate(t, g)
	if _, ok := g.Node(PageSlug(0)); !ok {
		t.Error("a page with an unknown lane should still be converted")
	}
}

func TestEmptyAdventure(t *testing.T) {
	g := ToGraph(Adventure{})
	mustValidate(t, g)
	if len(g.Nodes) != 2 {
		t.Errorf("an empty adventure should convert to start and end only, got %d nodes", len(g.Nodes))
	}
}

// The fidelity requirement: a converted shuffled lane must visit pages
// in exactly the order zv gives each team today, checked against the
// real adventure.LanePermutation rather than a reimplementation of it.
func TestShuffledLanePlaysInLanePermutationOrder(t *testing.T) {
	const pages = 5
	a := Adventure{Lanes: []Lane{{Idx: 0, Mode: shuffled}}}
	canonical := make([]int64, 0, pages)
	for i := range pages {
		a.Pages = append(a.Pages, Page{Idx: i, LaneIdx: 0, Accept: accept("x")})
		canonical = append(canonical, int64(i))
	}
	g := ToGraph(a)
	mustValidate(t, g)

	for slot := range 8 {
		want := adventure.LanePermutation(adventure.LaneModeShuffled, canonical, slot)
		got := playHub(t, g, slot)
		if len(got) != len(want) {
			t.Fatalf("slot %d: visited %d pages, want %d", slot, len(got), len(want))
		}
		for i := range want {
			if got[i] != PageSlug(int(want[i])) {
				t.Errorf("slot %d: visited %v, LanePermutation says %v", slot, got, want)
				break
			}
		}
	}
}

// Linear, shuffled and linear again: the realistic mixed shape.
func TestMixedLanes(t *testing.T) {
	g := ToGraph(Adventure{
		Lanes: []Lane{{Idx: 0, Mode: "linear"}, {Idx: 1, Mode: shuffled}, {Idx: 2, Mode: "linear"}},
		Pages: []Page{
			{Idx: 0, LaneIdx: 0, Accept: accept("a")},
			{Idx: 1, LaneIdx: 1, Accept: accept("b")},
			{Idx: 2, LaneIdx: 1, Accept: accept("c")},
			{Idx: 3, LaneIdx: 2, Accept: accept("d")},
		},
	})
	mustValidate(t, g)

	s := newState(0)
	var visited []string
	step, err := walk.First(g, s)
	for err == nil && !step.Finished {
		visited = append(visited, step.To)
		s.solve(step)
		step, err = walk.Advance(g, s, step.To, "solved")
	}
	if err != nil {
		t.Fatalf("walking the mixed graph: %v", err)
	}
	if len(visited) != 4 || visited[0] != PageSlug(0) || visited[3] != PageSlug(3) {
		t.Errorf("visited %v; want page-0 first, page-3 last, both shuffled pages between", visited)
	}
}

// --- a minimal State for walking converted graphs --------------------

type state struct {
	slot     int
	branches map[string]map[string]bool
}

func newState(slot int) *state {
	return &state{slot: slot, branches: map[string]map[string]bool{}}
}

// solve records the hub branch a step entered, the way internal/play
// does: an entered branch counts as done once the team is back at the hub.
func (s *state) solve(step walk.Step) {
	for _, e := range step.Entered {
		if s.branches[e.Hub] == nil {
			s.branches[e.Hub] = map[string]bool{}
		}
		s.branches[e.Hub][e.Port] = true
	}
}

func (s *state) Solved(string) bool                     { return false }
func (s *state) Visited(string) bool                    { return false }
func (s *state) Outcome(string) (string, bool)          { return "", false }
func (s *state) Attempts(string) int                    { return 0 }
func (s *state) ItemQty(string) int                     { return 0 }
func (s *state) CounterValue(string) float64            { return 0 }
func (s *state) Score() float64                         { return 0 }
func (s *state) ElapsedSeconds() float64                { return 0 }
func (s *state) NodeElapsedSeconds() float64            { return 0 }
func (s *state) TeamSize() int                          { return 1 }
func (s *state) InZone(string) bool                     { return true }
func (s *state) LocalTime() time.Time                   { return time.Time{} }
func (s *state) Slot() int                              { return s.slot }
func (s *state) Occupancy(string, string) int           { return 0 }
func (s *state) ApplyEffect(string, graph.EffectConfig) {}
func (s *state) BranchDone(h, p string) bool            { return s.branches[h][p] }
func (s *state) CompletedBranches(h string) int         { return len(s.branches[h]) }

func playLinear(t *testing.T, g *graph.Graph) []string {
	t.Helper()
	return playHub(t, g, 0)
}

func playHub(t *testing.T, g *graph.Graph, slot int) []string {
	t.Helper()
	s := newState(slot)
	var visited []string
	step, err := walk.First(g, s)
	for err == nil && !step.Finished {
		visited = append(visited, step.To)
		s.solve(step)
		port := "solved"
		if n, _ := g.Node(step.To); n.Type == graph.Story {
			port = graph.PortOut
		}
		step, err = walk.Advance(g, s, step.To, port)
	}
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	return visited
}
