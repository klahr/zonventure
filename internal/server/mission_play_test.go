package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
)

// A mission over HTTP, with three players: two go to the post and pick,
// the police betraying; the third cannot pick from afar, the team waits
// until both have, and the next step shows the tampered key. (A location
// may be posted only so often, so no one moves twice.)
func TestMissionOverHTTP(t *testing.T) {
	betrayed := cond.Predicate{Op: cond.OpBetrayed, Node: "pickup", Block: "pkg"}
	event, view, act, has := playServer(t, graph.Graph{
		Roles: []graph.Role{{Slug: "police", Name: "Police", Secret: true}},
		Zones: []graph.Zone{{Slug: "bridge", Lat: 59.33, Lng: 18.06, RadiusM: 50}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "intro", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Welcome, crew.", nil)}},
			{Slug: "pickup", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  []graph.Block{{ID: "pkg", Kind: graph.BlockMission, Role: "police", Zone: "bridge", Count: 2, Text: "Seal the package."}},
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
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "intro"},
			{From: "intro", FromPort: "out", To: "pickup"},
			{From: "pickup", FromPort: "done", To: "vault"},
			{From: "vault", FromPort: "done", To: "end"},
		},
	})
	crew := event(true)
	if s := act(crew[0], "continue", map[string]string{"node": "intro"}); s != 200 {
		t.Fatalf("first move = %d", s)
	}
	police := -1
	for i, c := range crew {
		for _, r := range view(c).Roles {
			if r.Secret && r.Mine {
				police = i
			}
		}
	}
	if police < 0 {
		t.Fatal("no one was dealt the police")
	}
	thief, third := (police+1)%3, (police+2)%3
	at := func(i int, lat float64) {
		if s := crew[i].doAny("POST", "/api/player/location", map[string]any{"lat": lat, "lng": 18.06, "accuracy": 5}, nil); s != 200 && s != 204 {
			t.Fatalf("player %d location = %d", i, s)
		}
	}
	seal := func(i int, choice string) int {
		return act(crew[i], "seal", map[string]string{"node": "pickup", "block": "pkg", "choice": choice})
	}

	at(police, 59.33)
	at(thief, 59.33)
	at(third, 59.40)
	if s := seal(third, "0"); s == 200 {
		t.Error("a player away from the post picked")
	}
	if s := act(crew[third], "answer", map[string]string{"node": "pickup", "guess": "go"}); s != 403 {
		t.Errorf("answering before the mission is sealed = %d, want 403", s)
	}
	if s := seal(police, "1"); s != 200 {
		t.Fatalf("police picking = %d", s)
	}
	if s := seal(police, "0"); s == 200 {
		t.Error("the police picked twice")
	}
	if s := seal(thief, "0"); s != 200 {
		t.Fatalf("thief picking = %d", s)
	}
	mission := func(i int) (seals, need int, sealed bool) {
		for _, b := range view(crew[i]).Blocks {
			if b.ID == "pkg" {
				return b.Seals, b.Need, b.Sealed
			}
		}
		t.Fatalf("player %d has no mission", i)
		return
	}
	ps, pn, pd := mission(police)
	ts, tn, td := mission(thief)
	if ps != 2 || pn != 2 || !pd || ps != ts || pn != tn || pd != td {
		t.Errorf("police sees %d/%d sealed %v, thief %d/%d sealed %v", ps, pn, pd, ts, tn, td)
	}

	if s := act(crew[third], "answer", map[string]string{"node": "pickup", "guess": "go"}); s != 200 {
		t.Fatalf("answering once sealed = %d", s)
	}
	if v := view(crew[third]); !has(v, "fake") || has(v, "real") {
		t.Errorf("after a betrayal the vault shows %v", v.Blocks)
	}
}
