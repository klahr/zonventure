package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// An event's statistics count every team that played without naming
// any: guesses per puzzle, first-try solves, and the commonest wrong
// guesses folded across case and space.
func TestEventStatistics_Graph(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	answer := func(v string) []match.Matcher { return []match.Matcher{{Kind: match.Exact, Value: v}} }
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "p1", Type: graph.Puzzle, Title: "Colour", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("What colour?", nil),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: answer("blue")}},
			}},
			{Slug: "p2", Type: graph.Puzzle, Title: "Animal", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("Which animal?", nil),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 5, Matchers: answer("cat")}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "p1"},
			{From: "p1", FromPort: "solved", To: "p2"},
			{From: "p2", FromPort: "solved", To: "end"},
		},
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
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	players := map[string]*client{}
	join := func(name, token string) {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		players[name] = c
	}
	for _, names := range [][]string{{"Alpha", "Alpha 2"}, {"Beta"}} {
		var team struct {
			Members []struct {
				Token string `json:"token"`
			} `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": names[0], "member_names": names}, &team); s != 201 {
			t.Fatalf("create team = %d", s)
		}
		for i, name := range names {
			join(name, team.Members[i].Token)
		}
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	guess := func(name, node, v string) {
		if s := players[name].doAny("POST", "/api/player/play/answer", map[string]string{"node": node, "guess": v}, nil); s != 200 {
			t.Fatalf("%s %s %q = %d", name, node, v, s)
		}
	}
	// Pings are rate-limited per player, so Alpha's walk is one from each.
	for name, lat := range map[string]float64{"Alpha": 59.3326, "Alpha 2": 59.3336} {
		if s := players[name].do("POST", "/api/player/location", map[string]any{"lat": lat, "lng": 18.0649}, nil); s != 204 {
			t.Fatalf("location = %d", s)
		}
	}
	// Both teams guess "Red" first on p1, one of them twice; only Alpha
	// solves p2.
	guess("Alpha", "p1", "Red")
	guess("Alpha", "p1", "blue")
	guess("Alpha", "p2", "cat")
	guess("Beta", "p1", " red ")
	guess("Beta", "p1", "green")
	guess("Beta", "p1", "RED")
	guess("Beta", "p1", "blue")

	var st struct {
		Teams    int      `json:"teams"`
		Finished int      `json:"finished"`
		Guesses  int      `json:"guesses"`
		Wrong    int      `json:"wrong"`
		FirstTry int      `json:"first_try"`
		Distance *float64 `json:"avg_distance_m"`
		Puzzles  []struct {
			Node         string `json:"node"`
			Title        string `json:"title"`
			Reached      int    `json:"reached"`
			Solved       int    `json:"solved"`
			FirstTry     int    `json:"first_try"`
			Wrong        int    `json:"wrong"`
			WrongAnswers []struct {
				Answer string `json:"answer"`
				Count  int    `json:"count"`
				Teams  int    `json:"teams"`
			} `json:"wrong_answers"`
		} `json:"puzzles"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/statistics", nil, &st); s != 200 {
		t.Fatalf("statistics = %d", s)
	}
	if st.Teams != 2 || st.Finished != 1 || st.Wrong != 4 || st.Guesses != 7 || st.FirstTry != 1 {
		t.Fatalf("totals = %+v", st)
	}
	// Alpha walked 0.001° of latitude, about 111 m; Beta sent no pings.
	if st.Distance == nil {
		t.Fatal("no avg distance")
	}
	if *st.Distance < 105 || *st.Distance > 117 {
		t.Fatalf("avg distance = %v", *st.Distance)
	}
	if len(st.Puzzles) != 2 || st.Puzzles[0].Node != "p1" || st.Puzzles[0].Title != "Colour" {
		t.Fatalf("puzzles = %+v", st.Puzzles)
	}
	p1, p2 := st.Puzzles[0], st.Puzzles[1]
	if p1.Reached != 2 || p1.Solved != 2 || p1.FirstTry != 0 || p1.Wrong != 4 {
		t.Fatalf("p1 = %+v", p1)
	}
	if len(p1.WrongAnswers) != 2 || p1.WrongAnswers[0].Answer != "red" || p1.WrongAnswers[0].Count != 3 || p1.WrongAnswers[0].Teams != 2 || p1.WrongAnswers[1].Answer != "green" {
		t.Fatalf("p1 wrong answers = %+v", p1.WrongAnswers)
	}
	if p2.Reached != 2 || p2.Solved != 1 || p2.FirstTry != 1 || p2.Wrong != 0 || len(p2.WrongAnswers) != 0 {
		t.Fatalf("p2 = %+v", p2)
	}

}
