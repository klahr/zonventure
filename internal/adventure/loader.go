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
	"errors"

	"github.com/klahr/zonventure/internal/graph/look"
)

// Adventure is an in-memory representation ready to be handed to handlers.
type Adventure struct {
	ID     int64
	Slug   string
	Meta   Meta
	Colors Colors
	// StrictAssets, when true, limits a team's media to the steps it has
	// reached (see play's media gate). Default false keeps the permissive
	// "any team in the adventure" behaviour that suits flavour-image
	// hunts; opt-in for hunts where the photo IS the puzzle.
	StrictAssets bool
	// Start is the optional "go here first" location. Both coordinates
	// must be set together. The label is a short
	// free-text hint ("vid bänken under trappan") that the intro
	// overlay and catalog page surface alongside the map link.
	Start StartLocation
}

// StartLocation is the explicit starting point for an adventure.
// Lat / Lng are pointers because both have to be present together for
// the location to render — a partial config (one coordinate, or only
// a label) gets rendered as "no start set".
type StartLocation struct {
	Lat   *float64
	Lng   *float64
	Label string
	// RadiusM is how near the start a team must be to begin; nil when
	// it may begin anywhere.
	RadiusM *float64
}

// HasCoords reports whether the start location is fully specified —
// the gate before any "open in maps" / "Start here" link renders.
func (s StartLocation) HasCoords() bool { return s.Lat != nil && s.Lng != nil }

// Meta bundles the adventure's descriptive fields.
type Meta struct {
	Title       string
	Description string
	Author      string
	// IntroText is shown on the first-run welcome overlay when set;
	// otherwise the default built-in copy applies. Multi-line plain
	// text — newlines are preserved by the client via white-space.
	IntroText string
	// OutroText appears under the stats on the end-of-adventure
	// completion screen. Same rules as IntroText.
	OutroText string
}

// Colors is the adventure's look: its palette, typefaces, shapes and
// backdrop. It lives in the graph domain (look.Look) because a theme
// saved in a graph is the same look; these names keep the adventure's
// callers unchanged.
type Colors = look.Look

// Backdrop is a pattern behind the cards: see look.Backdrop.
type Backdrop = look.Backdrop

// Style is a look's finer choices: see look.Style.
type Style = look.Style

// The lists a look's choices come from: see package look.
var (
	Corners = look.Corners
	Cards   = look.Cards
	Fonts   = look.Fonts
)

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
	var (
		adv                                Adventure
		accent, accent2, bg, ink, submitBG sql.NullString
		startLat, startLng, startRadius    sql.NullFloat64
	)
	err := l.db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
		       strict_assets,
		       start_lat, start_lng, start_label, start_radius_m
		FROM adventures WHERE slug = $1
	`, slug).Scan(&adv.ID, &adv.Slug, &adv.Meta.Title, &adv.Meta.Description, &adv.Meta.Author,
		&adv.Meta.IntroText, &adv.Meta.OutroText,
		&accent, &accent2, &bg, &ink, &submitBG,
		&adv.StrictAssets,
		&startLat, &startLng, &adv.Start.Label, &startRadius)
	if startLat.Valid {
		v := startLat.Float64
		adv.Start.Lat = &v
	}
	if startLng.Valid {
		v := startLng.Float64
		adv.Start.Lng = &v
	}
	if startRadius.Valid {
		v := startRadius.Float64
		adv.Start.RadiusM = &v
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	adv.Colors = Colors{
		Accent:           accent.String,
		AccentSecondary:  accent2.String,
		Background:       bg.String,
		Text:             ink.String,
		SubmitBackground: submitBG.String,
	}
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
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
		       strict_assets,
		       start_lat, start_lng, start_label, start_radius_m
		FROM adventures ORDER BY slug
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Adventure
	for rows.Next() {
		a := &Adventure{}
		var (
			accent, accent2, bg, ink, submitBG sql.NullString
			startLat, startLng, startRadius    sql.NullFloat64
		)
		if err := rows.Scan(&a.ID, &a.Slug, &a.Meta.Title, &a.Meta.Description, &a.Meta.Author,
			&a.Meta.IntroText, &a.Meta.OutroText,
			&accent, &accent2, &bg, &ink, &submitBG,
			&a.StrictAssets,
			&startLat, &startLng, &a.Start.Label, &startRadius); err != nil {
			return nil, err
		}
		a.Colors = Colors{
			Accent:           accent.String,
			AccentSecondary:  accent2.String,
			Background:       bg.String,
			Text:             ink.String,
			SubmitBackground: submitBG.String,
		}
		if startLat.Valid {
			v := startLat.Float64
			a.Start.Lat = &v
		}
		if startLng.Valid {
			v := startLng.Float64
			a.Start.Lng = &v
		}
		if startRadius.Valid {
			v := startRadius.Float64
			a.Start.RadiusM = &v
		}
		out = append(out, a)
	}
	return out, nil
}
