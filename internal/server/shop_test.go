package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServeShop covers /shop/ path mapping: index, extensionless pages,
// plain files, and the 404 fallbacks for directories, traversal and
// missing files.
func TestServeShop(t *testing.T) {
	t.Cleanup(snapshotLandingState())
	dir := t.TempDir()
	shop := filepath.Join(dir, "shop")
	files := map[string]string{
		"index.html": "SHOP INDEX",
		"book.html":  "BOOK PAGE",
		"404.html":   "SHOP 404",
		"img/a.webp": "IMG",
		"../x.html":  "OUTSIDE",
	}
	for name, body := range files {
		p := filepath.Join(shop, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	webOverrideDir = dir

	cases := []struct {
		path   string
		status int
		body   string
	}{
		{"/shop/", http.StatusOK, "SHOP INDEX"},
		{"/shop/book", http.StatusOK, "BOOK PAGE"},
		{"/shop/book.html", http.StatusOK, "BOOK PAGE"},
		{"/shop/img/a.webp", http.StatusOK, "IMG"},
		{"/shop/img", http.StatusNotFound, "SHOP 404"},
		{"/shop/img/", http.StatusNotFound, "SHOP 404"},
		{"/shop/missing", http.StatusNotFound, "SHOP 404"},
		{"/shop/../x.html", http.StatusNotFound, "SHOP 404"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = c.path
		rec := httptest.NewRecorder()
		serveShop(rec, req)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if got := rec.Body.String(); !strings.Contains(got, c.body) {
			t.Errorf("%s: body %q, want %q", c.path, got, c.body)
		}
	}
}

// TestServeShopWithoutDir: no override dir means /shop/ is zv's 404.
func TestServeShopWithoutDir(t *testing.T) {
	t.Cleanup(snapshotLandingState())
	webOverrideDir = t.TempDir()
	rec := httptest.NewRecorder()
	serveShop(rec, httptest.NewRequest(http.MethodGet, "/shop/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}
