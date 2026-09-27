package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"reflect"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// uploadPhoto sends a picture for a photo block, as the player app does.
func (c *client) uploadPhoto(node, block string, content []byte) (int, map[string]any) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("node", node)
	_ = mw.WriteField("block", block)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="team.png"`)
	h.Set("Content-Type", "image/png")
	fw, _ := mw.CreatePart(h)
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.ts.URL+"/api/player/play/photo", &body)
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

// The blocks a player's view is finished for, over HTTP: a split secret
// dealt out among the players who have opened their links, a vote each
// player casts from their own phone, and a photo an arranger approves
// for points or denies for a retake — the team moving on regardless.
func TestPerPlayerBlocksOverHTTP(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Story, Title: "The statue", StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "sec", Kind: graph.BlockSplit, Parts: []string{"one", "two", "three"}},
				{ID: "v", Kind: graph.BlockVote, Text: "Which door?", Options: []string{"Red", "Blue"}},
				{ID: "ph", Kind: graph.BlockPhoto, Text: "Your team with the statue.", Worth: 5},
				{ID: "sus", Kind: graph.BlockSuspects, Cards: []graph.Card{{Name: "The baker"}, {Name: "The mayor"}}},
				{ID: "deck", Kind: graph.BlockDraw, Options: []string{"The tower", "The star"}},
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
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	type member struct {
		Token string `json:"token"`
	}
	team := func(name string, members ...string) []member {
		var out struct {
			Members []member `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": name, "member_names": members}, &out); s != 201 {
			t.Fatalf("create team %s = %d", name, s)
		}
		return out.Members
	}
	alpha := team("Alpha", "Ann", "Bo", "Cy")
	beta := team("Beta", "Dee")
	login := func(m member) *client {
		c := newClient(t, ts)
		resp, err := c.c.Get(ts.URL + "/play?t=" + m.Token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return c
	}
	ann := login(alpha[0])
	dee := login(beta[0])
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	block := func(c *client, id string) session.BlockView {
		t.Helper()
		var st playStateDTO
		if s := c.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play = %d", s)
		}
		for _, b := range st.View.Blocks {
			if b.ID == id {
				return b
			}
		}
		t.Fatalf("no block %s in %+v", id, st.View.Blocks)
		return session.BlockView{}
	}

	// The split secret, dealt among the players who have opened their link.
	if b := block(ann, "sec"); !reflect.DeepEqual(b.Parts, []string{"one", "two", "three"}) || b.Players != 1 {
		t.Errorf("alone, Ann sees %v of %d players; want every part", b.Parts, b.Players)
	}
	bo := login(alpha[1])
	if a, b := block(ann, "sec"), block(bo, "sec"); !reflect.DeepEqual(a.Parts, []string{"one", "three"}) || !reflect.DeepEqual(b.Parts, []string{"two"}) || a.Players != 2 || b.Player != 2 {
		t.Errorf("two players: Ann %v (%d/%d), Bo %v (%d/%d)", a.Parts, a.Player, a.Players, b.Parts, b.Player, b.Players)
	}
	cy := login(alpha[2])
	for i, c := range []*client{ann, bo, cy} {
		if b := block(c, "sec"); !reflect.DeepEqual(b.Parts, []string{[]string{"one", "two", "three"}[i]}) || b.Player != i+1 || b.Players != 3 {
			t.Errorf("three players: player %d sees %v (%d/%d)", i+1, b.Parts, b.Player, b.Players)
		}
	}

	// Votes: each player's own, changeable, tallied for all.
	vote := func(c *client, node, choice string) (int, playRefusalDTO) {
		var r playRefusalDTO
		s := c.doAny("POST", "/api/player/play/vote", map[string]string{"node": node, "block": "v", "choice": choice}, &r)
		return s, r
	}
	for _, v := range []struct {
		c      *client
		choice string
	}{{ann, "0"}, {bo, "0"}, {cy, "1"}, {bo, "1"}} {
		if s, _ := vote(v.c, "s", v.choice); s != 200 {
			t.Fatalf("vote %s = %d", v.choice, s)
		}
	}
	if b := block(bo, "v"); !reflect.DeepEqual(b.Tally, []int{1, 2}) || b.Voted != "1" || b.Voters != 3 {
		t.Errorf("Bo's vote view = tally %v voted %q voters %d", b.Tally, b.Voted, b.Voters)
	}
	var res session.Result
	if s := cy.doAny("POST", "/api/player/play/vote", map[string]string{"node": "s", "block": "v", "choice": "1", "client_action_id": "cy-vote-1"}, &res); s != 200 {
		t.Fatalf("vote with an action id = %d", s)
	}
	for _, b := range res.View.Blocks {
		if b.ID == "v" && (b.Voted != "1" || !reflect.DeepEqual(b.Tally, []int{1, 2})) {
			t.Errorf("the vote's reply view = voted %q tally %v", b.Voted, b.Tally)
		}
		if b.ID == "sec" && !reflect.DeepEqual(b.Parts, []string{"three"}) {
			t.Errorf("an action's reply view deals Cy %v of the secret, want only [three]", b.Parts)
		}
	}
	if b := block(ann, "v"); b.Voted != "0" {
		t.Errorf("Ann's own vote = %q, want 0", b.Voted)
	}
	if s, r := vote(ann, "end", "0"); s != 409 || r.Code != "stale" {
		t.Errorf("a vote on a step the team is not on = %d %q, want 409 stale", s, r.Code)
	}
	if s, r := vote(ann, "s", "5"); s != 400 || r.Code != "wrong_node" {
		t.Errorf("a vote for no option = %d %q, want 400 wrong_node", s, r.Code)
	}

	// Photos: pending, approved for points, denied and retaken.
	score := func() float64 {
		var st playStateDTO
		ann.doAny("GET", "/api/player/play", nil, &st)
		return st.View.Score
	}
	review := func(id any, status string) int {
		return arr.do("POST", fmt.Sprintf("/api/admin/events/%d/photos/%v/review", ev.ID, id), map[string]string{"status": status}, nil)
	}
	s, out := ann.uploadPhoto("s", "ph", tinyPNG(t, 40))
	if s != 200 {
		t.Fatalf("upload = %d %v", s, out)
	}
	first := out["photo"].(map[string]any)["id"]
	if b := block(bo, "ph"); b.Photo == nil || b.Photo.Status != "pending" {
		t.Fatalf("teammate sees photo %+v, want pending", b.Photo)
	}
	path := fmt.Sprintf("/api/player/play/photo/%v", first)
	if s := bo.status(path); s != 200 {
		t.Errorf("a teammate fetching the photo = %d, want 200", s)
	}
	if s := dee.status(path); s != 404 {
		t.Errorf("another team fetching the photo = %d, want 404", s)
	}
	var list struct {
		Photos []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			Prompt string `json:"prompt"`
		} `json:"photos"`
		Challenges int `json:"challenges"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(ev.ID)+"/photos", nil, &list); s != 200 || len(list.Photos) != 1 || list.Challenges != 1 || list.Photos[0].Prompt != "Your team with the statue." {
		t.Fatalf("arranger's list = %d %+v", s, list)
	}
	if s := review(first, "approved"); s != 200 || score() != 5 {
		t.Fatalf("approve = %d, score %v; want 200 and 5", s, score())
	}
	if s, _ := ann.uploadPhoto("s", "ph", tinyPNG(t, 80)); s != 409 {
		t.Errorf("a new photo after approval = %d, want 409", s)
	}
	if s := review(first, "denied"); s != 200 || score() != 0 {
		t.Fatalf("deny = %d, score %v; want 200 and 0", s, score())
	}
	s, out = cy.uploadPhoto("s", "ph", tinyPNG(t, 120))
	if s != 200 {
		t.Fatalf("retake after denial = %d %v", s, out)
	}
	second := out["photo"].(map[string]any)["id"]
	if s := review(second, "approved"); s != 200 || score() != 5 {
		t.Fatalf("approve the retake = %d, score %v", s, score())
	}
	if s := review(first, "approved"); s != 409 {
		t.Errorf("approving a second photo for the same challenge = %d, want 409", s)
	}

	// A suspect board struck and cleared, and a draw, through the pick
	// action with the empty choice the phone sends for both.
	pick := func(block, choice string) int {
		return bo.doAny("POST", "/api/player/play/pick", map[string]string{"node": "s", "block": block, "choice": choice}, nil)
	}
	if s := pick("sus", "1"); s != 200 || block(ann, "sus").Picked != "1" {
		t.Errorf("strike = %d, picked %q", s, block(ann, "sus").Picked)
	}
	if s := pick("sus", ""); s != 200 || block(ann, "sus").Picked != "" {
		t.Errorf("clear the board = %d, picked %q", s, block(ann, "sus").Picked)
	}
	if s := pick("deck", ""); s != 200 {
		t.Errorf("draw = %d", s)
	}
	if d := block(ann, "deck"); d.Dealt == "" || d.Deck != 2 || d.Options != nil {
		t.Errorf("drawn card = %+v; want one dealt, the deck face down", d)
	}

	// None of it holds the team back.
	if s := ann.doAny("POST", "/api/player/play/continue", map[string]string{"node": "s"}, nil); s != 200 {
		t.Errorf("continue = %d", s)
	}
}
