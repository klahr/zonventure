package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
// field overrides on top of the compiled-in defaults.
//
// Called once from server.New(). A missing dir is treated as "no
// override configured" rather than fatal so a deploy that renames or
// hasn't yet mounted the dir doesn't take the service down — it just
// serves the embedded defaults.
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
func readWebFile(name string) ([]byte, error) {
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
			f.SetString(val)
		}
		landingLocales[lang] = base
	}
	return nil
}
