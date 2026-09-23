package session

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
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
				Prompt: "Left or right?",
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
			{Slug: "walk", Type: graph.Story, StoryCfg: &graph.StoryConfig{Text: "Walk to the park.", Zone: "park"}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Text: "Done."}},
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
			if *got != c.want {
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
			g.Nodes[i].PuzzleCfg.Media = []graph.Media{{ID: 1, Kind: graph.MediaImage}}
			g.Nodes[i].PuzzleCfg.Hints[0].Media = []graph.Media{{ID: 2, Kind: graph.MediaAudio}}
		case "walk":
			g.Nodes[i].StoryCfg.Media = []graph.Media{{ID: 3, Kind: graph.MediaImage}}
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
			{Slug: "a", Type: graph.Story, StoryCfg: &graph.StoryConfig{Text: "north route"}},
			{Slug: "b", Type: graph.Story, StoryCfg: &graph.StoryConfig{Text: "south route"}},
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
	if _, _, err := tm.ChooseStart("s-north"); !errors.Is(err, ErrNotHere) {
		t.Errorf("a zoned start away from its zone = %v, want ErrNotHere", err)
	}
	if _, _, err := tm.ChooseStart("a"); !errors.Is(err, ErrWrongNode) {
		t.Errorf("choosing a non-start = %v", err)
	}
	tm.Position = &Position{Lat: 59.34, Lng: 18.06}
	tm.Now = t0.Add(20 * time.Minute)
	_, step2, err := tm.ChooseStart("s-north")
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(*step2)
	if tm.Current != "a" || !tm.StartedAt.Equal(tm.Now) {
		t.Errorf("after choosing north: on %q, started %v", tm.Current, tm.StartedAt)
	}
	if _, _, err := tm.ChooseStart("s-south"); !errors.Is(err, ErrStale) {
		t.Errorf("choosing again = %v, want ErrStale", err)
	}
}
