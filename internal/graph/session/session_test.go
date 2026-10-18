package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/walk"
)

var t0 = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

// hunt is start → door (left/right) → end, with a story on the right
// path, a hint ladder and a skip on the door.
func hunt() *graph.Graph {
	return &graph.Graph{
		Zones: []graph.Zone{{Slug: "park", Lat: 59.33, Lng: 18.07, RadiusM: 50}},
		Items: []graph.Item{{Slug: "key", Name: "Brass key"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "door", Type: graph.Puzzle, Title: "Which door?", PuzzleCfg: &graph.PuzzleConfig{
				Blocks: graph.LegacyBlocks("Left or right?", nil),
				Outputs: []graph.Output{
					{Port: "left", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "left"}}},
					{Port: "right", Trigger: graph.OnAnswer, Points: 5, Matchers: []match.Matcher{{Kind: match.Exact, Value: "right"}}},
					{Port: "skip", Trigger: graph.OnSkip},
				},
				Responses:        []graph.Response{{Text: "No."}, {AfterAttempts: 2, Text: "Think about your hands."}},
				Hints:            []graph.Hint{{Text: "Not up.", Cost: 3}, {Text: "Not right.", DelaySeconds: 60, Cost: 5}},
				SkipAfterGuesses: 2,
				SkipAfterSeconds: 120,
			}},
			{Slug: "gift", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}},
			{Slug: "walk", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Walk to the park.", nil), Zone: "park"}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "door"},
			{From: "door", FromPort: "left", To: "gift"},
			{From: "gift", FromPort: "out", To: "end"},
			{From: "door", FromPort: "right", To: "walk"},
			{From: "door", FromPort: "skip", To: "end"},
			{From: "walk", FromPort: "out", To: "end"},
		},
	}
}

func started(t *testing.T) *Team {
	t.Helper()
	tm, _, err := Start(hunt(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Current != "door" {
		t.Fatalf("start should land on the first puzzle, got %q", tm.Current)
	}
	return tm
}

func TestAnswerRoutesByOutcome(t *testing.T) {
	tm := started(t)
	res, step, err := tm.Answer("door", " LEFT ")
	if err != nil || step == nil || !res.Accepted || res.Port != "left" || res.Points != 10 {
		t.Fatalf("res=%+v step=%v err=%v", res, step, err)
	}
	tm.Apply(*step)
	v := tm.View()
	if !v.Finished || v.Outcome != graph.Complete || v.Score != 10 {
		t.Errorf("view after left = %+v", v)
	}
	if len(v.Items) != 1 || v.Items[0].Name != "Brass key" {
		t.Errorf("the effect on the way should have granted the key: %+v", v.Items)
	}
	if _, _, err := tm.Answer("door", "left"); !errors.Is(err, ErrFinished) {
		t.Errorf("answering after finishing = %v, want ErrFinished", err)
	}
}

func TestWrongAnswerLadderAndStale(t *testing.T) {
	tm := started(t)
	res, step, err := tm.Answer("door", "up")
	if err != nil || step != nil || res.Response != "No." {
		t.Fatalf("first wrong: %+v %v %v", res, step, err)
	}
	res, _, _ = tm.Answer("door", "down")
	if res.Response != "Think about your hands." {
		t.Errorf("second wrong should climb the ladder, got %q", res.Response)
	}
	if tm.View().Attempts != 2 {
		t.Errorf("attempts = %d", tm.View().Attempts)
	}
	if _, _, err := tm.Answer("walk", "x"); !errors.Is(err, ErrStale) {
		t.Errorf("acting on a node the team is not at = %v, want ErrStale", err)
	}
}

// A quiet step moves the team on at any answer: a right one by its own
// route for its points, a wrong one by the first answer route for none,
// with no reply, lockout or tries-based route. Which it was is kept: the
// step solved and the answer recorded only when right, the wrong guess
// counted.
func TestQuietAnswerMovesOn(t *testing.T) {
	quiet := func(t *testing.T) *Team {
		t.Helper()
		g := hunt()
		cfg := g.Nodes[1].PuzzleCfg
		cfg.Quiet, cfg.LockoutSeconds = true, 30
		cfg.Outputs = append(cfg.Outputs, graph.Output{Port: "pity", Trigger: graph.OnAttempts, AfterAttempts: 1})
		g.Edges = append(g.Edges, graph.Edge{From: "door", FromPort: "pity", To: "end"})
		tm, _, err := Start(g, t0)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	cases := []struct {
		guess, port, next string
		points            float64
		right             bool
	}{
		{"left", "left", "end", 10, true},
		{"right", "right", "walk", 5, true},
		{"up", "left", "end", 0, false},
	}
	for _, c := range cases {
		t.Run(c.guess, func(t *testing.T) {
			tm := quiet(t)
			res, step, err := tm.Answer("door", c.guess)
			if err != nil || step == nil {
				t.Fatalf("res=%+v step=%v err=%v", res, step, err)
			}
			if !res.Quiet || res.Accepted != c.right || res.Port != c.port || res.Points != c.points || res.Response != "" || res.BlockedUntil != nil {
				t.Errorf("result = %+v", res)
			}
			tm.Apply(*step)
			if tm.Current != c.next && !(c.next == "end" && tm.Completed) {
				t.Errorf("moved to %q, want %q", tm.Current, c.next)
			}
			if tm.Score() != c.points {
				t.Errorf("score = %v, want %v", tm.Score(), c.points)
			}
			r, wrong := tm.Nodes["door"], 1
			if c.right {
				wrong = 0
			}
			if _, answered := tm.AnswerTo("door"); r.Solved != c.right || answered != c.right || r.Attempts != wrong || r.Outcome != c.port {
				t.Errorf("record = %+v", r)
			}
		})
	}
}

// A reply may lock the team out, as legacy's answer responses could: the
// longer of its lockout and the puzzle's applies to the guess it answers.
func TestReplyLockout(t *testing.T) {
	g := hunt()
	cfg := g.Nodes[1].PuzzleCfg
	cfg.LockoutSeconds = 10
	cfg.Responses[1].LockoutSeconds = 300
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	res, _, _ := tm.Answer("door", "up")
	if res.BlockedUntil == nil || !res.BlockedUntil.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("first wrong should lock for the puzzle's 10 s: %+v", res)
	}
	tm.Now = t0.Add(10 * time.Second)
	res, _, _ = tm.Answer("door", "down")
	if res.Response != "Think about your hands." || res.BlockedUntil == nil || !res.BlockedUntil.Equal(tm.Now.Add(300*time.Second)) {
		t.Fatalf("second wrong should lock for its reply's 300 s: %+v", res)
	}
	tm.Now = tm.Now.Add(299 * time.Second)
	if _, _, err := tm.Answer("door", "left"); !errors.Is(err, ErrLocked) {
		t.Errorf("answering during the reply's lockout = %v, want ErrLocked", err)
	}
}

func TestHintsRevealInOrderAfterDelay(t *testing.T) {
	tm := started(t)
	v := tm.View()
	if v.HintCount != 2 || v.NextHint == nil || v.NextHint.Index != 0 || v.NextHint.Cost != 3 {
		t.Fatalf("next hint = %+v", v.NextHint)
	}
	if _, _, err := tm.RevealHint("door", 1); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("skipping ahead to hint 1 = %v", err)
	}
	res, step, err := tm.RevealHint("door", 0)
	if err != nil || step != nil || res.Hint != "Not up." {
		t.Fatalf("hint 0: %+v %v", res, err)
	}
	if _, _, err := tm.RevealHint("door", 1); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("hint 1 before its delay = %v", err)
	}
	v = tm.View()
	if !v.NextHint.AvailableAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("next hint opens at %v", v.NextHint.AvailableAt)
	}
	tm.Now = t0.Add(61 * time.Second)
	if _, _, err := tm.RevealHint("door", 1); err != nil {
		t.Fatal(err)
	}
	v = tm.View()
	if v.PenaltyScore != 8 || len(v.Hints) != 2 || v.NextHint != nil {
		t.Errorf("after both hints: penalty=%v hints=%v next=%v", v.PenaltyScore, v.Hints, v.NextHint)
	}
}

func TestSkipNeedsBothThresholds(t *testing.T) {
	tm := started(t)
	tm.Answer("door", "a")
	tm.Answer("door", "b")
	if tm.View().CanSkip {
		t.Fatal("two guesses alone must not unlock a skip")
	}
	tm.Now = t0.Add(2 * time.Minute)
	if !tm.View().CanSkip {
		t.Fatal("guesses and time together should unlock it")
	}
	res, step, err := tm.Skip("door")
	if err != nil || res.Port != "skip" || step == nil || step.To != "end" || !step.Finished {
		t.Fatalf("skip: %+v %+v %v", res, step, err)
	}
}

func TestCheatTakesFirstAnswerRouteForNoPoints(t *testing.T) {
	tm := started(t)
	res, step, err := tm.Cheat("door")
	if err != nil || !res.Accepted || res.Port != "left" || step == nil {
		t.Fatalf("cheat: %+v %+v %v", res, step, err)
	}
	if !tm.Cheated || !tm.Row("door").Solved || tm.Points != 0 {
		t.Errorf("after cheat: cheated=%v solved=%v points=%v", tm.Cheated, tm.Row("door").Solved, tm.Points)
	}
}

func TestCheatWorksAwayFromThePlace(t *testing.T) {
	g := hunt()
	g.Nodes[1].PuzzleCfg.Zone = "park"
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tm.Answer("door", "left"); !errors.Is(err, ErrNotHere) {
		t.Fatalf("an answer away from the park = %v", err)
	}
	if res, _, err := tm.Cheat("door"); err != nil || res.Port != "left" {
		t.Errorf("cheat away from the park: %+v %v", res, err)
	}
}

func TestCheatContinuesAStoryAwayFromThePlace(t *testing.T) {
	tm := started(t)
	_, step, _ := tm.Answer("door", "right")
	tm.Apply(*step)
	if _, _, err := tm.Continue("walk"); !errors.Is(err, ErrNotHere) {
		t.Fatalf("continue away from the park = %v", err)
	}
	if res, step, err := tm.Cheat("walk"); err != nil || res.Port != graph.PortOut || step == nil || !tm.Cheated {
		t.Errorf("cheat on the story: %+v %+v %v cheated=%v", res, step, err, tm.Cheated)
	}
}

func TestCheatHonoursLockout(t *testing.T) {
	tm := started(t)
	until := t0.Add(time.Minute)
	tm.Row("door").BlockedUntil = &until
	if _, _, err := tm.Cheat("door"); !errors.Is(err, ErrLocked) {
		t.Errorf("cheat while locked = %v", err)
	}
	if tm.Cheated {
		t.Error("a refused cheat must not mark the team")
	}
}

func TestZoneGatedStory(t *testing.T) {
	tm := started(t)
	_, step, _ := tm.Answer("door", "right")
	tm.Apply(*step)
	v := tm.View()
	if v.Type != graph.Story || v.Zone == nil || v.Here {
		t.Fatalf("story view = %+v", v)
	}
	if _, _, err := tm.Continue("walk"); !errors.Is(err, ErrNotHere) {
		t.Errorf("continue outside the zone = %v", err)
	}
	tm.Position = &Position{Lat: 59.33, Lng: 18.07}
	if !tm.View().Here {
		t.Error("standing on the zone centre should be Here")
	}
	if _, _, err := tm.Continue("walk"); err != nil {
		t.Error(err)
	}
}

// The simulator hands a Team to the browser and takes it back. Nothing
// the rules depend on may be lost on the way.
func TestTeamRoundTripsThroughJSON(t *testing.T) {
	tm := started(t)
	tm.Answer("door", "nope")
	tm.RevealHint("door", 0)

	b, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	var back Team
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	back.G = hunt()
	back.Init()

	a, z := tm.View(), back.View()
	ab, _ := json.Marshal(a)
	zb, _ := json.Marshal(z)
	if string(ab) != string(zb) {
		t.Errorf("view changed across a round trip:\n%s\n%s", ab, zb)
	}
	if _, _, err := back.Answer("door", "left"); err != nil {
		t.Errorf("a restored team should keep playing: %v", err)
	}
}

func TestInputShape(t *testing.T) {
	cases := []struct {
		name     string
		matchers []match.Matcher
		want     Input
	}{
		{"text", []match.Matcher{{Kind: match.Exact, Value: "x"}}, Input{Kind: "text"}},
		{"number", []match.Matcher{{Kind: match.Near, Target: 5, Tolerance: 1}}, Input{Kind: "number"}},
		{"number with catch-all", []match.Matcher{{Kind: match.Interval, Low: 1, High: 2}, {Kind: match.Anything}}, Input{Kind: "number"}},
		{"only a catch-all", []match.Matcher{{Kind: match.Anything}}, Input{Kind: "text"}},
		{"list", []match.Matcher{{Kind: match.Set, Members: []string{"a", "b", "c"}, Required: 2, Ordered: true}}, Input{Kind: "text", Parts: 2, Ordered: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := inputFor(&graph.PuzzleConfig{Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Matchers: c.matchers}}})
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("got %+v, want %+v", *got, c.want)
			}
		})
	}
}

func TestPlaceShowsOneStepWithoutHistory(t *testing.T) {
	tm, err := Place(hunt(), "walk", t0)
	if err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.Node != "walk" || v.Text != "Walk to the park." {
		t.Errorf("view = %+v", v)
	}
	if _, err := Place(hunt(), "nowhere", t0); err == nil {
		t.Error("placing on a missing node should fail")
	}
}

// A picture is often the puzzle itself, so media is visible only on a
// step the team has reached, and in a hint only once it is revealed.
func TestMediaVisibility(t *testing.T) {
	g := hunt()
	for i := range g.Nodes {
		switch g.Nodes[i].Slug {
		case "door":
			g.Nodes[i].PuzzleCfg.Blocks = append(g.Nodes[i].PuzzleCfg.Blocks, graph.Block{ID: "pic", Kind: graph.BlockImage, Media: 1})
			g.Nodes[i].PuzzleCfg.Hints[0].Media = []graph.Media{{ID: 2, Kind: graph.MediaAudio}}
		case "walk":
			g.Nodes[i].StoryCfg.Blocks = append(g.Nodes[i].StoryCfg.Blocks, graph.Block{ID: "pic", Kind: graph.BlockImage, Media: 3})
		}
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !tm.CanSeeMedia(1, true) || tm.CanSeeMedia(2, true) || tm.CanSeeMedia(3, true) || tm.CanSeeMedia(99, true) {
		t.Fatal("strict, on the door: only its own picture should be visible")
	}
	if !tm.CanSeeMedia(3, false) || tm.CanSeeMedia(2, false) {
		t.Fatal("not strict: every step's picture, but still no unrevealed hint's")
	}
	if got := tm.VisibleMedia(false); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("prefetch list = %v, want [1 3]", got)
	}
	if v := tm.View(); len(v.Media) != 1 || v.Media[0].ID != 1 {
		t.Errorf("view media = %+v", v.Media)
	}
	tm.RevealHint("door", 0)
	if !tm.CanSeeMedia(2, true) {
		t.Error("a revealed hint's sound should be visible")
	}
	if v := tm.View(); len(v.Hints) != 1 || len(v.Hints[0].Media) != 1 {
		t.Errorf("hint view = %+v", v.Hints)
	}
	_, step, _ := tm.Answer("door", "right")
	tm.Apply(*step)
	if !tm.CanSeeMedia(3, true) || !tm.CanSeeMedia(1, true) {
		t.Error("media on reached steps stays visible")
	}
}

// A hub's branch pictures come with the hub, as its tiles show them.
func TestHubBranchImageVisibility(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Required: 1, Branches: []graph.Branch{
				{Port: "a", Label: "A", Image: &graph.Media{ID: 5, Kind: graph.MediaImage}},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "hub"},
			{From: "hub", FromPort: "a", To: "hub"},
			{From: "hub", FromPort: graph.PortDone, To: "end"},
		},
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !tm.CanSeeMedia(5, true) {
		t.Error("a team at the hub should see its branch picture")
	}
}

// A role's picture is everyone's to see, and in the view; a secret
// role's only its player's.
func TestRoleImageVisibility(t *testing.T) {
	g := hunt()
	g.Roles = []graph.Role{
		{Slug: "scout", Image: &graph.Media{ID: 7, Kind: graph.MediaImage}},
		{Slug: "mole", Secret: true, Image: &graph.Media{ID: 8, Kind: graph.MediaImage}},
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Viewer = &Viewer{Slot: 1}
	if !tm.CanSeeMedia(7, true) || tm.CanSeeMedia(8, true) {
		t.Error("a player should see the public role's picture, not the secret one's")
	}
	if v := tm.View(); len(v.Roles) != 1 || v.Roles[0].Image == nil || v.Roles[0].Image.ID != 7 {
		t.Errorf("roles view = %+v", v.Roles)
	}
	tm.Viewer = &Viewer{Slot: 2, Roles: []string{"mole"}}
	if !tm.CanSeeMedia(8, true) {
		t.Error("the secret role's player should see its picture")
	}
}

// A puzzle accepts any answer and carries it down its output; a switch
// routes on what it was. Cases are tried in order, "otherwise" catches
// the rest, and a switch naming a puzzle reads that puzzle's answer
// even after later answers.
func TestSwitchRoutesOnTheAnswer(t *testing.T) {
	anything := []match.Matcher{{Kind: match.Anything}}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "colour", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Outputs: []graph.Output{{Port: "said", Trigger: graph.OnAnswer, Matchers: anything}},
			}},
			{Slug: "by-colour", Type: graph.Switch, SwitchCfg: &graph.SwitchConfig{Cases: []graph.Case{
				{Port: "red", Matchers: []match.Matcher{{Kind: match.Exact, Value: "red"}, {Kind: match.Contains, Value: "crimson"}}},
				{Port: "blue", Matchers: []match.Matcher{{Kind: match.Exact, Value: "blue"}}},
			}}},
			{Slug: "red-path", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Outputs: []graph.Output{{Port: "said", Trigger: graph.OnAnswer, Matchers: anything}},
			}},
			{Slug: "recall", Type: graph.Switch, SwitchCfg: &graph.SwitchConfig{From: "colour", Cases: []graph.Case{
				{Port: "red", Matchers: []match.Matcher{{Kind: match.Contains, Value: "crimson"}}},
			}}},
			{Slug: "blue-end", Type: graph.End},
			{Slug: "else-end", Type: graph.End},
			{Slug: "crimson-end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "colour"},
			{From: "colour", FromPort: "said", To: "by-colour"},
			{From: "by-colour", FromPort: "red", To: "red-path"},
			{From: "by-colour", FromPort: "blue", To: "blue-end"},
			{From: "by-colour", FromPort: "else", To: "else-end"},
			{From: "red-path", FromPort: "said", To: "recall"},
			{From: "recall", FromPort: "red", To: "crimson-end"},
			{From: "recall", FromPort: "else", To: "else-end"},
		},
	}
	play := func(answers ...string) string {
		tm, _, err := Start(g, t0)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range answers {
			_, step, err := tm.Answer(tm.Current, a)
			if err != nil || step == nil {
				t.Fatalf("answer %q: %v", a, err)
			}
			tm.Apply(*step)
		}
		return tm.Current
	}
	if got := play(" Blue "); got != "blue-end" {
		t.Errorf("blue went to %q", got)
	}
	if got := play("green"); got != "else-end" {
		t.Errorf("an unmatched answer should take otherwise, went to %q", got)
	}
	if got := play("red"); got != "red-path" {
		t.Errorf("red went to %q", got)
	}
	// The second switch reads the first puzzle's answer, not the latest.
	if got := play("dark crimson", "whatever"); got != "crimson-end" {
		t.Errorf("recall of the first answer went to %q", got)
	}
}

// A path that refers to a puzzle's answer follows the answer, not its
// spelling: correcting the text in the puzzle keeps the route working.
func TestCrossroadsFollowsAnswerReferences(t *testing.T) {
	build := func(northText string) *graph.Graph {
		return &graph.Graph{
			Nodes: []graph.Node{
				{Slug: "start", Type: graph.Start},
				{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Outputs: []graph.Output{{
					Port: "ok", Trigger: graph.OnAnswer, Matchers: []match.Matcher{
						{ID: "n", Kind: match.Exact, Value: northText},
						{ID: "s", Kind: match.Exact, Value: "south"},
					},
				}}}},
				{Slug: "x", Type: graph.Switch, SwitchCfg: &graph.SwitchConfig{Cases: []graph.Case{
					{Port: "north", Answers: []string{graph.AnswerRef("q", "n")}},
				}}},
				{Slug: "north-end", Type: graph.End},
				{Slug: "else-end", Type: graph.End},
			},
			Edges: []graph.Edge{
				{From: "start", FromPort: "out", To: "q"},
				{From: "q", FromPort: "ok", To: "x"},
				{From: "x", FromPort: "north", To: "north-end"},
				{From: "x", FromPort: "else", To: "else-end"},
			},
		}
	}
	for _, c := range []struct{ text, guess, want string }{
		{"nroth", "nroth", "north-end"},
		{"north", "North", "north-end"},
		{"north", "south", "else-end"},
	} {
		tm, _, _ := Start(build(c.text), t0)
		_, step, err := tm.Answer("q", c.guess)
		if err != nil {
			t.Fatal(err)
		}
		tm.Apply(*step)
		if tm.Current != c.want {
			t.Errorf("%q with the answer spelt %q went to %q, want %q", c.guess, c.text, tm.Current, c.want)
		}
	}
}

func TestPolygonZone(t *testing.T) {
	// A square around Sergels torg, drawn clockwise.
	z := graph.Zone{Slug: "sq", Lat: 59.3325, Lng: 18.0645, Polygon: [][2]float64{
		{59.3330, 18.0640}, {59.3330, 18.0650}, {59.3320, 18.0650}, {59.3320, 18.0640},
	}}
	if !ZoneContains(z, 59.3325, 18.0645) {
		t.Error("the centre is inside")
	}
	if ZoneContains(z, 59.3335, 18.0645) || ZoneContains(z, 59.3325, 18.0660) {
		t.Error("points beyond an edge are outside")
	}
	// An L shape: the notch is outside even though it is inside the
	// bounding box — the case a circle or a box could not express.
	l := graph.Zone{Slug: "l", Polygon: [][2]float64{{0, 0}, {0, 2}, {1, 2}, {1, 1}, {2, 1}, {2, 0}}}
	if ZoneContains(l, 1.5, 1.5) || !ZoneContains(l, 0.5, 1.5) {
		t.Error("the notch of an L is outside, its arm inside")
	}
}

// With several starts the team waits, chooses one, and may choose a
// zoned start only from inside its zone.
func TestChoosingAStart(t *testing.T) {
	g := &graph.Graph{
		Zones: []graph.Zone{{Slug: "north", Lat: 59.34, Lng: 18.06, RadiusM: 40}},
		Nodes: []graph.Node{
			{Slug: "s-north", Type: graph.Start, Title: "North gate", StartCfg: &graph.StartConfig{Zone: "north"}},
			{Slug: "s-south", Type: graph.Start, Title: "South gate"},
			{Slug: "a", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("north route", nil)}},
			{Slug: "b", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("south route", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "s-north", FromPort: "out", To: "a"},
			{From: "s-south", FromPort: "out", To: "b"},
			{From: "a", FromPort: "out", To: "end"},
			{From: "b", FromPort: "out", To: "end"},
		},
	}
	tm, step, err := Start(g, t0)
	if err != nil || tm.Current != "" || len(step.Passed) != 0 {
		t.Fatalf("a team that chooses its start must wait unplaced: %v %+v", err, tm)
	}
	v := tm.View()
	if v.Type != graph.Start || len(v.Starts) != 2 || v.Starts[0].Here || !v.Starts[1].Here {
		t.Fatalf("start choice view = %+v", v)
	}
	if _, _, err := tm.ChooseStart("s-north", ""); !errors.Is(err, ErrNotHere) {
		t.Errorf("a zoned start away from its zone = %v, want ErrNotHere", err)
	}
	if _, _, err := tm.ChooseStart("a", ""); !errors.Is(err, ErrWrongNode) {
		t.Errorf("choosing a non-start = %v", err)
	}
	tm.Position = &Position{Lat: 59.34, Lng: 18.06}
	tm.Now = t0.Add(20 * time.Minute)
	_, step2, err := tm.ChooseStart("s-north", "")
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(*step2)
	if tm.Current != "a" || !tm.StartedAt.Equal(tm.Now) {
		t.Errorf("after choosing north: on %q, started %v", tm.Current, tm.StartedAt)
	}
	if _, _, err := tm.ChooseStart("s-south", ""); !errors.Is(err, ErrStale) {
		t.Errorf("choosing again = %v, want ErrStale", err)
	}
}

// An adventure that requires its starting point holds even a single-start
// team at the start screen, shows the point as the start's place, and
// lets it begin only once a player stands there.
func TestStartPlaceGate(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "a", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("hello", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "a"},
			{From: "a", FromPort: "out", To: "end"},
		},
	}
	tm := New(g, t0)
	tm.StartPlace = &graph.Zone{Slug: "_start", Name: "The fountain", Lat: 59.34, Lng: 18.06, RadiusM: 30}
	if !tm.HoldsAtStart() {
		t.Fatal("a required starting point must hold the team at the start screen")
	}
	v := tm.View()
	if v.Type != graph.Start || len(v.Starts) != 1 || v.Starts[0].Here || v.Starts[0].Zone == nil || v.Starts[0].Zone.Name != "The fountain" {
		t.Fatalf("start view away from the starting point = %+v", v)
	}
	if _, _, err := tm.ChooseStart("start", ""); !errors.Is(err, ErrNotHere) {
		t.Errorf("beginning away from the starting point = %v, want ErrNotHere", err)
	}
	tm.Position = &Position{Lat: 59.3401, Lng: 18.0601}
	if v := tm.View(); !v.Starts[0].Here {
		t.Errorf("start view at the starting point = %+v", v.Starts[0])
	}
	tm.Now = t0.Add(15 * time.Minute)
	_, step, err := tm.ChooseStart("start", "")
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(*step)
	if tm.Current != "a" || !tm.StartedAt.Equal(tm.Now) {
		t.Errorf("after beginning at the starting point: on %q, started %v", tm.Current, tm.StartedAt)
	}
}

// A tile puzzle is solved by the tile order alone: the solved order
// takes the answer output, anything else is a wrong guess.
func TestTilePuzzle(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "painting", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer:    &graph.Answer{Kind: graph.AnswerTiles, Image: &graph.Media{ID: 7, Kind: graph.MediaImage}, Rows: 2, Cols: 3},
				Outputs:   []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 20}},
				Responses: []graph.Response{{Text: "Not quite."}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "painting"},
			{From: "painting", FromPort: "solved", To: "end"},
		},
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	v := tm.View()
	if v.Input == nil || v.Input.Kind != "tiles" || v.Input.Rows != 2 || v.Input.Cols != 3 || v.Input.Image == nil || v.Input.Image.ID != 7 {
		t.Fatalf("the view should describe the tiles: %+v", v.Input)
	}
	if ids := tm.VisibleMedia(true); len(ids) != 1 || ids[0] != 7 {
		t.Errorf("the tile picture should be fetchable once the team is there, got %v", ids)
	}

	for _, wrong := range []string{"1,0,2,3,4,5", "0,1,2,3,4", "0,1,2,3,4,5,6", ""} {
		res, step, err := tm.Answer("painting", wrong)
		if err != nil || step != nil || res.Accepted {
			t.Fatalf("%q should be a wrong guess: %+v %v", wrong, res, err)
		}
	}
	if tm.Attempts("painting") != 4 {
		t.Errorf("wrong orders count as attempts, got %d", tm.Attempts("painting"))
	}

	res, step, err := tm.Answer("painting", "0, 1, 2, 3, 4, 5")
	if err != nil || !res.Accepted || res.Points != 20 || step == nil {
		t.Fatalf("the solved order should take the output: %+v %v", res, err)
	}
}

// A compass or map arrives with the zone it shows, in the team's language.
func TestBlockPlaces(t *testing.T) {
	g := &graph.Graph{
		Language: "sv",
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "t", Kind: graph.BlockText, Text: "Gå till parken."},
				{ID: "c", Kind: graph.BlockCompass, Zone: "park"},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
		Zones: []graph.Zone{{Slug: "park", Name: "Parken", Lat: 59.3, Lng: 18.0, RadiusM: 40}},
		Translations: map[string]map[string]string{"en": {
			graph.BlockKey("s", "t"): "Walk to the park.",
			graph.ZoneKey("park"):    "The park",
		}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	v := tm.View()
	if len(v.Blocks) != 2 || v.Blocks[1].Place == nil || v.Blocks[1].Place.Name != "Parken" || v.Blocks[0].Place != nil {
		t.Fatalf("blocks = %+v", v.Blocks)
	}
	if err := tm.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	v = tm.View()
	if v.Blocks[0].Text != "Walk to the park." || v.Blocks[1].Place.Name != "The park" || v.Text != "Walk to the park." {
		t.Errorf("translated blocks = %+v, text %q", v.Blocks, v.Text)
	}
	if g.Zones[0].Name != "Parken" {
		t.Error("translating a view changed the graph")
	}
	// The compass says the phone has arrived by the server's positions,
	// the ones an answer is judged on: a phone it has none for is not there.
	if h := v.Blocks[1].Here; h == nil || *h {
		t.Errorf("a phone nowhere is not at the park: %v", h)
	}
	if v.Blocks[0].Here != nil {
		t.Error("only a compass says whether the phone is there")
	}
	tm.Viewer = &Viewer{Slot: 1, Position: &Position{Lat: 59.3, Lng: 18.0}}
	if h := tm.View().Blocks[1].Here; h == nil || !*h {
		t.Errorf("a phone inside the zone is at the park: %v", h)
	}
}

// A block with a show condition is withheld, file and all, until the
// condition holds; a teaser says it is there; the phone learns when to
// ask again.
func TestBlockShow(t *testing.T) {
	near := cond.Predicate{Op: cond.OpInZone, Zone: "park"}
	later := cond.Predicate{Op: cond.OpNodeElapsed, Cmp: cond.Gte, N: 60}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "t", Kind: graph.BlockText, Text: "Score {{score}}, coins {{counter:coins}}."},
				{ID: "photo", Kind: graph.BlockImage, Media: 5, Show: &graph.Show{When: near, Tease: true}},
				{ID: "late", Kind: graph.BlockText, Text: "A minute on.", Show: &graph.Show{When: later}},
				{ID: "film", Kind: graph.BlockVideo, Media: 6},
				{ID: "clock", Kind: graph.BlockCountdown, Seconds: 90},
				{ID: "st", Kind: graph.BlockStatus, Counters: []string{"coins"}},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges:    []graph.Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
		Zones:    []graph.Zone{{Slug: "park", Name: "Parken", Lat: 59.3, Lng: 18.0, RadiusM: 40}},
		Counters: []graph.Counter{{Slug: "coins", Name: "Coins", Initial: 3}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	v := tm.View()
	ids := func(v View) (out []string) {
		for _, b := range v.Blocks {
			id := b.ID
			if b.Locked {
				id += "(locked)"
			}
			out = append(out, id)
		}
		return out
	}
	if got := ids(v); !reflect.DeepEqual(got, []string{"t", "photo(locked)", "film", "clock", "st"}) {
		t.Fatalf("blocks = %v", got)
	}
	if b := v.Blocks[1]; b.Media != 0 || b.Show != nil || b.Place == nil || b.Place.Slug != "park" {
		t.Errorf("a teaser carries only its kind and the zone it waits for: %+v", b)
	}
	if v.Blocks[0].Text != "Score 0, coins 3." {
		t.Errorf("tokens = %q", v.Blocks[0].Text)
	}
	if !v.WatchPosition || v.RecheckAt == nil || !v.RecheckAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("watch = %v, recheck = %v", v.WatchPosition, v.RecheckAt)
	}
	if e := v.Blocks[3].EndsAt; e == nil || !e.Equal(t0.Add(90*time.Second)) {
		t.Errorf("countdown ends %v", e)
	}
	if c := v.Blocks[4].CounterValues; len(c) != 1 || c[0].Name != "Coins" || c[0].Value != 3 {
		t.Errorf("status = %+v", c)
	}
	if tm.CanSeeMedia(5, false) {
		t.Error("a hidden picture must not be fetchable")
	}
	if !tm.CanSeeMedia(6, false) || slices.Contains(tm.PrefetchMedia(false), 6) {
		t.Error("a video is fetchable but not prefetched")
	}

	tm.Now = t0.Add(time.Minute)
	tm.Position = &Position{Lat: 59.3, Lng: 18.0}
	v = tm.View()
	if got := ids(v); !reflect.DeepEqual(got, []string{"t", "photo", "late", "film", "clock", "st"}) {
		t.Fatalf("once near and a minute on, blocks = %v", got)
	}
	if !tm.CanSeeMedia(5, false) || v.RecheckAt != nil || v.WatchPosition {
		t.Errorf("revealed: media %v, recheck %v, watch %v", tm.CanSeeMedia(5, false), v.RecheckAt, v.WatchPosition)
	}
}

// A title block shows the step's title in the team's language, and a
// points block what the puzzle's best answer is worth.
func TestTitleAndPointsBlocks(t *testing.T) {
	g := &graph.Graph{
		Language: "sv",
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, Title: "Frågan", PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{{ID: "points", Kind: graph.BlockPoints}, {ID: "title", Kind: graph.BlockTitle}},
				Outputs: []graph.Output{
					{Port: "a", Trigger: graph.OnAnswer, Points: 5, Matchers: []match.Matcher{{Kind: match.Exact, Value: "a"}}},
					{Port: "b", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "b"}}},
					{Port: "late", Trigger: graph.OnTimeout, Points: 50},
				},
				TimeLimitSeconds: 60,
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges:        []graph.Edge{{From: "start", FromPort: "out", To: "q"}, {From: "q", FromPort: "a", To: "end"}, {From: "q", FromPort: "b", To: "end"}, {From: "q", FromPort: "late", To: "end"}},
		Translations: map[string]map[string]string{"en": {graph.NodeKey("q", "title"): "The question"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); v.Blocks[0].Points != 10 || v.Blocks[1].Text != "Frågan" {
		t.Fatalf("blocks = %+v", v.Blocks)
	}
	if err := tm.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.Blocks[1].Text != "The question" {
		t.Errorf("title in English = %q", v.Blocks[1].Text)
	}
}

// A block switched off is as if it were not there: the phone never gets
// it, the title and points included, and an unanswered question switched
// off does not hold the team back.
func TestDisabledBlocks(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, Title: "Hidden", StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "title", Kind: graph.BlockTitle, Disabled: true},
				{ID: "on", Kind: graph.BlockText, Text: "Shown."},
				{ID: "off", Kind: graph.BlockText, Text: "Not shown.", Disabled: true},
				{ID: "q", Kind: graph.BlockQuiz, Text: "Capital?", Options: []string{"Oslo", "Stockholm", "Malmö"}, Answer: "X", Disabled: true},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); len(v.Blocks) != 1 || v.Blocks[0].ID != "on" || v.Text != "Shown." {
		t.Fatalf("blocks = %+v, text = %q", v.Blocks, v.Text)
	}
	if _, _, err := tm.Continue("s"); err != nil {
		t.Fatalf("continue past a switched-off question = %v", err)
	}
	if q := tm.Quiz(); q.Total != 0 {
		t.Errorf("a switched-off question was marked: %+v", q)
	}
}

// An adventure that hides the score says so in every view; the points
// are still kept.
func TestHideScore(t *testing.T) {
	g := &graph.Graph{
		HideScore: true,
		Nodes:     []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End}},
		Edges:     []graph.Edge{{From: "start", FromPort: "out", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	tm.Points = 7
	if v := tm.View(); !v.HideScore || v.Score != 7 {
		t.Errorf("view = hide %v, score %v; want hidden, 7", v.HideScore, v.Score)
	}
}

// A suspect board keeps what the team struck, any time and never final;
// a draw deals one card once and keeps the rest face down; a block may
// wait for the card drawn.
func TestSuspectsAndDraw(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "board", Kind: graph.BlockSuspects, Cards: []graph.Card{{Name: "Baker"}, {Name: "Mayor"}, {Name: "Maid"}}},
				{ID: "deck", Kind: graph.BlockDraw, Options: []string{"Tower", "Star"}},
				{ID: "star", Kind: graph.BlockText, Text: "The star shines.", Show: &graph.Show{When: cond.Predicate{Op: cond.OpDrawn, Node: "s", Block: "deck", Is: "1"}}},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if _, _, err := tm.Pick("s", "board", "2, 0,2"); err != nil {
		t.Fatal(err)
	}
	if got := tm.Picked("s", "board"); got != "0,2" {
		t.Errorf("struck = %q, want 0,2", got)
	}
	if _, _, err := tm.Pick("s", "board", "3"); err != ErrWrongNode {
		t.Errorf("striking a card that is not there = %v", err)
	}
	v := tm.View()
	if v.Blocks[1].Deck != 2 || v.Blocks[1].Options != nil || v.Blocks[1].Dealt != "" {
		t.Fatalf("an undrawn deck showed its cards: %+v", v.Blocks[1])
	}
	if _, _, err := tm.Pick("s", "deck", ""); err != nil {
		t.Fatal(err)
	}
	card := tm.Picked("s", "deck")
	if _, _, err := tm.Pick("s", "deck", ""); err != nil || tm.Picked("s", "deck") != card {
		t.Fatalf("a second draw changed the card from %q to %q (%v)", card, tm.Picked("s", "deck"), err)
	}
	v = tm.View()
	want := map[string]string{"0": "Tower", "1": "Star"}[card]
	if v.Blocks[1].Dealt != want {
		t.Errorf("dealt %q, want %q for card %s", v.Blocks[1].Dealt, want, card)
	}
	if shown := len(v.Blocks) == 3; shown != (card == "1") {
		t.Errorf("drew card %s, but the star block shown = %v", card, shown)
	}
	_, next, err := tm.Continue("s")
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(*next)
	if _, _, err := tm.Pick("s", "board", "1"); err == nil {
		t.Error("struck a card on a step the team has left")
	}
}

// A quiz walk: a story waits for its questions, a pick can change until
// the team moves on, the phone never sees the right answer before the
// end, and finishing marks it and adds the points.
func TestQuizWalk(t *testing.T) {
	target := 1887.0
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q1", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "a", Kind: graph.BlockQuiz, Text: "Capital?", Options: []string{"Oslo", "Stockholm", "Malmö"}, Answer: "X", Worth: 2},
				{ID: "b", Kind: graph.BlockQuiz, Text: "Colour?", Options: []string{"Red", "Blue", "Green"}, Answer: "1"},
			}}},
			{Slug: "q2", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "tb", Kind: graph.BlockTiebreak, Text: "The year?", Target: &target},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "q1"}, {From: "q1", FromPort: "out", To: "q2"}, {From: "q2", FromPort: "out", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); v.Blocks[0].Answer != "" || v.Blocks[1].Answer != "" {
		t.Fatalf("the right answer reached the phone: %+v", v.Blocks)
	}
	if _, _, err := tm.Continue("q1"); err != ErrUnanswered {
		t.Fatalf("continue before answering = %v, want ErrUnanswered", err)
	}
	if _, _, err := tm.Pick("q1", "a", "4"); err != ErrWrongNode {
		t.Errorf("a choice that is not 1, X or 2 = %v", err)
	}
	for _, p := range [][2]string{{"a", "x"}, {"b", "2"}} {
		if _, _, err := tm.Pick("q1", p[0], p[1]); err != nil {
			t.Fatalf("pick %v: %v", p, err)
		}
	}
	for _, c := range []string{"1", "X"} {
		if _, _, err := tm.Pick("q1", "a", c); err != nil {
			t.Errorf("changing the pick to %s while on the step: %v", c, err)
		}
	}
	if v := tm.View(); v.Blocks[0].Picked != "X" || v.Blocks[0].Final || tm.Points != 0 {
		t.Errorf("picked %q final %v, points %v: nothing scores before the end", v.Blocks[0].Picked, v.Blocks[0].Final, tm.Points)
	}
	res, st, err := tm.Continue("q1")
	act(t, tm, "q2", res, st, err)
	if !tm.PickFinal("q1") || tm.Picks[PickKey("q1", "a")] != "X" {
		t.Error("moving on makes the step's answers final")
	}
	tm.Current = "q1" // as if a hub loop brought the team back
	if _, _, err := tm.Pick("q1", "a", "1"); err != ErrNotAllowed {
		t.Errorf("a pick on a step already left = %v, want ErrNotAllowed", err)
	}
	tm.Current = "q2"
	if _, _, err := tm.Pick("q2", "tb", "1 890,5"); err != ErrWrongNode {
		t.Errorf("a number with a space = %v", err)
	}
	if _, _, err := tm.Pick("q2", "tb", "1890,5"); err != nil {
		t.Fatal(err)
	}
	res, st, err = tm.Continue("q2")
	act(t, tm, "end", res, st, err)
	if tm.Points != 2 || tm.TiebreakMiss() != 3.5 {
		t.Errorf("points %v, tie-break miss %v", tm.Points, tm.TiebreakMiss())
	}
	v := tm.View()
	if q := v.Quiz; q == nil || q.Right != 1 || q.Total != 2 || q.Items[1].Picked != "2" || q.Items[1].Answer != "1" || q.Tiebreak == nil || *q.Tiebreak.Guess != 1890.5 {
		t.Errorf("quiz = %+v", v.Quiz)
	}
}

// A puzzle answered by its questions waits for every shown pick, then
// takes its first answer route, paying that route's points at once and
// the quiz's at the end.
func TestQuestionsPuzzle(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "p", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer: &graph.Answer{Kind: graph.AnswerQuestions},
				Blocks: []graph.Block{
					{ID: "a", Kind: graph.BlockQuiz, Text: "Capital?", Options: []string{"Oslo", "Stockholm", "Malmö"}, Answer: "X"},
					// Hidden until the team carries the key: not asked yet.
					{ID: "b", Kind: graph.BlockQuiz, Text: "Later?", Options: []string{"a", "b", "c"}, Answer: "1",
						Show: &graph.Show{When: cond.Predicate{Op: cond.OpItem, Item: "key"}}},
				},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer, Points: 5}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Items: []graph.Item{{Slug: "key"}},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "p"}, {From: "p", FromPort: "done", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if in := tm.View().Input; in == nil || in.Kind != "questions" {
		t.Fatalf("input = %+v, want questions", in)
	}
	if _, _, err := tm.Answer("p", ""); err != ErrUnanswered {
		t.Fatalf("continue before answering = %v, want ErrUnanswered", err)
	}
	if tm.Row("p").Attempts != 0 {
		t.Errorf("an unanswered continue counted as a wrong guess")
	}
	if _, _, err := tm.Pick("p", "a", "X"); err != nil {
		t.Fatal(err)
	}
	res, st, err := tm.Answer("p", "")
	act(t, tm, "end", res, st, err)
	if tm.Points != 6 {
		t.Errorf("points = %v, want the route's 5 and the question's 1", tm.Points)
	}
}

// The author's answer bar reaches the phone, in the team's language; a
// scan-only puzzle is a scan button, and what it decodes is a guess.
func TestAnswerBar(t *testing.T) {
	g := &graph.Graph{
		Language: "sv",
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "p", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Bar:     &graph.AnswerBar{Placeholder: "Årtalet", Button: "Gissa", Keyboard: "text", NoScan: true},
				Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "1887"}}}},
			}},
			{Slug: "s", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer:  &graph.Answer{Kind: graph.AnswerScan},
				Bar:     &graph.AnswerBar{Button: "Skanna skylten"},
				Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "SIGN-7"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "p"}, {From: "p", FromPort: "ok", To: "s"}, {From: "s", FromPort: "ok", To: "end"}},
		Translations: map[string]map[string]string{"en": {
			graph.NodeKey("p", "bar:placeholder"): "The year",
		}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	want := Input{Kind: "text", Placeholder: "Årtalet", Button: "Gissa", NoScan: true}
	if in := tm.View().Input; in == nil || !reflect.DeepEqual(*in, want) {
		t.Errorf("input = %+v, want %+v: the author's keyboard wins over the numeric answer", in, want)
	}
	if err := tm.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if in := tm.View().Input; in.Placeholder != "The year" || in.Button != "Gissa" {
		t.Errorf("translated input = %+v, want the placeholder translated and the button as written", in)
	}
	res, st, err := tm.Answer("p", "1887")
	act(t, tm, "s", res, st, err)
	if in := tm.View().Input; in == nil || in.Kind != "scan" || in.Button != "Skanna skylten" {
		t.Errorf("scan input = %+v", in)
	}
	res, st, err = tm.Answer("s", "SIGN-7")
	act(t, tm, "end", res, st, err)
}

// Where the players are: a team kept together stands as many players at
// its position as it has; a split one is each fresh phone, or — with
// none fresh — one player wherever it was last seen. A player standing
// where two zones overlap holds only one of them.
func TestPlayersInZones(t *testing.T) {
	g := &graph.Graph{Zones: []graph.Zone{
		{Slug: "gate", Lat: 59.30, Lng: 18.00, RadiusM: 40},
		{Slug: "arch", Lat: 59.30, Lng: 18.0002, RadiusM: 40}, // overlaps gate
		{Slug: "bridge", Lat: 59.31, Lng: 18.00, RadiusM: 40},
	}}
	gate := Position{Lat: 59.30, Lng: 18.00}
	bridge := Position{Lat: 59.31, Lng: 18.00}
	team := func(pos *Position, size int, phones []Position) *Team {
		tm := New(g, t0)
		tm.Position, tm.Size, tm.Phones = pos, size, phones
		return tm
	}

	together := team(&gate, 3, nil)
	if n := together.PlayersInZone("gate"); n != 3 {
		t.Errorf("a team together: %d at the gate, want 3", n)
	}
	if !together.ZonesHeld([]string{"gate", "arch"}) {
		t.Error("three players standing in both overlapping zones hold both")
	}
	if together.ZonesHeld([]string{"gate", "bridge"}) {
		t.Error("a team together cannot hold zones apart")
	}

	split := team(&bridge, 3, []Position{gate, bridge})
	if !split.InZone("gate") || !split.InZone("bridge") {
		t.Error("a split team is in every zone one of its players is in")
	}
	if n := split.PlayersInZone("gate"); n != 1 {
		t.Errorf("split: %d at the gate, want 1", n)
	}
	if !split.ZonesHeld([]string{"gate", "bridge"}) {
		t.Error("one player at the gate and one at the bridge hold both")
	}
	if split.ZonesHeld([]string{"gate", "arch", "bridge"}) {
		t.Error("the player at the gate holds the gate or the arch, not both")
	}
	if !split.ZonesHeld([]string{"arch", "bridge", "bridge"}) {
		t.Error("a zone listed twice needs one player")
	}
	if split.ZonesHeld([]string{"gate", "attic"}) {
		t.Error("a zone that does not exist is never held")
	}

	stale := team(&gate, 3, []Position{})
	if n := stale.PlayersInZone("gate"); n != 1 {
		t.Errorf("no phone fresh: %d at the gate, want the one last seen there", n)
	}
	if team(nil, 3, []Position{}).InZone("gate") {
		t.Error("a team never seen is nowhere")
	}
}

// "Here" is the phone a view is for. Without one — the organisers'
// look, a simulated team — it is wherever one of the team's players is;
// a phone that has not reported lately is nowhere.
func TestHereIsTheViewersPhone(t *testing.T) {
	g := &graph.Graph{Zones: []graph.Zone{
		{Slug: "gate", Lat: 59.30, Lng: 18.00, RadiusM: 40},
		{Slug: "bridge", Lat: 59.31, Lng: 18.00, RadiusM: 40},
	}}
	gate := Position{Lat: 59.30, Lng: 18.00}
	bridge := Position{Lat: 59.31, Lng: 18.00}
	tm := New(g, t0)
	tm.Position, tm.Size, tm.Phones = &bridge, 2, []Position{gate, bridge}

	if !tm.Here("gate") || !tm.Here("bridge") {
		t.Error("for the team as a whole, here is wherever a player is")
	}
	tm.Viewer = &Viewer{Position: &tm.Phones[0]}
	if !tm.Here("gate") || tm.Here("bridge") {
		t.Error("the phone at the gate is at the gate only")
	}
	tm.Viewer = &Viewer{}
	if tm.Here("gate") || tm.Here("bridge") {
		t.Error("a phone that has not reported is nowhere")
	}
	tm.Viewer = &Viewer{Position: &gate}
	if tm.Here("attic") {
		t.Error("a zone that does not exist is nowhere")
	}
}

// The team as a whole holds every role; a phone holds its player's.
func TestHasRole(t *testing.T) {
	tm := New(&graph.Graph{Roles: []graph.Role{{Slug: "navigator"}, {Slug: "decoder"}}}, t0)
	if !tm.HasRole("navigator") || !tm.HasRole("decoder") {
		t.Error("the team as a whole holds every role")
	}
	tm.Viewer = &Viewer{Roles: []string{"decoder"}}
	if tm.HasRole("navigator") || !tm.HasRole("decoder") {
		t.Error("the decoder's phone holds the decoder only")
	}
	if v := tm.View(); len(v.Roles) != 2 || v.Roles[0].Slug != "navigator" {
		t.Errorf("the view lists the adventure's roles: %+v", v.Roles)
	}
}

// together builds start → p → end with p's config, and a team of the
// given players standing on p.
func together(t *testing.T, cfg graph.PuzzleConfig, zones []graph.Zone, players ...int64) *Team {
	t.Helper()
	cfg.Outputs = append(cfg.Outputs, graph.Output{Port: "solved", Trigger: graph.OnAnswer, Points: 5,
		Matchers: []match.Matcher{{Kind: match.Exact, Value: "seven"}, {Kind: match.Exact, Value: "7"}}})
	g := &graph.Graph{
		Zones: zones,
		Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "p", Type: graph.Puzzle, PuzzleCfg: &cfg}, {Slug: "end", Type: graph.End}},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "p"}, {From: "p", FromPort: "solved", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	tm.Players = players
	tm.Size = len(players)
	return tm
}

// as makes the team's actions the given player's.
func (t *Team) as(slot int64, pos *Position) *Team {
	t.Viewer = &Viewer{Slot: slot, Position: pos}
	t.PressChanged = nil
	return t
}

// Two keys turned within the window open the launch; one alone waits,
// and one turned too long before the other no longer counts.
func TestTwoKeyLaunch(t *testing.T) {
	tm := together(t, graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys}}, nil, 1, 2)
	res, step, err := tm.as(1, nil).Turn("p")
	if err != nil || step != nil || res.Waiting == nil || res.Waiting.Done != 1 || res.Waiting.Of != 2 {
		t.Fatalf("first key: %+v %v %v", res, step, err)
	}
	if tm.PressChanged == nil || tm.PressChanged.Slot != 1 {
		t.Errorf("the first key should be recorded: %+v", tm.PressChanged)
	}
	if v := tm.View(); v.Together == nil || v.Together.Done != 1 || !v.Together.Mine || v.RecheckAt == nil {
		t.Errorf("the turner's view: %+v", v.Together)
	}

	// Eleven seconds on, the first key has gone back.
	tm.Now = t0.Add(11 * time.Second)
	if res, _, _ := tm.as(2, nil).Turn("p"); res.Waiting == nil || res.Waiting.Done != 1 {
		t.Fatalf("keys 11 s apart: %+v", res)
	}
	tm.Now = t0.Add(15 * time.Second)
	res, step, err = tm.as(1, nil).Turn("p")
	if err != nil || !res.Accepted || step == nil || res.Points != 5 {
		t.Fatalf("both keys within 4 s: %+v %v %v", res, step, err)
	}

	// A player alone turns the one key the launch needs of them.
	solo := together(t, graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys, Keys: 3}}, nil, 9)
	if res, _, _ := solo.as(9, nil).Turn("p"); !res.Accepted {
		t.Errorf("alone: %+v", res)
	}
	// Answering a launch is not how it opens.
	if _, _, err := together(t, graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys}}, nil, 1, 2).as(1, nil).Answer("p", "7"); err == nil {
		t.Error("a launch should refuse a typed answer")
	}
}

// With posts, each key is turned at a post of its own: two at the same
// post are one, and a player at no post is told to go to one.
func TestTwoKeyLaunchAtPosts(t *testing.T) {
	zones := []graph.Zone{
		{Slug: "east", Lat: 59.30, Lng: 18.00, RadiusM: 40},
		{Slug: "west", Lat: 59.31, Lng: 18.00, RadiusM: 40},
	}
	east, west, nowhere := Position{Lat: 59.30, Lng: 18.00}, Position{Lat: 59.31, Lng: 18.00}, Position{Lat: 59.40, Lng: 18.00}
	tm := together(t, graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys, Posts: []string{"east", "west"}}}, zones, 1, 2, 3)
	if _, _, err := tm.as(1, &nowhere).Turn("p"); err != ErrNotHere {
		t.Errorf("at no post: %v, want ErrNotHere", err)
	}
	tm.as(1, &east).Turn("p")
	if res, _, _ := tm.as(2, &east).Turn("p"); res.Waiting == nil || res.Waiting.Done != 1 || res.Waiting.Of != 2 {
		t.Fatalf("two at the east post: %+v", res.Waiting)
	}
	if res, _, _ := tm.as(3, &west).Turn("p"); !res.Accepted {
		t.Errorf("east and west: %+v", res)
	}
}

// With roles on the posts, each key is turned by its role's player at
// its post: anyone else there is told to go to their own. A player
// holding both roles turns one key, and a holder who stopped playing
// leaves the post to anyone.
func TestTwoKeyLaunchRolePosts(t *testing.T) {
	zones := []graph.Zone{
		{Slug: "east", Lat: 59.30, Lng: 18.00, RadiusM: 40},
		{Slug: "west", Lat: 59.31, Lng: 18.00, RadiusM: 40},
	}
	east, west := Position{Lat: 59.30, Lng: 18.00}, Position{Lat: 59.31, Lng: 18.00}
	cfg := graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys, Posts: []string{"east", "west"}, PostRoles: []string{"smith", "mech"}}}
	tm := together(t, cfg, zones, 1, 2, 3)
	tm.G.Roles = []graph.Role{{Slug: "smith", Name: "Smith"}, {Slug: "mech", Name: "Mech"}}
	tm.RoleHolders = map[string]int64{"smith": 1, "mech": 2}

	if _, _, err := tm.as(2, &east).Turn("p"); err != ErrNotHere {
		t.Errorf("the mech at the smith's post: %v, want ErrNotHere", err)
	}
	if res, _, _ := tm.as(1, &east).Turn("p"); res.Waiting == nil || res.Waiting.Done != 1 || res.Waiting.Of != 2 {
		t.Fatalf("the smith at their post: %+v", res.Waiting)
	}
	if _, _, err := tm.as(3, &west).Turn("p"); err != ErrNotHere {
		t.Errorf("a player with neither role at the mech's post: %v, want ErrNotHere", err)
	}
	in := tm.as(2, &west).View().Input
	if in == nil || len(in.PostRoles) != 2 || in.PostRoles[1] != "Mech" || in.MyPosts[0] || !in.MyPosts[1] {
		t.Errorf("the mech's phone: %+v", in)
	}
	if res, _, _ := tm.as(2, &west).Turn("p"); !res.Accepted {
		t.Errorf("smith east and mech west: %+v", res)
	}

	both := together(t, cfg, zones, 1, 2)
	both.RoleHolders = map[string]int64{"smith": 1, "mech": 1}
	if res, _, _ := both.as(1, &west).Turn("p"); !res.Accepted {
		t.Errorf("one player holding both roles turns one key: %+v", res)
	}

	gone := together(t, cfg, zones, 1, 3)
	gone.RoleHolders = map[string]int64{"smith": 1, "mech": 2}
	gone.as(1, &east).Turn("p")
	if res, _, _ := gone.as(3, &west).Turn("p"); !res.Accepted {
		t.Errorf("the mech's post with the mech not playing: %+v", res)
	}

	// The mech plays, but their phone has gone quiet: their post is anyone's.
	quiet := together(t, cfg, zones, 1, 2, 3)
	quiet.RoleHolders = map[string]int64{"smith": 1, "mech": 2}
	quiet.Away = map[int64]bool{2: true}
	quiet.as(1, &east).Turn("p")
	if res, _, _ := quiet.as(3, &west).Turn("p"); !res.Accepted {
		t.Errorf("the mech's post with the mech away: %+v", res)
	}

	// A phone that has not found itself is told so, not sent to its post.
	if _, _, err := together(t, cfg, zones, 1, 2).as(1, nil).Turn("p"); err != ErrNoFix {
		t.Errorf("no position: %v, want ErrNoFix", err)
	}
}

// The phone shows which posts have their player in place: the role's
// holder, not just anyone, at a role's post.
func TestRolePostsHeld(t *testing.T) {
	zones := []graph.Zone{
		{Slug: "east", Lat: 59.30, Lng: 18.00, RadiusM: 40},
		{Slug: "west", Lat: 59.31, Lng: 18.00, RadiusM: 40},
	}
	east, west := Position{Lat: 59.30, Lng: 18.00}, Position{Lat: 59.31, Lng: 18.00}
	cfg := graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys, Posts: []string{"east", "west"}, PostRoles: []string{"smith", "mech"}}}
	tm := together(t, cfg, zones, 1, 2, 3)
	tm.RoleHolders = map[string]int64{"smith": 1, "mech": 2}
	// The smith is at their post; at the mech's stands someone else.
	tm.PhoneAt = map[int64]Position{1: east, 3: west}
	tm.Phones = []Position{east, west}
	in := tm.as(1, &east).View().Input
	if in == nil || len(in.PostsHeld) != 2 || !in.PostsHeld[0] || in.PostsHeld[1] {
		t.Errorf("held: %+v", in)
	}
	tm.PhoneAt[2] = west
	if in := tm.as(1, &east).View().Input; !in.PostsHeld[1] {
		t.Errorf("the mech in place: %+v", in.PostsHeld)
	}
}

// One teammate at the place opens the step for all, but only they are
// told they are there; the others how many are.
func TestHereMe(t *testing.T) {
	zones := []graph.Zone{{Slug: "east", Lat: 59.30, Lng: 18.00, RadiusM: 40}}
	east, away := Position{Lat: 59.30, Lng: 18.00}, Position{Lat: 59.40, Lng: 18.00}
	tm := together(t, graph.PuzzleConfig{Zone: "east"}, zones, 1, 2)
	tm.Phones = []Position{east, away}
	v := tm.as(2, &away).View()
	if !v.Here || v.HereMe == nil || *v.HereMe || v.HereCount != 1 {
		t.Errorf("the player away: here %v, me %v, count %d", v.Here, v.HereMe, v.HereCount)
	}
	if v := tm.as(1, &east).View(); v.HereMe == nil || !*v.HereMe {
		t.Errorf("the player there: %v", v.HereMe)
	}
	tm.Viewer = nil
	if v := tm.View(); v.HereMe != nil {
		t.Errorf("the team as a whole has no one phone: %v", v.HereMe)
	}
	// Alone, with the team placed by a fix that is not this phone's.
	solo := together(t, graph.PuzzleConfig{Zone: "east"}, zones, 1)
	solo.Phones = []Position{east}
	if v := solo.as(1, nil).View(); !v.Here || v.HereMe == nil || !*v.HereMe {
		t.Errorf("a team of one is never told a teammate is there: here %v, me %v", v.Here, v.HereMe)
	}
}

// A unanimous answer counts once every player has given a right one that
// leads the same way; a wrong one counts as ever and withdraws theirs.
func TestUnanimousAnswer(t *testing.T) {
	tm := together(t, graph.PuzzleConfig{Unanimous: true}, nil, 1, 2, 3)
	res, step, err := tm.as(1, nil).Answer("p", "seven")
	if err != nil || step != nil || res.Accepted || res.Waiting == nil || res.Waiting.Done != 1 || res.Waiting.Of != 3 {
		t.Fatalf("one of three: %+v %v %v", res, step, err)
	}
	if tm.Nodes["p"].Attempts != 0 {
		t.Error("a right answer waiting for the others is not a wrong guess")
	}
	tm.as(2, nil).Answer("p", "7")
	// Player 1 changes their mind to something wrong: counted, and withdrawn.
	if res, _, _ := tm.as(1, nil).Answer("p", "eight"); res.Waiting != nil || tm.Nodes["p"].Attempts != 1 {
		t.Errorf("a wrong answer: %+v, attempts %d", res, tm.Nodes["p"].Attempts)
	}
	if tm.PressChanged == nil || tm.PressChanged.Press != nil {
		t.Errorf("the wrong answer should withdraw player 1's: %+v", tm.PressChanged)
	}
	if v := tm.View(); v.Together == nil || v.Together.Done != 1 || v.Together.Mine {
		t.Errorf("player 1's view after withdrawing: %+v", v.Together)
	}
	tm.as(3, nil).Answer("p", "seven")
	res, step, err = tm.as(1, nil).Answer("p", "7")
	if err != nil || !res.Accepted || step == nil {
		t.Fatalf("all three: %+v %v %v", res, step, err)
	}

	// For the team as a whole, and for a player alone, one answer is all.
	if res, _, _ := together(t, graph.PuzzleConfig{Unanimous: true}, nil, 1, 2).Answer("p", "7"); !res.Accepted {
		t.Errorf("the team as a whole: %+v", res)
	}
	if res, _, _ := together(t, graph.PuzzleConfig{Unanimous: true}, nil, 5).as(5, nil).Answer("p", "7"); !res.Accepted {
		t.Errorf("alone: %+v", res)
	}
}

// Only the role's player answers a puzzle answered by it; the others
// are told it is not theirs, and nothing counts against the team.
func TestAnsweredByRole(t *testing.T) {
	tm := together(t, graph.PuzzleConfig{By: "locksmith"}, nil, 1, 2)
	tm.G.Roles = []graph.Role{{Slug: "locksmith", Name: "Locksmith"}, {Slug: "scout"}}
	tm.Viewer = &Viewer{Slot: 2, Roles: []string{"scout"}}
	if _, _, err := tm.Answer("p", "7"); err != ErrNotYours {
		t.Fatalf("the scout answering: %v, want ErrNotYours", err)
	}
	if tm.Nodes["p"].Attempts != 0 {
		t.Error("someone else's turn is not a wrong guess")
	}
	if v := tm.View(); v.Input == nil || v.Input.By != "Locksmith" || !v.Input.NotYours {
		t.Errorf("the scout's view: %+v", v.Input)
	}
	tm.Viewer = &Viewer{Slot: 1, Roles: []string{"locksmith"}}
	if v := tm.View(); v.Input.NotYours {
		t.Error("the locksmith's own puzzle is theirs")
	}
	if res, _, err := tm.Answer("p", "7"); err != nil || !res.Accepted {
		t.Errorf("the locksmith answering: %+v %v", res, err)
	}
}

// A role's kit is in the backpack from the start, labelled whose it is;
// only that role's player can place it in a slot.
func TestRoleKit(t *testing.T) {
	cfg := graph.PuzzleConfig{
		Answer:  &graph.Answer{Kind: graph.AnswerItems, Slots: []graph.ItemSlot{{Label: "The lock", Item: "lockpick"}}, Keep: true},
		Outputs: []graph.Output{{Port: "open", Trigger: graph.OnAnswer}},
	}
	g := &graph.Graph{
		Roles: []graph.Role{{Slug: "locksmith", Name: "Locksmith", Kit: []string{"lockpick"}}, {Slug: "scout"}},
		Items: []graph.Item{{Slug: "lockpick", Name: "Lockpick"}},
		Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "door", Type: graph.Puzzle, PuzzleCfg: &cfg}, {Slug: "end", Type: graph.End}},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "door"}, {From: "door", FromPort: "open", To: "end"}},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if tm.Items["lockpick"] != 1 {
		t.Fatalf("the team should start with the locksmith's lockpick: %v", tm.Items)
	}
	tm.Viewer = &Viewer{Slot: 2, Roles: []string{"scout"}}
	if v := tm.View(); len(v.Items) != 1 || v.Items[0].Role != "Locksmith" || !v.Items[0].NotMine {
		t.Errorf("the scout's backpack: %+v", v.Items)
	}
	if _, _, err := tm.Answer("door", "lockpick"); err != ErrNotYours {
		t.Errorf("the scout using the lockpick: %v, want ErrNotYours", err)
	}
	tm.Viewer = &Viewer{Slot: 1, Roles: []string{"locksmith"}}
	if res, _, err := tm.Answer("door", "lockpick"); err != nil || !res.Accepted {
		t.Errorf("the locksmith using it: %+v %v", res, err)
	}
}

// saboteurGraph is start → heist (a puzzle with a sabotage and an
// accusation) → end, with a secret saboteur whose goal is a low score.
func saboteurGraph() *graph.Graph {
	return &graph.Graph{
		Roles: []graph.Role{
			{Slug: "scout", Name: "Scout"},
			{Slug: "saboteur", Name: "Saboteur", Secret: true, Goals: []graph.RoleGoal{
				{Text: "Keep the score under 10.", When: cond.Predicate{Op: cond.OpScore, Cmp: cond.Lt, N: 10}},
			}},
		},
		Journal: []graph.JournalEntry{{Slug: "lie", Title: "The vault is in the north."}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "heist", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{
					{ID: "cut", Kind: graph.BlockSabotage, Role: "saboteur", Text: "Cut the alarm wire.", Worth: 3, Seconds: 60, Journal: "lie"},
					{ID: "who", Kind: graph.BlockAccuse, Role: "saboteur", Text: "Who is the saboteur?", Worth: 20},
				},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer, Points: 5, Matchers: []match.Matcher{{Kind: match.Exact, Value: "go"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "heist"}, {From: "heist", FromPort: "done", To: "end"}},
	}
}

// Only the saboteur sees the sabotage and their secret role; pressing it
// costs the team, locks the answer and plants a note, once. The
// accusation is asked only of a team that has a saboteur.
func TestSabotage(t *testing.T) {
	tm, step, err := Start(saboteurGraph(), t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	blocks := func() (out []string) {
		for _, b := range tm.View().Blocks {
			out = append(out, b.ID)
		}
		return out
	}
	tm.Viewer = &Viewer{Slot: 1, Roles: []string{"scout"}}
	if got := blocks(); slices.Contains(got, "cut") || slices.Contains(got, "who") {
		t.Errorf("no saboteur dealt: the scout sees %v", got)
	}
	tm.Secrets = map[string]int64{"saboteur": 2}
	if got := blocks(); slices.Contains(got, "cut") || !slices.Contains(got, "who") {
		t.Errorf("a saboteur among them: the scout sees %v", got)
	}
	for _, r := range tm.View().Roles {
		if r.Secret {
			t.Errorf("the scout should not see the secret role: %+v", r)
		}
	}
	if _, _, err := tm.Sabotage("heist", "cut"); err == nil {
		t.Error("the scout should not be able to sabotage")
	}

	tm.Viewer = &Viewer{Slot: 2, Roles: []string{"saboteur"}}
	if got := blocks(); !slices.Contains(got, "cut") {
		t.Errorf("the saboteur should see the sabotage: %v", got)
	}
	if !slices.ContainsFunc(tm.View().Roles, func(r RoleView) bool { return r.Secret && r.Mine }) {
		t.Error("the saboteur should see their own secret role")
	}
	if _, _, err := tm.Sabotage("heist", "cut"); err != nil {
		t.Fatal(err)
	}
	if tm.PenaltyScore != 3 || tm.Nodes["heist"].BlockedUntil == nil || !tm.Journal["lie"] || !slices.Contains(tm.NewJournal, "lie") {
		t.Errorf("after sabotage: penalty %v, blocked %v, journal %v", tm.PenaltyScore, tm.Nodes["heist"].BlockedUntil, tm.Journal)
	}
	if _, _, err := tm.Sabotage("heist", "cut"); err != ErrNotAllowed {
		t.Errorf("a second press: %v, want ErrNotAllowed", err)
	}
}

// Naming the saboteur pays the accusation's worth when the team
// finishes, and the end reveals them, their goals and whether they were
// caught.
func TestAccusationAndReveal(t *testing.T) {
	for _, named := range []int64{2, 3} {
		tm, step, err := Start(saboteurGraph(), t0)
		if err != nil {
			t.Fatal(err)
		}
		tm.Apply(step)
		tm.Secrets = map[string]int64{"saboteur": 2}
		tm.Accused = map[string]int64{PickKey("heist", "who"): named}
		res, next, err := tm.Answer("heist", "go")
		if err != nil || !res.Accepted {
			t.Fatal(res, err)
		}
		tm.Apply(*next)
		caught := named == 2
		want := 5.0
		if caught {
			want += 20
		}
		if tm.Points != want {
			t.Errorf("named %d: points %v, want %v", named, tm.Points, want)
		}
		rv := tm.View().Reveal
		if len(rv) != 1 || !rv[0].Dealt || rv[0].Slot != 2 || rv[0].Caught == nil || *rv[0].Caught != caught || len(rv[0].Goals) != 1 || rv[0].Goals[0].Met == caught {
			t.Errorf("named %d: reveal %+v", named, rv)
		}
	}
}

// A kit item is its role's player's to use and give, until it is handed
// to someone: then it is theirs, whoever holds the role.
func TestKitItemHandedOn(t *testing.T) {
	g := hunt()
	g.Items = []graph.Item{{Slug: "key"}, {Slug: "map"}}
	g.Roles = []graph.Role{{Slug: "locksmith", Kit: []string{"key"}}}
	tm := New(g, t0)
	tm.Items = map[string]int{"key": 1, "map": 1}

	tm.Viewer = &Viewer{Slot: 1, Roles: []string{"locksmith"}}
	if !tm.CanUse("key") || !tm.Giveable("key") || !tm.CanUse("map") || tm.Giveable("map") {
		t.Error("the locksmith uses and gives the key; the map is everyone's, and no one's to give")
	}
	tm.Viewer = &Viewer{Slot: 2}
	if tm.CanUse("key") || tm.Giveable("key") || !tm.usesOthersKit("key|map") {
		t.Error("another player neither uses nor gives the locksmith's key")
	}

	tm.Holders = map[string]int64{"key": 2}
	if !tm.CanUse("key") || !tm.Giveable("key") {
		t.Error("handed the key, player 2 uses and gives it")
	}
	tm.Viewer = &Viewer{Slot: 1, Roles: []string{"locksmith"}}
	if tm.CanUse("key") || tm.Giveable("key") {
		t.Error("the locksmith no longer has the key they gave away")
	}
	tm.Viewer = nil
	if !tm.CanUse("key") {
		t.Error("the team as a whole uses every item")
	}
}

func TestAnswerToken(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "name", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("What is my name?", nil),
				Outputs: []graph.Output{{Port: "out", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Anything}}}},
			}},
			{Slug: "bye", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Bye! /{{answer:name}}{{answer:none}}", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "name"},
			{From: "name", FromPort: "out", To: "bye"},
			{From: "bye", FromPort: "out", To: "end"},
		},
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	_, next, err := tm.Answer("name", " Tomtebisse ")
	if err != nil || next == nil {
		t.Fatalf("answer: step=%v err=%v", next, err)
	}
	tm.Apply(*next)
	if got := tm.View().Blocks[0].Text; got != "Bye! /Tomtebisse" {
		t.Errorf("text = %q, want the answer filled in and an unknown step empty", got)
	}
}

// A gate that turns the team back to the hub says so: nothing else on
// the phone changes.
func TestFailedGateBackToHubSaysNotYet(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Required: 1, Branches: []graph.Branch{
				{Port: "tower", Label: "Tower"}, {Port: "garden", Label: "Garden"},
			}}},
			{Slug: "key", Type: graph.Story},
			{Slug: "locked", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpSolved, Node: "key"}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "hub"},
			{From: "hub", FromPort: "tower", To: "key"},
			{From: "key", FromPort: "out", To: "hub"},
			{From: "hub", FromPort: "garden", To: "locked"},
			{From: "locked", FromPort: graph.PortPass, To: "end"},
			{From: "locked", FromPort: graph.PortFail, To: "hub"},
			{From: "hub", FromPort: graph.PortDone, To: "end"},
		},
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	res, st, err := tm.Act(func() (Result, *walk.Step, error) { return tm.Choose("hub", "garden") })
	if err != nil || st == nil || st.To != "hub" || !res.NotYet {
		t.Fatalf("a failed gate back at the hub is a not-yet: %+v %+v %v", res, st, err)
	}
	tm.Apply(*st)
	res, st, err = tm.Act(func() (Result, *walk.Step, error) { return tm.Choose("hub", "tower") })
	if err != nil || st == nil || st.To != "key" || res.NotYet {
		t.Fatalf("a branch with no gate is not: %+v %+v %v", res, st, err)
	}
}
