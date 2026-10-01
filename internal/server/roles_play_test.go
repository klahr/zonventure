package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A roles block that makes roles final: the phones learn once every
// player has chosen, the roles are fixed as the team leaves its step, no
// one takes or gives back one after, and a player who joins later holds
// none.
func TestRolesLockOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	t.Cleanup(cleanup)

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Roles: []graph.Role{{Slug: "navigator", Name: "Navigator"}, {Slug: "scribe", Name: "Scribe"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "brief", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "parts", Kind: graph.BlockRoles, Text: "Take your parts.", Choose: true, Lock: true},
			}}},
			{Slug: "walk", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Walk on.", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "brief"},
			{From: "brief", FromPort: "out", To: "walk"},
			{From: "walk", FromPort: "out", To: "end"},
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
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo", "Cy"}}, &team); s != 201 {
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
	ann, bo := login(team.Members[0].Token), login(team.Members[1].Token)
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	roles := func(c *client) map[string]session.RoleView {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		out := map[string]session.RoleView{}
		for _, r := range st.View.Roles {
			out[r.Slug] = r
		}
		return out
	}
	chosen := func(c *client) *bool {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		return st.View.RolesChosen
	}
	claim := func(c *client, role string, take bool) (int, string) {
		var ref playRefusalDTO
		s := c.doAny("POST", "/api/player/role", map[string]any{"role": role, "claim": take}, &ref)
		return s, ref.Code
	}

	if s, _ := claim(ann, "scribe", true); s != 204 {
		t.Fatalf("Ann taking the scribe = %d", s)
	}
	if r := roles(bo); !r["navigator"].Mine || r["navigator"].Final || !r["scribe"].Claimed || r["scribe"].Holder != "Ann" {
		t.Fatalf("before the lock, Bo sees %+v", r)
	}
	if c := chosen(bo); c == nil || *c {
		t.Errorf("with Bo yet to choose, roles_chosen = %v, want false", c)
	}
	if s, _ := claim(bo, "navigator", true); s != 204 {
		t.Fatalf("Bo taking the navigator = %d", s)
	}
	if c := chosen(ann); c == nil || !*c {
		t.Errorf("with both playing players chosen, roles_chosen = %v, want true", c)
	}
	if s := ann.doAny("POST", "/api/player/play/continue", map[string]string{"node": "brief"}, nil); s != 200 {
		t.Fatalf("leaving the briefing = %d", s)
	}

	cy := login(team.Members[2].Token)
	r := roles(cy)
	if !r["navigator"].Final || r["navigator"].Holder != "Bo" || r["scribe"].Holder != "Ann" || r["navigator"].Mine || r["scribe"].Mine {
		t.Errorf("after the lock, Cy sees %+v; want Bo navigator, Ann scribe, nothing for Cy", r)
	}
	if s, code := claim(cy, "navigator", true); s != 409 || code != "roles_final" {
		t.Errorf("Cy taking a final role = %d %q, want 409 roles_final", s, code)
	}
	if s, code := claim(ann, "scribe", false); s != 409 || code != "roles_final" {
		t.Errorf("Ann giving back a final role = %d %q, want 409 roles_final", s, code)
	}
	if r := roles(bo); !r["navigator"].Mine || !r["navigator"].Final {
		t.Errorf("Bo's role should be his for good: %+v", r)
	}
}
