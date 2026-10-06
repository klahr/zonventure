package admin

import (
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

func TestTouchedSteps(t *testing.T) {
	t.Parallel()
	before := &graph.Graph{
		Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "a", Type: graph.Story, Title: "A"}, {Slug: "end", Type: graph.End}},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "a"}, {From: "a", FromPort: "out", To: "end"}},
	}
	after := &graph.Graph{
		Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "a", Type: graph.Story, Title: "A"}, {Slug: "b", Type: graph.Story}, {Slug: "end", Type: graph.End, Title: "Done"}},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "a"}, {From: "a", FromPort: "out", To: "b"}, {From: "b", FromPort: "out", To: "end"}},
	}
	// New first, then rewired (a) and edited (end); start is untouched.
	if got, want := touchedSteps(before, after), []string{"b", "a", "end"}; !slices.Equal(got, want) {
		t.Errorf("touchedSteps = %v, want %v", got, want)
	}
	if got := touchedSteps(nil, before); len(got) != 3 {
		t.Errorf("without a draft every step is new, got %v", got)
	}
}
