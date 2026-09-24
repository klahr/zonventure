package server_test

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A quiz walk over HTTP: picks are stored and final, a story waits for
// them, the right answer stays off the phone, and finishing scores the
// walk — with the tie-breaker ordering teams on the same score.
func TestQuizWalkOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	target := 11.0
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "a", Kind: graph.BlockQuiz, Text: "Capital?", Options: []string{"Oslo", "Stockholm", "Malmö"}, Answer: "X"},
				{ID: "tb", Kind: graph.BlockTiebreak, Text: "How many?", Target: &target},
			}}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Blocks: []graph.Block{{ID: "rank", Kind: graph.BlockStandings}}}},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "q"}, {From: "q", FromPort: "out", To: "end"}},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	var ev struct {
		ID        int64  `json:"id"`
		LiveToken string `json:"live_token"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	players := map[string]*client{}
	// Gamma never plays: the last team finishing completes the event and
	// ends every player session, and the checks below need live ones.
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		var team struct {
			Members []struct {
				Token string `json:"token"`
			} `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": name, "member_names": []string{name}}, &team); s != 201 {
			t.Fatalf("create team = %d", s)
		}
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + team.Members[0].Token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		players[name] = c
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	quiz := func(v *session.View) (q session.BlockView) {
		for _, b := range v.Blocks {
			if b.ID == "a" {
				q = b
			}
		}
		return q
	}
	var st playStateDTO
	if s := players["Alpha"].doAny("GET", "/api/player/play", nil, &st); s != 200 || quiz(st.View).Answer != "" {
		t.Fatalf("the right answer must stay off the phone: %d %+v", s, quiz(st.View))
	}
	var refused playRefusalDTO
	if s := players["Alpha"].doAny("POST", "/api/player/play/continue", map[string]string{"node": "q"}, &refused); s != 403 || refused.Code != "unanswered" {
		t.Fatalf("continue before answering = %d %q", s, refused.Code)
	}
	for name, guess := range map[string]string{"Alpha": "10", "Beta": "13"} {
		p := players[name]
		for _, pick := range []map[string]string{{"node": "q", "block": "a", "choice": "X"}, {"node": "q", "block": "tb", "choice": guess}} {
			if s := p.doAny("POST", "/api/player/play/pick", pick, nil); s != 200 {
				t.Fatalf("%s pick %v = %d", name, pick, s)
			}
		}
	}
	if s := players["Alpha"].doAny("POST", "/api/player/play/pick", map[string]string{"node": "q", "block": "a", "choice": "1"}, nil); s != 403 {
		t.Errorf("a second pick = %d, want 403", s)
	}
	st = playStateDTO{}
	if s := players["Alpha"].doAny("GET", "/api/player/play", nil, &st); s != 200 || quiz(st.View).Picked != "X" || st.View.Score != 0 {
		t.Fatalf("picks are stored and score nothing yet: %d %+v score %v", s, quiz(st.View), st.View.Score)
	}
	for _, name := range []string{"Alpha", "Beta"} {
		if s := players[name].doAny("POST", "/api/player/play/continue", map[string]string{"node": "q"}, nil); s != 200 {
			t.Fatalf("%s continue = %d", name, s)
		}
	}
	for name, rank := range map[string]int{"Alpha": 1, "Beta": 2} {
		st = playStateDTO{}
		if s := players[name].doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("%s end = %d", name, s)
		}
		if q := st.View.Quiz; q == nil || q.Right != 1 || q.Total != 1 || st.View.Score != 1 {
			t.Errorf("%s quiz = %+v, score %v", name, q, st.View.Score)
		}
		var standings *session.Standings
		for _, b := range st.View.Blocks {
			if b.Kind == graph.BlockStandings {
				standings = b.Standings
			}
		}
		if standings == nil || standings.Rank != rank {
			t.Errorf("%s standings = %+v, want rank %d: the closer tie-break guess wins a tie", name, standings, rank)
		}
	}

	// The TV leaderboard and the results export rank the same way. Gamma
	// started with the event, so it is ranked too, on no points.
	var live struct {
		Engine string `json:"engine"`
		Teams  []struct {
			Name          string   `json:"name"`
			Score         *float64 `json:"score"`
			TiebreakGuess *float64 `json:"tiebreak_guess"`
		} `json:"teams"`
	}
	if s := newClient(t, ts).do("GET", "/api/public/live/"+ev.LiveToken, nil, &live); s != 200 || live.Engine != "graph" || len(live.Teams) != 3 {
		t.Fatalf("live = %d %+v", s, live)
	}
	if live.Teams[0].Name != "Alpha" || live.Teams[1].Name != "Beta" || live.Teams[2].Name != "Gamma" ||
		live.Teams[0].Score == nil || *live.Teams[0].Score != 1 || live.Teams[2].Score == nil || *live.Teams[2].Score != 0 {
		t.Errorf("live board order and points = %+v", live.Teams)
	}
	if live.Teams[0].TiebreakGuess != nil {
		t.Error("tie-break guesses stay off the board while the event runs")
	}
	resp, err := arr.c.Get(ts.URL + "/api/admin/events/" + itoa(ev.ID) + "/results.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	recs, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"rank", "team", "points", "tiebreak_guess", "tiebreak_off_by"},
		{"1", "Alpha", "1", "10", "1"},
		{"2", "Beta", "1", "13", "2"},
		{"3", "Gamma", "0", "", ""},
	}
	if len(recs) != len(want) {
		t.Fatalf("csv = %q", recs)
	}
	for i, w := range want {
		got := []string{recs[i][0], recs[i][1], recs[i][3], recs[i][4], recs[i][5]}
		if strings.Join(got, "|") != strings.Join(w, "|") {
			t.Errorf("csv row %d = %q, want %q", i, got, w)
		}
	}
}
