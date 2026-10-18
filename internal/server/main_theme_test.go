package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/look"
)

// The main theme is the adventure's look: publishing makes it the look
// the store and the pages before play show, and the look can't be set
// past it by hand.
func TestMainThemeIsTheLook(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "hello", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Hello.", nil)}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete, Blocks: graph.LegacyBlocks("Well done.", nil)}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "hello"},
			{From: "hello", FromPort: "out", To: "end"},
		},
		Themes:    []graph.Theme{{Slug: "jungle", Name: "Jungle", Look: look.Look{Accent: "#2e7d32", Font: "baloo-2"}}},
		MainTheme: "jungle",
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: g}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"accent_color": "#ff0000"}, nil); s != 409 {
		t.Fatalf("setting the accent past the main theme = %d, want 409", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"title": "Demo"}, nil); s/100 != 2 {
		t.Fatalf("renaming = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	var edit map[string]any
	if s := pub.doAny("GET", "/api/admin/adventures/demo/edit", nil, &edit); s != 200 || edit["accent_color"] != "#2e7d32" || edit["font"] != "baloo-2" {
		t.Fatalf("after publishing, the look should be the main theme's: %d accent %v font %v", s, edit["accent_color"], edit["font"])
	}
}
