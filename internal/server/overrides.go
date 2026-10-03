package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/sitename"
)

// webOverrideDir is set once at startup from Config.WebOverrideDir.
// When non-empty, readWebFile prefers files from that directory over
// the embedded //go:embed bundle. Empty (the default) means "use the
// embedded bundle only" — the OSS / self-hosted-default configuration.
var webOverrideDir string

// loadOverrides points the server at an on-disk override directory for
// HTML templates and locale strings. The dir's layout flattens the
// embed paths: drop a landing.html in the override dir to shadow the
// embedded one; landing-locales.json (optional) merges per-locale
// field overrides on top of the embedded defaults that package init()
// loaded from web/locales/landing/<lang>.json.
//
// A missing dir is treated as "no override configured" rather than
// fatal so a deploy that renames or hasn't yet mounted the dir
// doesn't take the service down — it just serves the embedded
// defaults.
func loadOverrides(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			log.Printf("web override dir %q not found; using embedded defaults", dir)
			return nil
		}
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	webOverrideDir = dir
	if err := applyLandingLocaleOverrides(filepath.Join(dir, "landing-locales.json")); err != nil {
		return fmt.Errorf("locale overrides: %w", err)
	}
	return nil
}

// readWebFile reads name (e.g. "web/landing.html") from the override
// dir if a file at the equivalent path exists there, otherwise from
// the embedded FS. Override paths drop the leading "web/" prefix so
// the dir mirrors user-facing filenames (landing.html, pricing.html,
// …) — operators don't need to recreate the embed-relative layout.
//
// HTML files come back with {{SITE_NAME}} filled in (ZV_SITE_NAME).
func readWebFile(name string) ([]byte, error) {
	b, err := readWebFileRaw(name)
	if err != nil || !strings.HasSuffix(name, ".html") {
		return b, err
	}
	return []byte(sitename.Fill(string(b))), nil
}

func readWebFileRaw(name string) ([]byte, error) {
	if webOverrideDir != "" {
		rel := strings.TrimPrefix(name, "web/")
		b, err := os.ReadFile(filepath.Join(webOverrideDir, rel))
		if err == nil {
			return b, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		// Fall through to embedded.
	}
	return webFS.ReadFile(name)
}

// hasOverride reports whether the override dir ships name (e.g.
// "landing.html"). The public marketing pages (`/`, `/pricing`) are
// served only when the operator supplies them; the embedded copies are
// reference templates, not a default front door.
func hasOverride(name string) bool {
	if webOverrideDir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(webOverrideDir, name))
	return err == nil && info.Mode().IsRegular()
}

// overridePageName matches the single-segment paths an override-dir
// extra page can answer: /create serves create.html.
var overridePageName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// overridePage returns the override file an unmatched GET path maps
// to, or "" when there is none. Names the embedded bundle uses as
// templates for specific routes (landing, store, 404, …) are reserved:
// served under their own name they'd render with unfilled placeholders.
func overridePage(path string) string {
	name := strings.TrimPrefix(path, "/")
	if !overridePageName.MatchString(name) {
		return ""
	}
	file := name + ".html"
	if _, err := fs.Stat(webFS, "web/"+file); err == nil {
		return ""
	}
	if !hasOverride(file) {
		return ""
	}
	return file
}

// serveOverridePageOr404 is the router's NotFound handler. Every app
// route has already failed to match, so an operator's extra page can
// never shadow one; it gets the shared topbar/footer substitution like
// /manual. Anything else is the branded 404.
func serveOverridePageOr404(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if file := overridePage(r.URL.Path); file != "" {
			renderTopbarPage(w, r, "web/"+file)
			return
		}
	}
	render404(w, r)
}

// warnShadowedOverridePages logs override-dir extra pages that an app
// route answers first, so a release that adds e.g. /create doesn't
// silently hide the operator's create.html.
func warnShadowedOverridePages(routes chi.Routes) {
	if webOverrideDir == "" {
		return
	}
	entries, err := os.ReadDir(webOverrideDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".html")
		if !ok || !e.Type().IsRegular() || !overridePageName.MatchString(name) {
			continue
		}
		if _, err := fs.Stat(webFS, "web/"+e.Name()); err == nil {
			continue
		}
		if routes.Match(chi.NewRouteContext(), http.MethodGet, "/"+name) {
			log.Printf("web override %s is unreachable: /%s is an app route", e.Name(), name)
		}
	}
}

// applyLandingLocaleOverrides merges per-locale string overrides on
// top of landingLocales. The JSON shape is:
//
//	{ "en": { "Title": "…", "HeroHeading": "…" }, "sv": { … } }
//
// Field names match the landingStrings struct exactly. Missing fields
// keep the embedded default — operators only spell out the strings
// they actually want to change. Unknown languages and unknown field
// names log a warning and are skipped rather than failing startup, so
// a typo in the override file doesn't take the service down.
func applyLandingLocaleOverrides(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var overrides map[string]map[string]string
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for lang, fields := range overrides {
		base, ok := landingLocales[lang]
		if !ok {
			log.Printf("landing locale override: unknown lang %q (skipped)", lang)
			continue
		}
		v := reflect.ValueOf(&base).Elem()
		for name, val := range fields {
			f := v.FieldByName(name)
			if !f.IsValid() || f.Kind() != reflect.String || !f.CanSet() {
				log.Printf("landing locale override: %s has no string field %q (skipped)", lang, name)
				continue
			}
			f.SetString(sitename.Fill(val))
		}
		landingLocales[lang] = base
	}
	return nil
}
