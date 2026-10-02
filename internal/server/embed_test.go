package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestEmbed_AssetsPresent is the H17 regression guard. The //go:embed
// directive enumerates files by name; a contributor adding a new
// asset to internal/server/web/ but forgetting the embed line gets
// a build that succeeds, ships, and panics at request time. This
// test walks the on-disk tree and asserts each shippable asset is
// also visible through webFS — catching the gap in CI.
//
// Files that exist on disk but are intentionally NOT embedded
// (e.g. *.bak) live in the excluded set below.
func TestEmbed_AssetsPresent(t *testing.T) {
	excluded := map[string]bool{}
	wantDirs := []string{
		"web",
		"web/locales/web",
		"web/locales/landing",
	}

	for _, dir := range wantDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if excluded[name] {
				continue
			}
			// Skip dotfiles and editor backups so a stray .swp doesn't
			// cause a CI failure.
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
				continue
			}
			rel := path.Join(dir, name)
			if _, err := webFS.ReadFile(rel); err != nil {
				t.Errorf("on-disk asset %q is not embedded: %v", rel, err)
			}
		}
	}
}

// No server-rendered page may load Google Fonts: it hands every
// visitor's IP address to Google (LG München, 3 O 17493/20). The brand
// fonts ship from the binary at /assets/fontface.css.
func TestNoGoogleFonts(t *testing.T) {
	entries, err := os.ReadDir("web")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".html" {
			continue
		}
		body, err := os.ReadFile(path.Join("web", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "fonts.googleapis.com") || strings.Contains(string(body), "fonts.gstatic.com") {
			t.Errorf("web/%s references Google Fonts", e.Name())
		}
	}
	h := chi.NewRouter()
	registerFontRoutes(h)
	for _, p := range []string{"/assets/fontface.css", "/assets/fonts/open-sans-latin.woff2", "/assets/fonts/aclonica-latin-ext.woff2", "/assets/fonts/OFL-OpenSans.txt"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if rr.Code != http.StatusOK || rr.Body.Len() == 0 {
			t.Errorf("GET %s = %d", p, rr.Code)
		}
	}
}
