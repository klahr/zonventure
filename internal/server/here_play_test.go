package server_test

import (
	"strconv"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
)

// Teammates in different places see different things: a block shown
// "here" is the phone's that stands in the zone, picture and all, and
// one waiting on a player at each of two zones shows once both are held.
func TestHereBlocksOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	s, up := pub.upload("/api/admin/adventures/demo/media", tinyPNG(t, 10))
	if s != 201 {
		t.Fatalf("upload = %d", s)
	}
	pic := int64(up["id"].(float64))
	here := func(zone string) *graph.Show { return &graph.Show{When: cond.Predicate{Op: cond.OpHere, Zone: zone}} }
	g := graph.Graph{
		Zones: []graph.Zone{
			{Slug: "gate", Name: "The gate", Lat: 59.3300, Lng: 18.0600, RadiusM: 30},
			{Slug: "bridge", Name: "The bridge", Lat: 59.3350, Lng: 18.0700, RadiusM: 30},
		},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "gate-code", Kind: graph.BlockText, Text: "The gate says 7.", Show: here("gate")},
				{ID: "bridge-pic", Kind: graph.BlockImage, Media: pic, Show: here("bridge")},
				{ID: "both", Kind: graph.BlockText, Text: "Together now.", Show: &graph.Show{When: cond.Predicate{Op: cond.OpZonesHeld, Members: []string{"gate", "bridge"}}}},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"strict_assets": true}, nil)

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
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Alpha", "member_names": []string{"Ann", "Bo"}}, &team); s != 201 {
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
	at := func(c *client, lat, lng float64) {
		t.Helper()
		if s := c.do("POST", "/api/player/location", map[string]any{"lat": lat, "lng": lng, "accuracy": 5}, nil); s != 204 {
			t.Fatalf("location = %d", s)
		}
	}
	shown := func(c *client) map[string]bool {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		out := map[string]bool{}
		for _, b := range st.View.Blocks {
			if !b.Locked {
				out[b.ID] = true
			}
		}
		return out
	}
	media := "/media/" + strconv.FormatInt(pic, 10)

	// Ann at the gate, Bo still on his way: only Ann's phone has a secret.
	at(ann, 59.3300, 18.0600)
	if got := shown(ann); !got["gate-code"] || got["bridge-pic"] || got["both"] {
		t.Errorf("Ann at the gate sees %v", got)
	}
	if got := shown(bo); got["gate-code"] || got["bridge-pic"] {
		t.Errorf("Bo, nowhere yet, sees %v", got)
	}

	// Bo reaches the bridge: each sees their own, and both see the block
	// that needed one of them at each.
	at(bo, 59.3350, 18.0700)
	if got := shown(ann); !got["gate-code"] || got["bridge-pic"] || !got["both"] {
		t.Errorf("Ann at the gate, Bo at the bridge: Ann sees %v", got)
	}
	if got := shown(bo); got["gate-code"] || !got["bridge-pic"] || !got["both"] {
		t.Errorf("Ann at the gate, Bo at the bridge: Bo sees %v", got)
	}
	// The picture is the phone's that stands there, not the team's.
	if s := bo.status(media); s != 200 {
		t.Errorf("Bo at the bridge fetching its picture = %d, want 200", s)
	}
	if s := ann.status(media); s != 404 {
		t.Errorf("Ann at the gate fetching the bridge's picture = %d, want 404", s)
	}
}

// Roles over HTTP: each player sees the blocks of the roles they hold,
// dealt or claimed; a role a teammate claimed is not another's to take
// until they give it up.
func TestRolesOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	role := func(r string) *graph.Show { return &graph.Show{When: cond.Predicate{Op: cond.OpRole, Role: r}} }
	g := graph.Graph{
		Roles: []graph.Role{{Slug: "navigator", Name: "Navigator", Text: "You have the map."}, {Slug: "decoder", Name: "Decoder"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "map", Kind: graph.BlockText, Text: "Left at the fountain.", Show: role("navigator")},
				{ID: "key", Kind: graph.BlockText, Text: "A=Z", Show: role("decoder")},
			}}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
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
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Alpha", "member_names": []string{"Ann", "Bo"}}, &team); s != 201 {
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
	type seen struct {
		blocks map[string]bool
		mine   []string
		holder map[string]string
	}
	look := func(c *client) seen {
		t.Helper()
		var st struct {
			View *struct {
				Blocks []struct {
					ID     string `json:"id"`
					Locked bool   `json:"locked"`
				} `json:"blocks"`
				Roles []struct {
					Slug   string `json:"slug"`
					Name   string `json:"name"`
					Mine   bool   `json:"mine"`
					Holder string `json:"holder"`
				} `json:"roles"`
			} `json:"view"`
		}
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		out := seen{blocks: map[string]bool{}, holder: map[string]string{}}
		for _, b := range st.View.Blocks {
			if !b.Locked {
				out.blocks[b.ID] = true
			}
		}
		for _, r := range st.View.Roles {
			if r.Mine {
				out.mine = append(out.mine, r.Slug)
			}
			out.holder[r.Slug] = r.Holder
		}
		return out
	}
	claim := func(c *client, r string, on bool) int {
		t.Helper()
		return c.do("POST", "/api/player/role", map[string]any{"role": r, "claim": on}, nil)
	}

	// Alone, Ann holds both roles and sees both blocks.
	if got := look(ann); !got.blocks["map"] || !got.blocks["key"] || len(got.mine) != 2 {
		t.Errorf("Ann alone: %+v", got)
	}

	// Bo opens his link and is dealt the decoder.
	bo := login(team.Members[1].Token)
	a, b := look(ann), look(bo)
	if !a.blocks["map"] || a.blocks["key"] || b.blocks["map"] || !b.blocks["key"] {
		t.Errorf("dealt: Ann sees %v, Bo sees %v", a.blocks, b.blocks)
	}
	if a.holder["decoder"] != "Bo" || b.holder["navigator"] != "Ann" {
		t.Errorf("holders: %v", a.holder)
	}

	// Bo claims the navigator: a dealt role is anyone's to claim.
	if s := claim(bo, "navigator", true); s != 204 {
		t.Fatalf("Bo claiming the navigator = %d", s)
	}
	if got := look(bo); !got.blocks["map"] {
		t.Errorf("Bo, navigator, sees %v", got.blocks)
	}
	if got := look(ann); got.blocks["map"] || !got.blocks["key"] {
		t.Errorf("Ann, now dealt the decoder, sees %v", got.blocks)
	}
	// Ann cannot take it from him while he plays, until he gives it up.
	if s := claim(ann, "navigator", true); s != 409 {
		t.Errorf("Ann taking Bo's claimed role = %d, want 409", s)
	}
	if s := claim(bo, "navigator", false); s != 204 {
		t.Fatalf("Bo giving up the navigator = %d", s)
	}
	if s := claim(ann, "navigator", true); s != 204 {
		t.Errorf("Ann claiming the freed navigator = %d", s)
	}
	if s := claim(ann, "ghost", true); s != 404 {
		t.Errorf("claiming a role the adventure lacks = %d, want 404", s)
	}
}
