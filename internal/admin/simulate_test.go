package admin

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

var simT0 = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

// simGraph is start → riddle → end, where the riddle's "left" output is
// wired but "right" is not — an unfinished draft, which is exactly what
// the simulator runs on.
func simGraph() *graph.Graph {
	return &graph.Graph{
		Zones: []graph.Zone{{Slug: "square", Lat: 59.3, Lng: 18.0, RadiusM: 20}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "riddle", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Prompt: "Left or right?",
				Outputs: []graph.Output{
					{Port: "left", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "left"}}},
					{Port: "right", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "right"}}},
				},
				Hints: []graph.Hint{{Text: "Your writing hand, probably.", DelaySeconds: 300}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "riddle"},
			{From: "riddle", FromPort: "left", To: "end"},
		},
	}
}

// roundTrip passes the team through JSON as the browser would.
func roundTrip(t *testing.T, tm *session.Team) *session.Team {
	t.Helper()
	b, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	var back session.Team
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	return &back
}

func sim(t *testing.T, tm *session.Team, a simulateAction) simulateResponse {
	t.Helper()
	var in *session.Team
	if tm != nil {
		in = roundTrip(t, tm)
	}
	resp, status, msg := simulate(simGraph(), in, a, simT0)
	if status != http.StatusOK {
		t.Fatalf("%s: status %d: %s", a.Kind, status, msg)
	}
	return resp
}

func TestSimulatePlaysTheDraft(t *testing.T) {
	r := sim(t, nil, simulateAction{Kind: "start"})
	if r.Result.View.Node != "riddle" || r.Result.View.Input == nil {
		t.Fatalf("start should land on the riddle: %+v", r.Result.View)
	}

	r = sim(t, r.Team, simulateAction{Kind: "hint", Node: "riddle", Index: 0})
	if r.Refuse == nil || r.Refuse.Code != "not_allowed" {
		t.Fatalf("a hint before its delay should be refused, got %+v", r.Refuse)
	}

	r = sim(t, r.Team, simulateAction{Kind: "wait", Seconds: 300})
	r = sim(t, r.Team, simulateAction{Kind: "hint", Node: "riddle", Index: 0})
	if r.Refuse != nil || r.Result.Hint == "" {
		t.Fatalf("after waiting, the hint should open: %+v", r)
	}

	r = sim(t, r.Team, simulateAction{Kind: "answer", Node: "riddle", Guess: "left"})
	if !r.Result.Accepted || !r.Result.View.Finished || r.Result.View.Score != 10 {
		t.Fatalf("left should finish with 10 points: %+v", r.Result)
	}
	if len(r.Passed) != 1 || r.Passed[0] != "end" {
		t.Errorf("passed = %v", r.Passed)
	}
}

// A right answer on an unconnected output must not leave the simulated
// team half-moved: the live engine would roll the transaction back.
func TestSimulateRollsBackAFailedStep(t *testing.T) {
	start := sim(t, nil, simulateAction{Kind: "start"})
	r := sim(t, start.Team, simulateAction{Kind: "answer", Node: "riddle", Guess: "right"})
	if r.Refuse == nil || r.Refuse.Code != "graph" {
		t.Fatalf("an unconnected output should be reported as a graph problem, got %+v", r.Refuse)
	}
	if r.Team.Solved("riddle") || r.Team.Current != "riddle" {
		t.Error("the failed step leaked into the team")
	}
	if r.Result.View.Node != "riddle" {
		t.Errorf("the view should still show the riddle, got %q", r.Result.View.Node)
	}
}

func TestSimulateMoveAndPlace(t *testing.T) {
	r := sim(t, nil, simulateAction{Kind: "place", Node: "riddle"})
	r = sim(t, r.Team, simulateAction{Kind: "move", Zone: "square"})
	if r.Team.Position == nil || !r.Team.InZone("square") {
		t.Fatal("move should put the team in the zone")
	}
	r = sim(t, r.Team, simulateAction{Kind: "move"})
	if r.Team.Position != nil {
		t.Error("move with no zone should put the team nowhere")
	}

	if _, status, _ := simulate(simGraph(), nil, simulateAction{Kind: "answer", Node: "riddle"}, simT0); status != http.StatusBadRequest {
		t.Errorf("an action without a team should be a 400, got %d", status)
	}
	if _, status, _ := simulate(simGraph(), nil, simulateAction{Kind: "place", Node: "nope"}, simT0); status != http.StatusBadRequest {
		t.Errorf("placing on a missing node should be a 400, got %d", status)
	}
}

// Passing a puzzle takes the chosen output without its answer, and a
// map pick puts the team at a point rather than a zone's centre.
func TestSimulateForceAndPoint(t *testing.T) {
	r := sim(t, nil, simulateAction{Kind: "start"})
	r = sim(t, r.Team, simulateAction{Kind: "force", Node: "riddle", Port: "left"})
	if !r.Result.View.Finished || r.Result.View.Score != 10 {
		t.Fatalf("forcing left should finish with its points: %+v", r.Result)
	}

	r = sim(t, nil, simulateAction{Kind: "start"})
	r = sim(t, r.Team, simulateAction{Kind: "force", Node: "riddle", Port: "nope"})
	if r.Refuse == nil || r.Refuse.Code != "not_allowed" {
		t.Errorf("forcing a port the puzzle lacks should be refused, got %+v", r.Refuse)
	}

	near, far := 59.30005, 59.31
	lng := 18.0
	r = sim(t, r.Team, simulateAction{Kind: "move", Lat: &near, Lng: &lng})
	if !r.Team.InZone("square") {
		t.Error("a point 5 m from the centre should be in the square")
	}
	r = sim(t, r.Team, simulateAction{Kind: "move", Lat: &far, Lng: &lng})
	if r.Team.Position == nil || r.Team.InZone("square") {
		t.Error("a point a kilometre off should be placed, but outside the square")
	}
	bad := 91.0
	if _, status, _ := simulate(simGraph(), roundTrip(t, r.Team), simulateAction{Kind: "move", Lat: &bad, Lng: &lng}, simT0); status != http.StatusBadRequest {
		t.Errorf("a latitude off the globe should be a 400, got %d", status)
	}
}
