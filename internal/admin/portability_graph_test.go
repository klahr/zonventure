package admin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/graphstore"
)

// TestPortabilityGraphRoundTrip exports a graph adventure and imports
// the archive: the import gets its own media rows and a published graph
// whose picture block shows the same picture.
func TestPortabilityGraphRoundTrip(t *testing.T) {
	t.Parallel()
	db, advID, user, pic, snd := graphFixture(t, "graphy")
	ctx := context.Background()
	// The look travels too: fonts, style, and the style's sound as media.
	if _, err := db.Exec(`
		UPDATE adventures SET author = 'Vasteras Turism', font = 'fredoka', heading_font = 'luckiest-guy',
		       look_style = jsonb_build_object('transition', 'pop', 'cheer_text', 'Snyggt!',
		           'sound_right', (SELECT id FROM adventure_media WHERE adventure_id = $1 AND kind = 'audio' LIMIT 1))
		WHERE id = $1
	`, advID); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Get("/api/admin/adventures/{slug}/export", h.ExportAdventure)
	r.Post("/api/admin/adventures/import", h.ImportAdventure)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/adventures/graphy/export", nil))
	if w.Code != 200 {
		t.Fatalf("export status = %d; body = %s", w.Code, w.Body.String())
	}
	archive := w.Body.Bytes()

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Format string          `json:"format"`
		Graph  json.RawMessage `json:"graph"`
		Media  []struct {
			File string `json:"file"`
		} `json:"graph_media"`
	}
	files := map[string]bool{}
	for _, f := range zr.File {
		files[f.Name] = true
		if f.Name == "adventure.json" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			if err := json.Unmarshal(b, &manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	if manifest.Format != "zonventure-adventure-v1" {
		t.Errorf("format = %q", manifest.Format)
	}
	if len(manifest.Graph) == 0 || len(manifest.Media) != 3 {
		t.Fatalf("manifest graph %d bytes, %d graph media; want a graph and 3", len(manifest.Graph), len(manifest.Media))
	}
	for _, m := range manifest.Media {
		if !files[m.File] {
			t.Errorf("graph media %q not in the archive", m.File)
		}
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "graphy.zvadv.zip")
	fw.Write(archive)
	mw.Close()
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/adventures/import?slug=graphy-imported", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("import status = %d; body = %s", w.Code, w.Body.String())
	}

	var newID int64
	if err := db.QueryRow(`SELECT id FROM adventures WHERE slug = 'graphy-imported'`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	// A draft, to publish when the importer chooses, by the importing org.
	if _, err := graphstore.Published(ctx, db, newID); !errors.Is(err, graphstore.ErrNotFound) {
		t.Errorf("import published: %v, want a draft only", err)
	}
	var author, original string
	if err := db.QueryRow(`SELECT author, original_author FROM adventures WHERE id = $1`, newID).Scan(&author, &original); err != nil {
		t.Fatal(err)
	}
	if author != "Acme" || original != "Vasteras Turism" {
		t.Errorf("author %q, originally %q; want Acme, originally Vasteras Turism", author, original)
	}
	draft, err := graphstore.LoadDraft(ctx, db, newID)
	if err != nil {
		t.Fatalf("import has no draft: %v", err)
	}
	assertGraphMediaOwned(t, db, newID, draft.Graph, pic, pic, snd)

	var font, heading, transition, cheer string
	var soundOwned bool
	if err := db.QueryRow(`
		SELECT font, heading_font, look_style->>'transition', look_style->>'cheer_text',
		       EXISTS (SELECT 1 FROM adventure_media WHERE id = (look_style->>'sound_right')::bigint AND adventure_id = $1 AND kind = 'audio')
		FROM adventures WHERE id = $1
	`, newID).Scan(&font, &heading, &transition, &cheer, &soundOwned); err != nil {
		t.Fatal(err)
	}
	if font != "fredoka" || heading != "luckiest-guy" || transition != "pop" || cheer != "Snyggt!" || !soundOwned {
		t.Errorf("imported look = %q %q %q %q sound owned %v; want the exported one", font, heading, transition, cheer, soundOwned)
	}
}

// TestImportClassicEnvelopeRefused refuses an export from the classic
// editor — pages and no graph — since nothing can play it, and still
// imports an adventure with neither, as an empty one to design.
func TestImportClassicEnvelopeRefused(t *testing.T) {
	t.Parallel()
	db, _, user, _, _ := graphFixture(t, "graphy")
	ctx := context.Background()
	manifest := `{"format":"zonventure-adventure-v1","exported_at":"2026-01-01T00:00:00Z",
		"adventure":{"slug":"lanes-only","title":"Lanes","description":"",
		"lanes":[{"idx":0,"mode":"linear"}],
		"pages":[{"idx":0,"lane_idx":0,"title":"One","lat":0,"lng":0}]}}`
	if _, _, _, err := admin.ImportEnvelopeJSON(ctx, db, []byte(manifest), "", user); err == nil || !strings.Contains(err.Error(), "classic editor") {
		t.Fatalf("import: %v, want the classic-editor refusal", err)
	}

	empty := `{"format":"zonventure-adventure-v1","exported_at":"2026-01-01T00:00:00Z",
		"adventure":{"slug":"empty","title":"Empty","description":""}}`
	advID, _, _, err := admin.ImportEnvelopeJSON(ctx, db, []byte(empty), "", user)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := graphstore.Published(ctx, db, advID); !errors.Is(err, graphstore.ErrNotFound) {
		t.Errorf("published graph: %v, want none", err)
	}
}

// TestImportEnvelopeGraphMediaMissing refuses a graph whose media is not
// in the archive — as the MCP path, which has no archive, must.
func TestImportEnvelopeGraphMediaMissing(t *testing.T) {
	t.Parallel()
	db, srcID, user, _, _ := graphFixture(t, "graphy")
	ctx := context.Background()
	manifest, err := admin.ExportAdventureEnvelopeJSON(ctx, db, srcID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = admin.ImportEnvelopeJSON(ctx, db, manifest, "graphy-2", user)
	if err == nil || !strings.Contains(err.Error(), "missing from archive") {
		t.Fatalf("import: %v, want a missing-media refusal", err)
	}
}
