package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPortsByNodeType(t *testing.T) {
	cases := []struct {
		name string
		node Node
		want []string
	}{
		{"start", Node{Type: Start}, []string{PortOut}},
		{"story", Node{Type: Story}, []string{PortOut}},
		{"effect", Node{Type: Effect}, []string{PortOut}},
		{"gate", Node{Type: Gate}, []string{PortPass, PortFail}},
		{"end has no outputs", Node{Type: End}, nil},
		{
			"puzzle exposes one port per output",
			Node{Type: Puzzle, PuzzleCfg: &PuzzleConfig{Outputs: []Output{
				{Port: "left"}, {Port: "right"}, {Port: "gave-up"},
			}}},
			[]string{"left", "right", "gave-up"},
		},
		{
			"hub exposes its branches plus done",
			Node{Type: Hub, HubCfg: &HubConfig{Branches: []Branch{{Port: "a"}, {Port: "b"}}}},
			[]string{"a", "b", PortDone},
		},

		// A node whose config is missing is malformed, not a crash. The
		// validator reports it; Ports must not panic on the way there.
		{"puzzle with no config", Node{Type: Puzzle}, nil},
		{"hub with no config", Node{Type: Hub}, nil},
		{"unknown type", Node{Type: "invented"}, nil},
	}

	for _, c := range cases {
		got := c.node.Ports()
		if len(got) != len(c.want) {
			t.Errorf("%s: Ports() = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: Ports() = %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestLookups(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{Slug: "start", Type: Start},
			{Slug: "p", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Outputs: []Output{{Port: "solved"}}}},
		},
		Edges: []Edge{{From: "start", FromPort: PortOut, To: "p"}},
	}

	if n, ok := g.Node("p"); !ok || n.Type != Puzzle {
		t.Error("Node should find the puzzle")
	}
	if _, ok := g.Node("ghost"); ok {
		t.Error("Node should not invent a missing node")
	}
	if n, ok := g.StartNode(); !ok || n.Slug != "start" {
		t.Error("StartNode should find the entry")
	}
	if _, ok := (&Graph{}).StartNode(); ok {
		t.Error("an empty graph has no start node")
	}
	if to, ok := g.Target("start", PortOut); !ok || to != "p" {
		t.Errorf("Target = %q, %v; want p", to, ok)
	}
	if _, ok := g.Target("p", "solved"); ok {
		t.Error("an unconnected port should report no target")
	}
}

func TestOutputAndBranchLookup(t *testing.T) {
	p := Node{Type: Puzzle, PuzzleCfg: &PuzzleConfig{Outputs: []Output{{Port: "left", Label: "Left"}}}}
	if o, ok := p.Output("left"); !ok || o.Label != "Left" {
		t.Error("Output should find the port")
	}
	if _, ok := p.Output("nope"); ok {
		t.Error("Output should not invent a port")
	}
	if _, ok := (Node{Type: Puzzle}).Output("left"); ok {
		t.Error("a puzzle with no config has no outputs")
	}

	h := Node{Type: Hub, HubCfg: &HubConfig{Branches: []Branch{{Port: "a", Capacity: 2}}}}
	if b, ok := h.Branch("a"); !ok || b.Capacity != 2 {
		t.Error("Branch should find the branch")
	}
	if _, ok := h.Branch("nope"); ok {
		t.Error("Branch should not invent a branch")
	}
	if _, ok := (Node{Type: Hub}).Branch("a"); ok {
		t.Error("a hub with no config has no branches")
	}
}

// "Zero means all" is the default an author gets without touching the
// field, and a Required above the branch count is a slip that should
// clamp rather than make the hub unsatisfiable.
func TestRequiredBranches(t *testing.T) {
	three := []Branch{{Port: "a"}, {Port: "b"}, {Port: "c"}}
	cases := []struct {
		required int
		want     int
	}{
		{0, 3}, {2, 2}, {3, 3}, {9, 3}, {-1, 3},
	}
	for _, c := range cases {
		h := HubConfig{Branches: three, Required: c.required}
		if got := h.RequiredBranches(); got != c.want {
			t.Errorf("Required=%d: got %d, want %d", c.required, got, c.want)
		}
	}
	if got := (&HubConfig{}).RequiredBranches(); got != 0 {
		t.Errorf("a hub with no branches requires %d, want 0", got)
	}
}

// The ladder: the 1st wrong guess should read differently from the 5th,
// and the last rung repeats rather than falling off the end.
func TestResponseLadder(t *testing.T) {
	p := PuzzleConfig{Responses: []Response{
		{AfterAttempts: 0, Text: "Not quite."},
		{AfterAttempts: 3, Text: "Think about the colour."},
		{AfterAttempts: 5, Text: "It is the blue one."},
	}}

	cases := []struct {
		attempts int
		want     string
	}{
		{0, "Not quite."},
		{1, "Not quite."},
		{2, "Not quite."},
		{3, "Think about the colour."},
		{4, "Think about the colour."},
		{5, "It is the blue one."},
		{50, "It is the blue one."}, // the last rung repeats
	}
	for _, c := range cases {
		r, ok := p.ResponseFor(c.attempts)
		if !ok || r.Text != c.want {
			t.Errorf("after %d attempts got %q (ok=%v), want %q", c.attempts, r.Text, ok, c.want)
		}
	}

	if _, ok := (&PuzzleConfig{}).ResponseFor(3); ok {
		t.Error("a puzzle with no responses should report none")
	}
}

// A ladder whose first rung starts above zero must stay silent until
// the player has actually earned it.
func TestResponseLadderSilentBeforeFirstRung(t *testing.T) {
	p := PuzzleConfig{Responses: []Response{{AfterAttempts: 2, Text: "Getting warmer."}}}
	if _, ok := p.ResponseFor(1); ok {
		t.Error("should be silent before the first rung")
	}
	if r, ok := p.ResponseFor(2); !ok || r.Text != "Getting warmer." {
		t.Error("should speak from the first rung on")
	}
}

func TestSlugsAreSorted(t *testing.T) {
	g := &Graph{Nodes: []Node{{Slug: "zeta"}, {Slug: "alpha"}, {Slug: "mid"}}}
	got := g.Slugs()
	want := []string{"alpha", "mid", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Slugs() = %v, want %v", got, want)
		}
	}
}

func TestNodeString(t *testing.T) {
	if got := (Node{Slug: "p1", Type: Puzzle}).String(); got != `puzzle "p1"` {
		t.Errorf("String() = %q", got)
	}
	got := (Node{Slug: "p1", Type: Puzzle, Title: "The Statue"}).String()
	if !strings.Contains(got, "The Statue") || !strings.Contains(got, "p1") {
		t.Errorf("String() = %q; should carry both the title and the slug", got)
	}
}

// A graph is exported, hand-edited and re-imported, so it has to survive
// a JSON round-trip exactly. In particular an unconditional hub branch
// must not gain an empty condition object — that is why Condition is
// tagged omitzero rather than omitempty, which is a no-op on a struct.
func TestJSONRoundTrip(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{Slug: "start", Type: Start},
			{Slug: "door", Type: Puzzle, PuzzleCfg: &PuzzleConfig{
				Blocks:  LegacyBlocks("Which door?", nil),
				Outputs: []Output{{Port: "left", Trigger: OnAnswer, Points: 10}},
			}},
			{Slug: "hub", Type: Hub, HubCfg: &HubConfig{
				Policy:   PolicyChoice,
				Branches: []Branch{{Port: "a"}},
			}},
		},
		Edges: []Edge{{From: "start", FromPort: PortOut, To: "door"}},
		Zones: []Zone{{Slug: "square", Lat: 59.3, Lng: 18.1, RadiusM: 25}},
	}

	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"condition":{}`) {
		t.Error("an unconditional branch should not serialise an empty condition object")
	}

	var back Graph
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	again, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(again) != string(b) {
		t.Errorf("round-trip changed the graph:\n first: %s\nsecond: %s", b, again)
	}
}
