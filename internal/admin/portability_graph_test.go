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
	db, _, user, pic, snd := graphFixture(t, "graphy")
	ctx := context.Background()

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
	if len(manifest.Graph) == 0 || len(manifest.Media) != 2 {
		t.Fatalf("manifest graph %d bytes, %d graph media; want a graph and 2", len(manifest.Graph), len(manifest.Media))
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
	pub, err := graphstore.Published(ctx, db, newID)
	if err != nil {
		t.Fatalf("import has no published graph: %v", err)
	}
	assertGraphMediaOwned(t, db, newID, pub.Graph, pic, pic, snd)
	draft, err := graphstore.LoadDraft(ctx, db, newID)
	if err != nil {
		t.Fatalf("import has no draft: %v", err)
	}
	assertGraphMediaOwned(t, db, newID, draft.Graph, pic, pic, snd)
}

// TestImportClassicEnvelopeRefused refuses an export from the classic
// editor — pages and no graph — since nothing can play it, and still
// imports an adventure with neither, as an empty one to design.
func TestImportClassicEnvelopeRefused(t *testing.T) {
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
