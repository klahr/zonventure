package server

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// serveShop serves the static site in the shop/ subdirectory of
// ZV_WEB_OVERRIDE_DIR at /shop/. Unlike the rest of the override dir,
// which only shadows files zv already knows, shop/ is served as is:
// any file in it is reachable, and an extensionless path maps to its
// .html sibling so /shop/book serves shop/book.html. Unknown paths get
// shop/404.html when there is one. Without an override dir, or without
// a shop/ in it, /shop/ is an ordinary 404.
func serveShop(w http.ResponseWriter, r *http.Request) {
	if webOverrideDir == "" {
		render404(w, r)
		return
	}
	// os.Root keeps a symlink inside shop/ from reaching the rest of
	// the host's filesystem.
	root, err := os.OpenRoot(filepath.Join(webOverrideDir, "shop"))
	if err != nil {
		render404(w, r)
		return
	}
	defer root.Close()
	fsys := root.FS()

	name := strings.TrimPrefix(r.URL.Path, "/shop/")
	if name == "" {
		name = "index.html"
	} else if path.Ext(name) == "" {
		name += ".html"
	}
	info, err := fs.Stat(fsys, name)
	if !fs.ValidPath(name) || err != nil || info.IsDir() {
		body, err := fs.ReadFile(fsys, "404.html")
		if err != nil {
			render404(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	http.ServeFileFS(w, r, fsys, name)
}
