package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strings"
)

// i18n.js DICT injection + landing-locale population from JSON.
//
// JSON layout (embedded under web/locales/):
//
//	web/locales/web/<lang>.json     — flat key→value (key = English source)
//	web/locales/landing/<lang>.json — flat field-id→string (matches landingStrings)
//
// Source of truth lives in those files. The legacy hardcoded DICT in
// internal/server/web/i18n.js is replaced by a single placeholder
// (`__ZV_I18N_DICT__`) which renderI18nJS substitutes at startup.
// landingLocales is empty at package-init time and populated here.
//
// Operator overrides (ZV_WEB_OVERRIDE_DIR + landing-locales.json) layer
// on top of the embedded JSON in overrides.go::loadOverrides. Order:
//   1. (this file) embedded JSON populates landingLocales / DICT.
//   2. (overrides.go) override dir merges per-field deltas on top.
//
// So the override seam keeps working; this file just changes where the
// "step 1" defaults come from.

// renderedI18nJS holds the served bytes of /i18n.js. Computed once at
// startup by renderI18nJS — read-only after that, no mutex needed.
var renderedI18nJS []byte

// serveRenderedI18n returns the http.HandlerFunc for /i18n.js. It
// serves the bytes initI18nFromJSON computed at startup. A panic
// here means the route was wired before initI18nFromJSON ran —
// fail loud rather than silently shipping a broken DICT.
func serveRenderedI18n() http.HandlerFunc {
	if len(renderedI18nJS) == 0 {
		panic("serveRenderedI18n: i18n.js not rendered (initI18nFromJSON didn't run)")
	}
	body := renderedI18nJS
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// init populates landingLocales from the embedded JSON at package
// load. Done in init() rather than server.New because tests in this
// package reach for landingLocales directly (resolveLang,
// renderLandingPage, applyLandingLocaleOverrides) without
// constructing a full server. Failing here is appropriate — a
// missing or malformed embedded JSON is a build-time bug, not a
// runtime config issue.
//
// renderI18nJS is NOT called here: the i18n.js template can be
// shadowed by ZV_WEB_OVERRIDE_DIR, which only takes effect after
// loadOverrides has set webOverrideDir. The rendered bytes are
// produced from server.New via initI18nFromJSON below.
func init() {
	if err := loadLandingFromJSON(); err != nil {
		panic("server: load landing locales: " + err.Error())
	}
}

// initI18nFromJSON renders the /i18n.js bytes by reading the JSON
// locales (and the i18n.js template, possibly via the override
// dir) and substituting the DICT placeholder. Called from
// server.New after loadOverrides so the override seam is honoured.
func initI18nFromJSON() error {
	rendered, err := renderI18nJS()
	if err != nil {
		return fmt.Errorf("render i18n.js: %w", err)
	}
	renderedI18nJS = rendered
	return nil
}

// loadLandingFromJSON reads web/locales/landing/<lang>.json for each
// embedded file and populates landingLocales by reflection. Mirrors
// the apply-merge pattern in applyLandingLocaleOverrides — a field
// missing from the JSON keeps the struct's zero value, which is the
// "intentionally blank for the embedded template" case noted in the
// comment above landingLocales' historical declaration.
func loadLandingFromJSON() error {
	entries, err := fs.ReadDir(webFS, "web/locales/landing")
	if err != nil {
		return fmt.Errorf("read landing locales dir: %w", err)
	}
	if landingLocales == nil {
		landingLocales = map[string]landingStrings{}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		raw, err := webFS.ReadFile("web/locales/landing/" + e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		var s landingStrings
		v := reflect.ValueOf(&s).Elem()
		for name, val := range fields {
			f := v.FieldByName(name)
			if !f.IsValid() || f.Kind() != reflect.String || !f.CanSet() {
				log.Printf("landing locale %s: unknown field %q (skipped)", lang, name)
				continue
			}
			f.SetString(val)
		}
		landingLocales[lang] = s
	}
	if len(landingLocales) == 0 {
		return fmt.Errorf("no landing locale files found under web/locales/landing/")
	}
	return nil
}

// renderI18nJS reads web/i18n.js, builds a JSON-formatted DICT from
// every web/locales/web/<lang>.json, and substitutes it into the
// `__ZV_I18N_DICT__` placeholder. Returns the bytes the /i18n.js
// route serves to clients.
//
// The substitution is a literal string replace, not a template
// engine — there is exactly one placeholder by design and any other
// mention of the marker (e.g. a comment about it) would also get
// hit. We anchor on the exact identifier to make accidental hits
// trivially unlikely.
func renderI18nJS() ([]byte, error) {
	tmpl, err := readWebFile("web/i18n.js")
	if err != nil {
		return nil, fmt.Errorf("read i18n.js template: %w", err)
	}

	dict, err := loadWebDict()
	if err != nil {
		return nil, err
	}
	dictJSON, err := marshalDict(dict)
	if err != nil {
		return nil, fmt.Errorf("marshal dict: %w", err)
	}

	const marker = "__ZV_I18N_DICT__"
	if !bytes.Contains(tmpl, []byte(marker)) {
		return nil, fmt.Errorf("i18n.js missing %s placeholder — template out of sync with loader", marker)
	}
	out := bytes.Replace(tmpl, []byte(marker), dictJSON, 1)
	return out, nil
}

// loadWebDict walks web/locales/web/, parses each <lang>.json file
// and returns a map[lang]map[key]value. Languages are listed
// directly from the embedded FS so a new locale is auto-discovered
// — no code change needed beyond dropping a pl.json into the dir.
func loadWebDict() (map[string]map[string]string, error) {
	entries, err := fs.ReadDir(webFS, "web/locales/web")
	if err != nil {
		return nil, fmt.Errorf("read web locales dir: %w", err)
	}
	out := map[string]map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		raw, err := webFS.ReadFile("web/locales/web/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		// English source fronts as identity-translation in the on-
		// disk format (so Crowdin has a real source file). At
		// runtime we drop those values so the JS t() fallthrough
		// (table[key] === undefined → return key) decides — saves
		// 150 KB of redundant inlined bytes per page load. Keys that
		// aren't their own English text (the namespaced errors.*
		// codes) are kept, or English users would read the raw key.
		if lang == "en" {
			kept := map[string]string{}
			for k, v := range m {
				if k != v {
					kept[k] = v
				}
			}
			out[lang] = kept
			continue
		}
		out[lang] = m
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no web locale files found under web/locales/web/")
	}
	return out, nil
}

// marshalDict produces deterministic JSON bytes for the i18n.js
// DICT. Locales sorted, keys within each locale sorted. A stable
// rendering means the served bytes hash-stable across rebuilds —
// useful for CDN/etag headers if we add them later, and for
// reproducible diffing during development.
func marshalDict(dict map[string]map[string]string) ([]byte, error) {
	langs := make([]string, 0, len(dict))
	for l := range dict {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	var buf bytes.Buffer
	buf.WriteByte('{')
	for li, lang := range langs {
		if li > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(lang)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		entries := dict[lang]
		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			vb, err := json.Marshal(entries[k])
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			buf.Write(vb)
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
