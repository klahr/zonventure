package session

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/look"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// A theme effect naming a saved theme hands the phone that theme's whole
// look; a built-in theme is only named, as the phone knows it.
func TestSavedThemeReachesTheView(t *testing.T) {
	jungle := look.Look{Background: "#fff5d6", Font: "baloo-2", Backdrop: &look.Backdrop{Kind: "dots", Opacity: 0.1, Scale: 24}}
	theme := func(name string) graph.Node {
		return graph.Node{Slug: "to-" + name, Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.SetTheme, Theme: name}}
	}
	story := func(slug string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{{ID: "b1", Kind: graph.BlockText, Text: "Hi"}}}}
	}
	g := &graph.Graph{
		Themes: []graph.Theme{{Slug: "jungle", Name: "Jungle", Look: jungle}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			theme("jungle"), story("in-jungle"), theme("night"), story("at-night"),
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "to-jungle"},
			{From: "to-jungle", FromPort: "out", To: "in-jungle"},
			{From: "in-jungle", FromPort: "out", To: "to-night"},
			{From: "to-night", FromPort: "out", To: "at-night"},
			{From: "at-night", FromPort: "out", To: "end"},
		},
	}
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	v := tm.View()
	if v.Node != "in-jungle" || v.Theme != "jungle" || v.ThemeLook == nil || v.ThemeLook.Font != "baloo-2" || v.ThemeLook.Backdrop == nil {
		t.Fatalf("in the jungle: node %q theme %q look %+v", v.Node, v.Theme, v.ThemeLook)
	}
	if _, _, err := tm.Continue("in-jungle"); err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.Theme != "night" || v.ThemeLook != nil {
		t.Fatalf("at night: theme %q look %+v, want the built-in by name only", v.Theme, v.ThemeLook)
	}
}

// Until a step changes the theme, and after one goes back to default,
// the phone wears the main theme of the version it plays. An empty main
// theme hands nothing, so the organisation's look can show.
func TestMainThemeReachesTheView(t *testing.T) {
	jungle := look.Look{Background: "#fff5d6", Font: "baloo-2"}
	story := func(slug string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{{ID: "b1", Kind: graph.BlockText, Text: "Hi"}}}}
	}
	build := func(main look.Look) *graph.Graph {
		return &graph.Graph{
			Themes:    []graph.Theme{{Slug: "main", Name: "Main", Look: main}},
			MainTheme: "main",
			Nodes: []graph.Node{
				{Slug: "start", Type: graph.Start}, story("first"),
				{Slug: "to-default", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.SetTheme, Theme: "default"}},
				story("second"),
				{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
			},
			Edges: []graph.Edge{
				{From: "start", FromPort: "out", To: "first"},
				{From: "first", FromPort: "out", To: "to-default"},
				{From: "to-default", FromPort: "out", To: "second"},
				{From: "second", FromPort: "out", To: "end"},
			},
		}
	}
	g := build(jungle)
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.Node != "first" || v.ThemeLook == nil || v.ThemeLook.Font != "baloo-2" {
		t.Fatalf("first: node %q look %+v, want the main theme", v.Node, v.ThemeLook)
	}
	if _, _, err := tm.Continue("first"); err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.Theme != "default" || v.ThemeLook == nil || v.ThemeLook.Font != "baloo-2" {
		t.Fatalf("back to default: theme %q look %+v, want the main theme", v.Theme, v.ThemeLook)
	}
	tm, _, err = Start(build(look.Look{}), t0)
	if err != nil {
		t.Fatal(err)
	}
	if v := tm.View(); v.ThemeLook != nil {
		t.Fatalf("empty main theme: look %+v, want none", v.ThemeLook)
	}
}
