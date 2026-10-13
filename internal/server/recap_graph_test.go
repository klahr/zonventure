package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// A graph team's recap comes from its graph state, not the lane tables:
// points, rank among the event's teams, the steps it reached in graph
// order, the zones they are gated on as pins, misses and the quiz walk.
func TestRecap_GraphTeam(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	answer := func(v string) []match.Matcher { return []match.Matcher{{Kind: match.Exact, Value: v}} }
	g := graph.Graph{
		Zones: []graph.Zone{
			{Slug: "fountain", Name: "Fountain", Lat: 59.3326, Lng: 18.0649, RadiusM: 100},
			{Slug: "unused", Name: "Unused", Lat: 59.1, Lng: 18.1, RadiusM: 50},
		},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "p1", Type: graph.Puzzle, Title: "Fountain puzzle", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("What colour?", nil),
				Zone:    "fountain",
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: answer("blue")}},
			}},
			{Slug: "s", Type: graph.Story, Title: "Interlude", StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "a", Kind: graph.BlockQuiz, Text: "Capital?", Options: []string{"Oslo", "Stockholm", "Malmö"}, Answer: "X"},
			}}},
			{Slug: "p2", Type: graph.Puzzle, Title: "Last puzzle", PuzzleCfg: &graph.PuzzleConfig{
				Blocks: graph.LegacyBlocks("Say it.", nil),
				Outputs: []graph.Output{
					{Port: "good", Trigger: graph.OnAnswer, Points: 5, Matchers: answer("good")},
					{Port: "meh", Trigger: graph.OnAnswer, Matchers: answer("meh")},
				},
			}},
			{Slug: "unreached", Type: graph.Puzzle, Title: "Never seen", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("Nope.", nil),
				Zone:    "unused",
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: answer("x")}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "p1"},
			{From: "p1", FromPort: "solved", To: "s"},
			{From: "s", FromPort: "out", To: "p2"},
			{From: "p2", FromPort: "good", To: "end"},
			{From: "p2", FromPort: "meh", To: "end"},
			{From: "unreached", FromPort: "solved", To: "end"},
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
	for _, name := range []string{"Alpha", "Beta"} {
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

	// Alpha misses once, answers the quiz right and takes the full
	// points; Beta gets the quiz wrong and the pointless ending.
	play := func(name string, steps ...[3]string) {
		p := players[name]
		if s := p.do("POST", "/api/player/location", map[string]any{"lat": 59.3326, "lng": 18.0649}, nil); s != 204 {
			t.Fatalf("%s location = %d", name, s)
		}
		for _, st := range steps {
			var s int
			switch st[0] {
			case "answer":
				s = p.doAny("POST", "/api/player/play/answer", map[string]string{"node": st[1], "guess": st[2]}, nil)
			case "pick":
				s = p.doAny("POST", "/api/player/play/pick", map[string]string{"node": "s", "block": "a", "choice": st[2]}, nil)
			case "continue":
				s = p.doAny("POST", "/api/player/play/continue", map[string]string{"node": st[1]}, nil)
			}
			if s != 200 {
				t.Fatalf("%s %v = %d", name, st, s)
			}
		}
	}
	play("Alpha", [3]string{"answer", "p1", "green"}, [3]string{"answer", "p1", "blue"},
		[3]string{"pick", "s", "X"}, [3]string{"continue", "s"}, [3]string{"answer", "p2", "good"})
	play("Beta", [3]string{"answer", "p1", "blue"},
		[3]string{"pick", "s", "1"}, [3]string{"continue", "s"}, [3]string{"answer", "p2", "meh"})

	var detail struct {
		State string `json:"state"`
		Teams []struct {
			Name       string `json:"name"`
			RecapToken string `json:"recap_token"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &detail); s != 200 {
		t.Fatalf("event detail = %d", s)
	}
	tokens := map[string]string{}
	for _, tm := range detail.Teams {
		tokens[tm.Name] = tm.RecapToken
	}

	type recap struct {
		Points          *float64 `json:"points"`
		Rank            int      `json:"rank"`
		TeamCount       int      `json:"team_count"`
		Outcome         string   `json:"outcome"`
		AnswersRight    int      `json:"answers_right"`
		AnswersWrong    int      `json:"answers_wrong"`
		DurationSeconds int      `json:"duration_seconds"`
		StartedAt       string   `json:"started_at"`
		CompletedAt     string   `json:"completed_at"`
		Puzzles         []any    `json:"puzzles"`
		Steps           []struct {
			Node     string `json:"node"`
			Kind     string `json:"kind"`
			Title    string `json:"title"`
			Solved   bool   `json:"solved"`
			Attempts int    `json:"attempts"`
			Zone     string `json:"zone"`
			Quiz     bool   `json:"quiz"`
		} `json:"steps"`
		Zones []struct {
			Slug   string  `json:"slug"`
			Title  string  `json:"title"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Solved bool    `json:"solved"`
		} `json:"zones"`
		Quiz *struct {
			Right int `json:"right"`
			Total int `json:"total"`
		} `json:"quiz"`
	}
	get := func(name string) recap {
		status, body := readRecap(t, ts.URL, tokens[name])
		if status != http.StatusOK {
			t.Fatalf("%s recap = %d %s (event %s)", name, status, body, detail.State)
		}
		var r recap
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return r
	}

	a := get("Alpha")
	if a.Points == nil || *a.Points != 16 || a.Rank != 1 || a.TeamCount != 2 || a.Outcome != "complete" {
		t.Errorf("alpha score = %+v points %v", a, a.Points)
	}
	if a.AnswersRight != 2 || a.AnswersWrong != 1 {
		t.Errorf("alpha right/wrong = %d/%d, want 2/1", a.AnswersRight, a.AnswersWrong)
	}
	if a.Quiz == nil || a.Quiz.Right != 1 || a.Quiz.Total != 1 {
		t.Errorf("alpha quiz = %+v", a.Quiz)
	}
	if a.StartedAt == "" || a.CompletedAt == "" || len(a.Puzzles) != 0 {
		t.Errorf("alpha times/puzzles = %q %q %v", a.StartedAt, a.CompletedAt, a.Puzzles)
	}
	want := []struct {
		node, kind string
		solved     bool
		attempts   int
	}{{"p1", "puzzle", true, 1}, {"s", "story", true, 0}, {"p2", "puzzle", true, 0}}
	if len(a.Steps) != len(want) {
		t.Fatalf("alpha steps = %+v", a.Steps)
	}
	for i, w := range want {
		s := a.Steps[i]
		if s.Node != w.node || s.Kind != w.kind || s.Solved != w.solved || s.Attempts != w.attempts {
			t.Errorf("step %d = %+v, want %+v", i, s, w)
		}
	}
	// The story's quiz question was answered right: it is ticked like a puzzle.
	if !a.Steps[1].Quiz || a.Steps[0].Quiz {
		t.Errorf("quiz steps = %+v", a.Steps)
	}
	if a.Steps[0].Title != "Fountain puzzle" || a.Steps[0].Zone != "fountain" {
		t.Errorf("step 0 = %+v", a.Steps[0])
	}
	if len(a.Zones) != 1 || a.Zones[0].Slug != "fountain" || !a.Zones[0].Solved ||
		a.Zones[0].Lat != 59.3326 || a.Zones[0].Title != "Fountain puzzle" {
		t.Errorf("alpha zones = %+v: only the reached steps' zones are pins", a.Zones)
	}

	b := get("Beta")
	if b.Points == nil || *b.Points != 10 || b.Rank != 2 || b.TeamCount != 2 {
		t.Errorf("beta score = %v rank %d/%d", b.Points, b.Rank, b.TeamCount)
	}
	if b.AnswersRight != 2 || b.AnswersWrong != 0 || b.Quiz == nil || b.Quiz.Right != 0 || b.Quiz.Total != 1 {
		t.Errorf("beta counts = %d/%d quiz %+v", b.AnswersRight, b.AnswersWrong, b.Quiz)
	}
}

// A step that just moves on takes a wrong answer as it does a right one,
// without a word of either; the recap still tells them apart.
func TestRecap_QuietStep(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "p1", Type: graph.Puzzle, Title: "Colour", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:         graph.LegacyBlocks("What colour?", nil),
				Outputs:        []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "blue"}}}},
				Responses:      []graph.Response{{Text: "No."}},
				LockoutSeconds: 60,
				Quiet:          true,
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "p1"},
			{From: "p1", FromPort: "solved", To: "end"},
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
	for _, name := range []string{"Alpha", "Beta"} {
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
	for name, guess := range map[string]string{"Alpha": "blue", "Beta": "green"} {
		var res struct {
			Quiet    bool   `json:"quiet"`
			Port     string `json:"port"`
			Response string `json:"response"`
			View     struct {
				Finished bool `json:"finished"`
			} `json:"view"`
		}
		if s := players[name].doAny("POST", "/api/player/play/answer", map[string]string{"node": "p1", "guess": guess}, &res); s != 200 {
			t.Fatalf("%s answer = %d", name, s)
		}
		if !res.Quiet || res.Port != "solved" || res.Response != "" || !res.View.Finished {
			t.Errorf("%s answer = %+v", name, res)
		}
	}

	var detail struct {
		Teams []struct {
			Name       string `json:"name"`
			RecapToken string `json:"recap_token"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &detail); s != 200 {
		t.Fatalf("event detail = %d", s)
	}
	for _, tm := range detail.Teams {
		status, body := readRecap(t, ts.URL, tm.RecapToken)
		if status != http.StatusOK {
			t.Fatalf("%s recap = %d %s", tm.Name, status, body)
		}
		var r struct {
			Points       *float64 `json:"points"`
			Outcome      string   `json:"outcome"`
			AnswersRight int      `json:"answers_right"`
			AnswersWrong int      `json:"answers_wrong"`
			Steps        []struct {
				Solved   bool `json:"solved"`
				Attempts int  `json:"attempts"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatalf("decode: %v", err)
		}
		right := tm.Name == "Alpha"
		points, wrong := 10.0, 0
		if !right {
			points, wrong = 0, 1
		}
		if r.Points == nil || *r.Points != points || r.Outcome != "complete" || r.AnswersRight != 1-wrong || r.AnswersWrong != wrong ||
			len(r.Steps) != 1 || r.Steps[0].Solved != right || r.Steps[0].Attempts != wrong {
			t.Errorf("%s recap = %+v points %v", tm.Name, r, r.Points)
		}
	}
}
