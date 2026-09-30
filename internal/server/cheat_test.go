package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A tester with the adventure's cheat password solves a puzzle without
// answering it, for no points, and the team drops out of the ranking.
// Without the password, or once it is removed, nothing is unlocked.
func TestGraphCheat(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hello", Type: graph.Puzzle, Title: "Greeting", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("Say hello.", nil),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "hello"}}}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Well done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "hello"},
			{From: "hello", FromPort: "solved", To: "end"},
		},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"cheat_password": "letmein"}, nil); s/100 != 2 {
		t.Fatalf("set cheat password = %d", s)
	}
	var edit map[string]any
	if s := pub.doAny("GET", "/api/admin/adventures/demo/edit", nil, &edit); s != 200 || edit["cheat_password"] != "letmein" {
		t.Fatalf("the editor should show the password: %d %v", s, edit["cheat_password"])
	}

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	type teamDTO struct {
		ID      int64 `json:"id"`
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	var alpha, beta teamDTO
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Alpha", "member_names": []string{"alice"}}, &alpha); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Beta", "member_names": []string{"carol"}}, &beta); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	// Gamma never plays: the last team finishing completes the event and
	// signs every player out, and the checks at the end need Alpha's.
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Gamma", "member_names": []string{"dave"}}, nil); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}

	alice := newClient(t, ts)
	if s := alice.do("GET", "/play?t="+alpha.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("alice login = %d", s)
	}
	var st struct {
		Event struct {
			Cheat bool `json:"cheat"`
		} `json:"event"`
	}
	if s := alice.doAny("GET", "/api/player/play", nil, &st); s != 200 || !st.Event.Cheat {
		t.Fatalf("the phone should know a cheat password is set: %d %+v", s, st)
	}

	var refused playRefusalDTO
	if s := alice.doAny("POST", "/api/player/play/unlock", map[string]string{"password": "nope"}, &refused); s != 403 || refused.Code != "bad_password" {
		t.Fatalf("unlock with a wrong password = %d %+v", s, refused)
	}
	if s := alice.doAny("POST", "/api/player/play/unlock", map[string]string{"password": "letmein"}, nil); s != 204 {
		t.Fatalf("unlock = %d", s)
	}
	if s := alice.doAny("POST", "/api/player/play/cheat", map[string]string{"node": "hello", "password": "nope"}, &refused); s != 403 || refused.Code != "bad_password" {
		t.Fatalf("cheat with a wrong password = %d %+v", s, refused)
	}
	var res session.Result
	if s := alice.doAny("POST", "/api/player/play/cheat", map[string]string{"node": "hello", "password": "letmein"}, &res); s != 200 || !res.Accepted || res.Points != 0 || !res.View.Finished {
		t.Fatalf("cheat = %d %+v", s, res)
	}

	carol := newClient(t, ts)
	if s := carol.do("GET", "/play?t="+beta.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("carol login = %d", s)
	}
	if s := carol.doAny("POST", "/api/player/play/answer", map[string]string{"node": "hello", "guess": "hello"}, &res); s != 200 || !res.Accepted {
		t.Fatalf("beta's answer = %d %+v", s, res)
	}

	var detail struct {
		Teams []struct {
			ID      int64 `json:"id"`
			Rank    int   `json:"rank"`
			Cheated bool  `json:"cheated"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &detail); s != 200 {
		t.Fatalf("event detail = %d", s)
	}
	for _, tm := range detail.Teams {
		switch tm.ID {
		case alpha.ID:
			if !tm.Cheated || tm.Rank != 0 {
				t.Errorf("alpha cheated: %+v", tm)
			}
		case beta.ID:
			if tm.Cheated || tm.Rank != 1 {
				t.Errorf("beta played fair and should rank first: %+v", tm)
			}
		}
	}

	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"cheat_password": ""}, nil); s/100 != 2 {
		t.Fatalf("remove cheat password = %d", s)
	}
	st.Event.Cheat = false
	if s := alice.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.Event.Cheat {
		t.Fatalf("after removing the password the phone should not offer it: %d %+v", s, st)
	}
	if s := alice.doAny("POST", "/api/player/play/unlock", map[string]string{"password": "letmein"}, &refused); s != 403 {
		t.Fatalf("unlock after removal = %d", s)
	}
}
