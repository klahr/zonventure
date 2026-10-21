package server_test

import (
	"math"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// Race control for a split team: a trail and a marker per group, from
// its own players' phones; each group's step and since when in the
// leaderboard; and each player's roles and groups in the team dialog.
func TestRaceControlSplitTeam(t *testing.T) {
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
	type created struct {
		ID      int64 `json:"id"`
		Members []struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		} `json:"members"`
	}
	var crew, solo created
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo", "Cy", "Di"}}, &crew); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Solo", "member_names": []string{"Eve"}}, &solo); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	login := func(token string) *client {
		c := newClient(t, ts)
		if s := c.do("GET", "/play?t="+token, nil, nil); s != 200 {
			t.Fatalf("magic link = %d", s)
		}
		return c
	}
	var phones []*client
	for _, m := range crew.Members {
		phones = append(phones, login(m.Token))
	}
	eve := login(solo.Members[0].Token)
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}

	// The team players before the split: Ann the scout, the rest crew.
	type player struct {
		ID      int64    `json:"id"`
		Name    string   `json:"name"`
		Playing bool     `json:"playing"`
		Roles   []string `json:"roles"`
		Groups  []string `json:"groups"`
	}
	var pl struct {
		Players []player `json:"players"`
	}
	if s := arr.do("GET", "/api/admin/teams/"+itoa(crew.ID)+"/players", nil, &pl); s != 200 || len(pl.Players) != 4 {
		t.Fatalf("players = %d %+v", s, pl)
	}
	if p := pl.Players[0]; p.Name != "Ann" || len(p.Roles) != 1 || p.Roles[0] != "Scout" || len(p.Groups) != 0 || !p.Playing {
		t.Errorf("Ann before the split: %+v", p)
	}

	// Ann at home before the split: the team's trail, none of a group's.
	home := [2]float64{59.3250, 18.0600}
	if s := phones[0].doAny("POST", "/api/player/location", map[string]any{"lat": home[0], "lng": home[1], "accuracy": 5}, nil); s != 200 && s != 204 {
		t.Fatalf("location = %d", s)
	}

	// Bo sets the well's group off. Church: Ann, Cy; well: Bo, Di.
	if s := phones[1].doAny("POST", "/api/player/play/continue", map[string]string{"node": "split"}, nil); s != 200 {
		t.Fatalf("Bo sets off = %d", s)
	}
	pl.Players = nil
	if s := arr.do("GET", "/api/admin/teams/"+itoa(crew.ID)+"/players", nil, &pl); s != 200 {
		t.Fatalf("players = %d", s)
	}
	want := map[string]string{"Ann": "Church", "Bo": "Well", "Cy": "Church", "Di": "Well"}
	for _, p := range pl.Players {
		if len(p.Groups) != 1 || p.Groups[0] != want[p.Name] {
			t.Errorf("%s is in %v, want %s", p.Name, p.Groups, want[p.Name])
		}
	}

	// The church party at the church, the well party 1 km off at the well.
	church, well := [2]float64{59.3300, 18.0600}, [2]float64{59.3390, 18.0600}
	ping := func(c *client, at [2]float64) {
		t.Helper()
		if s := c.doAny("POST", "/api/player/location", map[string]any{"lat": at[0], "lng": at[1], "accuracy": 5}, nil); s != 200 && s != 204 {
			t.Fatalf("location = %d", s)
		}
	}
	for i, c := range phones {
		if i == 0 {
			continue // Ann pinged a moment ago, from home.
		}
		if want[[]string{"Ann", "Bo", "Cy", "Di"}[i]] == "Church" {
			ping(c, church)
		} else {
			ping(c, well)
		}
	}
	ping(eve, [2]float64{59.3200, 18.0500})

	var tr struct {
		Tracks []struct {
			TeamID      int64    `json:"team_id"`
			Group       string   `json:"group"`
			GroupLabel  string   `json:"group_label"`
			GroupLabels []string `json:"group_labels"`
			Past        bool     `json:"past"`
			Points      []struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"points"`
		} `json:"tracks"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/tracks", nil, &tr); s != 200 {
		t.Fatalf("tracks = %d", s)
	}
	near := func(a, b [2]float64) bool { return math.Abs(a[0]-b[0]) < 1e-6 && math.Abs(a[1]-b[1]) < 1e-6 }
	at := map[string][2]float64{}
	for _, x := range tr.Tracks {
		last := x.Points[len(x.Points)-1]
		switch x.TeamID {
		case crew.ID:
			if x.Group == "" {
				// Ann's walk from home, before the split: no one is on
				// it now, so no marker.
				if !x.Past || !near([2]float64{last.Lat, last.Lng}, home) {
					t.Errorf("the team's own trail while split: %+v", x)
				}
				continue
			}
			if x.Past || len(x.GroupLabels) != 1 || x.GroupLabels[0] != x.GroupLabel {
				t.Errorf("a group out now: %+v", x)
			}
			at[x.GroupLabel] = [2]float64{last.Lat, last.Lng}
		case solo.ID:
			// Solo is split too — at its first step — but has played
			// nothing, so no one is dealt into a group: one track.
			if x.Group != "" {
				t.Errorf("a phone in no group keeps its team's track: %+v", x)
			}
			at["solo"] = [2]float64{last.Lat, last.Lng}
		}
	}
	if len(at) != 3 || !near(at["Church"], church) || !near(at["Well"], well) {
		t.Fatalf("one track per group, each where its own players are — not between them: %v", at)
	}

	// The staff positions: the team at its split, each group where its
	// own players are.
	var pos struct {
		Positions []struct {
			TeamID int64  `json:"team_id"`
			Node   string `json:"node"`
			Groups []struct {
				Label string  `json:"label"`
				Node  string  `json:"node"`
				Lat   float64 `json:"lat"`
				Lng   float64 `json:"lng"`
			} `json:"groups"`
		} `json:"positions"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/positions", nil, &pos); s != 200 {
		t.Fatalf("positions = %d", s)
	}
	for _, p := range pos.Positions {
		if p.TeamID != crew.ID {
			continue
		}
		if p.Node != "split" || len(p.Groups) != 2 || p.Groups[0].Label != "Church" || p.Groups[1].Node != "pb" ||
			!near([2]float64{p.Groups[0].Lat, p.Groups[0].Lng}, church) || !near([2]float64{p.Groups[1].Lat, p.Groups[1].Lng}, well) {
			t.Fatalf("positions %+v", p)
		}
	}

	// The leaderboard's groups: the well's on its step, the church still at
	// the split, each saying since when.
	var detail struct {
		Teams []struct {
			ID     int64 `json:"id"`
			Groups []struct {
				Label   string `json:"label"`
				Node    string `json:"node"`
				Since   string `json:"since"`
				Arrived bool   `json:"arrived"`
			} `json:"groups"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &detail); s != 200 {
		t.Fatalf("detail = %d", s)
	}
	for _, tm := range detail.Teams {
		if tm.ID != crew.ID {
			continue
		}
		if len(tm.Groups) != 2 || tm.Groups[1].Node != "pb" || tm.Groups[0].Node != "split" || tm.Groups[1].Since == "" || tm.Groups[1].Arrived {
			t.Fatalf("groups %+v", tm.Groups)
		}
	}
	if s := phones[3].doAny("POST", "/api/player/play/answer", map[string]string{"node": "pb", "guess": "x"}, nil); s != 200 {
		t.Fatalf("Di answers = %d", s)
	}
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &detail)
	for _, tm := range detail.Teams {
		if tm.ID == crew.ID && (tm.Groups[1].Node != "join" || !tm.Groups[1].Arrived) {
			t.Fatalf("the well's group waits at the join: %+v", tm.Groups)
		}
	}

	// The church's group, still at the split no one there set off from,
	// is set off by the organisers by name.
	if s := arr.do("POST", "/api/admin/teams/"+itoa(crew.ID)+"/unlock", map[string]string{"node": "split", "group": "a"}, nil); s != 200 {
		t.Fatalf("setting the church's group off = %d", s)
	}
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &detail)
	for _, tm := range detail.Teams {
		if tm.ID == crew.ID && (len(tm.Groups) != 2 || tm.Groups[0].Node != "pa") {
			t.Fatalf("the church's group set off: %+v", tm.Groups)
		}
	}
	if s := arr.do("POST", "/api/admin/teams/"+itoa(crew.ID)+"/unlock", map[string]string{"node": "split", "group": "a"}, nil); s != 409 {
		t.Errorf("setting it off again = %d, want 409", s)
	}
	// The log says which group the organisers moved.
	var act struct {
		Events []struct {
			Kind    string         `json:"kind"`
			Payload map[string]any `json:"payload"`
		} `json:"events"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/activity", nil, &act); s != 200 {
		t.Fatalf("activity = %d", s)
	}
	logged := false
	for _, e := range act.Events {
		if e.Kind == "team.unlocked" && e.Payload["group"] == "Church" && e.Payload["node"] == "split" {
			logged = true
		}
	}
	if !logged {
		t.Errorf("the rescue's log should name the church's group: %+v", act.Events)
	}

	// The church's group solves its puzzle and the groups meet. The
	// audience's map: the same trails, each group's kept apart after the
	// meeting with no marker, and the team's its own again — a new trail,
	// not one drawn on from home, its marker where a phone of it pinged
	// last (Di, at the well), not back home.
	if s := phones[0].doAny("POST", "/api/player/play/answer", map[string]string{"node": "pa", "guess": "x"}, nil); s != 200 {
		t.Fatalf("Ann answers = %d", s)
	}
	var evd struct {
		Event struct {
			LiveToken string `json:"live_token"`
		} `json:"event"`
	}
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID), nil, &evd)
	if s := arr.doAny("PATCH", "/api/admin/events/"+itoa(ev.ID), map[string]any{"show_map": true}, nil); s != 200 && s != 204 {
		t.Fatalf("show map = %d", s)
	}
	tr.Tracks = nil
	if s := newClient(t, ts).do("GET", "/api/public/live/"+evd.Event.LiveToken+"/tracks", nil, &tr); s != 200 {
		t.Fatalf("public tracks = %d", s)
	}
	after := map[string]bool{}
	for _, x := range tr.Tracks {
		if x.TeamID != crew.ID {
			continue
		}
		last := x.Points[len(x.Points)-1]
		switch x.GroupLabel {
		case "Church":
			after["Church"] = x.Past && x.Group == "a" && near([2]float64{last.Lat, last.Lng}, church)
		case "Well":
			after["Well"] = x.Past && x.Group == "b" && near([2]float64{last.Lat, last.Lng}, well)
		case "":
			if x.Past {
				after["home"] = near([2]float64{last.Lat, last.Lng}, home)
			} else {
				after["team"] = near([2]float64{last.Lat, last.Lng}, well)
			}
		}
	}
	if !after["Church"] || !after["Well"] || !after["home"] || !after["team"] || len(after) != 4 {
		t.Errorf("after the meeting, public trails %v: %+v", after, tr.Tracks)
	}

	// A console still showing the church's group, the groups met since:
	// moving it on is stale — never the whole team past its vault.
	for _, body := range []map[string]string{{"group": "a"}, {"node": "vault", "group": "a"}} {
		var refused struct {
			Code string `json:"code"`
		}
		if s := arr.doAny("POST", "/api/admin/teams/"+itoa(crew.ID)+"/unlock", body, &refused); s != 409 || refused.Code != "team_moved" {
			t.Errorf("moving on a group met since %v = %d %q, want 409 team_moved", body, s, refused.Code)
		}
	}
	pos.Positions = nil
	arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/positions", nil, &pos)
	for _, p := range pos.Positions {
		if p.TeamID == crew.ID && p.Node != "vault" {
			t.Errorf("the team moved to %q", p.Node)
		}
	}

	// A team not yet started still lists its players, roles dealt.
	if s := arr.do("GET", "/api/admin/teams/"+itoa(solo.ID)+"/players", nil, &pl); s != 200 || len(pl.Players) != 1 || pl.Players[0].Roles[0] != "Scout" {
		t.Errorf("a player alone holds every role: %d %+v", s, pl)
	}
}
