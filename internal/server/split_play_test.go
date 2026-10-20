package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
)

// splitPlayGraph: the scout's group to pa, the others' to pb, meeting at
// the square; then the vault, together.
func splitPlayGraph() graph.Graph {
	right := []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Points: 10, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}}
	return graph.Graph{
		Roles: []graph.Role{{Slug: "scout", Name: "Scout"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "split", Type: graph.Split, Title: "Split up", SplitCfg: &graph.SplitConfig{Text: "Go.", Paths: []graph.SplitPath{
				{Port: "a", Role: "scout", Label: "Church"}, {Port: "b", Label: "Well"},
			}}},
			{Slug: "pa", Type: graph.Puzzle, Title: "Church", PuzzleCfg: &graph.PuzzleConfig{Outputs: right}},
			{Slug: "pb", Type: graph.Puzzle, Title: "Well", PuzzleCfg: &graph.PuzzleConfig{Outputs: right}},
			{Slug: "join", Type: graph.Join, Title: "Square"},
			{Slug: "vault", Type: graph.Puzzle, Title: "Vault", PuzzleCfg: &graph.PuzzleConfig{Outputs: right}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "split"},
			{From: "split", FromPort: "a", To: "pa"}, {From: "split", FromPort: "b", To: "pb"},
			{From: "pa", FromPort: "ok", To: "join"}, {From: "pb", FromPort: "ok", To: "join"},
			{From: "join", FromPort: "out", To: "vault"}, {From: "vault", FromPort: "ok", To: "end"},
		},
	}
}

// A split through the real endpoints: the graph saved and published as
// an author's would be, three players' phones logged in by their links,
// each shown its own group, refused another's step with the code the
// phone reads, and together again after the join.
func TestSplitOverHTTP(t *testing.T) {
	event, view, act, _ := playServer(t, splitPlayGraph())
	crew := event(false) // Ann, Bo, Cy: Ann the scout; Bo to the well, Cy to the church
	ann, bo, cy := crew[0], crew[1], crew[2]

	v := view(ann)
	if v.Type != graph.Split || v.Group != "Church" || v.Text != "Go." || len(v.Groups) != 2 {
		t.Fatalf("Ann's view: %+v", v)
	}
	if v := view(bo); v.Group != "Well" || !v.Groups[1].Mine || v.Groups[1].Players != 1 {
		t.Fatalf("Bo's view: %+v", v)
	}
	if v := view(cy); v.Group != "Church" || v.Groups[0].Players != 2 {
		t.Fatalf("Cy's view: %+v", v)
	}

	if s := act(bo, "continue", map[string]string{"node": "split"}); s != 200 {
		t.Fatalf("Bo sets off: %d", s)
	}
	if v := view(bo); v.Node != "pb" {
		t.Fatalf("Bo at %q", v.Node)
	}
	// Ann acting on the well's step: refused, with the code the phone
	// shows as another's to do.
	var ref playRefusalDTO
	if s := ann.doAny("POST", "/api/player/play/answer", map[string]string{"node": "pb", "guess": "x"}, &ref); s != 403 || ref.Code != "not_yours" {
		t.Fatalf("Ann at the well's step: %d %q", s, ref.Code)
	}
	if s := act(bo, "answer", map[string]string{"node": "pb", "guess": "x"}); s != 200 {
		t.Fatalf("Bo answers: %d", s)
	}
	if v := view(bo); v.Type != graph.Join || !v.Groups[1].Arrived || v.Groups[0].Arrived {
		t.Fatalf("Bo waits at the square: %+v", v)
	}
	if s := act(cy, "continue", map[string]string{"node": "split"}); s != 200 {
		t.Fatalf("Cy sets off: %d", s)
	}
	// Ann, a moment late: stale, the phone's "the team has moved on".
	if s := ann.doAny("POST", "/api/player/play/continue", map[string]string{"node": "split"}, &ref); s != 409 || ref.Code != "stale" {
		t.Fatalf("Ann setting off late: %d %q", s, ref.Code)
	}
	if v := view(ann); v.Node != "pa" {
		t.Fatalf("Ann is with Cy at the church: %+v", v)
	}
	if s := act(ann, "answer", map[string]string{"node": "pa", "guess": "x"}); s != 200 {
		t.Fatalf("Ann answers: %d", s)
	}
	for i, c := range crew {
		if v := view(c); v.Node != "vault" || v.Groups != nil || v.Score != 20 {
			t.Fatalf("player %d after the join: %+v", i, v)
		}
	}
	// The last answer finishes the team, and with it this one-team event,
	// which ends the phones' sessions: read the finish from the answer.
	var res session.Result
	if s := bo.doAny("POST", "/api/player/play/answer", map[string]string{"node": "vault", "guess": "x"}, &res); s != 200 {
		t.Fatalf("vault: %d", s)
	}
	if !res.View.Finished || res.View.Score != 30 {
		t.Fatalf("finished: %+v", res.View)
	}
}

// The designer's save refuses a split whose paths cross, as the
// validator says, before anything reaches a team.
func TestSplitCrossingRefusedOnSave(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	t.Cleanup(cleanup)
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := splitPlayGraph()
	for i, e := range g.Edges {
		if e.From == "pa" {
			g.Edges[i].To = "pb" // the church party walks on to the well's step
		}
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save draft = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s == 200 {
		t.Fatal("published a split whose paths cross")
	}
}

// The organisers see where each group of a split team is, and can move
// one stuck group on by itself through the console's rescue.
func TestConsoleRescuesAGroup(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: splitPlayGraph()}, nil); s != 200 {
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
	var team struct {
		ID      int64 `json:"id"`
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	var phones []*client
	for _, m := range team.Members {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + m.Token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		phones = append(phones, c)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	// Bo's group (the well) sets off and gets stuck at its puzzle.
	if s := phones[1].doAny("POST", "/api/player/play/continue", map[string]string{"node": "split"}, nil); s != 200 {
		t.Fatalf("Bo sets off = %d", s)
	}
	type detail struct {
		Teams []struct {
			Node   string `json:"node"`
			Groups []struct {
				Port, Label, Node string
			} `json:"groups"`
		} `json:"teams"`
		Graph *struct {
			Rescuable []string `json:"rescuable"`
		} `json:"graph"`
	}
	var ed detail
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &ed); s != 200 || len(ed.Teams) != 1 {
		t.Fatalf("event detail = %d %+v", s, ed)
	}
	tm := ed.Teams[0]
	if tm.Node != "split" || len(tm.Groups) != 2 || tm.Groups[0].Label != "Church" || tm.Groups[0].Node != "split" || tm.Groups[1].Node != "pb" {
		t.Fatalf("console team = %+v", tm)
	}
	// The split itself is no puzzle to move past; the group's step is.
	if s := arr.do("POST", "/api/admin/teams/"+itoa(team.ID)+"/unlock", map[string]string{"node": "split"}, nil); s != 409 {
		t.Errorf("rescuing the split = %d, want 409", s)
	}
	if s := arr.do("POST", "/api/admin/teams/"+itoa(team.ID)+"/unlock", map[string]string{"node": "pb"}, nil); s != 200 {
		t.Fatalf("rescuing the well's group = %d", s)
	}
	ed = detail{}
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &ed)
	if g := ed.Teams[0].Groups; len(g) != 2 || g[1].Node != "join" {
		t.Fatalf("after the rescue: %+v", ed.Teams[0])
	}
	var st playStateDTO
	if s := phones[1].doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View.Type != graph.Join {
		t.Fatalf("Bo's phone after the rescue = %d %+v", s, st.View)
	}
}
