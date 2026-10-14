package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/settings"
	"github.com/klahr/zonventure/internal/sitename"
)

// The React apps, built by Vite into web/dist/<app>/ and compiled into
// the binary so `go build` still yields one self-contained artifact.
//
// The `all:` prefix is mandatory, not stylistic: plain //go:embed skips
// files and directories whose names begin with "." or "_", and Vite
// emits exactly that — .vite/manifest.json, and _-prefixed chunk
// directories under some configurations. Without `all:` the manifest
// silently vanishes from the binary and spaAssetsPresent can't verify
// anything.
//
// web/dist/index.html is committed as a placeholder so this pattern
// always matches at least one file; go:embed is a compile-time error
// otherwise, which would break a fresh clone before `make web` ran.
// See internal/server/web/dist/index.html.
//
//go:embed all:web/dist
var spaFS embed.FS

// spaApps are the app directories the router will serve. Adding an app
// means adding its Vite project under web/apps/<name> with a matching
// `base` of /app/<name>/ and listing it here.
//
// Phase 0 ships one app as a seam check. The existing hand-written
// pages at /play, /arrange, /live and the marketing routes are
// deliberately untouched: old and new frontends coexist in the same
// binary for the whole migration, and a surface is switched over by
// changing one route registration, never by a big-bang cutover.
var spaApps = []string{"arrange", "play", "party"}

// registerSPARoutes registers /app/<name>/ for each built app.
//
// Two cache policies, and the split is what makes deploys behave:
//
//   - index.html is no-store, so a deploy reaches users on their next
//     navigation rather than whenever a cache entry happens to expire.
//   - Hashed assets are immutable for a year. Vite puts a content hash
//     in every asset filename, so a changed file is a changed URL and
//     a stale one can never be served.
func registerSPARoutes(r chi.Router) {
	for _, app := range spaApps {
		prefix := "/app/" + app

		index, err := spaFS.ReadFile(path.Join("web/dist", app, "index.html"))
		if err != nil {
			// The frontend build hasn't run. Serve the placeholder
			// rather than refusing to boot — the API, the existing
			// pages and every operator task still work, and a broken
			// asset pipeline should not take a running event down.
			index, err = spaFS.ReadFile("web/dist/index.html")
			if err != nil {
				panic("embed: web/dist placeholder missing: " + err.Error())
			}
		}

		index = brandSPAIndex(index)

		r.Handle(prefix+"/assets/*", http.StripPrefix(prefix+"/", spaAssetHandler(app)))

		// Both the bare prefix and everything under it return the
		// shell, so client-side routes survive a reload and a shared
		// deep link opens the right view.
		serveIndex := func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(index)
		}
		rootFile := spaRootFileHandler(app)
		r.Get(prefix, serveIndex)
		r.Get(prefix+"/", serveIndex)
		r.Get(prefix+"/*", func(w http.ResponseWriter, req *http.Request) {
			if rootFile(w, req) {
				return
			}
			serveIndex(w, req)
		})
	}
}

// spaTitle and spaAppName are how each app's index.html names the
// site; brandSPAIndex swaps in ZV_SITE_NAME. The apps read the
// application-name meta at runtime (siteName() in @zonventure/api).
// The source keeps the literal default so the Vite dev server shows
// a sensible title. spaCurrency carries ZV_CURRENCY the same way
// (siteCurrency()).
const (
	spaTitle    = "<title>Zonventure</title>"
	spaAppName  = `<meta name="application-name" content="Zonventure" />`
	spaCurrency = `<meta name="zv-currency" content="sek" />`
)

func brandSPAIndex(index []byte) []byte {
	n := html.EscapeString(sitename.Name())
	s := strings.Replace(string(index), spaTitle, "<title>"+n+"</title>", 1)
	s = strings.Replace(s, spaAppName, `<meta name="application-name" content="`+n+`" />`, 1)
	s = strings.Replace(s, spaCurrency, `<meta name="zv-currency" content="`+settings.Currency+`" />`, 1)
	return []byte(s)
}

// spaAssetHandler serves content-hashed build output for one app.
func spaAssetHandler(app string) http.HandlerFunc {
	sub, err := fs.Sub(spaFS, path.Join("web/dist", app))
	if err != nil {
		// Only reachable if the app directory is absent, i.e. the
		// frontend build never ran. 404 every asset; the shell above
		// already explains what to do.
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend not built", http.StatusNotFound)
		}
	}
	fileServer := http.FileServer(http.FS(sub))
	return func(w http.ResponseWriter, r *http.Request) {
		// Safe because the filename contains a content hash: the bytes
		// at a given URL can never change, so nothing needs to
		// revalidate. This is the whole point of hashed filenames.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	}
}

// spaRootFileHandler serves the un-hashed files Vite copies from an
// app's public/ directory to the root of its build — a PWA's service
// worker, web manifest and icons. It reports false for anything else,
// which the caller answers with the app shell.
//
// These files must revalidate on every load, unlike /assets/: their URLs
// are fixed by the manifest and the service-worker registration, so a
// year of immutability would pin every player to the first deploy's
// worker. no-cache lets the browser keep a copy and ask whether it is
// still current.
func spaRootFileHandler(app string) func(http.ResponseWriter, *http.Request) bool {
	sub, err := fs.Sub(spaFS, path.Join("web/dist", app))
	if err != nil {
		return func(http.ResponseWriter, *http.Request) bool { return false }
	}
	prefix := "/app/" + app + "/"
	return func(w http.ResponseWriter, r *http.Request) bool {
		name := strings.TrimPrefix(r.URL.Path, prefix)
		// Root-level files only: no subdirectories, nothing hidden, and
		// not the shell itself, which has its own no-store policy.
		if name == "" || strings.Contains(name, "/") || strings.HasPrefix(name, ".") || name == "index.html" {
			return false
		}
		st, err := fs.Stat(sub, name)
		if err != nil || st.IsDir() {
			return false
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, sub, name)
		return true
	}
}

// spaAssetsPresent reports, for one app, whether every local asset its
// index.html references is actually present in the embedded FS.
//
// This exists because the failure it catches is silent and nasty:
// `go build` succeeds against a stale or half-written web/dist, and the
// result is a binary that serves an index.html pointing at chunks that
// aren't in it. The page loads blank, in production, with a 404 in the
// console and nothing in the server log. The embed test calls this so
// the build fails instead.
//
// Returns the missing paths; empty means consistent. A missing app
// directory reports the app's own index.html as missing.
func spaAssetsPresent(app string) []string {
	indexPath := path.Join("web/dist", app, "index.html")
	index, err := spaFS.ReadFile(indexPath)
	if err != nil {
		return []string{indexPath}
	}

	var missing []string
	prefix := "/app/" + app + "/"
	for _, ref := range extractAssetRefs(string(index)) {
		// Only local, absolute refs under this app's base are ours;
		// skip anything external or served by another handler.
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		rel := strings.TrimPrefix(ref, prefix)
		full := path.Join("web/dist", app, rel)
		if _, err := spaFS.Open(full); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, full)
			}
		}
	}
	return missing
}

// extractAssetRefs pulls src="…" and href="…" values out of HTML.
//
// A regex rather than a real parser is proportionate here: the input is
// Vite's own generated index.html, not arbitrary user markup, and the
// consequence of a miss is a weaker assertion in one test rather than
// anything user-visible.
func extractAssetRefs(html string) []string {
	var refs []string
	for _, attr := range []string{`src="`, `href="`} {
		rest := html
		for {
			i := strings.Index(rest, attr)
			if i < 0 {
				break
			}
			rest = rest[i+len(attr):]
			j := strings.IndexByte(rest, '"')
			if j < 0 {
				break
			}
			refs = append(refs, rest[:j])
			rest = rest[j:]
		}
	}
	return refs
}

// spaBuildID is a short digest of an app's index.html, exposed for
// future cache-busting of the service worker shell list. Kept here so
// the hashing convention lives next to the embed it describes.
func spaBuildID(app string) string {
	b, err := spaFS.ReadFile(path.Join("web/dist", app, "index.html"))
	if err != nil {
		return "dev"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
