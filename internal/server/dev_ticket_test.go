package server_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/server"
)

// A dev ticket plays the draft live: an author's phone sees each save at
// once, can be moved to any step and started over, and the draft's dev
// row never shows among the adventure's versions.
func TestDevTicketPlaysTheDraftLive(t *testing.T) {
	ts, _, cleanup := newTestServerWith(t, server.Config{PublicURL: "http://zv.test"})
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	story := func(slug, text string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks(text, nil)}}
	}
	draft := func(first graph.Node) graph.Envelope {
		return graph.Envelope{Format: graph.FormatV2, Graph: graph.Graph{
			Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, first, {Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}},
			Edges: []graph.Edge{{From: "start", FromPort: "out", To: first.Slug}, {From: first.Slug, FromPort: "out", To: "end"}},
		}}
	}
	save := func(env graph.Envelope) {
		t.Helper()
		if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", env, nil); s != 200 {
			t.Fatalf("save draft = %d", s)
		}
	}
	save(draft(story("hello", "Hello there.")))

	var tk, again struct {
		TeamURL string `json:"team_url"`
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-ticket", nil, &tk); s != 200 || !strings.HasPrefix(tk.TeamURL, "http://zv.test/team/") {
		t.Fatalf("dev ticket = %d %+v", s, tk)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-ticket", nil, &again); s != 200 || again.TeamURL != tk.TeamURL {
		t.Errorf("a second dev ticket = %d %q, want the same %q", s, again.TeamURL, tk.TeamURL)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-ticket/send", map[string]string{"email": "colleague@example.com"}, nil); s != 204 {
		t.Errorf("send the dev ticket = %d, want 204", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-ticket/send", map[string]string{"email": "not an email"}, nil); s != 400 {
		t.Errorf("send to a bad address = %d, want 400", s)
	}

	anon := newClient(t, ts)
	_, page := getRaw(t, anon, strings.TrimPrefix(tk.TeamURL, "http://zv.test"))
	m := regexp.MustCompile(`/play\?t=([A-Za-z0-9_-]+)`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no magic link on the dev team's page")
	}
	phone := newClient(t, ts)
	if s := phone.do("GET", "/play?t="+m[1], nil, nil); s != 200 {
		t.Fatalf("magic link = %d", s)
	}
	type devState struct {
		View *struct {
			Node string `json:"node"`
			Text string `json:"text"`
		} `json:"view"`
		Dev *struct {
			Steps []struct {
				Slug string `json:"slug"`
			} `json:"steps"`
		} `json:"dev"`
	}
	state := func() devState {
		t.Helper()
		var st devState
		if s := phone.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil {
			t.Fatalf("play state = %d %+v", s, st)
		}
		return st
	}
	if st := state(); st.View.Node != "hello" || st.View.Text != "Hello there." || st.Dev == nil || len(st.Dev.Steps) != 2 {
		t.Fatalf("the draft on the dev phone: %+v %+v", st.View, st.Dev)
	}

	// A save shows at once — nothing published, no event restarted.
	save(draft(story("hello", "Hello again!")))
	if st := state(); st.View.Text != "Hello again!" {
		t.Errorf("after the edit the phone shows %q", st.View.Text)
	}

	// The assistant moves the phone on; the phone moves itself back.
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-goto", map[string]string{"node": "end"}, nil); s != 204 {
		t.Fatalf("dev goto end = %d", s)
	}
	if s := phone.doAny("POST", "/api/player/dev/goto", map[string]string{"node": "hello"}, nil); s != 204 {
		t.Fatalf("phone back to hello = %d", s)
	}
	var screen struct {
		View struct {
			Node string `json:"node"`
		} `json:"view"`
	}
	if s := pub.doAny("GET", "/api/admin/adventures/demo/dev-screen", nil, &screen); s != 200 || screen.View.Node != "hello" {
		t.Errorf("dev screen = %d %+v", s, screen.View)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-goto", map[string]string{"node": "nowhere"}, nil); s != 400 {
		t.Errorf("a step the draft lacks = %d, want 400", s)
	}

	// The step the phone stands on is taken out: it waits at the start.
	save(draft(story("intro", "A new beginning.")))
	if st := state(); st.View.Node == "hello" {
		t.Errorf("still on a removed step: %+v", st.View)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-reset", nil, nil); s != 204 {
		t.Fatalf("dev reset = %d", s)
	}
	if st := state(); st.View.Node != "intro" || st.View.Text != "A new beginning." {
		t.Errorf("after starting over: %+v", st.View)
	}

	// Finished, the dev event is not: the phone starts over and plays on.
	if s := phone.doAny("POST", "/api/player/play/continue", map[string]string{"node": "intro"}, nil); s != 200 {
		t.Fatalf("continue to the end = %d", s)
	}
	if s := pub.doAny("GET", "/api/admin/adventures/demo/dev-screen", nil, nil); s != 200 {
		t.Errorf("dev screen after finishing = %d", s)
	}
	if s := phone.doAny("POST", "/api/player/dev/goto", map[string]string{"node": "intro"}, nil); s != 204 {
		t.Fatalf("phone back after finishing = %d", s)
	}
	if st := state(); st.View.Node != "intro" {
		t.Errorf("after stepping back from the end: %+v", st.View)
	}
	if s := phone.doAny("POST", "/api/player/play/continue", map[string]string{"node": "intro"}, nil); s != 200 {
		t.Fatalf("continue to the end again = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-reset", nil, nil); s != 204 {
		t.Fatalf("dev reset after finishing = %d", s)
	}
	if st := state(); st.View.Node != "intro" {
		t.Errorf("after starting over from the end: %+v", st.View)
	}

	// The dev row is no version of the adventure.
	var versions struct {
		Versions []struct {
			Version int `json:"version"`
		} `json:"versions"`
	}
	if s := pub.doAny("GET", "/api/admin/adventures/demo/graph/versions", nil, &versions); s == 200 {
		for _, v := range versions.Versions {
			if v.Version == 0 {
				t.Errorf("the dev row is listed as a version")
			}
		}
	}
}
