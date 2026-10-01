package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// A kit item handed on: its player shows a code, a teammate scans it,
// and from then on it is the teammate's to use and to give — once per
// code, never by its own giver, and not by whoever no longer has it.
func TestGiveItemOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	t.Cleanup(cleanup)

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Items: []graph.Item{{Slug: "key", Name: "Key"}},
		Roles: []graph.Role{{Slug: "locksmith", Name: "Locksmith", Kit: []string{"key"}}, {Slug: "scout", Name: "Scout"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "walk", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Walk on.", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "walk"}, {From: "walk", FromPort: "out", To: "end"}},
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
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo"}}, &team); s != 201 {
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
	for c, role := range map[*client]string{ann: "locksmith", bo: "scout"} {
		if s := c.doAny("POST", "/api/player/role", map[string]any{"role": role, "claim": true}, nil); s != 204 {
			t.Fatalf("claiming %s = %d", role, s)
		}
	}
	key := func(c *client) session.ItemView {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		for _, it := range st.View.Items {
			if it.Slug == "key" {
				return it
			}
		}
		t.Fatal("the team carries no key")
		return session.ItemView{}
	}
	type gift struct {
		Code string `json:"code"`
		QR   string `json:"qr"`
	}
	give := func(c *client) (int, gift) {
		var g gift
		return c.doAny("POST", "/api/player/items/give", map[string]string{"item": "key"}, &g), g
	}
	receive := func(c *client, code string) (int, string) {
		var ref playRefusalDTO
		s := c.doAny("POST", "/api/player/items/receive", map[string]string{"code": "zv-gift:" + code}, &ref)
		return s, ref.Code
	}

	if k := key(ann); !k.Give || k.NotMine {
		t.Fatalf("the locksmith's key, on Ann's phone: %+v", k)
	}
	if s, _ := give(bo); s != 409 {
		t.Errorf("Bo giving the locksmith's key = %d, want 409", s)
	}
	s, g1 := give(ann)
	if s != 200 || g1.Code == "" || len(g1.QR) < 100 {
		t.Fatalf("Ann giving the key = %d %+v", s, g1)
	}
	if s, code := receive(ann, g1.Code); s != 409 || code != "gift_own" {
		t.Errorf("Ann scanning her own code = %d %q, want 409 gift_own", s, code)
	}
	if s, _ := receive(bo, g1.Code); s != 200 {
		t.Fatalf("Bo receiving the key = %d", s)
	}
	if k := key(bo); !k.Give || k.NotMine {
		t.Errorf("after the gift, on Bo's phone: %+v; want his to use and give", k)
	}
	if k := key(ann); k.Give || !k.NotMine || k.Holder != "Bo" {
		t.Errorf("after the gift, on Ann's phone: %+v; want with Bo", k)
	}
	if s, code := receive(bo, g1.Code); s != 404 || code != "gift_unknown" {
		t.Errorf("a used code again = %d %q, want 404 gift_unknown", s, code)
	}
	if s, _ := give(ann); s != 409 {
		t.Errorf("Ann giving a key she no longer has = %d, want 409", s)
	}
	// And back: the code a gift made goes stale once the giver hands it on another way.
	s, g2 := give(bo)
	if s != 200 {
		t.Fatalf("Bo giving it back = %d", s)
	}
	if s, _ := receive(ann, g2.Code); s != 200 {
		t.Fatalf("Ann receiving it back = %d", s)
	}
	if k := key(ann); !k.Give || k.NotMine {
		t.Errorf("given back, on Ann's phone: %+v", k)
	}
}
