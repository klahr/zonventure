package graphstore_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
)

func linear(answers ...string) *graph.Graph {
	g := &graph.Graph{Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End}}}
	prev, port := "start", graph.PortOut
	for i, a := range answers {
		slug := fmt.Sprintf("p%d", i+1)
		g.Nodes = append(g.Nodes, graph.Node{Slug: slug, Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer,
				Matchers: []match.Matcher{{Kind: match.Exact, Value: a}}}},
		}})
		g.Edges = append(g.Edges, graph.Edge{From: prev, FromPort: port, To: slug})
		prev, port = slug, "solved"
	}
	g.Edges = append(g.Edges, graph.Edge{From: prev, FromPort: port, To: "end"})
	return g
}

func TestGraphstoreVersionsAndRefusesInvalid(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author) VALUES ('gs', 'gs', '', '') RETURNING id`).Scan(&advID); err != nil {
		t.Fatal(err)
	}

	if _, err := graphstore.Latest(ctx, db, advID); !errors.Is(err, graphstore.ErrNotFound) {
		t.Errorf("Latest with nothing saved: got %v, want ErrNotFound", err)
	}

	s1, _, err := graphstore.Save(ctx, db, advID, 0, linear("a"))
	if err != nil || s1.Version != 1 {
		t.Fatalf("first save: version %d, %v", s1.Version, err)
	}
	s2, _, err := graphstore.Save(ctx, db, advID, 0, linear("a", "b"))
	if err != nil || s2.Version != 2 {
		t.Fatalf("second save: version %d, %v", s2.Version, err)
	}
	latest, err := graphstore.Latest(ctx, db, advID)
	if err != nil || latest.Version != 2 || len(latest.Graph.Nodes) != 4 {
		t.Errorf("Latest = version %d with %d nodes, %v", latest.Version, len(latest.Graph.Nodes), err)
	}

	// A broken graph never reaches storage.
	broken := linear("a")
	broken.Edges = nil
	_, report, err := graphstore.Save(ctx, db, advID, 0, broken)
	var inv *graphstore.InvalidError
	if !errors.As(err, &inv) || !report.HasErrors() {
		t.Errorf("saving an invalid graph: got %v, want an InvalidError", err)
	}
	if latest, _ := graphstore.Latest(ctx, db, advID); latest.Version != 2 {
		t.Errorf("a refused save must not create a version; latest is %d", latest.Version)
	}
}

func TestByIDAndInvalidErrorMessage(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author) VALUES ('gs2', 'gs2', '', '') RETURNING id`).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	saved, _, err := graphstore.Save(ctx, db, advID, 0, linear("a"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := graphstore.ByID(ctx, db, saved.ID)
	if err != nil || got.Version != saved.Version || got.AdventureID != advID {
		t.Errorf("ByID = %+v, %v", got, err)
	}
	if _, err := graphstore.ByID(ctx, db, saved.ID+999); !errors.Is(err, graphstore.ErrNotFound) {
		t.Errorf("ByID for a missing id: got %v, want ErrNotFound", err)
	}

	broken := linear("a")
	broken.Edges = nil
	_, _, err = graphstore.Save(ctx, db, advID, 0, broken)
	if err == nil || err.Error() == "" {
		t.Fatal("expected a descriptive error")
	}
	var inv *graphstore.InvalidError
	if !errors.As(err, &inv) || len(inv.Report.Errors()) == 0 {
		t.Errorf("the error should carry the full report, got %v", err)
	}
}
