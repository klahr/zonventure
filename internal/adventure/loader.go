// Package adventure loads adventures from the database. The legacy
// on-disk TOML loader is gone — admins edit adventures through the admin
// HTTP surface.
//
// The Library exposes the same Get/List shape the rest of the codebase
// used before, so callers don't need to care where the data lives.
package adventure

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Adventure is an in-memory representation ready to be handed to handlers.
type Adventure struct {
	ID          int64
	Slug        string
	Meta        Meta
	Pages       []Page
}

// Meta bundles the adventure's descriptive fields.
type Meta struct {
	Title       string
	Description string
	Author      string
}

// Page is one puzzle in an adventure.
type Page struct {
	ID       int64
	Index    int
	Title    string
	Prompt   string
	Hint     string
	Accept   []string
	Location Location
	Images   []Image
}

// Image describes one attached image. Binary content is fetched separately
// via the image-serving endpoint; Page carries only the metadata needed to
// build URLs and render layout.
type Image struct {
	ID        int64
	Index     int
	MimeType  string
	SizeBytes int64
}

// Location carries a puzzle's GPS coordinates. If RadiusM is set the
// puzzle is gated (prompt withheld until the team arrives).
type Location struct {
	Lat     float64
	Lng     float64
	RadiusM *float64
}

// Gated reports whether this location is a gate (has a radius).
func (l Location) Gated() bool { return l.RadiusM != nil && *l.RadiusM > 0 }

// Library is the concurrency-safe lookup surface for adventures. Queries
// go straight to the database — simpler than caching, and the data is
// small.
type Library struct {
	db *sql.DB
}

// New constructs a Library bound to the given database.
func New(db *sql.DB) *Library { return &Library{db: db} }

// Get returns the adventure with the given slug, or nil if none exists.
// Uses the request's context when possible via GetContext; this variant
// uses context.Background for backwards compatibility with older callers.
func (l *Library) Get(slug string) *Adventure {
	adv, _ := l.GetContext(context.Background(), slug)
	return adv
}

// GetContext is the context-aware variant of Get.
func (l *Library) GetContext(ctx context.Context, slug string) (*Adventure, error) {
	if slug == "" {
		return nil, nil
	}
	var adv Adventure
	err := l.db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author
		FROM adventures WHERE slug = ?
	`, slug).Scan(&adv.ID, &adv.Slug, &adv.Meta.Title, &adv.Meta.Description, &adv.Meta.Author)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pages, err := l.loadPages(ctx, adv.ID)
	if err != nil {
		return nil, err
	}
	adv.Pages = pages
	return &adv, nil
}

// List returns every adventure, sorted by slug.
func (l *Library) List() []*Adventure {
	advs, _ := l.ListContext(context.Background())
	return advs
}

// ListContext is the context-aware variant of List.
func (l *Library) ListContext(ctx context.Context) ([]*Adventure, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, slug, title, description, author
		FROM adventures ORDER BY slug
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Adventure
	for rows.Next() {
		a := &Adventure{}
		if err := rows.Scan(&a.ID, &a.Slug, &a.Meta.Title, &a.Meta.Description, &a.Meta.Author); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	// Fill pages for each (small N * small N — acceptable).
	for _, a := range out {
		pages, err := l.loadPages(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		a.Pages = pages
	}
	return out, nil
}

func (l *Library) loadPages(ctx context.Context, adventureID int64) ([]Page, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, title, prompt, hint, accept, lat, lng, radius_m
		FROM adventure_pages
		WHERE adventure_id = ?
		ORDER BY idx
	`, adventureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pages []Page
	for rows.Next() {
		var (
			p        Page
			acceptJS string
			radius   sql.NullFloat64
		)
		if err := rows.Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &p.Hint, &acceptJS,
			&p.Location.Lat, &p.Location.Lng, &radius); err != nil {
			return nil, err
		}
		if acceptJS != "" {
			_ = json.Unmarshal([]byte(acceptJS), &p.Accept)
		}
		if radius.Valid {
			v := radius.Float64
			p.Location.RadiusM = &v
		}
		pages = append(pages, p)
	}
	// Fill each page's images.
	for i := range pages {
		imgs, err := l.loadImages(ctx, pages[i].ID)
		if err != nil {
			return nil, err
		}
		pages[i].Images = imgs
	}
	return pages, nil
}

func (l *Library) loadImages(ctx context.Context, pageID int64) ([]Image, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes
		FROM adventure_page_images WHERE page_id = ?
		ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Image
	for rows.Next() {
		var im Image
		if err := rows.Scan(&im.ID, &im.Index, &im.MimeType, &im.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, nil
}

// CheckAnswer reports whether submitted matches any of the page's accepted
// answers under the normalisation rules: case-insensitive, whitespace
// trimmed, runs of internal whitespace collapsed to a single space.
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
