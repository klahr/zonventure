package admin_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
)

// graphFixture seeds an org_admin and an adventure of their org with a
// published graph: a puzzle with a picture block, a hint with a sound,
// and a journal note showing the picture again.
func graphFixture(t *testing.T, slug string) (db *sql.DB, advID int64, user auth.User, pic, snd []byte) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)

	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1)
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		userID, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, author, org_id) VALUES ($1, 'Graph Tour', 'Acme', $2)
		RETURNING id
	`, slug, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}

	pic = fixtureAvatarPNG
	snd = []byte("ID3 not really an mp3")
	picID := insertMedia(t, db, advID, "image", "image/png", pic)
	sndID := insertMedia(t, db, advID, "audio", "audio/mpeg", snd)
	pdfID := insertMedia(t, db, advID, "pdf", "application/pdf", fixturePDF)

	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "p1", Type: graph.Puzzle, Unlocks: []string{"clue"}, PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{
					{ID: "t", Kind: graph.BlockText, Text: "What is it?"},
					{ID: "pic", Kind: graph.BlockImage, Media: picID, Alt: "a pixel"},
				},
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer,
					Matchers: []match.Matcher{{Kind: match.Exact, Value: "pixel"}}}},
				Hints: []graph.Hint{{Text: "Listen", Media: []graph.Media{{ID: sndID, Kind: graph.MediaAudio}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: graph.PortOut, To: "p1"},
			{From: "p1", FromPort: "solved", To: "end"},
		},
		Journal: []graph.JournalEntry{{Slug: "clue", Title: "Clue",
			Media: []graph.Media{{ID: picID, Kind: graph.MediaImage}}}},
		PrintMaterials: []graph.PrintMaterial{{ID: "letter", Name: "Letter", Delivery: graph.DeliveryPosted,
			Files: map[string]int64{"en": pdfID}}},
	}
	if _, _, err := graphstore.Publish(context.Background(), db, advID, userID, g, "First"); err != nil {
		t.Fatalf("publish source graph: %v", err)
	}
	user = auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	return db, advID, user, pic, snd
}

// fixturePDF is a print material's file in graphFixture.
var fixturePDF = []byte("%PDF-1.4\n%%EOF\n")

func insertMedia(t *testing.T, db *sql.DB, advID int64, kind, mime string, content []byte) int64 {
	t.Helper()
	sum := sha256.Sum256(content)
	var id int64
	if err := db.QueryRow(`
		INSERT INTO adventure_media(adventure_id, kind, mime_type, size_bytes, sha256, content)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, advID, kind, mime, int64(len(content)), hex.EncodeToString(sum[:]), content).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// assertGraphMediaOwned checks every media id g names is a row of advID
// holding one of want, that the picture block shows pic, and that the
// letter is fixturePDF.
func assertGraphMediaOwned(t *testing.T, db *sql.DB, advID int64, g *graph.Graph, pic []byte, want ...[]byte) {
	t.Helper()
	ids := g.MediaIDs()
	if len(ids) != len(want)+1 {
		t.Fatalf("graph names %d media, want %d and a letter", len(ids), len(want))
	}
	for _, id := range ids {
		var owner int64
		var content []byte
		if err := db.QueryRow(`SELECT adventure_id, content FROM adventure_media WHERE id = $1`, id).Scan(&owner, &content); err != nil {
			t.Fatalf("media %d: %v", id, err)
		}
		if owner != advID {
			t.Errorf("media %d belongs to adventure %d, want %d", id, owner, advID)
		}
	}
	n, _ := g.Node("p1")
	var picID int64
	for _, b := range n.PuzzleCfg.Blocks {
		if b.Kind == graph.BlockImage {
			picID = b.Media
		}
	}
	var content []byte
	if err := db.QueryRow(`SELECT content FROM adventure_media WHERE id = $1`, picID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if string(content) != string(pic) {
		t.Errorf("picture block shows the wrong file")
	}
	if je := g.Journal[0].Media[0].ID; je != picID {
		t.Errorf("journal picture = %d, want the block's %d", je, picID)
	}
	var owner int64
	if err := db.QueryRow(`SELECT adventure_id, content FROM adventure_media WHERE id = $1 AND kind = 'pdf'`,
		g.PrintMaterials[0].Files["en"]).Scan(&owner, &content); err != nil {
		t.Fatalf("letter: %v", err)
	}
	if owner != advID || string(content) != string(fixturePDF) {
		t.Errorf("letter belongs to adventure %d with %d bytes, want %d with the fixture's", owner, len(content), advID)
	}
}

func TestDuplicateAdventure_CopiesGraphAndMedia(t *testing.T) {
	t.Parallel()
	db, srcID, user, pic, snd := graphFixture(t, "graphy")
	ctx := context.Background()

	// A draft ahead of the release, to see it carried as the draft.
	d, err := graphstore.Published(ctx, db, srcID)
	if err != nil {
		t.Fatal(err)
	}
	d.Graph.Nodes[1].Title = "Draft title"
	if _, _, err := graphstore.SaveDraft(ctx, db, srcID, user.ID, d.Graph); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Post("/api/admin/adventures/{slug}/duplicate", h.DuplicateAdventure)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/adventures/graphy/duplicate", strings.NewReader(`{"slug":"graphy-copy"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}

	var newID int64
	if err := db.QueryRow(`SELECT id FROM adventures WHERE slug = 'graphy-copy'`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM adventure_media WHERE adventure_id = $1`, newID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("copy has %d media rows, want 3", n)
	}

	pub, err := graphstore.Published(ctx, db, newID)
	if err != nil {
		t.Fatalf("copy has no published graph: %v", err)
	}
	if pub.Label != "First" {
		t.Errorf("published label = %q, want First", pub.Label)
	}
	assertGraphMediaOwned(t, db, newID, pub.Graph, pic, pic, snd)

	draft, err := graphstore.LoadDraft(ctx, db, newID)
	if err != nil {
		t.Fatalf("copy has no draft: %v", err)
	}
	if got := draft.Graph.Nodes[1].Title; got != "Draft title" {
		t.Errorf("draft title = %q, want the source draft's", got)
	}
	assertGraphMediaOwned(t, db, newID, draft.Graph, pic, pic, snd)

	// The source is untouched.
	src, err := graphstore.Published(ctx, db, srcID)
	if err != nil {
		t.Fatal(err)
	}
	assertGraphMediaOwned(t, db, srcID, src.Graph, pic, pic, snd)
	var published int
	if err := db.QueryRow(`SELECT published FROM adventures WHERE id = $1`, newID).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 0 {
		t.Errorf("copy published = %d, want 0", published)
	}
}
