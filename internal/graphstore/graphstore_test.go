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
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('gs', 'gs', '', '', $1) RETURNING id`, dbtest.PublisherOrg(t, db)).Scan(&advID); err != nil {
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
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('gs2', 'gs2', '', '', $1) RETURNING id`, dbtest.PublisherOrg(t, db)).Scan(&advID); err != nil {
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

// The dazyflow release model: drafts accept anything, publishing refuses
// errors, the published pointer moves on publish and on rollback, and
// history lists every release.
func TestDraftsAndReleases(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('rel', 'rel', '', '', $1) RETURNING id`, dbtest.PublisherOrg(t, db)).Scan(&advID); err != nil {
		t.Fatal(err)
	}

	broken := linear("a")
	broken.Edges = broken.Edges[:1]
	if _, report, err := graphstore.SaveDraft(ctx, db, advID, 0, broken); err != nil || !report.HasErrors() {
		t.Fatalf("a broken draft must save, and report its errors: %v %v", err, report)
	}
	if d, err := graphstore.LoadDraft(ctx, db, advID); err != nil || len(d.Graph.Edges) != 1 {
		t.Fatalf("draft = %+v, %v", d, err)
	}
	var invalid *graphstore.InvalidError
	if _, _, err := graphstore.Publish(ctx, db, advID, 0, broken, ""); !errors.As(err, &invalid) {
		t.Fatalf("publishing a broken graph must be refused, got %v", err)
	}
	if _, err := graphstore.Published(ctx, db, advID); !errors.Is(err, graphstore.ErrNotFound) {
		t.Fatal("nothing should be published yet")
	}

	v1, _, err := graphstore.Publish(ctx, db, advID, 0, linear("a"), "First")
	if err != nil {
		t.Fatal(err)
	}
	v2, _, err := graphstore.Publish(ctx, db, advID, 0, linear("a", "b"), "")
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := graphstore.Published(ctx, db, advID); p.Version != v2.Version {
		t.Errorf("published = v%d, want the newest", p.Version)
	}
	if _, err := graphstore.PublishVersion(ctx, db, advID, v1.Version); err != nil {
		t.Fatal(err)
	}
	if p, _ := graphstore.Published(ctx, db, advID); p.Version != v1.Version || p.Label != "First" {
		t.Errorf("after rollback published = v%d %q", p.Version, p.Label)
	}
	if err := graphstore.SetLabel(ctx, db, advID, v2.Version, "Second"); err != nil {
		t.Fatal(err)
	}
	vs, err := graphstore.Versions(ctx, db, advID)
	if err != nil || len(vs) != 2 || vs[0].Label != "Second" || vs[0].Published || !vs[1].Published {
		t.Errorf("history = %+v, %v", vs, err)
	}

	moved := linear("a")
	moved.Nodes[1].X = 500
	if graphstore.Differs(linear("a"), moved) {
		t.Error("moving a card is not a change to publish")
	}
	framed := linear("a")
	framed.Frames = []graph.Frame{{ID: "f1", Title: "Carl", Width: 400, Height: 300}}
	if graphstore.Differs(linear("a"), framed) {
		t.Error("drawing a frame is not a change to publish")
	}
	if !graphstore.Differs(linear("a"), linear("b")) {
		t.Error("a changed answer is")
	}
}

// The adventure's language column is the one place the language is set:
// saving writes it into the graph, over whatever the graph said.
func TestGraphstoreStampsAdventureLanguage(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, language, org_id) VALUES ('gl', 'gl', '', '', 'sv', $1) RETURNING id`, dbtest.PublisherOrg(t, db)).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	g := linear("a")
	g.Language = "de"
	if _, _, err := graphstore.SaveDraft(ctx, db, advID, 0, g); err != nil {
		t.Fatal(err)
	}
	d, err := graphstore.LoadDraft(ctx, db, advID)
	if err != nil || d.Graph.Language != "sv" {
		t.Fatalf("draft language = %q, %v; want the adventure's", d.Graph.Language, err)
	}
	s, _, err := graphstore.Publish(ctx, db, advID, 0, linear("a"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE adventures SET language = 'en' WHERE id = $1`, advID); err != nil {
		t.Fatal(err)
	}
	if got, err := graphstore.ByID(ctx, db, s.ID); err != nil || got.Graph.Language != "sv" {
		t.Errorf("a published version keeps the language it was published with: %q, %v", got.Graph.Language, err)
	}
	if d, err := graphstore.LoadDraft(ctx, db, advID); err != nil || d.Graph.Language != "en" {
		t.Errorf("the draft follows the adventure: %q, %v", d.Graph.Language, err)
	}
}

// A save based on an older draft is refused, so the designer and an
// assistant editing at once cannot silently overwrite each other.
func TestSaveDraftIfRefusesStaleBase(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('base', 'b', '', '', $1) RETURNING id`, dbtest.PublisherOrg(t, db)).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	first, _, err := graphstore.SaveDraftIf(ctx, db, advID, 0, linear("a"), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := graphstore.SaveDraftIf(ctx, db, advID, 0, linear("b"), &first.UpdatedAt)
	if err != nil {
		t.Fatalf("a save based on the current draft should succeed: %v", err)
	}
	if _, _, err := graphstore.SaveDraftIf(ctx, db, advID, 0, linear("c"), &first.UpdatedAt); !errors.Is(err, graphstore.ErrDraftChanged) {
		t.Fatalf("a save based on an older draft should be refused, got %v", err)
	}
	d, err := graphstore.LoadDraft(ctx, db, advID)
	if err != nil || !d.UpdatedAt.Equal(second.UpdatedAt) {
		t.Fatalf("the refused save changed the draft: %v %v", d.UpdatedAt, err)
	}
}
