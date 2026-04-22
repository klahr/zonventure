// Package adventure loads free-standing adventures from disk and holds them
// in memory so the server can serve puzzles without consulting the database.
//
// An adventure lives in its own directory:
//
//	adventures/<slug>/adventure.toml
//	adventures/<slug>/pages/NN-*.toml
//
// Pages are ordered by filename; the leading NN- prefix is a convention to
// make the order visible. Each page's `accept` list contains all strings the
// server will treat as correct answers (compared case-insensitively, with
// whitespace trimmed, and any run of internal whitespace collapsed).
package adventure

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// Adventure is an in-memory, loaded adventure ready to be played.
type Adventure struct {
	Slug  string
	Meta  Meta
	Pages []Page
}

// Meta is the top-level adventure.toml structure.
type Meta struct {
	Title       string `toml:"title"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
}

// Page is one puzzle in an adventure.
type Page struct {
	Index    int
	Title    string   `toml:"title"`
	Prompt   string   `toml:"prompt"`
	Hint     string   `toml:"hint,omitempty"`
	Accept   []string `toml:"accept"`
	Location Location `toml:"location"`
}

// Location is a puzzle's real-world position. Every puzzle has one so the
// arranger map can show where each puzzle sits. If RadiusM is set, the
// puzzle is "gated": the prompt is withheld until the team's latest ping is
// within RadiusM metres of (Lat,Lng) and the server refuses answer
// submissions made from outside that radius.
type Location struct {
	Lat     float64  `toml:"lat"`
	Lng     float64  `toml:"lng"`
	RadiusM *float64 `toml:"radius_m,omitempty"`
}

// Gated reports whether this location is a gate (has a radius).
func (l Location) Gated() bool { return l.RadiusM != nil && *l.RadiusM > 0 }

// Library is a concurrent in-memory registry of loaded adventures.
type Library struct {
	mu sync.RWMutex
	m  map[string]*Adventure
}

// Load reads every adventure directory under root and returns a populated
// Library. Directories without an adventure.toml are skipped silently.
func Load(root string) (*Library, error) {
	lib := &Library{m: make(map[string]*Adventure)}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return lib, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		adv, err := loadOne(filepath.Join(root, slug), slug)
		if err != nil {
			return nil, fmt.Errorf("adventure %q: %w", slug, err)
		}
		if adv != nil {
			lib.m[slug] = adv
		}
	}
	return lib, nil
}

func loadOne(dir, slug string) (*Adventure, error) {
	metaPath := filepath.Join(dir, "adventure.toml")
	if _, err := os.Stat(metaPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta Meta
	if _, err := toml.DecodeFile(metaPath, &meta); err != nil {
		return nil, fmt.Errorf("decode adventure.toml: %w", err)
	}
	if strings.TrimSpace(meta.Title) == "" {
		return nil, fmt.Errorf("adventure.toml: title required")
	}

	pagesDir := filepath.Join(dir, "pages")
	pageEntries, err := os.ReadDir(pagesDir)
	if err != nil {
		return nil, fmt.Errorf("read pages: %w", err)
	}
	var files []string
	for _, pe := range pageEntries {
		if pe.IsDir() || !strings.HasSuffix(pe.Name(), ".toml") {
			continue
		}
		files = append(files, pe.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no pages")
	}

	pages := make([]Page, 0, len(files))
	for i, fname := range files {
		var p Page
		if _, err := toml.DecodeFile(filepath.Join(pagesDir, fname), &p); err != nil {
			return nil, fmt.Errorf("decode %s: %w", fname, err)
		}
		if strings.TrimSpace(p.Prompt) == "" {
			return nil, fmt.Errorf("%s: prompt required", fname)
		}
		if len(p.Accept) == 0 {
			return nil, fmt.Errorf("%s: at least one accept entry required", fname)
		}
		if p.Location.Lat == 0 && p.Location.Lng == 0 {
			return nil, fmt.Errorf("%s: [location] with lat and lng is required", fname)
		}
		p.Index = i
		pages = append(pages, p)
	}

	return &Adventure{Slug: slug, Meta: meta, Pages: pages}, nil
}

// Get returns the adventure for a slug, or nil if none is loaded.
func (l *Library) Get(slug string) *Adventure {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.m[slug]
}

// List returns a snapshot of all loaded adventures, sorted by slug.
func (l *Library) List() []*Adventure {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*Adventure, 0, len(l.m))
	for _, a := range l.m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// CheckAnswer returns true when submitted matches one of the page's accepted
// answers under the canonicalisation rules documented on the package.
func (p Page) CheckAnswer(submitted string) bool {
	norm := normalise(submitted)
	if norm == "" {
		return false
	}
	for _, a := range p.Accept {
		if normalise(a) == norm {
			return true
		}
	}
	return false
}

func normalise(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}
