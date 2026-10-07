package admin_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// pngOf is a w×h picture.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, h/2, color.RGBA{200, 30, 90, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTrail_Cover: a look's own cover picture must be one of the
// adventure's pictures, outlasts a change of starter theme, and is what
// the join page shows — served by the player link — and print uses.
func TestTrail_Cover(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.trailRoutes()
	f.public.(*chi.Mux).Get("/api/join/{token}/cover", f.h.JoinCover)
	slug := f.newTrail(t, "kids")

	pic := pngOf(t, 400, 400)
	sum := sha256.Sum256(pic)
	var media, foreign int64
	if err := f.h.DB.QueryRow(`
		INSERT INTO adventure_media (adventure_id, kind, mime_type, size_bytes, sha256, content)
		SELECT id, 'image', 'image/png', $2, $3, $4 FROM adventures WHERE slug = $1 RETURNING id
	`, slug, len(pic), hex.EncodeToString(sum[:]), pic).Scan(&media); err != nil {
		t.Fatal(err)
	}
	if err := f.h.DB.QueryRow(`
		INSERT INTO adventure_media (adventure_id, kind, mime_type, size_bytes, sha256, content)
		SELECT id, 'image', 'image/png', $1, 'other', $2 FROM adventures WHERE slug = 'treasure' RETURNING id
	`, len(pic), pic).Scan(&foreign); err != nil {
		t.Fatal(err)
	}

	path := "/api/admin/adventures/" + slug + "/cards/theme"
	look := func(cover int64) map[string]any {
		l := map[string]any{"accent": "#e91e63"}
		if cover > 0 {
			l["style"] = map[string]any{"cover": cover}
		}
		return l
	}
	if w := call(t, f.console, "PUT", path, map[string]any{"id": "pirate", "look": look(foreign)}); w.Code != http.StatusBadRequest {
		t.Errorf("another adventure's picture: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.console, "PUT", path, map[string]any{"id": "pirate", "look": look(media)}); w.Code != http.StatusNoContent {
		t.Fatalf("cover: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.console, "PUT", path, map[string]any{"id": "space", "look": look(0)}); w.Code != http.StatusNoContent {
		t.Fatalf("new theme: %d %s", w.Code, w.Body)
	}
	var cover int64
	_ = f.h.DB.QueryRow(`SELECT COALESCE((look_style->>'cover')::bigint, 0) FROM adventures WHERE slug = $1`, slug).Scan(&cover)
	if cover != media {
		t.Errorf("cover after a change of theme = %d, want %d", cover, media)
	}

	if w := call(t, f.console, "POST", "/api/admin/adventures/"+slug+"/host", map[string]any{"blocks": 1}); w.Code != http.StatusCreated {
		t.Fatalf("host it: %d %s", w.Code, w.Body)
	}
	host := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.lastMail(t))[1]
	player := f.host(t, host).playerToken()
	w := call(t, f.public, "GET", "/api/join/"+player, nil)
	if !strings.Contains(w.Body.String(), `"cover_url":"/api/join/`+player+`/cover`) {
		t.Errorf("join page: %s", w.Body)
	}
	w = call(t, f.public, "GET", "/api/join/"+player+"/cover", nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), pic) {
		t.Errorf("cover: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if w := call(t, f.public, "GET", "/api/join/nope/cover", nil); w.Code != http.StatusNotFound {
		t.Errorf("cover by a wrong link: %d", w.Code)
	}
	if w := call(t, f.console, "GET", "/api/admin/adventures/"+slug+"/cards.pdf", nil); w.Code != http.StatusOK || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("pdf with a cover: %d", w.Code)
	}
}
