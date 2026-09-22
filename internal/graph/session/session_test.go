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
