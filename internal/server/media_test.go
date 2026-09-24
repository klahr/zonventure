package server_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"strconv"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

func tinyPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (c *client) upload(path string, content []byte) (int, map[string]any) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "x.png")
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.ts.URL+path, &body)
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

func (c *client) status(path string) int {
	c.t.Helper()
	resp, err := c.c.Get(c.ts.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Media is uploaded once per adventure, refused when it is another
// adventure's, and shown to a player only on steps their team has
// reached — a picture ahead is often the next puzzle's answer.
func TestGraphMedia(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	s1, first := pub.upload("/api/admin/adventures/demo/media", tinyPNG(t, 10))
	s2, again := pub.upload("/api/admin/adventures/demo/media", tinyPNG(t, 10))
	s3, later := pub.upload("/api/admin/adventures/demo/media", tinyPNG(t, 200))
	if s1 != 201 || s2 != 201 || s3 != 201 || first["kind"] != "image" {
		t.Fatalf("uploads: %d %d %d %v", s1, s2, s3, first)
	}
	if first["id"] != again["id"] {
		t.Errorf("the same bytes twice should be stored once: %v vs %v", first["id"], again["id"])
	}
	now := int64(first["id"].(float64))
	ahead := int64(later["id"].(float64))
	if s, _ := pub.upload("/api/admin/adventures/demo/media", []byte("#!/bin/sh\necho hi\n")); s != 400 {
		t.Errorf("a non-media upload should be refused, got %d", s)
	}

	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "look", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Prompt: "What colour?",
				Media:  []graph.Media{{ID: now, Kind: graph.MediaImage, Alt: "a grey square"}},
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer,
					Matchers: []match.Matcher{{Kind: match.Exact, Value: "grey"}}}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{
				Media: []graph.Media{{ID: ahead, Kind: graph.MediaImage}},
			}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "look"},
			{From: "look", FromPort: "solved", To: "end"},
		},
	}
	foreign := g
	foreign.Nodes = append([]graph.Node(nil), g.Nodes...)
	foreign.Nodes[1].PuzzleCfg = &graph.PuzzleConfig{
		Media:   []graph.Media{{ID: ahead + 1000, Kind: graph.MediaImage}},
		Outputs: g.Nodes[1].PuzzleCfg.Outputs,
	}
	var refused refusalDTO
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: foreign}, &refused); s != 422 || refused.Code != "media_foreign" {
		t.Fatalf("a graph naming unknown media should be refused: %d %+v", s, refused)
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	if s := pub.status("/media/" + strconv.FormatInt(ahead, 10)); s != 200 {
		t.Errorf("staff should see any media of the adventure, got %d", s)
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
	arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "A", "member_names": []string{"a"}}, &team)
	arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "B", "member_names": []string{"b"}}, nil)
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}

	p := newClient(t, ts)
	p.status("/play?t=" + team.Members[0].Token)
	if s := p.status("/media/" + strconv.FormatInt(now, 10)); s != 200 {
		t.Errorf("the current step's picture should be visible, got %d", s)
	}
	// Not strict (the default): pictures ahead are fetchable, and listed
	// for download, so they show offline.
	var st struct {
		Prefetch []int64 `json:"prefetch"`
	}
	p.doAny("GET", "/api/player/play", nil, &st)
	if len(st.Prefetch) != 2 {
		t.Errorf("prefetch = %v, want both pictures", st.Prefetch)
	}
	if s := p.status("/media/" + strconv.FormatInt(ahead, 10)); s != 200 {
		t.Errorf("not strict: a picture ahead should be fetchable, got %d", s)
	}
	// Strict: only reached steps.
	pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"strict_assets": true}, nil)
	if s := p.status("/media/" + strconv.FormatInt(ahead, 10)); s != 404 {
		t.Errorf("strict: a picture on a step ahead must stay hidden, got %d", s)
	}
	p.doAny("POST", "/api/player/play/answer", map[string]string{"node": "look", "guess": "grey"}, nil)
	if s := p.status("/media/" + strconv.FormatInt(ahead, 10)); s != 200 {
		t.Errorf("after reaching the end its picture should be visible, got %d", s)
	}
	if s := newClient(t, ts).status("/media/" + strconv.FormatInt(now, 10)); s != 404 {
		t.Errorf("an anonymous caller must see nothing, got %d", s)
	}
}
