package session

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// altar: the team is given two red gems and a blue one, then fills the
// altar's slots — red, blue, red. A gate after it asks whether a red
// gem is left, so it shows what the altar used up.
func altar(keep bool) *graph.Graph {
	give := func(slug, item string, qty int) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: item, Qty: qty}}
	}
	return &graph.Graph{
		Items: []graph.Item{{Slug: "red", Name: "Red gem", Icon: "🔴"}, {Slug: "blue", Name: "Blue gem"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			give("give-red", "red", 2),
			give("give-blue", "blue", 1),
			{Slug: "altar", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer: &graph.Answer{Kind: graph.AnswerItems, Keep: keep, Slots: []graph.ItemSlot{
					{Label: "Left", Item: "red"}, {Label: "Middle", Item: "blue"}, {Label: "Right", Item: "red"},
				}},
				Outputs:   []graph.Output{{Port: "open", Trigger: graph.OnAnswer, Points: 20}},
				Responses: []graph.Response{{Text: "Nothing happens."}},
			}},
			{Slug: "left", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpItem, Item: "red"}}},
			{Slug: "kept", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
			{Slug: "spent", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "give-red"},
			{From: "give-red", FromPort: "out", To: "give-blue"},
			{From: "give-blue", FromPort: "out", To: "altar"},
			{From: "altar", FromPort: "open", To: "left"},
			{From: "left", FromPort: "pass", To: "kept"},
			{From: "left", FromPort: "fail", To: "spent"},
		},
	}
}

func atAltar(t *testing.T, keep bool) *Team {
	t.Helper()
	g := altar(keep)
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Current != "altar" {
		t.Fatalf("at %q, want the altar", tm.Current)
	}
	return tm
}

func TestItemSlotsShowOnlyTheirLabels(t *testing.T) {
	tm := atAltar(t, false)
	in := tm.View().Input
	if in.Kind != "items" || len(in.Slots) != 3 || in.Slots[0] != "Left" || in.Slots[2] != "Right" {
		t.Fatalf("the phone gets the slots by name: %+v", in)
	}
	g := tm.G
	g.Translations = map[string]map[string]string{"sv": {graph.NodeKey("altar", "slot:1"): "Mitten"}}
	tm.Lang = "sv"
	if s := tm.View().Input.Slots; s[1] != "Mitten" || s[0] != "Left" {
		t.Errorf("a slot's name is translated, the rest fall back: %v", s)
	}
}

func TestItemSlotsWrongFillCostsNothing(t *testing.T) {
	tm := atAltar(t, false)
	for _, guess := range []string{
		"blue|red|red",  // right items, wrong slots
		"red|blue",      // a slot left empty
		"red|blue|blue", // more blue gems than the team has
		"red|blue|red|red",
		"",
	} {
		res, st, err := tm.Answer("altar", guess)
		if err != nil || st != nil || res.Accepted || res.Response != "Nothing happens." {
			t.Errorf("%q: a wrong fill is a wrong guess: %+v %v", guess, res, err)
		}
	}
	if tm.Items["red"] != 2 || tm.Items["blue"] != 1 {
		t.Errorf("wrong fills leave the backpack alone: %v", tm.Items)
	}
	if tm.View().Attempts != 5 {
		t.Errorf("each counts as an attempt: %d", tm.View().Attempts)
	}
}

func TestItemSlotsUseTheItemsUp(t *testing.T) {
	tm := atAltar(t, false)
	res, st, err := tm.Answer("altar", "red|blue|red")
	if err != nil || !res.Accepted || res.Port != "open" || res.Points != 20 || st == nil {
		t.Fatalf("the right fill opens it: %+v %v", res, err)
	}
	tm.Apply(*st)
	if len(tm.Items) != 0 {
		t.Errorf("both reds and the blue are used up: %v", tm.Items)
	}
	if tm.Ending == "" || tm.Current != "spent" {
		t.Errorf("the gate after the altar sees the reds gone: at %q", tm.Current)
	}
}

func TestItemSlotsCanKeepTheItems(t *testing.T) {
	tm := atAltar(t, true)
	_, st, err := tm.Answer("altar", "red|blue|red")
	if err != nil || st == nil {
		t.Fatal(err)
	}
	tm.Apply(*st)
	if tm.Items["red"] != 2 || tm.Items["blue"] != 1 || tm.Current != "kept" {
		t.Errorf("kept: backpack %v, at %q", tm.Items, tm.Current)
	}
}

func TestItemSlotsNeedTheItemsInTheBackpack(t *testing.T) {
	tm := atAltar(t, false)
	tm.Items["red"] = 1
	if res, st, _ := tm.Answer("altar", "red|blue|red"); res.Accepted || st != nil {
		t.Errorf("one red gem cannot fill two red slots: %+v", res)
	}
	if tm.Items["red"] != 1 {
		t.Errorf("and the one is not taken: %v", tm.Items)
	}
}

func TestItemSlotsValidation(t *testing.T) {
	codes := func(g *graph.Graph) map[string]bool {
		out := map[string]bool{}
		for _, i := range valid.Check(g).Issues {
			out[i.Code] = true
		}
		return out
	}
	slots := func(g *graph.Graph) *graph.Answer { return g.Nodes[3].PuzzleCfg.Answer }

	g := altar(false)
	slots(g).Slots = nil
	if !codes(g)["slots.count"] {
		t.Error("no slots is an error")
	}
	g = altar(false)
	for range graph.MaxSlots {
		slots(g).Slots = append(slots(g).Slots, graph.ItemSlot{Item: "red"})
	}
	if !codes(g)["slots.count"] {
		t.Error("too many slots is an error")
	}
	g = altar(false)
	slots(g).Slots[0].Item = ""
	if !codes(g)["slots.item.missing"] {
		t.Error("a slot wanting nothing is an error")
	}
	g = altar(false)
	slots(g).Slots[1].Item = "green"
	if c := codes(g); !c["ref.item.unknown"] || !c["slots.item.never"] {
		t.Errorf("an undeclared item is an error, and never given a warning: %v", c)
	}
	g = altar(false)
	g.Items = append(g.Items, graph.Item{Slug: "green"})
	slots(g).Slots[1].Item = "green"
	if c := codes(g); c["ref.item.unknown"] || !c["slots.item.never"] {
		t.Errorf("a declared item no step gives is only a warning: %v", c)
	}
}

// errand: the team reaches the altar with only its red gems. The altar's
// "missing" route sends it to look around, where it finds the blue gem,
// and back to the altar — presented again, not passed by.
func errand(missing string) *graph.Graph {
	g := altar(false)
	g.Nodes[3].PuzzleCfg.Answer.Missing = missing
	g.Nodes[3].PuzzleCfg.Outputs = append(g.Nodes[3].PuzzleCfg.Outputs, graph.Output{Port: "away", Label: "Look around", Trigger: graph.OnMissing})
	g.Nodes = append(g.Nodes, graph.Node{Slug: "search", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Something glints under a stone.", nil)}})
	g.Edges = []graph.Edge{
		{From: "start", FromPort: "out", To: "give-red"},
		{From: "give-red", FromPort: "out", To: "altar"},
		{From: "altar", FromPort: "away", To: "search"},
		{From: "search", FromPort: "out", To: "give-blue"},
		{From: "give-blue", FromPort: "out", To: "altar"},
		{From: "altar", FromPort: "open", To: "left"},
		{From: "left", FromPort: "pass", To: "kept"},
		{From: "left", FromPort: "fail", To: "spent"},
	}
	return g
}

func TestItemSlotsLeaveToFetchWhatIsMissing(t *testing.T) {
	g := errand(graph.MissingName)
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, _, err := Start(g, t0)
	if err != nil || tm.Current != "altar" {
		t.Fatalf("at %q: %v", tm.Current, err)
	}
	in := tm.View().Input
	if !in.Missing || len(in.MissingItems) != 1 || in.MissingItems[0] != "Blue gem" || !in.Leave || in.LeaveLabel != "Look around" {
		t.Fatalf("the phone says what is missing and offers the way out: %+v", in)
	}

	res, st, err := tm.Leave("altar")
	if err != nil || st == nil || res.Port != "away" {
		t.Fatalf("leave: %+v %v", res, err)
	}
	tm.Apply(*st)
	if tm.Current != "search" {
		t.Fatalf("leaving goes by the route: at %q", tm.Current)
	}
	if _, ok := tm.Outcome("altar"); ok || tm.Row("altar").Solved {
		t.Error("leaving records nothing for the altar")
	}
	_, st, err = tm.Continue("search")
	if err != nil || st == nil {
		t.Fatal(err)
	}
	tm.Apply(*st)
	if tm.Current != "altar" {
		t.Fatalf("back at the altar, not passed by: at %q", tm.Current)
	}
	if in := tm.View().Input; in.Missing || in.Leave || len(in.MissingItems) > 0 {
		t.Errorf("carrying everything, nothing is missing and there is no way out: %+v", in)
	}
	if _, _, err := tm.Leave("altar"); err != ErrNotAllowed {
		t.Errorf("a team with the items cannot leave: %v", err)
	}
	if res, _, _ := tm.Answer("altar", "red|blue|red"); !res.Accepted {
		t.Errorf("and fills the altar: %+v", res)
	}
}

func TestItemSlotsMissingHint(t *testing.T) {
	for _, c := range []struct {
		missing     string
		say, leave  bool
		names       int
	}{
		{graph.MissingSay, true, true, 0},
		{graph.MissingName, true, true, 1},
		{graph.MissingNone, false, true, 0},
	} {
		tm, _, err := Start(errand(c.missing), t0)
		if err != nil {
			t.Fatal(err)
		}
		in := tm.View().Input
		if in.Missing != c.say || in.Leave != c.leave || len(in.MissingItems) != c.names {
			t.Errorf("missing=%q: %+v", c.missing, in)
		}
	}
	// No way out: the hint still shows, but there is no button.
	tm := atAltar(t, false)
	tm.Items["blue"] = 0
	if in := tm.View().Input; !in.Missing || in.Leave {
		t.Errorf("without the route: %+v", in)
	}
	if _, _, err := tm.Leave("altar"); err != ErrNotAllowed {
		t.Errorf("and leaving is refused: %v", err)
	}
}

func TestItemSlotsMissingValidation(t *testing.T) {
	codes := func(g *graph.Graph) map[string]bool {
		out := map[string]bool{}
		for _, i := range valid.Check(g).Issues {
			out[i.Code] = true
		}
		return out
	}
	g := errand("")
	g.Nodes[3].PuzzleCfg.Answer.Missing = "shout"
	if !codes(g)["slots.missing"] {
		t.Error("an unknown hint setting is an error")
	}
	g = hunt()
	g.Nodes[1].PuzzleCfg.Outputs = append(g.Nodes[1].PuzzleCfg.Outputs, graph.Output{Port: "away", Trigger: graph.OnMissing})
	g.Edges = append(g.Edges, graph.Edge{From: "door", FromPort: "away", To: "end"})
	if !codes(g)["output.missing.kind"] {
		t.Error("a typed puzzle cannot be left for lack of items")
	}
}

// Leaving an ordinary hub's branch for the items is one-way: the hub
// counts the branch done once entered. A home screen keeps it open.
func TestItemSlotsLeaveFromHubBranch(t *testing.T) {
	build := func(home bool) *graph.Graph {
		g := errand("")
		hub := graph.Node{Slug: "square", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: home, Branches: []graph.Branch{{Port: "altar", Label: "The altar"}, {Port: "ruins", Label: "The ruins"}}}}
		if !home {
			hub.HubCfg.Required = 2
		}
		g.Nodes = append(g.Nodes, hub)
		g.Edges = []graph.Edge{
			{From: "start", FromPort: "out", To: "give-red"},
			{From: "give-red", FromPort: "out", To: "square"},
			{From: "square", FromPort: "altar", To: "altar"},
			{From: "square", FromPort: "ruins", To: "search"},
			{From: "search", FromPort: "out", To: "give-blue"},
			{From: "give-blue", FromPort: "out", To: "square"},
			{From: "altar", FromPort: "away", To: "square"},
			{From: "altar", FromPort: "open", To: "left"},
			{From: "left", FromPort: "pass", To: "kept"},
			{From: "left", FromPort: "fail", To: "spent"},
		}
		if !home {
			g.Edges = append(g.Edges, graph.Edge{From: "square", FromPort: graph.PortDone, To: "kept"})
		}
		return g
	}
	has := func(g *graph.Graph, code string) bool {
		for _, i := range valid.Check(g).Issues {
			if i.Code == code {
				return true
			}
		}
		return false
	}
	if !has(build(false), "slots.leave.oneway") {
		t.Error("an ordinary hub's branch warns that leaving is one-way")
	}
	if has(build(true), "slots.leave.oneway") {
		t.Error("a home screen's branch does not")
	}
}
