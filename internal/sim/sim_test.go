package sim

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/walk"
)

var t0 = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

// course is start → riddle at the square (two hints, a lockout) → a hub
// of two stations, one needed → end.
func course() *graph.Graph {
	story := func(slug, zone string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Story, StoryCfg: &graph.StoryConfig{Zone: zone, Blocks: graph.LegacyBlocks("You are at the "+zone+".", nil)}}
	}
	return &graph.Graph{
		Zones: []graph.Zone{
			{Slug: "square", Name: "Square", Lat: 59.3300, Lng: 18.0600, RadiusM: 25},
			{Slug: "church", Name: "Church", Lat: 59.3330, Lng: 18.0650, RadiusM: 25},
			{Slug: "harbour", Name: "Harbour", Lat: 59.3270, Lng: 18.0700, RadiusM: 25},
		},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "riddle", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Zone:           "square",
				Blocks:         graph.LegacyBlocks("What has hands but no arms?", nil),
				LockoutSeconds: 30,
				Outputs: []graph.Output{
					{Port: "right", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "clock face"}}},
				},
				Hints: []graph.Hint{{Text: "Look up at the tower.", DelaySeconds: 120, Cost: 2}, {Text: "It tells the time.", DelaySeconds: 300, Cost: 3}},
			}},
			{Slug: "stations", Type: graph.Hub, HubCfg: &graph.HubConfig{
				Policy:   graph.PolicyChoice,
				Required: 1,
				Branches: []graph.Branch{{Port: "church", Label: "Church"}, {Port: "harbour", Label: "Harbour"}},
			}},
			story("at-church", "church"),
			story("at-harbour", "harbour"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "riddle"},
			{From: "riddle", FromPort: "right", To: "stations"},
			{From: "stations", FromPort: "church", To: "at-church"},
			{From: "stations", FromPort: "harbour", To: "at-harbour"},
			{From: "at-church", FromPort: "out", To: "stations"},
			{From: "at-harbour", FromPort: "out", To: "stations"},
			{From: "stations", FromPort: "done", To: "end"},
		},
	}
}

func config() Config {
	return Config{Teams: 30, Members: 4, Spread: 0.7, MessageEvery: 600, StartSpread: 300, Start: t0, Seed: 7}
}

func run(t *testing.T, g *graph.Graph, c Config) *Result {
	t.Helper()
	res, err := Run(g, c)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestEveryTeamFinishesTheCourse(t *testing.T) {
	res := run(t, course(), config())
	if res.Ended != "finished" {
		t.Fatalf("ended %q, want finished", res.Ended)
	}
	var wrong, hints int
	var first, last float64
	for i, tm := range res.Teams {
		if tm.Finished == nil {
			t.Fatalf("%s did not finish", tm.Name)
		}
		if tm.Members != 4 {
			t.Errorf("%s has %d members", tm.Name, tm.Members)
		}
		if tm.Score <= 0 {
			t.Errorf("%s scored %v", tm.Name, tm.Score)
		}
		if i == 0 || *tm.Finished < first {
			first = *tm.Finished
		}
		last = max(last, *tm.Finished)
		wrong += tm.Wrong
		hints += tm.Hints
		for j := 1; j < len(tm.Path); j++ {
			if tm.Path[j].T < tm.Path[j-1].T {
				t.Fatalf("%s's path goes back in time at %d", tm.Name, j)
			}
		}
	}
	// Thirty unlike teams: some guess wrong, some take hints, and they do
	// not all finish together.
	if wrong == 0 || hints == 0 {
		t.Errorf("wrong %d, hints %d: want some of each", wrong, hints)
	}
	if last-first < 300 {
		t.Errorf("finishes span %.0f s, want teams spread out", last-first)
	}
	for _, s := range res.Steps {
		if s.Node == "riddle" && (s.Reached != 30 || s.Left != 30 || s.Median <= 0) {
			t.Errorf("riddle stats %+v", s)
		}
	}
}

func TestSameSeedSameRun(t *testing.T) {
	a, _ := json.Marshal(run(t, course(), config()))
	b, _ := json.Marshal(run(t, course(), config()))
	if string(a) != string(b) {
		t.Fatal("two runs with one seed differ")
	}
	c := config()
	c.Seed = 8
	d, _ := json.Marshal(run(t, course(), c))
	if string(a) == string(d) {
		t.Fatal("another seed gave the same run")
	}
}

func TestArrangerNudgesStuckTeams(t *testing.T) {
	c := config()
	c.MessageEvery = 120
	res := run(t, course(), c)
	nudged := 0
	for _, e := range res.Events {
		if e.Kind == "nudge" {
			nudged++
		}
	}
	if nudged == 0 {
		t.Fatal("no team acted on a nudge")
	}
}

func TestUnwiredOutputStallsTheTeam(t *testing.T) {
	g := course()
	g.Edges = g.Edges[:1] // the riddle leads nowhere
	c := config()
	c.Teams = 3
	res := run(t, g, c)
	if res.Ended != "stalled" {
		t.Fatalf("ended %q, want stalled", res.Ended)
	}
	for _, tm := range res.Teams {
		if !tm.Stalled {
			t.Errorf("%s not stalled", tm.Name)
		}
	}
	// Stalled, the teams wait on the step they are stuck on: the report
	// says where.
	waiting := 0
	for _, st := range res.Steps {
		waiting += st.Waiting
	}
	if waiting != 3 {
		t.Errorf("%d teams waiting at the end, want the 3 stalled", waiting)
	}
}

func TestTimeLimitEndsTheRun(t *testing.T) {
	c := config()
	c.MaxSeconds = 600
	res := run(t, course(), c)
	if res.Ended != "time" || res.Duration > 600 {
		t.Fatalf("ended %q after %.0f s", res.Ended, res.Duration)
	}
}

func TestConfigChecks(t *testing.T) {
	for _, c := range []Config{
		{Teams: 0, Members: 1},
		{Teams: MaxTeams + 1, Members: 1},
		{Teams: 1, Members: 0},
		{Teams: 1, Members: 1, Spread: 2},
		{Teams: 1, Members: 1, MaxSeconds: MaxSeconds + 1},
	} {
		if err := c.Check(); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}

// A detective case: a home hub of suspects, one of whom has a home hub
// of questions with no way on but back, and a repeatable search of three
// places. Teams must use back to get out, and look somewhere new each
// time they search, or they go round for ever.
func TestTeamsGoBackFromHomeHubs(t *testing.T) {
	g := course()
	g.Zones = append(g.Zones, graph.Zone{Slug: "park", Name: "Park", Lat: 59.3310, Lng: 18.0580, RadiusM: 25, Group: "sites"})
	for i := range g.Zones {
		g.Zones[i].Group = "sites"
	}
	g.Nodes = []graph.Node{
		{Slug: "start", Type: graph.Start},
		{Slug: "case", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{{Port: "witness"}, {Port: "scan"}, {Port: "accuse"}}}},
		{Slug: "witness", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{{Port: "where"}, {Port: "when"}}}},
		{Slug: "where", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("At home.", nil)}},
		{Slug: "when", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("At three.", nil)}},
		{Slug: "scan", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Answer:     &graph.Answer{Kind: graph.AnswerSearch, Group: "sites"},
			Repeatable: true,
			Outputs: []graph.Output{
				{Port: "square", Trigger: graph.OnAnswer, Zone: "square"},
				{Port: "church", Trigger: graph.OnAnswer, Zone: "church"},
			},
		}},
		{Slug: "found", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("A clue.", nil)}},
		{Slug: "accuse", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Blocks:  graph.LegacyBlocks("Who did it?", nil),
			Outputs: []graph.Output{{Port: "right", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "erika"}}}},
		}},
		{Slug: "end", Type: graph.End},
	}
	g.Edges = []graph.Edge{
		{From: "start", FromPort: "out", To: "case"},
		{From: "case", FromPort: "witness", To: "witness"},
		{From: "witness", FromPort: "where", To: "where"},
		{From: "witness", FromPort: "when", To: "when"},
		{From: "where", FromPort: "out", To: "witness"},
		{From: "when", FromPort: "out", To: "witness"},
		{From: "case", FromPort: "scan", To: "scan"},
		{From: "scan", FromPort: "square", To: "found"},
		{From: "scan", FromPort: "church", To: "found"},
		{From: "found", FromPort: "out", To: "scan"},
		{From: "case", FromPort: "accuse", To: "accuse"},
		{From: "accuse", FromPort: "right", To: "end"},
	}
	c := config()
	c.Teams = 10
	res := run(t, g, c)
	if res.Ended != "finished" {
		t.Fatalf("ended %q, want finished", res.Ended)
	}
	backs := 0
	for _, e := range res.Events {
		if e.Kind == "back" {
			backs++
		}
	}
	if backs == 0 {
		t.Fatal("no team went back from a home hub")
	}
	for _, s := range res.Steps {
		if s.Node == "scan" && s.Reached > 10*4 {
			t.Errorf("teams reached the search %d times: going round", s.Reached)
		}
	}
}

// A branch that wants a player at the church and one at the harbour at
// once: a team big enough waits at the hub, splits up to open it and
// gathers again after.
func TestTeamsSplitUpToOpenABranch(t *testing.T) {
	g := course()
	for i, n := range g.Nodes {
		if n.Slug == "stations" {
			cfg := *n.HubCfg
			cfg.Required = 3
			cfg.Branches = append(slices.Clone(cfg.Branches), graph.Branch{
				Port: "signal", Label: "Signal",
				Condition: cond.Predicate{Op: cond.OpZonesHeld, Members: []string{"church", "harbour"}},
			})
			g.Nodes[i].HubCfg = &cfg
		}
	}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "signal", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("The lights answer.", nil)}})
	g.Edges = append(g.Edges,
		graph.Edge{From: "stations", FromPort: "signal", To: "signal"},
		graph.Edge{From: "signal", FromPort: "out", To: "stations"})

	c := config()
	c.Teams = 5
	res := run(t, g, c)
	if res.Ended != "finished" {
		t.Fatalf("ended %q, want finished", res.Ended)
	}
	splits := 0
	for _, e := range res.Events {
		if e.Kind == "split" {
			splits++
		}
	}
	if splits != c.Teams {
		t.Errorf("%d splits, want one per team", splits)
	}
	for _, tm := range res.Teams {
		for j := 1; j < len(tm.Path); j++ {
			if tm.Path[j].T < tm.Path[j-1].T {
				t.Fatalf("%s's path goes back in time at %d", tm.Name, j)
			}
		}
	}

	// A team of one cannot be in two places, so the branch is shut for it
	// as any unreachable branch is, and it moves on without it.
	c.Members = 1
	res = run(t, g, c)
	if res.Ended != "finished" {
		t.Errorf("teams of one: ended %q, want finished", res.Ended)
	}
	for _, e := range res.Events {
		if e.Kind == "split" {
			t.Fatal("a team of one split up")
		}
	}
}

// A branch locked until the team is at the harbour keeps the team at the
// hub; it walks there, the branch opens, and it goes on — rather than
// being moved past it.
func TestTeamsWalkToOpenABranch(t *testing.T) {
	g := course()
	for i, n := range g.Nodes {
		if n.Slug == "stations" {
			cfg := *n.HubCfg
			cfg.Required = 3
			cfg.Branches = append(slices.Clone(cfg.Branches), graph.Branch{
				Port: "signal", Label: "Signal",
				Condition: cond.Predicate{Op: cond.OpInZone, Zone: "square"},
			})
			g.Nodes[i].HubCfg = &cfg
		}
	}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "signal", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("The lights answer.", nil)}})
	g.Edges = append(g.Edges,
		graph.Edge{From: "stations", FromPort: "signal", To: "signal"},
		graph.Edge{From: "signal", FromPort: "out", To: "stations"})

	c := config()
	c.Teams = 5
	res := run(t, g, c)
	if res.Ended != "finished" {
		t.Fatalf("ended %q, want finished", res.Ended)
	}
	for _, e := range res.Events {
		if e.Kind == "split" {
			t.Fatal("one place is no reason to split up")
		}
	}
	for _, s := range res.Steps {
		if s.Node == "signal" && s.Reached != c.Teams {
			t.Errorf("%d teams reached the signal, want all %d", s.Reached, c.Teams)
		}
	}
}

// A two-key launch with a key at the church and one at the harbour: a
// team splits up to turn them, and moves on; unanimous answers pass for
// a simulated team, which answers as one.
func TestTeamsTurnKeysAtPosts(t *testing.T) {
	g := course()
	for i, n := range g.Nodes {
		if n.Slug == "riddle" {
			cfg := *n.PuzzleCfg
			cfg.Unanimous = true
			g.Nodes[i].PuzzleCfg = &cfg
		}
	}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "launch", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
		Answer:  &graph.Answer{Kind: graph.AnswerKeys, Posts: []string{"church", "harbour"}},
		Outputs: []graph.Output{{Port: "launched", Trigger: graph.OnAnswer, Points: 10}},
	}})
	for i, e := range g.Edges {
		if e.From == "stations" && e.FromPort == "done" {
			g.Edges[i].To = "launch"
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: "launch", FromPort: "launched", To: "end"})

	c := config()
	c.Teams = 5
	res := run(t, g, c)
	if res.Ended != "finished" {
		t.Fatalf("ended %q, want finished", res.Ended)
	}
	for _, s := range res.Steps {
		if s.Node == "launch" && s.Left != c.Teams {
			t.Errorf("%d teams launched, want %d", s.Left, c.Teams)
		}
	}
}

func TestWrongGuessesComeFromTheAdventure(t *testing.T) {
	t.Parallel()
	puzzle := func(slug, answer string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: answer}}}},
		}}
	}
	choice := graph.Node{Slug: "c", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
		Answer: &graph.Answer{Kind: graph.AnswerChoice, Fields: []graph.ChoiceField{{Options: []graph.ChoiceOption{{Value: "a", Label: "Grön"}}}}},
	}}
	g := &graph.Graph{Nodes: []graph.Node{puzzle("q", "kyrka"), puzzle("r", "Torget"), choice}}
	got := contentGuesses(g, g.Nodes[0])
	if want := []string{"kyrk", "ykrka", "Torget", "Grön"}; !slices.Equal(got, want) {
		t.Errorf("contentGuesses = %v, want %v", got, want)
	}
}

// A branch puzzle that times out is passed through for good, so the key
// behind its answer is out of reach and the team goes round between the
// home screen and the gate: it is given up on, not left to loop.
func TestHubGateLoopStallsTheTeam(t *testing.T) {
	g := &graph.Graph{
		Items: []graph.Item{{Slug: "key", Name: "Key"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true,
				Branches: []graph.Branch{{Port: "tower", Label: "Tower"}, {Port: "door", Label: "Door"}}}},
			{Slug: "riddle", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks:           graph.LegacyBlocks("What has hands but no arms?", nil),
				TimeLimitSeconds: 1,
				Outputs: []graph.Output{
					{Port: "right", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "clock face"}}},
					{Port: "late", Trigger: graph.OnTimeout},
				},
			}},
			{Slug: "give", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key", Qty: 1}},
			{Slug: "gate", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpItem, Item: "key"}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "hub"},
			{From: "hub", FromPort: "tower", To: "riddle"},
			{From: "hub", FromPort: "door", To: "gate"},
			{From: "riddle", FromPort: "right", To: "give"},
			{From: "riddle", FromPort: "late", To: "hub"},
			{From: "give", FromPort: "out", To: "hub"},
			{From: "gate", FromPort: "pass", To: "end"},
			{From: "gate", FromPort: "fail", To: "hub"},
		},
	}
	c := config()
	c.Teams = 3
	res := run(t, g, c)
	if res.Ended != "stalled" {
		t.Fatalf("ended %q, want stalled", res.Ended)
	}
	for _, tm := range res.Teams {
		if !tm.Stalled {
			t.Errorf("%s not stalled", tm.Name)
		}
	}
	for _, e := range res.Events {
		if e.Kind == "stalled" && (e.Node != "hub" || !strings.Contains(e.Text, "out of reach")) {
			t.Errorf("stalled at %q: %s", e.Node, e.Text)
		}
	}
}

// A bot answers an order puzzle with where each card was dealt, which
// the server takes; untouched, the cards as dealt are wrong.
func TestOrderSolution(t *testing.T) {
	a := &graph.Answer{Kind: graph.AnswerOrder, Start: []int{3, 1, 0, 2}, Cards: make([]graph.OrderCard, 4)}
	if got := orderSolution(a); got != "2,1,3,0" || !a.InOrder(got) {
		t.Errorf("orderSolution = %q, which InOrder says is %v", got, a.InOrder(got))
	}
	if a.InOrder("0,1,2,3") {
		t.Error("the cards as dealt should not be in order")
	}
}

// Bot teams do a step's tasks, each at its place, and move on.
func TestBotsDoTasks(t *testing.T) {
	g := &graph.Graph{
		Roles: []graph.Role{{Slug: "hacker", Name: "Hacker"}, {Slug: "crew", Name: "Crew", Default: true}},
		Zones: []graph.Zone{
			{Slug: "square", Name: "Square", Lat: 59.3300, Lng: 18.0600, RadiusM: 25},
			{Slug: "terminal", Name: "Terminal", Lat: 59.3320, Lng: 18.0620, RadiusM: 25},
		},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "breakin", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Zone: "square",
				Blocks: []graph.Block{
					{ID: "hack", Kind: graph.BlockTask, Role: "hacker", Zone: "terminal", Text: "Crack it.", Answer: "swordfish"},
					{ID: "watch", Kind: graph.BlockTask, Role: "crew", Text: "Count the guards.", Answer: "3"},
				},
				Answer:  &graph.Answer{Kind: graph.AnswerTasks},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "breakin"}, {From: "breakin", FromPort: "done", To: "end"}},
	}
	res := run(t, g, Config{Teams: 5, Members: 4, Start: t0, Seed: 3})
	for _, tm := range res.Teams {
		if tm.Finished == nil {
			t.Errorf("%s did not finish its tasks", tm.Name)
		}
	}
}

// Simulated teams walk each path of a split in turn, and finish.
func TestTeamsWalkEveryPathOfASplit(t *testing.T) {
	g := course()
	g.Nodes = append(g.Nodes,
		graph.Node{Slug: "split", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}, {Port: "c"}}}},
		graph.Node{Slug: "to-church", Type: graph.Story, StoryCfg: &graph.StoryConfig{Zone: "church", Blocks: graph.LegacyBlocks("Church.", nil)}},
		graph.Node{Slug: "to-harbour", Type: graph.Story, StoryCfg: &graph.StoryConfig{Zone: "harbour", Blocks: graph.LegacyBlocks("Harbour.", nil)}},
		graph.Node{Slug: "to-square", Type: graph.Story, StoryCfg: &graph.StoryConfig{Zone: "square", Blocks: graph.LegacyBlocks("Square.", nil)}},
		graph.Node{Slug: "join", Type: graph.Join},
	)
	for i, e := range g.Edges {
		if e.From == "stations" && e.FromPort == "done" {
			g.Edges[i].To = "split"
		}
	}
	g.Edges = append(g.Edges,
		graph.Edge{From: "split", FromPort: "a", To: "to-church"},
		graph.Edge{From: "split", FromPort: "b", To: "to-harbour"},
		graph.Edge{From: "split", FromPort: "c", To: "to-square"},
		graph.Edge{From: "to-square", FromPort: "out", To: "join"},
		graph.Edge{From: "to-church", FromPort: "out", To: "join"},
		graph.Edge{From: "to-harbour", FromPort: "out", To: "join"},
		graph.Edge{From: "join", FromPort: "out", To: "end"},
	)
	c := config()
	c.Teams = 5
	res := run(t, g, c)
	if res.Ended != "finished" {
		t.Fatalf("ended %q, want finished", res.Ended)
	}
	for _, tm := range res.Teams {
		if tm.Finished == nil {
			t.Fatalf("%s did not finish", tm.Name)
		}
	}
	reached, left, median := map[string]int{}, map[string]int{}, map[string]float64{}
	for _, s := range res.Steps {
		reached[s.Node], left[s.Node], median[s.Node] = s.Reached, s.Left, s.Median
	}
	// The split is stood on until its last group sets off — after the
	// groups before it have walked their paths — as a join is until the
	// last group goes on through it.
	if median["split"] <= median["to-church"] {
		t.Errorf("the split held the team %v s, a path's step %v s: the split's time should run to its last group's setting off", median["split"], median["to-church"])
	}
	// Each once a team: a group's step by that group, the split by the
	// team, though the bot goes back to a group still on it, and the join
	// by the team, reached as one group waits and left as the last goes
	// on.
	for _, n := range []string{"to-church", "to-harbour", "to-square", "split", "join"} {
		if reached[n] != 5 || left[n] != 5 {
			t.Errorf("%s reached %d times and left %d, want 5 and 5", n, reached[n], left[n])
		}
	}
}

// A split team whose first group needs what the second finds hops to the
// second, and comes back with it: the vault on path a opens with the key
// path b grants. Regression: the team stood at the vault and stalled.
func TestTeamsHopGroupsForAKey(t *testing.T) {
	g := &graph.Graph{
		Items: []graph.Item{{Slug: "key", Name: "Key"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}},
			{Slug: "vault", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer:  &graph.Answer{Kind: graph.AnswerItems, Slots: []graph.ItemSlot{{Item: "key"}}},
				Outputs: []graph.Output{{Port: "open", Trigger: graph.OnAnswer}},
			}},
			{Slug: "gift", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}},
			{Slug: "b1", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("A key.", nil)}},
			{Slug: "j", Type: graph.Join},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "s"},
			{From: "s", FromPort: "a", To: "vault"}, {From: "vault", FromPort: "open", To: "j"},
			{From: "s", FromPort: "b", To: "gift"}, {From: "gift", FromPort: "out", To: "b1"}, {From: "b1", FromPort: "out", To: "j"},
			{From: "j", FromPort: "out", To: "end"},
		},
	}
	for _, members := range []int{1, 4} {
		c := config()
		c.Teams, c.Members = 5, members
		res := run(t, g, c)
		for _, tm := range res.Teams {
			if tm.Finished == nil {
				t.Fatalf("%d members: %s did not finish (stalled %v)", members, tm.Name, tm.Stalled)
			}
		}
	}
}

// A split team whose groups all wait on the clock waits for it, rather
// than hopping between them: it hops from a group found waiting to one
// not yet looked at, but not back to one still waiting — it sleeps to
// the first moment either is worth a look. Regression: it hopped every
// five seconds, burning its actions, and stalled after half an hour.
func TestGroupsAllWaitingWaitForTheClock(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a"}, {Port: "b"}}}},
			{Slug: "pa", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer}}}},
			{Slug: "pb", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer}}}},
			{Slug: "j", Type: graph.Join},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "s"},
			{From: "s", FromPort: "a", To: "pa"}, {From: "pa", FromPort: "ok", To: "j"},
			{From: "s", FromPort: "b", To: "pb"}, {From: "pb", FromPort: "ok", To: "j"},
			{From: "j", FromPort: "out", To: "end"},
		},
	}
	c := config()
	c.Teams, c.StartSpread = 1, 0
	r := &runner{g: g, c: c, rng: rand.New(rand.NewPCG(1, 2)), stats: map[string]*stepStat{}}
	if err := r.addTeam(0); err != nil {
		t.Fatal(err)
	}
	b := r.bots[0]
	r.now, b.t.Now = 100, r.at(100)
	if _, ok := b.do("continue", func() (session.Result, *walk.Step, error) { return b.t.Continue("s") }); !ok {
		t.Fatal("setting a off")
	}
	open := r.at(2700)
	waiting := func() session.View {
		v := b.t.View()
		v.RecheckAt = &open
		return v
	}
	hops := func() (n int) {
		for _, e := range r.events {
			if e.Kind == "walk" {
				n++
			}
		}
		return n
	}
	if v := waiting(); v.Group != "a" {
		t.Fatalf("on %q", v.Group)
	}
	b.waitFor(waiting())
	if hops() != 1 || b.t.View().Group != "b" {
		t.Fatalf("a waiting, the team should hop to b: %d hops, on %q", hops(), b.t.View().Group)
	}
	r.now, b.t.Now = 105, r.at(105)
	b.waitFor(waiting())
	if hops() != 1 || b.wake != 2701 {
		t.Fatalf("both waiting, the team should sleep to 2701: %d hops, wakes at %v", hops(), b.wake)
	}
	// Then a, due, is looked at again.
	r.now, b.t.Now = 2701, r.at(2701)
	open = r.at(3600)
	b.waitFor(waiting())
	if hops() != 2 || b.t.View().Group != "a" {
		t.Fatalf("a due again: %d hops, on %q", hops(), b.t.View().Group)
	}
}
