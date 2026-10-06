package server_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// uploadTeamAvatar sends a team picture, as the player app's menu does.
func (c *client) uploadTeamAvatar(content []byte) (int, map[string]any) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="team.png"`)
	h.Set("Content-Type", "image/png")
	fw, _ := mw.CreatePart(h)
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.ts.URL+"/api/player/team/avatar", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(auth.CSRFHeaderName, c.csrfCookie())
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// A team's players name themselves and the team, and choose its
// picture, until the team makes its first move — then those are fixed
// for them, though the organisers can still change them.
func TestTeamProfileFixedOnceTeamBegins(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "intro", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Welcome.", nil)}},
			{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "intro"},
			{From: "intro", FromPort: "out", To: "q"},
			{From: "q", FromPort: "ok", To: "end"},
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
	var team struct {
		ID      int64 `json:"id"`
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Team 1", "member_names": []string{"Player 1"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	// A second team keeps the event running while the first plays on.
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Team 2", "member_names": []string{"Player 1"}}, nil); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	p := newClient(t, ts)
	if s := p.do("GET", "/play?t="+team.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("magic-link login = %d", s)
	}

	type teamState struct {
		Team struct {
			AvatarURL string `json:"avatar_url"`
			Locked    bool   `json:"locked"`
		} `json:"team"`
	}
	state := func() teamState {
		t.Helper()
		var st teamState
		if s := p.doAny("GET", "/api/player/play", nil, &st); s != 200 {
			t.Fatalf("state = %d", s)
		}
		return st
	}
	editable := func(when string) {
		t.Helper()
		if s := p.do("POST", "/api/player/me/name", map[string]string{"name": "Klara " + when}, nil); s != 200 {
			t.Errorf("%s: rename myself = %d", when, s)
		}
		if s := p.do("POST", "/api/player/team/name", map[string]string{"name": "Otters " + when}, nil); s != 200 {
			t.Errorf("%s: rename team = %d", when, s)
		}
		if s, _ := p.uploadTeamAvatar(tinyPNG(t, 40)); s != 200 {
			t.Errorf("%s: team picture = %d", when, s)
		}
		if st := state(); st.Team.Locked || st.Team.AvatarURL == "" {
			t.Errorf("%s: %+v", when, st.Team)
		}
	}

	editable("before the start")
	// A name another team in the event holds is refused.
	if s := p.do("POST", "/api/player/team/name", map[string]string{"name": "Team 2"}, nil); s != 409 {
		t.Errorf("rename team to a taken name = %d, want 409", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	// Placed on its first step, the team has not begun yet.
	editable("on the first step")

	if s := p.doAny("POST", "/api/player/play/continue", map[string]string{"node": "intro"}, nil); s != 200 {
		t.Fatalf("continue = %d", s)
	}
	var refused struct {
		Code string `json:"code"`
	}
	for _, c := range []struct {
		name string
		do   func() int
	}{
		{"rename myself", func() int { return p.doAny("POST", "/api/player/me/name", map[string]string{"name": "Late"}, &refused) }},
		{"rename team", func() int {
			return p.doAny("POST", "/api/player/team/name", map[string]string{"name": "Late"}, &refused)
		}},
		{"team picture", func() int {
			s, out := p.uploadTeamAvatar(tinyPNG(t, 90))
			refused.Code, _ = out["code"].(string)
			return s
		}},
		{"remove picture", func() int { return p.doAny("DELETE", "/api/player/team/avatar", nil, &refused) }},
	} {
		refused.Code = ""
		if s := c.do(); s != 409 || refused.Code != "team_started" {
			t.Errorf("after the first move, %s = %d %q, want 409 team_started", c.name, s, refused.Code)
		}
	}
	if st := state(); !st.Team.Locked || st.Team.AvatarURL == "" {
		t.Errorf("after the first move: %+v", st.Team)
	}
	// The organisers still can.
	if s := arr.do("PATCH", "/api/admin/teams/"+itoa(team.ID), map[string]any{"name": "Renamed by staff"}, nil); s != 200 && s != 204 {
		t.Errorf("organiser rename after the start = %d", s)
	}
}
