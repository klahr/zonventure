package player

import (
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// The dev bar lists steps as a team meets them, not as they were made;
// a step nothing leads to comes last.
func TestRouteOrder(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "welcome", Type: graph.Story},
			{Slug: "end", Type: graph.End},
			{Slug: "orphan", Type: graph.Story},
			{Slug: "hall", Type: graph.Story},
		},
		Edges: []graph.Edge{
			{From: "hall", FromPort: graph.PortOut, To: "end"},
			{From: "start", FromPort: graph.PortOut, To: "welcome"},
			{From: "welcome", FromPort: graph.PortOut, To: "hall"},
		},
	}
	var got []string
	for _, n := range routeOrder(g) {
		got = append(got, n.Slug)
	}
	if want := []string{"start", "welcome", "hall", "end", "orphan"}; !slices.Equal(got, want) {
		t.Errorf("routeOrder = %v, want %v", got, want)
	}
}
