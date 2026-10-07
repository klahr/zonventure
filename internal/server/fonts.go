package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"path"

	"github.com/go-chi/chi/v5"
)

// fontFS holds the brand fonts the server-rendered pages link through
// /assets/fontface.css. Embedded on its own rather than in webFS: the
// files are immutable binaries an override dir has no reason to shadow.
//
//go:embed web/assets/fonts/*.woff2 web/assets/fonts/*.txt web/assets/fonts/fontface.css
var fontFS embed.FS

// registerFontRoutes serves /assets/fontface.css and every file under
// /assets/fonts/ from fontFS, so no page needs Google Fonts.
func registerFontRoutes(r chi.Router) {
	r.Get("/assets/fontface.css", serveFontFile("fontface.css", "text/css; charset=utf-8"))
	// An adventure look's typeface, for the pages that wear the look.
	r.Get("/assets/look-font/{file}", serveLookFont)
	entries, err := fontFS.ReadDir("web/assets/fonts")
	if err != nil {
		panic("embed: " + err.Error())
	}
	for _, e := range entries {
		name := e.Name()
		if name == "fontface.css" {
			continue
		}
		ct := "text/plain; charset=utf-8"
		if path.Ext(name) == ".woff2" {
			ct = "font/woff2"
		}
		r.Get("/assets/fonts/"+name, serveFontFile(name, ct))
	}
}

func serveFontFile(name, contentType string) http.HandlerFunc {
	body, err := fontFS.ReadFile("web/assets/fonts/" + name)
	if err != nil {
		panic("embed: " + err.Error())
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	}
}
