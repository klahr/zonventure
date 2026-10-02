package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A default role and a launch at roles' posts, over HTTP with three
// phones: the smith and the mech are dealt, the third player is crew,
// and each key counts only from its role's player at its post.
func TestRolePostsOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Zones: []graph.Zone{
			{Slug: "east", Name: "East", Lat: 59.30, Lng: 18.00, RadiusM: 40},
			{Slug: "west", Name: "West", Lat: 59.31, Lng: 18.00, RadiusM: 40},
		},
		Roles: []graph.Role{{Slug: "smith", Name: "Smith"}, {Slug: "mech", Name: "Mech"}, {Slug: "crew", Name: "Crew", Default: true}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "launch", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer:  &graph.Answer{Kind: graph.AnswerKeys, Posts: []string{"east", "west"}, PostRoles: []string{"smith", "mech"}},
				Outputs: []graph.Output{{Port: "launched", Trigger: graph.OnAnswer, Points: 10}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "launch"},
			{From: "launch", FromPort: "launched", To: "end"},
		},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	arr := newClient(t, ts)
	arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil)
	var ev struct {
		ID int64 `json:"id"`
	}
	arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev)
	var team struct {
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Alpha", "member_names": []string{"Ann", "Bo", "Cy"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	login := func(token string) *client {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return c
	}
	ann := login(team.Members[0].Token)
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	view := func(c *client) *session.View {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		return st.View
	}
	mine := func(v *session.View) map[string]bool {
		out := map[string]bool{}
		for _, r := range v.Roles {
			if r.Mine {
				out[r.Slug] = true
			}
		}
		return out
	}
	at := func(c *client, lat, lng float64) {
		t.Helper()
		if s := c.do("POST", "/api/player/location", map[string]any{"lat": lat, "lng": lng, "accuracy": 5}, nil); s != 204 {
			t.Fatalf("location = %d", s)
		}
	}
	turn := func(c *client) (int, session.Result) {
		t.Helper()
		var res session.Result
		s := c.doAny("POST", "/api/player/play/turn", map[string]string{"node": "launch"}, &res)
		return s, res
	}

	// Alone, Ann holds every role, the crew's too.
	if m := mine(view(ann)); !m["smith"] || !m["mech"] || !m["crew"] {
		t.Errorf("Ann alone holds %v", m)
	}

	bo, cy := login(team.Members[1].Token), login(team.Members[2].Token)
	if m := mine(view(cy)); len(m) != 1 || !m["crew"] {
		t.Errorf("Cy, with no role dealt, holds %v", m)
	}
	if m := mine(view(ann)); m["crew"] || !m["smith"] {
		t.Errorf("Ann, the smith, holds %v", m)
	}
	if s := cy.do("POST", "/api/player/role", map[string]any{"role": "crew", "claim": true}, nil); s != 404 {
		t.Errorf("claiming the default role = %d, want 404", s)
	}

	// Cy stands at the mech's post: not theirs.
	at(cy, 59.31, 18.00)
	if s, _ := turn(cy); s != 403 {
		t.Errorf("Cy at the mech's post = %d, want 403", s)
	}
	at(ann, 59.30, 18.00)
	if s, res := turn(ann); s != 200 || res.Waiting == nil || res.Waiting.Done != 1 || res.Waiting.Of != 2 {
		t.Fatalf("Ann at her post = %d %+v", s, res.Waiting)
	}
	// Ann's phone shows her post held, and the mech's not yet.
	if in := view(ann).Input; in == nil || len(in.PostsHeld) != 2 || !in.PostsHeld[0] || in.PostsHeld[1] || !in.MyPosts[0] || in.MyPosts[1] {
		t.Errorf("Ann's posts: %+v", in)
	}
	at(bo, 59.31, 18.00)
	if s, res := turn(bo); s != 200 || !res.Accepted {
		t.Errorf("Bo at his post = %d %+v", s, res)
	}
}
