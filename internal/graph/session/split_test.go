package session

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// expedition splits at "split" into the scout's way to the church (pa,
// which grants the key) and the medic's to the well (pb); they meet at
// "join", and the door after needs the key. "fall" on the scout's way
// ends the adventure.
func expedition() *graph.Graph {
	right := func(port, answer string, points int) graph.Output {
		return graph.Output{Port: port, Trigger: graph.OnAnswer, Points: points, Matchers: []match.Matcher{{Kind: match.Exact, Value: answer}}}
	}
	return &graph.Graph{
		Zones: []graph.Zone{
			{Slug: "church", Lat: 59.30, Lng: 18.00, RadiusM: 50},
			{Slug: "well", Lat: 59.40, Lng: 18.10, RadiusM: 50},
		},
		Items: []graph.Item{{Slug: "key", Name: "Key"}},
		Roles: []graph.Role{{Slug: "scout", Name: "Scout"}, {Slug: "medic", Name: "Medic"}, {Slug: "crew", Name: "Crew", Default: true}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "split", Type: graph.Split, Title: "Split up", SplitCfg: &graph.SplitConfig{Text: "Off you go.", Paths: []graph.SplitPath{
				{Port: "a", Role: "scout", Label: "Church party"},
				{Port: "b", Role: "medic"},
			}}},
			{Slug: "pa", Type: graph.Puzzle, Title: "The church", PuzzleCfg: &graph.PuzzleConfig{Zone: "church", Outputs: []graph.Output{right("solved", "bell", 10), right("fall", "jump", 0)}}},
			{Slug: "gift", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}},
			{Slug: "pb", Type: graph.Puzzle, Title: "The well", PuzzleCfg: &graph.PuzzleConfig{Zone: "well", Outputs: []graph.Output{right("solved", "water", 20)}}},
			{Slug: "join", Type: graph.Join, Title: "The square"},
			{Slug: "door", Type: graph.Puzzle, Title: "The door", PuzzleCfg: &graph.PuzzleConfig{Outputs: []graph.Output{right("solved", "open", 5)}}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
			{Slug: "dead", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Failed}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "split"},
			{From: "split", FromPort: "a", To: "pa"},
			{From: "pa", FromPort: "solved", To: "gift"},
			{From: "gift", FromPort: "out", To: "join"},
			{From: "pa", FromPort: "fall", To: "dead"},
			{From: "split", FromPort: "b", To: "pb"},
			{From: "pb", FromPort: "solved", To: "join"},
			{From: "join", FromPort: "out", To: "door"},
			{From: "door", FromPort: "solved", To: "end"},
		},
	}
}

var (
	atChurch = &Position{Lat: 59.30, Lng: 18.00}
	atWell   = &Position{Lat: 59.40, Lng: 18.10}
)

// moved applies an action's step, failing the test on a refusal:
// moved(t, tm, "what")(tm.Answer(…)).
func moved(t *testing.T, tm *Team, what string) func(Result, *walk.Step, error) Result {
	return func(res Result, step *walk.Step, err error) Result {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if step != nil {
			tm.Apply(*step)
		}
		return res
	}
}

func startedSplit(t *testing.T) *Team {
	t.Helper()
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Split != "split" || len(tm.Strands) != 2 {
		t.Fatalf("arriving at the split should divide the team: split %q, strands %+v", tm.Split, tm.Strands)
	}
	return tm
}

// The team as a whole — the designer's simulator — walks the groups one
// after another: the first path to the join, then the second, and the
// last to arrive takes the team on, carrying what each group found.
func TestSplitTeamAsWhole(t *testing.T) {
	tm := startedSplit(t)
	tm.Position = atChurch
	v := tm.View()
	if v.Type != graph.Split || v.Node != "split" || v.Text != "Off you go." || v.Group != "Church party" {
		t.Fatalf("split view = %+v", v)
	}
	if len(v.Groups) != 2 || v.Groups[0].Label != "Church party" || v.Groups[1].Label != "Medic" || !v.Groups[0].Current {
		t.Fatalf("groups = %+v", v.Groups)
	}
	moved(t, tm, "continue a")(tm.Continue("split"))
	if v := tm.View(); v.Node != "pa" || v.Groups[0].Node != "pa" || v.Groups[1].Node != "split" {
		t.Fatalf("after the first group set off: %+v", v)
	}
	moved(t, tm, "answer pa")(tm.Answer("pa", "bell"))
	if tm.Strands[0].Current != "join" || tm.Items["key"] != 1 {
		t.Fatalf("the first group waits at the join with the key: %+v items %v", tm.Strands, tm.Items)
	}
	// Its phones go on to the group still to walk.
	v = tm.View()
	if v.Node != "split" || !v.Groups[0].Arrived || v.Groups[1].Current != true {
		t.Fatalf("after the first group arrived: %+v", v)
	}
	moved(t, tm, "continue b")(tm.Continue("split"))
	tm.Position = atWell
	moved(t, tm, "answer pb")(tm.Answer("pb", "water"))
	if tm.Split != "" || tm.Strands != nil || tm.Current != "door" {
		t.Fatalf("the last group should take the team on: split %q strands %v current %q", tm.Split, tm.Strands, tm.Current)
	}
	if v := tm.View(); v.Groups != nil || v.Node != "door" || v.Score != 30 {
		t.Fatalf("together again: %+v", v)
	}
	if tm.Nodes["join"] == nil {
		t.Error("the join should be visited")
	}
	moved(t, tm, "door")(tm.Answer("door", "open"))
	if !tm.Completed {
		t.Error("the team should finish")
	}
}

// players deals a team: slots 1..n playing, 1 the scout and 2 the medic.
func players(tm *Team, n int) {
	tm.unfocus()
	tm.Players = nil
	for i := 1; i <= n; i++ {
		tm.Players = append(tm.Players, int64(i))
	}
	tm.Size = n
	tm.RoleHolders = map[string]int64{"scout": 1, "medic": 2}
}

// asPlayer makes the next view or action player slot's, holding the
// roles dealt to them.
func asPlayer(tm *Team, slot int64, pos *Position) *Team {
	tm.unfocus()
	v := &Viewer{Slot: slot, Position: pos}
	for role, holder := range tm.RoleHolders {
		if holder == slot {
			v.Roles = append(v.Roles, role)
		}
	}
	if len(v.Roles) == 0 || len(tm.Players) <= 1 {
		v.Roles = append(v.Roles, "crew")
	}
	tm.Viewer = v
	tm.PressChanged = nil
	return tm
}

// A path's role takes its player, and the rest are shared out among the
// paths, each to the one with fewest — so a hundred players split in
// two halves, the scout and the medic each leading one.
func TestDealSharesOutTheCrew(t *testing.T) {
	tm, _, _ := Start(expedition(), t0)
	players(tm, 100)
	tm.Deal(tm.Players, tm.RoleHolders)
	a, b := tm.Strands[0].Members, tm.Strands[1].Members
	if len(a) != 50 || len(b) != 50 || a[0] != 1 || b[0] != 2 {
		t.Fatalf("groups of %d and %d, led by %v and %v", len(a), len(b), a[:1], b[:1])
	}
	tm.Deal(tm.Players, tm.RoleHolders)
	if !slices.Equal(tm.Strands[0].Members, a) || !slices.Equal(tm.Strands[1].Members, b) {
		t.Error("dealing again should change nothing")
	}
}

// A default role's path takes everyone holding no other role.
func TestDealDefaultRolePath(t *testing.T) {
	g := expedition()
	n, _ := g.Node("split")
	n.SplitCfg.Paths[1].Role = "crew"
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "split" {
			g.Nodes[i] = n
		}
	}
	tm, _, _ := Start(g, t0)
	players(tm, 4)
	tm.Deal(tm.Players, tm.RoleHolders)
	// The medic holds neither path's role, so is shared out: to the
	// scout's, the smaller.
	if a, b := tm.Strands[0].Members, tm.Strands[1].Members; !slices.Equal(a, []int64{1, 2}) || !slices.Equal(b, []int64{3, 4}) {
		t.Fatalf("scout's group %v, crew's %v", a, b)
	}
}

// Arriving at the split with the players known deals them at once.
func TestSplitDealsAsTheTeamArrives(t *testing.T) {
	g := expedition()
	tm := New(g, t0)
	players(tm, 4)
	step, err := walk.First(g, tm)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if !slices.Equal(tm.Strands[0].Members, []int64{1, 3}) || !slices.Equal(tm.Strands[1].Members, []int64{2, 4}) {
		t.Fatalf("members: %+v", tm.Strands)
	}
}

// Each phone sees its own group, and acts on its own group's steps only.
func TestEachPhoneWalksItsGroup(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 4)
	tm.Deal(tm.Players, tm.RoleHolders)
	stand(tm, map[int64]*Position{1: atChurch, 3: atChurch, 2: atWell, 4: atWell})

	if v := asPlayer(tm, 4, atWell).View(); v.Group != "Medic" || !v.Groups[1].Mine || v.Groups[0].Mine || v.Groups[1].Players != 2 {
		t.Fatalf("player 4's view: %+v", v)
	}
	moved(t, tm, "4 sets off")(asPlayer(tm, 4, atWell).Continue("split"))
	if v := asPlayer(tm, 3, atChurch).View(); v.Node != "split" || v.Group != "Church party" {
		t.Fatalf("player 3 is still at the split with the church party: %+v", v)
	}
	if v := asPlayer(tm, 2, atWell).View(); v.Node != "pb" {
		t.Fatalf("player 2 is with player 4 at the well: %+v", v)
	}
	// Not theirs: the well is the medic's group's.
	if _, _, err := asPlayer(tm, 3, atWell).Answer("pb", "water"); !errors.Is(err, ErrOtherGroup) {
		t.Fatalf("player 3 answering at the well: %v, want ErrOtherGroup", err)
	}
	// A step no group is on is stale, as ever.
	if _, _, err := asPlayer(tm, 3, atChurch).Answer("door", "open"); !errors.Is(err, ErrStale) {
		t.Fatalf("answering the door early: %v, want ErrStale", err)
	}
	moved(t, tm, "2 answers")(asPlayer(tm, 2, atWell).Answer("pb", "water"))
	if v := asPlayer(tm, 4, atWell).View(); v.Type != graph.Join || v.Node != "join" || !v.Groups[1].Arrived || v.Groups[0].Arrived || v.Awaiting != 1 {
		t.Fatalf("player 4 waits at the join: %+v", v)
	}
	moved(t, tm, "1 sets off")(asPlayer(tm, 1, atChurch).Continue("split"))
	// Stale within a group: player 3 still sees the split, 1 has moved on.
	if _, _, err := asPlayer(tm, 3, atChurch).Continue("split"); !errors.Is(err, ErrStale) {
		t.Fatalf("player 3 continuing again: %v, want ErrStale", err)
	}
	moved(t, tm, "3 answers")(asPlayer(tm, 3, atChurch).Answer("pa", "bell"))
	for slot := int64(1); slot <= 4; slot++ {
		if v := asPlayer(tm, slot, nil).View(); v.Node != "door" || v.Groups != nil {
			t.Fatalf("player %d after the last group arrived: %+v", slot, v)
		}
	}
}

// A place on a group's path is reached by one of the group: a teammate
// of the other group standing there does not count.
func TestGroupZonesAreTheGroups(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 2)
	tm.Deal(tm.Players, tm.RoleHolders)
	// Player 1 (the church party) stands at the well; 2 is elsewhere.
	tm.PhoneAt = map[int64]Position{1: *atWell, 2: {Lat: 0, Lng: 0}}
	tm.Phones = []Position{*atWell, {Lat: 0, Lng: 0}}
	moved(t, tm, "2 sets off")(asPlayer(tm, 2, &Position{}).Continue("split"))
	if _, _, err := asPlayer(tm, 2, &Position{}).Answer("pb", "water"); !errors.Is(err, ErrNotHere) {
		t.Fatalf("the medic answering at the well from afar: %v, want ErrNotHere", err)
	}
	stand(tm, map[int64]*Position{1: atWell, 2: atWell})
	moved(t, tm, "2 at the well")(asPlayer(tm, 2, atWell).Answer("pb", "water"))
	if tm.unfocus(); len(tm.Phones) != 2 {
		t.Errorf("focusing should put the team's phones back after: %v", tm.Phones)
	}
}

// stand puts each player's phone where the live engine would say it is.
func stand(tm *Team, at map[int64]*Position) {
	tm.unfocus()
	tm.PhoneAt, tm.Phones = map[int64]Position{}, []Position{}
	for _, slot := range tm.Players {
		if p := at[slot]; p != nil {
			tm.PhoneAt[slot] = *p
			tm.Phones = append(tm.Phones, *p)
		}
	}
}

// A step done together waits for the group, not the whole team.
func TestUnanimousWithinAGroup(t *testing.T) {
	g := expedition()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.Unanimous = true
			g.Nodes[i].PuzzleCfg.Zone = ""
		}
	}
	tm, _, _ := Start(g, t0)
	players(tm, 6) // a: 1, 3, 5; b: 2, 4, 6
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "set off")(asPlayer(tm, 2, nil).Continue("split"))
	res := moved(t, tm, "2 agrees")(asPlayer(tm, 2, nil).Answer("pb", "water"))
	if res.Waiting == nil || res.Waiting.Of != 3 {
		t.Fatalf("waiting = %+v, want for the group's 3", res.Waiting)
	}
	moved(t, tm, "4 agrees")(asPlayer(tm, 4, nil).Answer("pb", "water"))
	res = moved(t, tm, "6 agrees")(asPlayer(tm, 6, nil).Answer("pb", "water"))
	if !res.Accepted || tm.Strands[1].Current != "join" {
		t.Fatalf("all three agreed: %+v, strands %+v", res, tm.Strands)
	}
	if !slices.Equal(tm.Players, []int64{1, 2, 3, 4, 5, 6}) {
		t.Errorf("the team's players should be put back after: %v", tm.Players)
	}
}

// A role no one in a group holds is everyone's in it: the church party
// can answer the medic's question when the medic is at the well.
func TestRoleOutsideTheGroupIsAnyones(t *testing.T) {
	g := expedition()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i].PuzzleCfg.By = "medic"
		}
	}
	tm, _, _ := Start(g, t0)
	players(tm, 3) // a: 1, 3; b: 2
	tm.Deal(tm.Players, tm.RoleHolders)
	stand(tm, map[int64]*Position{3: atChurch})
	moved(t, tm, "set off")(asPlayer(tm, 3, atChurch).Continue("split"))
	if v := asPlayer(tm, 3, atChurch).View(); v.Input == nil || v.Input.NotYours {
		t.Fatalf("player 3's view: %+v", v.Input)
	}
	moved(t, tm, "3 answers for the medic")(asPlayer(tm, 3, atChurch).Answer("pa", "bell"))
	if roles := asPlayer(tm, 3, nil).Viewer.Roles; slices.Contains(roles, "medic") {
		t.Errorf("player 3 holds the medic's role only inside the group: %v", roles)
	}
}

// A player alone walks every path, one after another.
func TestAPlayerAloneWalksEveryPath(t *testing.T) {
	tm := startedSplit(t)
	tm.Players, tm.Size = []int64{7}, 1
	tm.RoleHolders = map[string]int64{"scout": 7, "medic": 7}
	tm.Deal(tm.Players, tm.RoleHolders)
	me := func(pos *Position) *Team {
		stand(tm, map[int64]*Position{7: pos})
		return asPlayer(tm, 7, pos)
	}
	moved(t, tm, "a")(me(atChurch).Continue("split"))
	moved(t, tm, "pa")(me(atChurch).Answer("pa", "bell"))
	if v := me(atChurch).View(); v.Node != "split" || v.Group != "Medic" {
		t.Fatalf("on to the medic's path: %+v", v)
	}
	moved(t, tm, "b")(me(atWell).Continue("split"))
	moved(t, tm, "pb")(me(atWell).Answer("pb", "water"))
	if tm.Current != "door" || tm.Split != "" {
		t.Fatalf("current %q, split %q", tm.Current, tm.Split)
	}
}

// A group whose players have all gone is anyone's: whoever has finished
// their own walks it, so no team is held at the join for good.
func TestAGroupWithNoOneLeftIsAnyones(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 4)
	tm.Deal(tm.Players, tm.RoleHolders)
	// The medic's group, 2 and 4, go home.
	tm.Players = []int64{1, 3}
	stand(tm, map[int64]*Position{1: atChurch, 3: atChurch})
	moved(t, tm, "a")(asPlayer(tm, 1, atChurch).Continue("split"))
	moved(t, tm, "pa")(asPlayer(tm, 3, atChurch).Answer("pa", "bell"))
	v := asPlayer(tm, 3, atWell).View()
	if v.Node != "split" || v.Group != "Medic" || v.Groups[1].Players != 0 || !v.Groups[1].Open || v.Groups[1].Mine {
		t.Fatalf("player 3 should be offered the medic's empty group: %+v", v)
	}
	moved(t, tm, "b")(asPlayer(tm, 3, atWell).Continue("split"))
	stand(tm, map[int64]*Position{1: atWell, 3: atWell})
	moved(t, tm, "pb")(asPlayer(tm, 1, atWell).Answer("pb", "water"))
	if tm.Current != "door" {
		t.Fatalf("current %q", tm.Current)
	}
}

// A player who joins mid-split is dealt to the smallest group, and no
// one else changes group.
func TestLateJoinerChangesNoOnesGroup(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 3) // a: 1, 3; b: 2
	tm.Deal(tm.Players, tm.RoleHolders)
	players(tm, 5)
	tm.Deal(tm.Players, tm.RoleHolders)
	if a, b := tm.Strands[0].Members, tm.Strands[1].Members; !slices.Equal(a, []int64{1, 3, 5}) || !slices.Equal(b, []int64{2, 4}) {
		t.Fatalf("groups %v and %v", a, b)
	}
}

// The adventure's time limit sends the whole team on, together.
func TestTimeLimitEndsTheSplit(t *testing.T) {
	g := expedition()
	g.TimeLimit = &graph.TimeLimit{Seconds: 600, To: "door"}
	tm, _, _ := Start(g, t0)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.Now = t0.Add(11 * time.Minute)
	res, step, err := tm.Act(func() (Result, *walk.Step, error) { return tm.Answer("pa", "bell") })
	moved(t, tm, "time up")(res, step, err)
	if !res.TimeUp || tm.Split != "" || tm.Strands != nil || tm.Current != "door" {
		t.Fatalf("res %+v, split %q, current %q", res, tm.Split, tm.Current)
	}
}

// An ending on one group's path ends the adventure for every group.
func TestAnEndingOnAPathEndsItForEveryone(t *testing.T) {
	tm := startedSplit(t)
	tm.Position = atChurch
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "jump")(tm.Answer("pa", "jump"))
	if !tm.Completed || tm.Ending != graph.Failed || tm.Split != "" || tm.Current != "dead" {
		t.Fatalf("completed %v %q, split %q, current %q", tm.Completed, tm.Ending, tm.Split, tm.Current)
	}
	if _, _, err := tm.Continue("split"); !errors.Is(err, ErrFinished) {
		t.Errorf("the other group continuing: %v, want ErrFinished", err)
	}
}

// The simulator keeps a split team in the browser between calls.
func TestSplitTeamRoundTripsThroughJSON(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.View() // focused on the church party
	raw, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	var back Team
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	back.G = tm.G
	back.Init()
	back.Position = atChurch
	if back.Split != "split" || len(back.Strands) != 2 || back.Strands[0].Current != "pa" {
		t.Fatalf("decoded: split %q strands %+v", back.Split, back.Strands)
	}
	if v := back.View(); v.Node != "pa" {
		t.Fatalf("decoded view at %q", v.Node)
	}
	moved(t, &back, "pa")(back.Answer("pa", "bell"))
	if back.Strands[0].Current != "join" {
		t.Fatalf("strands %+v", back.Strands)
	}
}

// The team as a whole is at the split for the organisers, wherever its
// groups are, and no group goes back to a home screen alone.
func TestWholeAndNoBack(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	if tm.Whole() != "split" {
		t.Errorf("Whole = %q, want the split", tm.Whole())
	}
	if _, ok := tm.backTo(); ok {
		t.Error("no going back while split")
	}
}

// The designer's preview of a split shows it as a team would arrive.
func TestPlaceOnASplit(t *testing.T) {
	tm, err := Place(expedition(), "split", t0)
	if err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.Type != graph.Split || len(v.Groups) != 2 {
		t.Fatalf("view = %+v", v)
	}
	moved(t, tm, "a")(tm.Continue("split"))
}

// Fewer players than the adventure needs counts the whole team, not
// the group a phone is with.
func TestTooFewCountsTheWholeTeam(t *testing.T) {
	g := expedition()
	g.MinPlayers = 4
	tm, _, _ := Start(g, t0)
	players(tm, 4)
	tm.Deal(tm.Players, tm.RoleHolders)
	if v := asPlayer(tm, 1, atChurch).View(); v.PlayersMin != 0 {
		t.Fatalf("a group of two in a team of four is not too few: %+v", v)
	}
}

// Translations reach the split's words and the groups' names.
func TestSplitTranslated(t *testing.T) {
	g := expedition()
	g.Language = "en"
	g.Translations = map[string]map[string]string{"sv": {
		graph.NodeKey("split", "split-text"):   "Gå nu.",
		graph.NodeKey("split", "path:a:label"): "Kyrkgänget",
		graph.RoleKey("medic", "name"):         "Läkare",
		graph.NodeKey("split", "title"):        "Dela upp",
	}}
	tm, _, _ := Start(g, t0)
	tm.Lang = "sv"
	v := tm.View()
	if v.Text != "Gå nu." || v.Title != "Dela upp" || v.Groups[0].Label != "Kyrkgänget" || v.Groups[1].Label != "Läkare" || v.Groups[0].At != "Dela upp" {
		t.Fatalf("view = %+v", v)
	}
}

// Where the simulator moves a team while it is focused on a group stays
// put: focusing narrows only what it knows per phone. Regression: the
// bots' moves were undone, and they never arrived.
func TestFocusKeepsTheSimulatorsMoves(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.View()
	tm.Position = atChurch
	tm.AddWalk(0, 120)
	tm.View()
	if tm.Position != atChurch {
		t.Fatalf("position %v, want the church", tm.Position)
	}
	// The walk is the group's the team's view is for, a view or not.
	if tm.unfocus(); tm.WalkedMetres != 0 || tm.Strands[0].WalkedMetres != 120 {
		t.Errorf("after a view: the team's walk %v, the group's %v; want 0 and 120", tm.WalkedMetres, tm.Strands[0].WalkedMetres)
	}
	moved(t, tm, "a wrong answer")(tm.Answer("pa", "nope"))
	if tm.unfocus(); tm.Strands[0].WalkedMetres != 120 {
		t.Errorf("the group's walk %v, want 120", tm.Strands[0].WalkedMetres)
	}
	moved(t, tm, "pa")(tm.Answer("pa", "bell"))
}

// A team sent through JSON while focused on a group comes back with its
// own cursor once unfocused: the focus does not travel. Found by random
// play.
func TestFocusDoesNotTravelInJSON(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.View() // focused on the church party, at pa
	raw, _ := json.Marshal(tm)
	var back Team
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	back.G = tm.G
	back.Init()
	if back.unfocus(); back.Current != "split" {
		t.Fatalf("unfocused, the decoded team is at %q", back.Current)
	}
}

// A phone pressing to set off after its group already has is late, not
// acting for another group still at the split: stale, which the phone
// takes in its stride. Found by the live stress test, where ten players
// of a group pressed at once and the rest were told it was not theirs.
func TestLateSetOffIsStale(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 4) // a: 1, 3; b: 2, 4
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "1 sets off")(asPlayer(tm, 1, nil).Continue("split"))
	if _, _, err := asPlayer(tm, 3, nil).Continue("split"); !errors.Is(err, ErrStale) {
		t.Fatalf("3 pressing late: %v, want ErrStale", err)
	}
	// Another group's step on its path is still not theirs.
	moved(t, tm, "2 sets off")(asPlayer(tm, 2, nil).Continue("split"))
	if _, _, err := asPlayer(tm, 3, nil).Answer("pb", "water"); !errors.Is(err, ErrOtherGroup) {
		t.Fatalf("3 at the well's step: %v, want ErrOtherGroup", err)
	}
}

// A phone pressing to set off twice sets off its group once: the second
// press is a repeat, stale, not one for a group no one is left in, nor
// for the player's own next group — whose turn comes after the first is
// at the join. Regression: a double tap sent the empty group off too.
func TestSetOffTwiceIsStale(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 4)
	tm.Deal(tm.Players, tm.RoleHolders)
	tm.Players = []int64{1, 3} // the medic's group goes home
	moved(t, tm, "1 sets off")(asPlayer(tm, 1, nil).Continue("split"))
	if _, _, err := asPlayer(tm, 1, nil).Continue("split"); !errors.Is(err, ErrStale) || tm.Strands[1].Current != "split" {
		t.Fatalf("1 again: %v, strands %+v", err, tm.Strands)
	}

	tm = startedSplit(t)
	tm.Players, tm.Size = []int64{7}, 1
	tm.RoleHolders = map[string]int64{"scout": 7, "medic": 7}
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "a")(asPlayer(tm, 7, atChurch).Continue("split"))
	if _, _, err := asPlayer(tm, 7, atChurch).Continue("split"); !errors.Is(err, ErrStale) || tm.Strands[1].Current != "split" {
		t.Fatalf("alone, again: %v, strands %+v", err, tm.Strands)
	}
	stand(tm, map[int64]*Position{7: atChurch})
	moved(t, tm, "pa")(asPlayer(tm, 7, atChurch).Answer("pa", "bell"))
	moved(t, tm, "b, in its turn")(asPlayer(tm, 7, atWell).Continue("split"))
	if tm.Strands[1].Current != "pb" {
		t.Fatalf("strands %+v", tm.Strands)
	}
}

// A group's presses on its step are kept for its view: the first key
// turned shows turned. Regression: the view showed none until the
// second.
func TestGroupPressesShowInItsView(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("b1", "x").join("j").end("end").
		add(graph.Node{Slug: "launch", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Answer:  &graph.Answer{Kind: graph.AnswerKeys},
			Outputs: []graph.Output{{Port: "go", Trigger: graph.OnAnswer}},
		}}).
		wire("start", "out", "s").wire("s", "a", "launch").wire("s", "b", "b1").
		wire("launch", "go", "j").wire("b1", "solved", "j").wire("j", "out", "end")
	tm := begin(t, b.valid(t))
	tm.unfocus()
	tm.Players, tm.Size = []int64{1, 2, 3, 4}, 4 // a: 1, 3
	tm.Deal(tm.Players, nil)
	moved(t, tm, "a")(asPlayer(tm, 1, nil).Continue("s"))
	moved(t, tm, "1 turns")(asPlayer(tm, 1, nil).Turn("launch"))
	if v := asPlayer(tm, 1, nil).View(); v.Together == nil || v.Together.Done != 1 || !v.Together.Mine {
		t.Fatalf("1's key should show turned: %+v", v.Together)
	}
	if v := asPlayer(tm, 3, nil).View(); v.Together == nil || v.Together.Done != 1 || v.Together.Mine {
		t.Fatalf("3 should see 1's key turned: %+v", v.Together)
	}
}

// How far a group walks counts, though the team was sent through JSON
// and moved, as the designer's simulator does (AddWalk): the walk is the
// group's. Regression: it was lost, every time.
func TestWalkedThroughJSONIsTheGroups(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	trip := func(tm *Team) *Team {
		t.Helper()
		raw, err := json.Marshal(tm)
		if err != nil {
			t.Fatal(err)
		}
		var back Team
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		back.G = tm.G
		back.Init()
		return &back
	}
	tm = trip(tm)
	tm.AddWalk(0, 1100)
	tm = trip(tm)
	tm.AddWalk(0, 1100)
	// The next action on the church party's step is the walk's.
	tm.Position = atChurch
	moved(t, tm, "a wrong answer")(tm.Answer("pa", "nope"))
	if tm.unfocus(); tm.Strands[0].WalkedMetres != 2200 || tm.Strands[1].WalkedMetres != 0 || tm.WalkedMetres != 0 {
		t.Fatalf("team %v, strands %+v", tm.WalkedMetres, tm.Strands)
	}
}

// Looking at the team credits no group the simulator's moves: Step, as
// the crowd simulator follows each team, leaves them the team's.
func TestStepCreditsNoGroup(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.WalkedMetres = 300
	tm.Step()
	tm.View()
	if tm.WalkedMetres != 300 || tm.Strands[0].WalkedMetres != 0 || tm.Strands[1].WalkedMetres != 0 {
		t.Fatalf("a look moved the walk: team %v, strands %+v", tm.WalkedMetres, tm.Strands)
	}
}

// A team whose groups no longer fit its graph splits afresh where it
// stands, if that is a split, or is one team.
func TestSplitAfresh(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.unfocus()
	tm.Strands[0].Port = "gone"
	tm.SplitAfresh()
	if tm.Split != "split" || len(tm.Strands) != 2 || tm.Strands[0].Current != "split" || tm.Strands[0].Port != "a" {
		t.Fatalf("split %q strands %+v", tm.Split, tm.Strands)
	}
	// The split edited into a story: the team is one, on it.
	g := expedition()
	g.Nodes[1].Type, g.Nodes[1].SplitCfg = graph.Story, nil
	tm.G = g
	tm.SplitAfresh()
	if tm.Split != "" || tm.Strands != nil || tm.Current != "split" {
		t.Fatalf("split %q strands %+v at %q", tm.Split, tm.Strands, tm.Current)
	}
}

// vaultAndKey splits at "s": the vault on path a opens with the key the
// gift on path b grants.
func vaultAndKey(t *testing.T) *graph.Graph {
	t.Helper()
	b := newGB()
	b.g.Items = []graph.Item{{Slug: "key", Name: "Key"}}
	return b.split("s", "a", "b").
		add(graph.Node{Slug: "vault", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Answer:  &graph.Answer{Kind: graph.AnswerItems, Slots: []graph.ItemSlot{{Item: "key"}}},
			Outputs: []graph.Output{{Port: "open", Trigger: graph.OnAnswer}},
		}}).
		add(graph.Node{Slug: "gift", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}}).
		add(graph.Node{Slug: "b1", Type: graph.Story, Title: "The key"}).join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "vault").wire("s", "b", "gift").wire("gift", "out", "b1").
		wire("vault", "open", "j").wire("b1", "out", "j").wire("j", "out", "end").
		valid(t)
}

// A player in two groups chooses which they walk, and hops between them
// at any step: the vault on one path waits for the key the other finds.
// Regression: a player alone stood at the vault for good.
func TestWalkAnotherOfMyGroups(t *testing.T) {
	tm := begin(t, vaultAndKey(t))
	tm.unfocus()
	tm.Players, tm.Size = []int64{7}, 1
	tm.Deal(tm.Players, nil)
	moved(t, tm, "a")(asNobody(tm, 7).Continue("s"))
	if res, _, _ := asNobody(tm, 7).Answer("vault", "key"); res.Accepted {
		t.Fatal("the vault opened without the key")
	}
	moved(t, tm, "walk b")(asNobody(tm, 7).Walk("b"))
	if v := asNobody(tm, 7).View(); v.Node != "s" || !v.Groups[1].Current || v.Groups[0].Current {
		t.Fatalf("walking b: %+v", v)
	}
	moved(t, tm, "b")(asNobody(tm, 7).Continue("s"))
	// Back to the vault before b is done, and to b again: any step.
	moved(t, tm, "walk a")(asNobody(tm, 7).Walk("a"))
	if v := asNobody(tm, 7).View(); v.Node != "vault" {
		t.Fatalf("back at the vault: %+v", v)
	}
	moved(t, tm, "walk b again")(asNobody(tm, 7).Walk("b"))
	moved(t, tm, "b1")(asNobody(tm, 7).Continue("b1"))
	if _, _, err := asNobody(tm, 7).Walk("b"); !errors.Is(err, ErrStale) {
		t.Fatalf("walking a group at its join: %v", err)
	}
	// b arrived: the choice is forgotten, and the player is at the vault.
	if v := asNobody(tm, 7).View(); v.Node != "vault" {
		t.Fatalf("after b: %+v", v)
	}
	moved(t, tm, "vault")(asNobody(tm, 7).Answer("vault", "key"))
	if !tm.Completed {
		t.Fatalf("not finished: %v", keys(tm))
	}
}

// A phone walks only its own groups.
func TestWalkNotMyGroup(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 4) // a: 1, 3; b: 2, 4
	tm.Deal(tm.Players, tm.RoleHolders)
	if _, _, err := asPlayer(tm, 1, nil).Walk("b"); !errors.Is(err, ErrOtherGroup) {
		t.Fatalf("1 walking b: %v", err)
	}
	if _, _, err := asPlayer(tm, 1, nil).Walk("nowhere"); !errors.Is(err, ErrStale) {
		t.Fatalf("1 walking no group: %v", err)
	}
	// Its players away, b is anyone's: 1 may walk it.
	tm.Away = map[int64]bool{2: true, 4: true}
	moved(t, tm, "1 walks b")(asPlayer(tm, 1, nil).Walk("b"))
	if v := asPlayer(tm, 1, nil).View(); v.Group != "Medic" || !v.Groups[1].Open || v.Groups[1].Mine || !v.Groups[0].Mine {
		t.Fatalf("1's view: %+v", v)
	}
	// One of them back, it is theirs again.
	tm.Away = map[int64]bool{4: true}
	if v := asPlayer(tm, 1, nil).View(); v.Group != "Church party" || v.Groups[1].Mine || v.Groups[1].Open {
		t.Fatalf("1's view with 2 back: %+v", v)
	}
}

// The organisers move a group on from any step of its path with a way
// on: a story no one is left to read.
func TestRescueAGroupsStory(t *testing.T) {
	g := vaultAndKey(t)
	tm := begin(t, g)
	moved(t, tm, "b")(tm.RescueGroup("s", "b"))
	if _, _, err := tm.Rescue("start"); err == nil {
		t.Fatal("rescued from nowhere")
	}
	moved(t, tm, "past b1")(tm.RescueGroup("b1", "b"))
	if got := keys(tm); !slices.Equal(got, []string{"a@s", "b@j"}) {
		t.Fatalf("groups %v", got)
	}
	if CanRescue(graph.Node{Type: graph.Story}) {
		t.Error("a team's story is rescuable")
	}
}

// A group at a hub on its path is moved on into its first branch, and
// out once it has done enough there.
func TestRescueAGroupsHub(t *testing.T) {
	g := newGB().split("s", "a", "b").
		add(graph.Node{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Required: 1, Branches: []graph.Branch{{Port: "x"}, {Port: "y"}}}}).
		puzzle("hx", "x").puzzle("hy", "y").puzzle("b1", "b").join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "hub").wire("s", "b", "b1").
		wire("hub", "x", "hx").wire("hub", "y", "hy").wire("hx", "solved", "hub").wire("hy", "solved", "hub").
		wire("hub", "done", "j").wire("b1", "solved", "j").wire("j", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.RescueGroup("s", "a"))
	moved(t, tm, "into x")(tm.RescueGroup("hub", "a"))
	moved(t, tm, "past hx")(tm.RescueGroup("hx", "a"))
	moved(t, tm, "out")(tm.RescueGroup("hub", "a"))
	if got := keys(tm); !slices.Equal(got, []string{"a@j", "b@s"}) {
		t.Fatalf("groups %v", got)
	}
}

// A group anyone's because every one of its players is away is finished
// by those walking it: a step done together waits for them, not for the
// players away, and the role an away player holds is everyone's in it.
// One of them back, the group is theirs again. Regression: the away
// players were still counted, and the walker waited for them for good.
func TestAwayGroupIsFinishedByItsWalkers(t *testing.T) {
	g := expedition()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.Unanimous = true
			g.Nodes[i].PuzzleCfg.Zone = ""
		}
	}
	tm, _, _ := Start(g, t0)
	players(tm, 4) // a: 1, 3; b: 2, 4
	tm.Deal(tm.Players, tm.RoleHolders)
	tm.Away = map[int64]bool{2: true, 4: true}
	if w := tm.Walkers(tm.Everyone()); len(w["b"]) != 0 || !slices.Equal(w["a"], []int64{1, 3}) {
		t.Fatalf("walkers with b's players away: %v", w)
	}
	moved(t, tm, "1 walks b")(asPlayer(tm, 1, nil).Walk("b"))
	if w := tm.Walkers(tm.Everyone()); !slices.Equal(w["b"], []int64{1}) || !slices.Equal(w["a"], []int64{3}) {
		t.Fatalf("walkers with 1 on b: %v", w)
	}
	moved(t, tm, "1 sets b off")(asPlayer(tm, 1, nil).Continue("split"))
	// 2 back: b is 2's and 4's again, and 1 is back with a.
	tm.Away = map[int64]bool{4: true}
	if w := tm.Walkers(tm.Everyone()); !slices.Equal(w["b"], []int64{2, 4}) || !slices.Equal(w["a"], []int64{1, 3}) {
		t.Fatalf("walkers with 2 back: %v", w)
	}
	tm.Away = map[int64]bool{2: true, 4: true}
	res := moved(t, tm, "1 agrees")(asPlayer(tm, 1, nil).Answer("pb", "water"))
	if !res.Accepted || tm.Strands[1].Current != "join" {
		t.Fatalf("1, b's one walker, agreed: %+v, strands %+v", res, tm.Strands)
	}
}

// The role of a player away is everyone's in their group while it is
// anyone's: the one walking it answers for the medic.
func TestAwayHoldersRoleIsTheWalkers(t *testing.T) {
	g := expedition()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.By = "medic"
			g.Nodes[i].PuzzleCfg.Zone = ""
		}
	}
	tm, _, _ := Start(g, t0)
	players(tm, 3) // a: 1, 3; b: 2, the medic
	tm.Deal(tm.Players, tm.RoleHolders)
	tm.Away = map[int64]bool{2: true}
	moved(t, tm, "3 walks b")(asPlayer(tm, 3, nil).Walk("b"))
	moved(t, tm, "3 sets b off")(asPlayer(tm, 3, nil).Continue("split"))
	if v := asPlayer(tm, 3, nil).View(); v.Node != "pb" || v.Input == nil || v.Input.NotYours {
		t.Fatalf("3's view at the well: %+v", v.Input)
	}
	moved(t, tm, "3 answers for the medic")(asPlayer(tm, 3, nil).Answer("pb", "water"))
	if tm.Strands[1].Current != "join" {
		t.Fatalf("strands %+v", tm.Strands)
	}
}

// A rescue of a group the team no longer has — met at its join, or no
// group of the team at all — is stale, and moves no one. Regression: a
// team together again was moved on from its own step.
func TestRescueOfAGroupGoneIsStale(t *testing.T) {
	tm, err := Place(expedition(), "door", t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tm.RescueGroup("door", "a"); !errors.Is(err, ErrStale) || tm.Current != "door" {
		t.Fatalf("rescuing group a of a team together: %v, at %q", err, tm.Current)
	}
	tm = startedSplit(t)
	if _, _, err := tm.RescueGroup("split", "c"); !errors.Is(err, ErrStale) {
		t.Fatalf("rescuing no group: %v", err)
	}
}

// The team as a whole sets off the group it chose to walk, though the
// focus did not travel with it: the designer's playtest, sent back and
// forth as JSON. Regression: it set off the first group on the split.
func TestTeamAsWholeSetsOffItsChoice(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "walk b")(tm.Walk("b"))
	raw, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	var back Team
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	back.G = tm.G
	back.Init()
	moved(t, &back, "set off")(back.Continue("split"))
	if back.Strands[0].Current != "split" || back.Strands[1].Current != "pb" {
		t.Fatalf("strands %+v", back.Strands)
	}
}

// A group rescued at a home screen on its path is taken toward its join:
// by a branch that leads there, not one that comes back, nor one that
// ends the adventure; with none open, by one that comes back, but not
// round the same again — and never by one that ends the adventure, for
// every group. Regression: it went into the first branch, each time, for
// good; then, with nothing else, it took the ending, and finished the
// team while the other group had not set off.
func TestRescueAGroupsHomeHub(t *testing.T) {
	key := cond.Predicate{Op: cond.OpItem, Item: "key"}
	build := func(t *testing.T) *graph.Graph {
		b := newGB()
		b.g.Items = []graph.Item{{Slug: "key", Name: "Key"}}
		return b.split("s", "a", "b").
			add(graph.Node{Slug: "home", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{
				{Port: "look"}, {Port: "quit"}, {Port: "leave", Condition: key},
			}}}).
			puzzle("look1", "x").puzzle("quit1", "q").puzzle("leave1", "go").puzzle("b1", "b").join("j").end("end").end("over").
			wire("start", "out", "s").wire("s", "a", "home").wire("s", "b", "b1").
			wire("home", "look", "look1").wire("look1", "solved", "home").
			wire("home", "quit", "quit1").wire("quit1", "solved", "over").
			wire("home", "leave", "leave1").wire("leave1", "solved", "j").
			wire("b1", "solved", "j").wire("j", "out", "end").valid(t)
	}
	tm := begin(t, build(t))
	moved(t, tm, "a")(tm.RescueGroup("s", "a"))
	moved(t, tm, "into look")(tm.RescueGroup("home", "a"))
	if got := keys(tm); !slices.Equal(got, []string{"a@look1", "b@s"}) {
		t.Fatalf("with the way on shut, into the branch that comes back: %v", got)
	}
	moved(t, tm, "past look1")(tm.RescueGroup("look1", "a"))
	if _, _, err := tm.RescueGroup("home", "a"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("nothing else left but the branch that ends it: %v", err)
	}
	if got := keys(tm); !slices.Equal(got, []string{"a@home", "b@s"}) {
		t.Fatalf("refused, and no one moved: %v", got)
	}
	// Nor when b is the one at its join, a the last on its way.
	moved(t, tm, "b")(tm.RescueGroup("s", "b"))
	moved(t, tm, "past b1")(tm.RescueGroup("b1", "b"))
	if _, _, err := tm.RescueGroup("home", "a"); !errors.Is(err, ErrNotAllowed) || tm.Completed {
		t.Fatalf("the last group on its way, into the ending: %v, completed %v", err, tm.Completed)
	}
	// With the key, the way on.
	tm = begin(t, build(t))
	tm.Items["key"] = 1
	moved(t, tm, "a")(tm.RescueGroup("s", "a"))
	moved(t, tm, "into leave")(tm.RescueGroup("home", "a"))
	if got := keys(tm); !slices.Equal(got, []string{"a@leave1", "b@s"}) {
		t.Fatalf("toward the join: %v", got)
	}
	// No way on, and the branch that comes back done: not rescued.
	g := build(t)
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "home" {
			g.Nodes[i].HubCfg.Branches = g.Nodes[i].HubCfg.Branches[:1]
			g.Nodes[i].HubCfg.Branches = append(g.Nodes[i].HubCfg.Branches, graph.Branch{Port: "leave", Condition: key})
		}
	}
	tm = begin(t, g)
	moved(t, tm, "a")(tm.RescueGroup("s", "a"))
	moved(t, tm, "into look")(tm.RescueGroup("home", "a"))
	moved(t, tm, "past look1")(tm.RescueGroup("look1", "a"))
	if _, _, err := tm.RescueGroup("home", "a"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("round again: %v", err)
	}
}

// A team whose split was taken out of its graph is one team at the start
// screen — the engine having found its node gone — not on the split.
func TestSplitAfreshOnASplitTakenOut(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "a")(tm.Continue("split"))
	tm.unfocus()
	g := expedition()
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "split" })
	tm.G, tm.Current = g, ""
	tm.SplitAfresh()
	if tm.Split != "" || tm.Strands != nil || tm.Current != "" {
		t.Fatalf("split %q strands %+v at %q", tm.Split, tm.Strands, tm.Current)
	}
}

// An organiser's rescue at a split sets off the group it names, though
// the team as a whole chose to walk another standing there too.
// Regression: the chosen group was set off instead.
func TestRescueOnASplitSetsOffTheGroupNamed(t *testing.T) {
	tm := startedSplit(t)
	moved(t, tm, "walk a")(tm.Walk("a"))
	moved(t, tm, "rescue b")(tm.RescueGroup("split", "b"))
	if got := keys(tm); !slices.Equal(got, []string{"a@split", "b@pb"}) {
		t.Fatalf("groups %v", got)
	}
}

// A choice of a group anyone's only while its players were away is spent
// once one of them is back: the phone goes back to its own group, and
// stays there when the group goes quiet again, until it chooses anew.
// So is a choice of one group once the phone acts on another. Regression:
// the phone was taken back to the quiet group, mid-step, unasked.
func TestChoiceOfAQuietGroupIsSpent(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 4) // a: 1, 3; b: 2, 4
	tm.Deal(tm.Players, tm.RoleHolders)
	tm.Away = map[int64]bool{2: true, 4: true}
	moved(t, tm, "1 walks b")(asPlayer(tm, 1, nil).Walk("b"))
	if v := asPlayer(tm, 1, nil).View(); v.Group != "Medic" {
		t.Fatalf("1 walking b: %q", v.Group)
	}
	// 2 back: the engine deals the team afresh on the next request.
	tm.Away = map[int64]bool{4: true}
	tm.Deal(tm.Everyone(), tm.RoleHolders)
	if slices.Contains(tm.Strands[1].Chosen, 1) {
		t.Fatalf("1's choice of b kept with 2 back: %+v", tm.Strands[1].Chosen)
	}
	// 2 quiet again: 1 stays with a.
	tm.Away = map[int64]bool{2: true, 4: true}
	tm.Deal(tm.Everyone(), tm.RoleHolders)
	if v := asPlayer(tm, 1, nil).View(); v.Group != "Church party" {
		t.Fatalf("1 taken back to b unasked: %q", v.Group)
	}

	// Acting on a's step spends 1's choice of b.
	moved(t, tm, "1 walks b again")(asPlayer(tm, 1, nil).Walk("b"))
	moved(t, tm, "3 sets a off")(asPlayer(tm, 3, nil).Continue("split"))
	stand(tm, map[int64]*Position{1: atChurch})
	if res := moved(t, tm, "1 answers at a")(asPlayer(tm, 1, atChurch).Answer("pa", "nope")); res.Accepted {
		t.Fatal("a wrong answer accepted")
	}
	if slices.Contains(tm.Strands[1].Chosen, 1) {
		t.Fatalf("1's choice of b kept after acting on a: %+v", tm.Strands[1].Chosen)
	}
	if v := asPlayer(tm, 1, nil).View(); v.Group != "Church party" {
		t.Fatalf("1 after acting on a: %q", v.Group)
	}
}

// A phone acting on one of its groups while it would walk another — its
// choice of that one spent, or none made — is among the group's players
// for the action: it stands at the group's place.
func TestActingOnAnotherOfMyGroups(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 1)
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "a")(asPlayer(tm, 1, nil).Continue("split"))
	moved(t, tm, "walk b")(asPlayer(tm, 1, nil).Walk("b"))
	moved(t, tm, "b")(asPlayer(tm, 1, nil).Continue("split"))
	moved(t, tm, "walk a")(asPlayer(tm, 1, nil).Walk("a"))
	stand(tm, map[int64]*Position{1: atWell})
	if res := moved(t, tm, "1 answers at the well")(asPlayer(tm, 1, atWell).Answer("pb", "water")); !res.Accepted {
		t.Fatalf("1 at the well, acting on b: %+v", res)
	}
	tm.unfocus()
	if tm.Strands[1].Current != "join" || slices.Contains(tm.Strands[0].Chosen, 1) {
		t.Fatalf("strands %+v", tm.Strands)
	}
	if v := asPlayer(tm, 1, nil).View(); v.Node != "pa" {
		t.Fatalf("b at its join, 1 walks a: %+v", v.Node)
	}
}

// A group whose players all hold another role holds the default too: a
// two-player team splits into a scout alone and a medic alone, and the
// scout still does the crew's step on their path.
func TestSplitGroupOfRoleHoldersHoldsTheDefault(t *testing.T) {
	g := expedition()
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i].PuzzleCfg.By = "crew"
		}
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	players(tm, 2)
	tm.Deal(tm.Players, tm.RoleHolders)
	stand(tm, map[int64]*Position{1: atChurch, 2: atWell})
	moved(t, tm, "1 sets off")(asPlayer(tm, 1, atChurch).Continue("split"))
	if v := asPlayer(tm, 1, atChurch).View(); v.Node != "pa" || v.Input == nil || v.Input.NotYours {
		t.Fatalf("the scout alone should do the crew's step: %+v", v)
	}
	moved(t, tm, "1 answers")(asPlayer(tm, 1, atChurch).Answer("pa", "bell"))
	if tm.Strands[0].Current != "join" {
		t.Fatalf("the scout's group should be at the join: %+v", tm.Strands)
	}
}

// forked: start → split{a "North", b "South"} → a1 / b1 → join → post
// → end.
func forked(t *testing.T) *graph.Graph {
	t.Helper()
	b := newGB().split("split", "a", "b").puzzle("a1", "a").puzzle("b1", "b").join("join").puzzle("post", "p").end("end").
		wire("start", "out", "split").wire("split", "a", "a1").wire("split", "b", "b1").
		wire("a1", "solved", "join").wire("b1", "solved", "join").wire("join", "out", "post").wire("post", "solved", "end")
	for i := range b.g.Nodes {
		if b.g.Nodes[i].Slug == "split" {
			b.g.Nodes[i].SplitCfg.Paths[0].Label, b.g.Nodes[i].SplitCfg.Paths[1].Label = "North", "South"
		}
	}
	return b.valid(t)
}

func keys(tm *Team) []string {
	var out []string
	for _, st := range tm.Strands {
		out = append(out, st.Port+"@"+st.Current)
	}
	return out
}

// asNobody makes the next action player slot's, holding no role.
func asNobody(tm *Team, slot int64) *Team {
	tm.unfocus()
	tm.Viewer = &Viewer{Slot: slot}
	tm.PressChanged = nil
	return tm
}

// The organisers set off one named group from a split several groups
// wait on — the one they mean, not the first — and are told when the
// group has moved or met its siblings since they looked.
func TestRescueAGroupFromItsSplit(t *testing.T) {
	tm := begin(t, forked(t))
	tm.unfocus()
	tm.Viewer = nil
	moved(t, tm, "set the south off")(tm.RescueGroup("split", "b"))
	if got := keys(tm); !slices.Equal(got, []string{"a@split", "b@b1"}) {
		t.Fatalf("the south, not the north: %v", got)
	}
	if _, _, err := tm.RescueGroup("split", "b"); !errors.Is(err, ErrStale) {
		t.Fatalf("the south again: %v", err)
	}
	moved(t, tm, "set the north off")(tm.RescueGroup("split", "a"))
	moved(t, tm, "the north")(tm.RescueGroup("a1", "a"))
	if got := keys(tm); !slices.Equal(got, []string{"a@join", "b@b1"}) {
		t.Fatalf("north at the gate: %v", got)
	}
	moved(t, tm, "the south")(tm.RescueGroup("b1", "b"))
	// Met: a rescue naming either group is stale.
	if _, _, err := tm.RescueGroup("b1", "b"); !errors.Is(err, ErrStale) {
		t.Fatalf("a group that has met its siblings: %v", err)
	}
	if CanRescue(graph.Node{Type: graph.Split}) {
		t.Error("a team is moved on at a split")
	}
}

// A rescue at a split must say which group: the split names none.
func TestRescueAtASplitNamesTheGroup(t *testing.T) {
	tm := begin(t, forked(t))
	if _, _, err := tm.RescueGroup("split", ""); !errors.Is(err, ErrWhichGroup) || Code(err) != "which_group" {
		t.Fatalf("no group named: %v", err)
	}
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	// A group's own step needs no name.
	moved(t, tm, "a1")(tm.RescueGroup("a1", ""))
}

// A view lends the phone its group's roles on a copy: the caller's own
// Viewer keeps the roles it came with.
func TestFocusLeavesTheCallersViewer(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 2)
	tm.Deal(tm.Players, tm.RoleHolders)
	v := &Viewer{Slot: 1, Roles: []string{"scout"}}
	tm.Viewer = v
	if got := tm.View(); got.Group == "" {
		t.Fatalf("no group in the view: %+v", got)
	}
	tm.focusView()
	if lent := tm.Viewer.Roles; len(lent) <= 1 {
		t.Fatalf("the group lends the phone no roles: %v", lent)
	}
	tm.unfocus()
	if tm.Viewer != v || !slices.Equal(v.Roles, []string{"scout"}) {
		t.Fatalf("the caller's viewer changed: %+v", v)
	}
}

// A group's walk read from its phones (Walks) is read again each time,
// so it is not written into the group's stored cursor.
func TestReadWalkIsNotStored(t *testing.T) {
	tm := startedSplit(t)
	tm.Walks = map[string]float64{"a": 500}
	tm.focusOn(0)
	if tm.Walked() != 500 {
		t.Fatalf("walked %v, want the read 500", tm.Walked())
	}
	tm.unfocus()
	if tm.Strands[0].WalkedMetres != 0 {
		t.Fatalf("the read walk stored: %v", tm.Strands[0].WalkedMetres)
	}
}

// Past the join the team walks on together: a gate there counts every
// phone, not only those of the group that arrived last. Regression: a
// teammate waiting in the church was not counted, and the team fell to
// the failure ending.
func TestPastTheJoinTheTeamIsWhole(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "church?", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpInZone, Zone: "church"}}})
	for i, e := range g.Edges {
		if e.From == "join" {
			g.Edges[i].To = "church?"
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: "church?", FromPort: graph.PortPass, To: "door"}, graph.Edge{From: "church?", FromPort: graph.PortFail, To: "dead"})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	players(tm, 2)
	tm.Deal(tm.Players, tm.RoleHolders) // a: 1 (scout), b: 2 (medic)
	stand(tm, map[int64]*Position{1: atChurch, 2: atWell})
	moved(t, tm, "1 sets off")(asPlayer(tm, 1, atChurch).Continue("split"))
	moved(t, tm, "1 answers")(asPlayer(tm, 1, atChurch).Answer("pa", "bell"))
	moved(t, tm, "2 sets off")(asPlayer(tm, 2, atWell).Continue("split"))
	moved(t, tm, "2 answers last")(asPlayer(tm, 2, atWell).Answer("pb", "water"))
	if tm.Split != "" || tm.Current != "door" || tm.Completed {
		t.Fatalf("the team should be at the door, together: split %q at %q, completed %v", tm.Split, tm.Current, tm.Completed)
	}
}

// Moving one group on never ends the adventure for every group: a
// rescue takes a route toward the group's join, and a step with none is
// not moved on.
func TestRescueAGroupNeverEndsItForAll(t *testing.T) {
	g := expedition()
	for i := range g.Nodes {
		if n := &g.Nodes[i]; n.Slug == "pa" {
			// Its first answer route falls to the failure ending.
			outs := n.PuzzleCfg.Outputs
			n.PuzzleCfg.Outputs = []graph.Output{outs[1], outs[0]}
		}
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	moved(t, tm, "rescue a")(tm.RescueGroup("pa", "a"))
	if tm.Completed || tm.Split == "" {
		t.Fatalf("one group's rescue ended the adventure: %q", tm.Ending)
	}
	if got := keys(tm); !slices.Equal(got, []string{"a@join", "b@split"}) {
		t.Fatalf("groups %v", got)
	}

	// A story on a path that meets the others, but whose own way on is an
	// ending, is not moved on.
	g = expedition()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "cliff", Type: graph.Story, Title: "The cliff"})
	for i := range g.Nodes {
		if n := &g.Nodes[i]; n.Slug == "pb" {
			n.PuzzleCfg.Outputs = append(n.PuzzleCfg.Outputs, graph.Output{Port: "climb", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "climb"}}})
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: "pb", FromPort: "climb", To: "cliff"}, graph.Edge{From: "cliff", FromPort: graph.PortOut, To: "dead"})
	tm, _, err = Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	stand(tm, nil)
	moved(t, tm, "b")(tm.RescueGroup("split", "b"))
	tm.Position = atWell
	moved(t, tm, "b climbs")(tm.Answer("pb", "climb"))
	if _, _, err := tm.RescueGroup("cliff", "b"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("rescuing b off the cliff: %v, want ErrNotAllowed", err)
	}
}

// Presses are kept only where someone stands: a group moving on drops
// those of the step it left, not the other group's; the time limit,
// ending the split, drops every group's.
func TestPressesOnlyWhereSomeoneStands(t *testing.T) {
	g := expedition()
	g.TimeLimit = &graph.TimeLimit{Seconds: 600, To: "door"}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	moved(t, tm, "b")(tm.RescueGroup("split", "b"))
	press := map[int64]Press{1: {Value: "x", At: t0}}
	tm.Presses = map[string]map[int64]Press{"pa": press, "pb": press}
	moved(t, tm, "a moved on")(tm.RescueGroup("pa", "a"))
	if _, ok := tm.Presses["pa"]; ok || tm.Presses["pb"] == nil {
		t.Fatalf("after a moved on: %v", tm.Presses)
	}
	tm.Presses["pb"] = press
	tm.Now = t0.Add(11 * time.Minute)
	moved(t, tm, "time up")(tm.TimeUp())
	if len(tm.Presses) != 0 {
		t.Fatalf("after time up: %v", tm.Presses)
	}
}

// A group's hub that has done enough, but whose way out ends the
// adventure, is moved on by a branch toward the join instead.
// Regression: the rescue was refused, though such a branch was there.
func TestRescueAGroupsHubPastADeadEndDone(t *testing.T) {
	b := newGB().split("split", "a", "b").
		add(graph.Node{Slug: "h", Type: graph.Hub, Title: "h", HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Required: 1, Branches: []graph.Branch{{Port: "x"}, {Port: "y"}}}}).
		puzzle("hx", "x").puzzle("hy", "y").puzzle("pb", "b").join("join").end("end").end("dead").
		wire("start", "out", "split").wire("split", "a", "h").wire("split", "b", "pb").
		wire("h", "x", "hx").wire("hx", "solved", "h").wire("h", "y", "hy").wire("hy", "solved", "join").
		wire("h", graph.PortDone, "dead").wire("pb", "solved", "join").wire("join", "out", "end")
	b.valid(t)
	tm := begin(t, b.g)
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	moved(t, tm, "x")(tm.Choose("h", "x"))
	moved(t, tm, "hx")(tm.Answer("hx", "x"))
	moved(t, tm, "rescue a")(tm.RescueGroup("h", "a"))
	if got := keys(tm); tm.Completed || !slices.Equal(got, []string{"a@hy", "b@split"}) {
		t.Fatalf("groups %v, completed %v", got, tm.Completed)
	}
}

// A player joining while one group waits at the join goes to a group
// still on its way, not to wait idle with the one there.
func TestLateJoinerGoesToAGroupStillOut(t *testing.T) {
	tm := startedSplit(t)
	tm.unfocus()
	tm.Players, tm.Size = []int64{1, 2}, 2
	tm.Deal(tm.Players, nil) // a: 1, b: 2
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	moved(t, tm, "a at the join")(tm.RescueGroup("pa", "a"))
	tm.unfocus()
	tm.Players = []int64{1, 2, 3}
	tm.Deal(tm.Players, nil)
	if !slices.Contains(tm.Strands[1].Members, 3) {
		t.Fatalf("the newcomer should join b, still out: %+v", tm.Strands)
	}
}

// A group is where its own phones are, not where another group's phone
// last was.
func TestGroupPositionIsItsOwn(t *testing.T) {
	tm := startedSplit(t)
	players(tm, 2)
	tm.Deal(tm.Players, tm.RoleHolders) // a: 1, b: 2
	stand(tm, map[int64]*Position{1: atChurch, 2: atWell})
	tm.Position = atWell // the team's latest fix: b's phone
	tm.focusOn(0)
	if tm.Position == nil || *tm.Position != *atChurch {
		t.Fatalf("group a at %+v, want the church", tm.Position)
	}
	tm.unfocus()
	if tm.Position != atWell {
		t.Fatalf("the team's own fix not put back: %+v", tm.Position)
	}
}

// A group whose path meets the others at no join — it ends the adventure
// instead, as valid warns — is moved on toward that end all the same: it
// has no other way.
func TestRescueAGroupOnAPathWithNoJoin(t *testing.T) {
	g := expedition()
	for i, e := range g.Edges {
		if e.From == "pb" {
			g.Edges[i].To = "dead"
		}
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "b")(tm.RescueGroup("split", "b"))
	moved(t, tm, "rescue b")(tm.RescueGroup("pb", "b"))
	if !tm.Completed {
		t.Fatalf("b's way out is the end, every group's: groups %v", keys(tm))
	}
}

// Standing on nothing is an empty list, not nil: the engine clears
// presses where no one stands with it, and NULL would clear none.
func TestStandingOnNothing(t *testing.T) {
	tm := New(expedition(), t0)
	if got := tm.Standing(); got == nil || len(got) != 0 {
		t.Fatalf("standing %#v, want an empty list", got)
	}
}

// A newcomer holding a path's role, while that path's group waits at the
// join and another is out, joins the one out: there is nothing left to
// do in the group waiting.
func TestLateRoleHolderGoesToAGroupStillOut(t *testing.T) {
	tm := startedSplit(t)
	tm.unfocus()
	tm.Players, tm.Size = []int64{1, 2}, 2
	tm.Deal(tm.Players, map[string]int64{"scout": 1, "medic": 2}) // a: 1, b: 2
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	moved(t, tm, "a at the join")(tm.RescueGroup("pa", "a"))
	tm.unfocus()
	tm.Players = []int64{1, 2, 3}
	tm.Deal(tm.Players, map[string]int64{"scout": 3, "medic": 2})
	if !slices.Contains(tm.Strands[1].Members, 3) || slices.Contains(tm.Strands[0].Members, 3) {
		t.Fatalf("the scout come late should join b, still out: %+v", tm.Strands)
	}
}

// Rescuable says of each group what RescueGroup does: a group back at a
// hub from its one branch coming back, the others leading only to ends
// and out not yet open, cannot be moved on — though by the graph alone
// it might.
func TestRescuableIsWhatRescueDoes(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "bonus", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}})
	g.Nodes = append(g.Nodes, graph.Node{Slug: "h", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Required: 2, Branches: []graph.Branch{{Port: "b1"}, {Port: "b2"}, {Port: "b3"}}}})
	for i, e := range g.Edges {
		switch {
		case e.From == "split" && e.FromPort == "b":
			g.Edges[i].To = "h"
		case e.From == "pb":
			g.Edges[i].To = "h"
		}
	}
	g.Edges = append(g.Edges,
		graph.Edge{From: "h", FromPort: "b1", To: "bonus"},
		graph.Edge{From: "bonus", FromPort: graph.PortOut, To: "pb"},
		graph.Edge{From: "h", FromPort: "b2", To: "dead"},
		graph.Edge{From: "h", FromPort: "b3", To: "dead"},
		graph.Edge{From: "h", FromPort: graph.PortDone, To: "join"})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	agrees := func(want []bool) {
		t.Helper()
		if got := tm.Rescuable(); !slices.Equal(got, want) {
			t.Fatalf("rescuable %v at %v, want %v", got, keys(tm), want)
		}
	}
	agrees([]bool{true, true})
	moved(t, tm, "b off")(tm.RescueGroup("split", "b"))
	agrees([]bool{true, true})
	// Asking walks nothing: b1's key is not the team's for it.
	if tm.Items["key"] != 0 {
		t.Fatalf("asking granted the key: %v", tm.Items)
	}
	moved(t, tm, "into b1")(tm.RescueGroup("h", "b"))
	moved(t, tm, "past pb")(tm.RescueGroup("pb", "b"))
	agrees([]bool{true, false})
	if _, _, err := tm.RescueGroup("h", "b"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("b at its hub with nowhere toward the join: %v", err)
	}
	tm.unfocus()
	agrees([]bool{true, false})
}

// Groups that no longer fit are split afresh on the team's split as of
// when the team came to it, not as of now: read again and again before
// it is stored, the team has the same groups, their clocks running on.
func TestRefitKeepsTheTeamsArrival(t *testing.T) {
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Split != "split" {
		t.Fatalf("not at the split: %q", tm.Split)
	}
	arrived := tm.NodeEnteredAt
	tm.Strands = tm.Strands[:1]
	tm.Now = t0.Add(time.Hour)
	if !tm.Refit() || len(tm.Strands) != 2 {
		t.Fatalf("not split afresh: %v", keys(tm))
	}
	for _, st := range tm.Strands {
		if !st.NodeEnteredAt.Equal(arrived) {
			t.Fatalf("group %s on the split since %v, want the team's %v", st.Port, st.NodeEnteredAt, arrived)
		}
	}
	if !Refits(tm.Splits(), "split", "", nil) || Refits(tm.Splits(), "pa", "", nil) || Refits(tm.Splits(), "split", "split", tm.Strands) {
		t.Error("Refits: a team together on a split is split, not one on a puzzle, nor groups that fit")
	}
}

// A group whose way on, by the graph, reaches its join, but whose walk
// there goes round for good, cannot be moved on: Rescuable walks the
// way, as Rescue would, and keeps the team as it is.
func TestRescuableWalksTheWay(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes,
		graph.Node{Slug: "e1", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}},
		graph.Node{Slug: "round", Type: graph.Gate, GateCfg: &graph.GateConfig{}})
	for i, e := range g.Edges {
		if e.From == "split" && e.FromPort == "b" {
			g.Edges[i].To = "lost"
		}
	}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "lost", Type: graph.Story})
	g.Edges = append(g.Edges,
		graph.Edge{From: "lost", FromPort: graph.PortOut, To: "e1"},
		graph.Edge{From: "e1", FromPort: graph.PortOut, To: "round"},
		graph.Edge{From: "round", FromPort: graph.PortPass, To: "e1"},
		graph.Edge{From: "round", FromPort: graph.PortFail, To: "join"})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "b off")(tm.RescueGroup("split", "b"))
	if got := tm.Rescuable(); !slices.Equal(got, []bool{true, false}) || tm.Items["key"] != 0 {
		t.Fatalf("rescuable %v at %v, items %v", got, keys(tm), tm.Items)
	}
	if _, _, err := tm.RescueGroup("lost", "b"); err == nil {
		t.Fatal("the rescue walked round for good")
	}
}

// Rescuable asks the rescue itself, on a copy of the team: a way on that
// opens only by what it passes — a key granted, then a gate asking for
// it — is one, as Rescue finds; and with a time limit due, which Act runs
// out instead, no group's: the team is the limit's to move (TimeUpDue).
func TestRescuableIsTheRescueTried(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes,
		graph.Node{Slug: "grant", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}},
		graph.Node{Slug: "door2", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpItem, Item: "key"}}},
		graph.Node{Slug: "lost", Type: graph.Story})
	for i, e := range g.Edges {
		if e.From == "split" && e.FromPort == "b" {
			g.Edges[i].To = "lost"
		}
	}
	// The gate's fail way is not drawn: only the key gets past it.
	g.Edges = append(g.Edges,
		graph.Edge{From: "lost", FromPort: graph.PortOut, To: "grant"},
		graph.Edge{From: "grant", FromPort: graph.PortOut, To: "door2"},
		graph.Edge{From: "door2", FromPort: graph.PortPass, To: "join"})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "b off")(tm.RescueGroup("split", "b"))
	if got := tm.Rescuable(); !slices.Equal(got, []bool{true, true}) || tm.Items["key"] != 0 {
		t.Fatalf("rescuable %v at %v, items %v", got, keys(tm), tm.Items)
	}
	g.TimeLimit = &graph.TimeLimit{Seconds: 60, To: "end"}
	tm.Now = t0.Add(time.Hour)
	if got := tm.Rescuable(); !slices.Equal(got, []bool{false, false}) || !tm.TimeUpDue() {
		t.Fatalf("rescuable %v with the time limit due", got)
	}
	// Asked for all the same, the limit runs out instead: no group moved,
	// so the host logs no group's rescue (play.act, admin's unlock).
	res, step, err := tm.Act(func() (Result, *walk.Step, error) { return tm.RescueGroup("", "b") })
	if err != nil || step == nil || res.MovedKey != "" || step.To != "end" {
		t.Fatalf("rescue with the limit due: %+v %+v %v", res, step, err)
	}
}

// A rescue on a split names its group only where more than one is left
// on it: with one, it is that one's.
func TestRescueTheOneGroupLeftOnTheSplit(t *testing.T) {
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tm.RescueGroup("split", ""); !errors.Is(err, ErrWhichGroup) {
		t.Fatalf("both groups on the split: %v", err)
	}
	if _, _, err := tm.RescueGroup("", ""); !errors.Is(err, ErrWhichGroup) {
		t.Fatalf("naming neither step nor group of a split team: %v", err)
	}
	moved(t, tm, "a off")(tm.RescueGroup("split", "a"))
	res := moved(t, tm, "the one left")(tm.Act(func() (Result, *walk.Step, error) { return tm.RescueGroup("split", "") }))
	if got := keys(tm); got[1] == "b@split" || res.From != "split" || res.MovedKey == "" {
		t.Fatalf("groups %v, result %+v", got, res)
	}
	// None left on it: still which, the team's own step the split.
	if _, _, err := tm.RescueGroup("split", ""); !errors.Is(err, ErrWhichGroup) {
		t.Fatalf("no group left on the split: %v", err)
	}
}

// A walk in the simulator is the group's whose phone walked it; with no
// phone to say, the group's the team's own view is for.
func TestAddWalkIsTheWalkersGroups(t *testing.T) {
	tm := startedSplit(t)
	tm.unfocus()
	tm.Players, tm.Size = []int64{1, 2}, 2
	tm.Strands[0].Members, tm.Strands[1].Members = []int64{1}, []int64{2}
	tm.AddWalk(1, 500)
	if tm.Strands[0].WalkedMetres != 500 || tm.Strands[1].WalkedMetres != 0 || tm.WalkedMetres != 0 {
		t.Fatalf("walked %v %v, team %v", tm.Strands[0].WalkedMetres, tm.Strands[1].WalkedMetres, tm.WalkedMetres)
	}
	tm.AddWalk(0, 100)
	if tm.WalkedMetres != 0 || tm.Strands[0].WalkedMetres != 600 || tm.Strands[1].WalkedMetres != 0 {
		t.Fatalf("a walk no phone said whose: team %v, groups %v %v", tm.WalkedMetres, tm.Strands[0].WalkedMetres, tm.Strands[1].WalkedMetres)
	}
	// The team walking the well's group (Walk): the walk is the well's.
	tm.Strands[1].Chosen = []int64{0}
	tm.AddWalk(0, 50)
	if tm.Strands[1].WalkedMetres != 50 {
		t.Fatalf("the well's group chosen, walked %v", tm.Strands[1].WalkedMetres)
	}
}

// The last group moved on through its join walks on as the team: the
// result names no group moved, though it says the step it left.
func TestLastGroupThroughTheJoinIsTheTeam(t *testing.T) {
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "a off")(tm.RescueGroup("split", "a"))
	moved(t, tm, "b off")(tm.RescueGroup("split", "b"))
	res := moved(t, tm, "b to the join")(tm.Act(func() (Result, *walk.Step, error) { return tm.RescueGroup("pb", "b") }))
	if res.MovedKey == "" || res.To != "join" {
		t.Fatalf("b held at the join: %+v", res)
	}
	res = moved(t, tm, "a through")(tm.Act(func() (Result, *walk.Step, error) { return tm.RescueGroup("pa", "a") }))
	if res.MovedKey != "" || res.From != "pa" || tm.Split != "" {
		t.Fatalf("a through the join: %+v, split %q at %q", res, tm.Split, tm.Current)
	}
}

// A draft's split on a path — valid refuses it — holds the group that
// reaches it, the others' progress kept: the groups still fit.
func TestGroupOnASplitOnItsPathStillFits(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "inner", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "x"}}}})
	for i, e := range g.Edges {
		if e.From == "split" && e.FromPort == "b" {
			g.Edges[i].To = "inner"
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: "inner", FromPort: "x", To: "pb"})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	moved(t, tm, "b")(tm.RescueGroup("split", "b"))
	if got := keys(tm); !slices.Equal(got, []string{"a@pa", "b@inner"}) {
		t.Fatalf("groups %v", got)
	}
	if tm.Refit() || !slices.Equal(keys(tm), []string{"a@pa", "b@inner"}) {
		t.Fatalf("refit, groups %v", keys(tm))
	}
}

// Where the organisers' rescue put the team, or a group, is marked for
// its phones, until it moves on; a team moved into a split, each group.
func TestMarkMovedUntilItMovesOn(t *testing.T) {
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.MarkMoved("")
	if v := tm.View(); !v.MovedOn || !v.Groups[0].Moved || !v.Groups[1].Moved || tm.Moved {
		t.Fatalf("a team moved into its split — its groups, not its own cursor on the split: %+v", v)
	}
	moved(t, tm, "a off")(tm.RescueGroup("split", "a"))
	tm.MarkMoved("a")
	if tm.unfocus(); !tm.Strands[0].Moved || !tm.Strands[1].Moved {
		t.Fatalf("a moved, b still where it was put: %+v", tm.Strands)
	}
	tm.Position = atChurch
	moved(t, tm, "a's answer")(tm.Answer("pa", "bell"))
	if tm.unfocus(); tm.Strands[0].Moved || !tm.Strands[1].Moved {
		t.Fatalf("a moved on by itself, b still put: %+v", tm.Strands)
	}
}

// The organisers' rescue marks where it takes the team as it is applied,
// whoever hosts the team; a player's own move marks nothing.
func TestRescueMarksItsMove(t *testing.T) {
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	res := moved(t, tm, "b off")(tm.Act(func() (Result, *walk.Step, error) { return tm.RescueGroup("split", "b") }))
	if tm.unfocus(); !tm.Strands[1].Moved || tm.Strands[0].Moved || res.MovedKey != "b" || res.Moved == "" {
		t.Fatalf("b rescued off the split: %+v, result %+v", tm.Strands, res)
	}
	moved(t, tm, "a off")(tm.Act(func() (Result, *walk.Step, error) { return tm.Continue("split") }))
	if tm.unfocus(); tm.Strands[0].Moved || !tm.Strands[1].Moved {
		t.Fatalf("a set off by its own: %+v", tm.Strands)
	}
}

// A rescue naming a split the team is not on is stale, not a question of
// which group; one naming nothing for a team at its start screen has no
// step to move past.
func TestRescueAskedOfNoStep(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "other", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "x"}}}})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tm.RescueGroup("other", ""); !errors.Is(err, ErrStale) {
		t.Fatalf("another split: %v", err)
	}
	fresh := New(expedition(), t0)
	if _, _, err := fresh.RescueGroup("", ""); !errors.Is(err, ErrWrongNode) {
		t.Fatalf("at the start screen: %v", err)
	}
}

// A group's walk read from its phones was on the step it left: on the one
// it comes to, it has walked nothing yet, whatever the view says next.
func TestGroupsWalkStaysWithTheStepLeft(t *testing.T) {
	tm, _, err := Start(expedition(), t0)
	if err != nil {
		t.Fatal(err)
	}
	moved(t, tm, "a off")(tm.RescueGroup("split", "a"))
	moved(t, tm, "b off")(tm.RescueGroup("split", "b"))
	tm.Walks = map[string]float64{"a": 800, "b": 300}
	tm.Position = atChurch
	moved(t, tm, "a's answer")(tm.Answer("pa", "bell"))
	tm.focusOn(0)
	if tm.WalkedMetres != 0 {
		t.Fatalf("a on %s walked %v, want none yet", tm.Current, tm.WalkedMetres)
	}
	tm.focusOn(1)
	if tm.WalkedMetres != 300 {
		t.Fatalf("b, unmoved, walked %v, want its 300", tm.WalkedMetres)
	}
	tm.unfocus()
}

// The designer's preview placed on a step of a split's path shows it as
// the path's group plays it: split up, the group on the step, waiting at
// the join for the others.
func TestPlaceOnAPathStepIsTheGroups(t *testing.T) {
	tm, err := Place(expedition(), "pb", t0)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(tm); tm.Split != "split" || !slices.Equal(got, []string{"a@split", "b@pb"}) {
		t.Fatalf("placed on pb: split %q, groups %v", tm.Split, got)
	}
	if v := tm.View(); v.Node != "pb" || !tm.Visited("split") {
		t.Fatalf("the preview shows %q, split visited %v: want pb, come by the split", v.Node, tm.Visited("split"))
	}
	tm.Position = atWell
	moved(t, tm, "b's answer")(tm.Answer("pb", "water"))
	if got := keys(tm); !slices.Equal(got, []string{"a@split", "b@join"}) {
		t.Fatalf("b's answer: groups %v, want b waiting at the join", got)
	}
}

// A phone's own move into a split is its own on its group too: the groups
// come to the split by the move the team came by.
func TestOwnMoveIntoASplit(t *testing.T) {
	g := expedition()
	g.Nodes = append(g.Nodes, graph.Node{Slug: "intro", Type: graph.Story})
	for i, e := range g.Edges {
		if e.From == "start" {
			g.Edges[i].To = "intro"
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: "intro", FromPort: graph.PortOut, To: "split"})
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Viewer = &Viewer{Slot: 1}
	tm.Now = t0.Add(1500 * time.Millisecond)
	moved(t, tm, "into the split")(tm.Continue("intro"))
	if v := tm.View(); v.Node != "split" || v.MovedByOther || v.SplitAt == nil || v.SplitAt.Nanosecond() != 0 {
		t.Fatalf("slot 1's own move into the split: %+v — split at the second it came, as stored", v)
	}
	tm.Viewer = &Viewer{Slot: 2}
	if v := tm.View(); !v.MovedByOther {
		t.Fatalf("slot 2 not told another phone moved the team: %+v", v)
	}
}

// A timer run out moves the team by no phone's choice: none is credited
// with it, though one phone's request ran it.
func TestTimerMovesByNoOne(t *testing.T) {
	g := expedition()
	g.TimeLimit = &graph.TimeLimit{Seconds: 60, To: "end"}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Viewer = &Viewer{Slot: 1}
	tm.Now = t0.Add(time.Hour)
	moved(t, tm, "time up")(tm.Act(tm.TimeUp))
	if tm.MovedBy != 0 || tm.View().MovedByOther {
		t.Fatalf("the time limit credited to phone %d", tm.MovedBy)
	}
}
