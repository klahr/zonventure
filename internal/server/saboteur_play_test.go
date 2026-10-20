package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A saboteur over HTTP, with three players: dealt to one as the team
// first moves, every phone sees the sabotage and anyone may press it, the
// saboteur's press alone doing harm; the others name it, and the end
// reveals it to all. In an event with secret roles off
// no one is dealt it, and no one is asked.
func TestSaboteurOverHTTP(t *testing.T) {
	event, view, act, has := saboteurServer(t)

	// With secret roles off: the same adventure, no saboteur, no accusation.
	calm := event(false)
	if s := act(calm[0], "continue", map[string]string{"node": "intro"}); s != 200 {
		t.Fatalf("first move = %d", s)
	}
	for i, c := range calm {
		v := view(c)
		if has(v, "cut") || has(v, "who") {
			t.Errorf("secret roles off: player %d sees %v", i, v.Blocks)
		}
	}

	crew := event(true)
	t.Run("caught", func(t *testing.T) { playSaboteur(t, crew, view, act, has) })
}

type (
	viewFn func(*client) *session.View
	actFn  func(*client, string, map[string]string) int
	hasFn  func(*session.View, string) bool
)

// saboteurServer publishes the heist with a saboteur; see playServer.
func saboteurServer(t *testing.T) (func(secret bool) []*client, viewFn, actFn, hasFn) {
	return playServer(t, graph.Graph{
		Roles: []graph.Role{{Slug: "saboteur", Name: "Saboteur", Secret: true, Goals: []graph.RoleGoal{
			{Text: "Cost them points.", When: cond.Predicate{Op: cond.OpScore, Cmp: cond.Lt, N: 10}},
		}}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "intro", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Welcome, crew.", nil)}},
			{Slug: "heist", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{
					{ID: "cut", Kind: graph.BlockSabotage, Role: "saboteur", Text: "Cut the wire.", Worth: 3},
					{ID: "who", Kind: graph.BlockAccuse, Role: "saboteur", Text: "Who is the saboteur?", Worth: 20},
				},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer, Points: 5, Matchers: []match.Matcher{{Kind: match.Exact, Value: "go"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "intro"},
			{From: "intro", FromPort: "out", To: "heist"},
			{From: "heist", FromPort: "done", To: "end"},
		},
	})
}

// playServer publishes g as the demo adventure; its event func starts an
// event of one team of three, Ann, Bo and Cy, with secret roles on or
// off.
func playServer(t *testing.T, g graph.Graph) (func(secret bool) []*client, viewFn, actFn, hasFn) {
	ts, assignmentID, cleanup := newTestServer(t)
	t.Cleanup(cleanup)

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	arr := newClient(t, ts)
	arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil)
	login := func(token string) *client {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return c
	}
	// The seeded assignment backs the first event; later ones run on
	// the org's share of the demo.
	backing := map[string]any{"assignment_id": assignmentID}
	event := func(secret bool) []*client {
		var ev struct {
			ID int64 `json:"id"`
		}
		backing["attest_no_minors"] = true
		if s := arr.do("POST", "/api/admin/events", backing, &ev); s != 201 {
			t.Fatalf("create event = %d", s)
		}
		backing = map[string]any{"adventure_slug": "demo"}
		if !secret {
			if s := arr.do("PATCH", "/api/admin/events/"+itoa(ev.ID), map[string]any{"secret_roles": false}, nil); s != 200 && s != 204 {
				t.Fatalf("turning secret roles off = %d", s)
			}
		}
		var team struct {
			Members []struct {
				Token string `json:"token"`
			} `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo", "Cy"}}, &team); s != 201 {
			t.Fatalf("create team = %d", s)
		}
		var players []*client
		for _, m := range team.Members {
			players = append(players, login(m.Token))
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
			t.Fatalf("start = %d", s)
		}
		return players
	}
	view := func(c *client) *session.View {
		t := t
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		return st.View
	}
	act := func(c *client, kind string, body map[string]string) int {
		return c.doAny("POST", "/api/player/play/"+kind, body, nil)
	}
	has := func(v *session.View, id string) bool {
		for _, b := range v.Blocks {
			if b.ID == id && !b.Locked {
				return true
			}
		}
		return false
	}

	return event, view, act, has
}

func playSaboteur(t *testing.T, crew []*client, view viewFn, act actFn, has hasFn) {
	if s := act(crew[0], "continue", map[string]string{"node": "intro"}); s != 200 {
		t.Fatalf("first move = %d", s)
	}
	saboteur := -1
	for i, c := range crew {
		v := view(c)
		for _, r := range v.Roles {
			if r.Secret && r.Mine {
				saboteur = i
			}
		}
		if !has(v, "cut") {
			t.Errorf("player %d does not see the sabotage", i)
		}
		if !has(v, "who") {
			t.Errorf("player %d is not asked who the saboteur is", i)
		}
	}
	if saboteur < 0 {
		t.Fatal("no one was dealt the saboteur")
	}
	other := (saboteur + 1) % 3
	if s := act(crew[other], "sabotage", map[string]string{"node": "heist", "block": "cut"}); s != 200 {
		t.Fatalf("a decoy press = %d", s)
	}
	if v := view(crew[other]); v.PenaltyScore != 0 {
		t.Errorf("a decoy press cost the team %v", v.PenaltyScore)
	}
	if s := act(crew[other], "sabotage", map[string]string{"node": "heist", "block": "cut"}); s == 200 {
		t.Error("a player pressed the sabotage twice")
	}
	if s := act(crew[saboteur], "sabotage", map[string]string{"node": "heist", "block": "cut"}); s != 200 {
		t.Fatalf("sabotage = %d", s)
	}
	if v := view(crew[other]); v.PenaltyScore != 3 {
		t.Errorf("after sabotage the team has %v penalty, want 3", v.PenaltyScore)
	}

	// The other two name the saboteur; the saboteur names someone else.
	slotOf := func(i int) string {
		for _, s := range view(crew[(i+1)%3]).Blocks {
			for _, sus := range s.Suspects {
				if sus.Name == []string{"Ann", "Bo", "Cy"}[i] {
					return itoa(sus.Slot)
				}
			}
		}
		t.Fatalf("no slot for player %d", i)
		return ""
	}
	target := slotOf(saboteur)
	for i, c := range crew {
		choice := target
		if i == saboteur {
			choice = slotOf(other)
		}
		if s := act(c, "vote", map[string]string{"node": "heist", "block": "who", "choice": choice}); s != 200 {
			t.Fatalf("player %d accusing = %d", i, s)
		}
	}
	if s := act(crew[saboteur], "vote", map[string]string{"node": "heist", "block": "who", "choice": slotOf(saboteur)}); s == 200 {
		t.Error("the saboteur named themselves")
	}
	// The last team finishing ends the event, and its phones' sessions
	// with it: the answer's own reply carries the end.
	var res session.Result
	if s := crew[other].doAny("POST", "/api/player/play/answer", map[string]string{"node": "heist", "guess": "go"}, &res); s != 200 {
		t.Fatalf("finishing = %d", s)
	}
	v := &res.View
	if !v.Finished || v.Score != 25 {
		t.Errorf("finished %v with %v points, want 5 + 20 for naming the saboteur", v.Finished, v.Score)
	}
	if len(v.Reveal) != 1 || !v.Reveal[0].Dealt || v.Reveal[0].Holder != []string{"Ann", "Bo", "Cy"}[saboteur] || v.Reveal[0].Caught == nil || !*v.Reveal[0].Caught {
		t.Errorf("reveal: %+v", v.Reveal)
	}

}
