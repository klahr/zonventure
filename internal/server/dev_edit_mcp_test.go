package server_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/server"
)

// TestMCPEditsTheDevPhonesStep: the assistant changes words on the step
// the author's dev phone shows, by quoting them, without naming the step
// or reading the graph — and the phone shows the change.
func TestMCPEditsTheDevPhonesStep(t *testing.T) {
	t.Setenv("ZV_MCP_ENABLED", "1")
	ts, _, cleanup := newTestServerWith(t, server.Config{PublicURL: "http://zv.test"})
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	env := graph.Envelope{Format: graph.FormatV2, Graph: graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hello", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Hello there, explorers.", nil)}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "hello"}, {From: "hello", FromPort: "out", To: "end"}},
	}}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", env, nil); s != 200 {
		t.Fatalf("save draft = %d", s)
	}
	var tk struct {
		TeamURL string `json:"team_url"`
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-ticket", nil, &tk); s != 200 {
		t.Fatalf("dev ticket = %d", s)
	}
	_, page := getRaw(t, newClient(t, ts), strings.TrimPrefix(tk.TeamURL, "http://zv.test"))
	m := regexp.MustCompile(`/play\?t=([A-Za-z0-9_-]+)`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no magic link on the dev team's page")
	}
	phone := newClient(t, ts)
	if s := phone.do("GET", "/play?t="+m[1], nil, nil); s != 200 {
		t.Fatalf("magic link = %d", s)
	}

	call := mcpTool(t, ts, pub)

	if text, isErr := call("get_step", map[string]any{"slug": "demo"}); isErr || !strings.Contains(text, `"slug":"hello"`) || !strings.Contains(text, `"out":"end"`) {
		t.Fatalf("get_step of the phone's step: %v %s", isErr, text)
	}
	if text, isErr := call("edit_adventure_graph", map[string]any{"slug": "demo", "ops": []any{
		map[string]any{"op": "set_text", "find": "explorers", "replace": "**detectives**"},
	}}); isErr {
		t.Fatalf("set_text: %s", text)
	}
	var st struct {
		View struct {
			Node string `json:"node"`
			Text string `json:"text"`
		} `json:"view"`
	}
	if s := phone.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View.Text != "Hello there, **detectives**." {
		t.Errorf("the phone shows %d %+v", s, st.View)
	}
	if text, isErr := call("edit_adventure_graph", map[string]any{"slug": "demo", "ops": []any{
		map[string]any{"op": "set_text", "find": "pirates", "replace": "x"},
	}}); !isErr || !strings.Contains(text, "detectives") {
		t.Errorf("a missing text should list the step's texts: %s", text)
	}
}
