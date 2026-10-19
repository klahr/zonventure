package play

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

func TestThemeBefore(t *testing.T) {
	t.Parallel()
	theme := func(slug, th string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.SetTheme, Theme: th}}
	}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "a", Type: graph.Story},
			theme("night", "night"),
			{Slug: "b", Type: graph.Story},
			theme("day", "default"),
			{Slug: "c", Type: graph.Story},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "a"},
			{From: "a", FromPort: "out", To: "night"},
			{From: "night", FromPort: "out", To: "b"},
			{From: "b", FromPort: "out", To: "day"},
			{From: "day", FromPort: "out", To: "c"},
		},
	}
	for slug, want := range map[string]string{"a": "", "b": "night", "c": "default"} {
		if got := themeBefore(g, slug); got != want {
			t.Errorf("themeBefore(%q) = %q, want %q", slug, got, want)
		}
	}
}
