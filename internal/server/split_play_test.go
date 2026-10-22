package server_test

import (
	"slices"
	"testing"
	"time"

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
	if s := ann.doAny("POST", "/api/player/play/answer", map[string]string{"node": "pb", "guess": "x"}, &ref); s != 403 || ref.Code != "other_group" {
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
	// Both groups on the split: it names neither, so a rescue there must
	// say which.
	var refused struct {
		Code string `json:"code"`
	}
	if s := arr.doAny("POST", "/api/admin/teams/"+itoa(team.ID)+"/unlock", map[string]string{"node": "split"}, &refused); s != 409 || refused.Code != "which_group" {
		t.Errorf("rescuing the split without a group = %d %q, want 409 which_group", s, refused.Code)
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
	// A team on a split is moved on a group at a time, never as a whole:
	// the group still on the split may be, the team may not. The team's
	// players say so, as the rescue would find the team.
	type roster struct {
		Groups []struct {
			Port, Node string
			Rescuable  bool
		} `json:"groups"`
	}
	var ro roster
	if s := arr.do("GET", "/api/admin/teams/"+itoa(team.ID)+"/players", nil, &ro); s != 200 || len(ro.Groups) != 2 {
		t.Fatalf("players = %d %+v", s, ro)
	}
	if ed.Graph == nil || slices.Contains(ed.Graph.Rescuable, "split") || !ro.Groups[0].Rescuable || !ro.Groups[1].Rescuable {
		t.Errorf("rescuable: team-wide %v, the groups %+v", ed.Graph, ro.Groups)
	}
	// Named by its group alone, the well's group is moved on from its own
	// step, not the split the team is on meanwhile.
	if s := arr.do("POST", "/api/admin/teams/"+itoa(team.ID)+"/unlock", map[string]string{"group": "b"}, nil); s != 200 {
		t.Fatalf("rescuing the well's group = %d", s)
	}
	// The log names the step the group was moved from, though the
	// organiser named none.
	var act struct {
		Events []struct {
			Kind    string         `json:"kind"`
			Payload map[string]any `json:"payload"`
		} `json:"events"`
	}
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/activity", nil, &act)
	if !slices.ContainsFunc(act.Events, func(e struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}) bool {
		return e.Kind == "team.unlocked" && e.Payload["node"] == "pb"
	}) {
		t.Errorf("the rescue of the well's group by its key, logged as %+v", act.Events)
	}
	ed = detail{}
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &ed)
	if g := ed.Teams[0].Groups; len(g) != 2 || g[1].Node != "join" {
		t.Fatalf("after the rescue: %+v", ed.Teams[0])
	}
	ro = roster{}
	arr.do("GET", "/api/admin/teams/"+itoa(team.ID)+"/players", nil, &ro)
	if g := ro.Groups; len(g) != 2 || g[1].Node != "join" || g[1].Rescuable || !g[0].Rescuable {
		t.Fatalf("after the rescue, the well's group waiting at the join, not to be moved on: %+v", ro)
	}
	var st playStateDTO
	if s := phones[1].doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View.Type != graph.Join {
		t.Fatalf("Bo's phone after the rescue = %d %+v", s, st.View)
	}
}

// timeLimitedTeam starts a team of two, one phone logged in, on the
// split play graph with a time limit of two seconds, sending it to the
// vault.
func timeLimitedTeam(t *testing.T) (arr, phone *client, eventID, teamID int64) {
	t.Helper()
	ts, assignmentID, cleanup := newTestServer(t)
	t.Cleanup(cleanup)
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := splitPlayGraph()
	g.TimeLimit = &graph.TimeLimit{Seconds: 2, To: "vault"}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	arr = newClient(t, ts)
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
	phone = newClient(t, ts)
	resp, err := phone.c.Get(ts.URL + "/play?t=" + team.Members[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	return arr, phone, ev.ID, team.ID
}

// A time limit due that no phone has run out — the team's phones gone —
// is the organisers' to run out: refused before it is due, the roster
// saying when it is, the whole team sent on as the limit says, and the
// log saying the time ran out, not that the organisers moved the team.
func TestConsoleRunsOutTheTime(t *testing.T) {
	arr, phone, evID, teamID := timeLimitedTeam(t)
	type roster struct {
		TimeUp bool `json:"time_up"`
		Groups []struct {
			Rescuable bool `json:"rescuable"`
		} `json:"groups"`
	}
	var refused struct {
		Code string `json:"code"`
	}
	if s := arr.doAny("POST", "/api/admin/teams/"+itoa(teamID)+"/time-up", nil, &refused); s != 409 || refused.Code != "not_due" {
		t.Fatalf("running out the time before it is due = %d %q", s, refused.Code)
	}
	var ro roster
	if s := arr.do("GET", "/api/admin/teams/"+itoa(teamID)+"/players", nil, &ro); s != 200 || ro.TimeUp || len(ro.Groups) != 2 || !ro.Groups[0].Rescuable {
		t.Fatalf("roster before the limit = %d %+v", s, ro)
	}
	time.Sleep(2100 * time.Millisecond)
	ro = roster{}
	if s := arr.do("GET", "/api/admin/teams/"+itoa(teamID)+"/players", nil, &ro); s != 200 || !ro.TimeUp || ro.Groups[0].Rescuable || ro.Groups[1].Rescuable {
		t.Fatalf("roster with the limit due = %d %+v", s, ro)
	}
	var out struct {
		Node   string `json:"node"`
		TimeUp bool   `json:"time_up"`
	}
	if s := arr.do("POST", "/api/admin/teams/"+itoa(teamID)+"/time-up", nil, &out); s != 200 || !out.TimeUp || out.Node != "vault" {
		t.Fatalf("running out the time = %d %+v", s, out)
	}
	var act struct {
		Events []struct {
			Kind string `json:"kind"`
		} `json:"events"`
	}
	arr.do("GET", "/api/admin/events/"+itoa(evID)+"/activity", nil, &act)
	kinds := []string{}
	for _, e := range act.Events {
		kinds = append(kinds, e.Kind)
	}
	if !slices.Contains(kinds, "team.timed_out") || slices.Contains(kinds, "team.unlocked") {
		t.Errorf("logged %v, want the time run out, not a move on", kinds)
	}
	var ps struct {
		MovedOn bool          `json:"moved_on"`
		View    *session.View `json:"view"`
	}
	if s := phone.do("GET", "/api/player/play", nil, &ps); s != 200 || ps.MovedOn || ps.View == nil || ps.View.Node != "vault" {
		t.Fatalf("the phone after the time ran out = %d moved_on %v view %+v", s, ps.MovedOn, ps.View)
	}
	if s := arr.doAny("POST", "/api/admin/teams/"+itoa(teamID)+"/time-up", nil, &refused); s != 409 || refused.Code != "not_due" {
		t.Errorf("running out the time again = %d %q", s, refused.Code)
	}
}

// A phone's answer past the deadline is the time limit's to answer: the
// log says the time ran out, not that a wrong answer was given.
func TestPhoneAnswerPastTheDeadlineIsTheTimeUp(t *testing.T) {
	arr, phone, evID, teamID := timeLimitedTeam(t)
	time.Sleep(2100 * time.Millisecond)
	if s := phone.doAny("POST", "/api/player/play/answer", map[string]string{"node": "split", "guess": "x"}, nil); s != 200 {
		t.Fatalf("the late answer = %d", s)
	}
	var act struct {
		Events []struct {
			Kind string `json:"kind"`
		} `json:"events"`
	}
	arr.do("GET", "/api/admin/events/"+itoa(evID)+"/activity", nil, &act)
	kinds := []string{}
	for _, e := range act.Events {
		kinds = append(kinds, e.Kind)
	}
	if !slices.Contains(kinds, "team.timed_out") || slices.Contains(kinds, "answer.wrong") {
		t.Errorf("logged %v, want the time run out, no wrong answer", kinds)
	}
	var refused struct {
		Code string `json:"code"`
	}
	if s := arr.doAny("POST", "/api/admin/teams/"+itoa(teamID)+"/time-up", nil, &refused); s != 409 || refused.Code != "not_due" {
		t.Errorf("running out the time a phone ran out = %d %q", s, refused.Code)
	}
}

// A group's rescue past the deadline is the time limit's: logged as the
// time run out, keeping the group and step the organiser asked for.
func TestGroupRescuePastTheDeadline(t *testing.T) {
	arr, _, evID, teamID := timeLimitedTeam(t)
	time.Sleep(2100 * time.Millisecond)
	var out struct {
		TimeUp bool `json:"time_up"`
	}
	// Named by its group alone: its step is where it stood.
	if s := arr.do("POST", "/api/admin/teams/"+itoa(teamID)+"/unlock", map[string]string{"group": "b"}, &out); s != 200 || !out.TimeUp {
		t.Fatalf("the late rescue = %d %+v", s, out)
	}
	var act struct {
		Events []struct {
			Kind    string         `json:"kind"`
			Payload map[string]any `json:"payload"`
		} `json:"events"`
	}
	arr.do("GET", "/api/admin/events/"+itoa(evID)+"/activity", nil, &act)
	i := slices.IndexFunc(act.Events, func(e struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}) bool {
		return e.Kind == "team.timed_out"
	})
	if i < 0 || act.Events[i].Payload["group"] != "Well" || act.Events[i].Payload["node"] != "split" {
		t.Fatalf("logged %+v", act.Events)
	}
}

// The organisers moving one group on to its join tell its phones so, not
// those of a group come to the join by itself later.
func TestMovedOnIsTheGroupsMoved(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := splitPlayGraph()
	g.Roles = nil
	g.Nodes[1].SplitCfg.Paths = []graph.SplitPath{{Port: "a", Label: "Church"}, {Port: "b", Label: "Well"}, {Port: "c", Label: "Mill"}}
	right := g.Nodes[2].PuzzleCfg.Outputs
	g.Nodes = append(g.Nodes, graph.Node{Slug: "pc", Type: graph.Puzzle, Title: "Mill", PuzzleCfg: &graph.PuzzleConfig{Outputs: right}})
	g.Edges = append(g.Edges, graph.Edge{From: "split", FromPort: "c", To: "pc"}, graph.Edge{From: "pc", FromPort: "ok", To: "join"})
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
	var team struct {
		ID      int64 `json:"id"`
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo", "Cy"}}, &team); s != 201 {
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
	type play struct {
		MovedOn bool          `json:"moved_on"`
		View    *session.View `json:"view"`
	}
	// Each phone's own group: the one its view is for.
	phoneOf := map[string]*client{}
	for _, p := range phones {
		var ps play
		if s := p.do("GET", "/api/player/play", nil, &ps); s != 200 || ps.View == nil {
			t.Fatalf("play = %d", s)
		}
		for _, gv := range ps.View.Groups {
			if gv.Current {
				phoneOf[gv.Port] = p
			}
		}
	}
	if len(phoneOf) != 3 {
		t.Fatalf("a phone to each group: %v", phoneOf)
	}
	// The organisers move the well's group on, twice: to the join.
	for _, node := range []string{"split", "pb"} {
		if s := arr.do("POST", "/api/admin/teams/"+itoa(team.ID)+"/unlock", map[string]string{"node": node, "group": "b"}, nil); s != 200 {
			t.Fatalf("moving the well's group past %s = %d", node, s)
		}
	}
	// The mill's group comes to the join by itself.
	mill := phoneOf["c"]
	if s := mill.doAny("POST", "/api/player/play/continue", map[string]string{"node": "split"}, nil); s != 200 {
		t.Fatalf("the mill sets off = %d", s)
	}
	if s := mill.doAny("POST", "/api/player/play/answer", map[string]string{"node": "pc", "guess": "x"}, nil); s != 200 {
		t.Fatalf("the mill answers = %d", s)
	}
	// The organisers then move the church's group off the split: the
	// well's phones are still told theirs was moved.
	if s := arr.do("POST", "/api/admin/teams/"+itoa(team.ID)+"/unlock", map[string]string{"node": "split", "group": "a"}, nil); s != 200 {
		t.Fatalf("moving the church's group = %d", s)
	}
	for port, want := range map[string]bool{"b": true, "c": false} {
		var ps play
		if s := phoneOf[port].do("GET", "/api/player/play", nil, &ps); s != 200 || ps.View == nil || ps.View.Node != "join" || ps.MovedOn != want {
			t.Errorf("group %s's phone = %d at %v, moved_on %v, want %v", port, s, ps.View, ps.MovedOn, want)
		}
	}
}
