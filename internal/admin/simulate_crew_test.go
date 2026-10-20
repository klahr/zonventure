package admin

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

func crewGraph() *graph.Graph {
	betrayed := cond.Predicate{Op: cond.OpBetrayed, Node: "pickup", Block: "pkg"}
	return &graph.Graph{
		Roles: []graph.Role{{Slug: "scout", Name: "Scout"}, {Slug: "police", Name: "Police", Secret: true}},
		Zones: []graph.Zone{{Slug: "bridge", Lat: 59.3, Lng: 18.0, RadiusM: 30}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "pickup", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{
					{ID: "pkg", Kind: graph.BlockMission, Role: "police", Zone: "bridge", Count: 2, Text: "Seal the package."},
					{ID: "map", Kind: graph.BlockText, Text: "The way.", Show: &graph.Show{When: cond.Predicate{Op: cond.OpRole, Role: "scout"}}},
				},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "go"}}}},
			}},
			{Slug: "vault", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{
					{ID: "real", Kind: graph.BlockText, Text: "Add 3.", Show: &graph.Show{When: cond.Predicate{Op: cond.OpNot, Of: []cond.Predicate{betrayed}}}},
					{ID: "fake", Kind: graph.BlockText, Text: "Add 5.", Show: &graph.Show{When: betrayed}},
				},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "7"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "pickup"}, {From: "pickup", FromPort: "done", To: "vault"}, {From: "vault", FromPort: "done", To: "end"}},
	}
}

// A crew plays as several phones: each sees what its roles show, each
// moves and picks on its own, and every phone's screen comes back with
// each action. A refused action leaves the crew as it was.
func TestSimulateCrew(t *testing.T) {
	t.Parallel()
	var (
		team *session.Team
		crew = &simCrew{
			Players: []simPlayer{{Slot: 1, Name: "Ann"}, {Slot: 2, Name: "Bo"}, {Slot: 3, Name: "Cy"}},
			Secrets: map[string]int64{"police": 2},
		}
	)
	do := func(as int64, a simulateAction) simulateResponse {
		t.Helper()
		var in *session.Team
		if team != nil {
			in = roundTrip(t, team)
		}
		b, _ := json.Marshal(crew)
		var c simCrew
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		c.As = as
		r, status, msg := simulateCrew(crewGraph(), nil, in, &c, a, simT0)
		if status != http.StatusOK {
			t.Fatalf("%s as %d: status %d: %s", a.Kind, as, status, msg)
		}
		team, crew = r.Team, r.Crew
		return r
	}
	shows := func(r simulateResponse, slot int64, id string) bool {
		for _, p := range r.Phones {
			if p.Slot == slot {
				return slices.ContainsFunc(p.View.Blocks, func(b session.BlockView) bool { return b.ID == id })
			}
		}
		t.Fatalf("no phone %d", slot)
		return false
	}

	r := do(1, simulateAction{Kind: "start"})
	if len(r.Phones) != 3 || !shows(r, 1, "map") || shows(r, 2, "map") {
		t.Fatalf("the scout's block on the wrong phones: %+v", r.Phones)
	}
	if !slices.Contains(r.Phones[1].Roles, "police") || slices.Contains(r.Phones[0].Roles, "police") {
		t.Errorf("roles: %+v", r.Phones)
	}
	if r = do(3, simulateAction{Kind: "answer", Node: "pickup", Guess: "go"}); r.Refuse == nil || r.Refuse.Code != "unsealed" {
		t.Errorf("answering before the mission is sealed: %+v", r.Refuse)
	}
	if r = do(1, simulateAction{Kind: "seal", Node: "pickup", Block: "pkg", Choice: "0"}); r.Refuse == nil || r.Refuse.Code != "not_here" {
		t.Errorf("picking away from the post: %+v", r.Refuse)
	}
	do(1, simulateAction{Kind: "move", Zone: "bridge"})
	do(2, simulateAction{Kind: "move", Zone: "bridge"})
	do(1, simulateAction{Kind: "seal", Node: "pickup", Block: "pkg", Choice: "0"})
	if r = do(1, simulateAction{Kind: "seal", Node: "pickup", Block: "pkg", Choice: "1"}); r.Refuse == nil {
		t.Error("a second pick was taken")
	}
	r = do(2, simulateAction{Kind: "seal", Node: "pickup", Block: "pkg", Choice: "1"})
	for _, p := range r.Phones {
		for _, b := range p.View.Blocks {
			if b.ID == "pkg" && (b.Seals != 2 || b.Sealed != (p.Slot != 3)) {
				t.Errorf("phone %d's mission: %+v", p.Slot, b)
			}
		}
	}
	r = do(3, simulateAction{Kind: "answer", Node: "pickup", Guess: "go"})
	if r.Refuse != nil || !shows(r, 3, "fake") || shows(r, 1, "real") {
		t.Errorf("after the police betrayed: %+v %+v", r.Refuse, r.Phones)
	}
}

// Secret roles nobody was given are dealt once the crew is three; with
// fewer no one holds them.
func TestSimulateCrewDeals(t *testing.T) {
	t.Parallel()
	two := &simCrew{Players: []simPlayer{{Slot: 1}, {Slot: 2}}}
	r, _, _ := simulateCrew(crewGraph(), nil, nil, two, simulateAction{Kind: "start"}, simT0)
	if len(r.Crew.Secrets) != 0 {
		t.Errorf("two players were dealt %v", r.Crew.Secrets)
	}
	three := &simCrew{Players: []simPlayer{{Slot: 1}, {Slot: 2}, {Slot: 3}}}
	r, _, _ = simulateCrew(crewGraph(), nil, nil, three, simulateAction{Kind: "start"}, simT0)
	if s := r.Crew.Secrets["police"]; s < 1 || s > 3 {
		t.Errorf("three players: police dealt to %d", s)
	}
	if _, status, _ := simulateCrew(crewGraph(), nil, nil, &simCrew{Players: []simPlayer{{Slot: 1}, {Slot: 1}}}, simulateAction{Kind: "start"}, simT0); status != http.StatusBadRequest {
		t.Errorf("two players on one slot: %d", status)
	}
}

// A split secret is dealt round the crew's phones, each its own part, as
// on real phones; the team as a whole sees every part.
func TestSimulateCrewSplit(t *testing.T) {
	t.Parallel()
	g := crewGraph()
	g.Nodes[1].PuzzleCfg.Blocks = append(g.Nodes[1].PuzzleCfg.Blocks, graph.Block{ID: "msg", Kind: graph.BlockSplit, Parts: []string{"one", "two", "three"}})
	crew := &simCrew{Players: []simPlayer{{Slot: 1}, {Slot: 2}, {Slot: 3}}, As: 2}
	r, status, msg := simulateCrew(g, nil, nil, crew, simulateAction{Kind: "start"}, simT0)
	if status != http.StatusOK {
		t.Fatalf("start: %d %s", status, msg)
	}
	parts := func(v session.View) []string {
		for _, b := range v.Blocks {
			if b.ID == "msg" {
				return b.Parts
			}
		}
		return nil
	}
	for i, p := range r.Phones {
		if got := parts(p.View); len(got) != 1 || got[0] != []string{"one", "two", "three"}[i] {
			t.Errorf("phone %d sees %v", p.Slot, got)
		}
	}
	if got := parts(r.Result.View); !slices.Equal(got, []string{"two"}) {
		t.Errorf("the acting phone's view shows %v, want its own part", got)
	}
	whole, _, _ := simulate(g, nil, nil, simulateAction{Kind: "start"}, simT0)
	if got := parts(whole.Result.View); len(got) != 3 {
		t.Errorf("the team as a whole sees %v, want every part", got)
	}
}

// Each crew phone sees its own roles marked as its own, public and
// secret, as a player's phone does.
func TestSimulateCrewRoles(t *testing.T) {
	t.Parallel()
	crew := &simCrew{Players: []simPlayer{{Slot: 1, Name: "Ann"}, {Slot: 2, Name: "Bo"}, {Slot: 3, Name: "Cy"}}, Secrets: map[string]int64{"police": 2}}
	r, _, _ := simulateCrew(crewGraph(), nil, nil, crew, simulateAction{Kind: "start"}, simT0)
	mine := func(p simPhone) (out []string) {
		for _, ro := range p.View.Roles {
			if ro.Mine {
				out = append(out, ro.Slug)
			}
		}
		return out
	}
	for _, p := range r.Phones {
		if !slices.Equal(mine(p), p.Roles) {
			t.Errorf("phone %d marks %v as its own, holds %v", p.Slot, mine(p), p.Roles)
		}
	}
}
