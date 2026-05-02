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
	"fmt"
	"regexp"
	"strings"
)

// Adventure is an in-memory representation ready to be handed to handlers.
type Adventure struct {
	ID     int64
	Slug   string
	Meta   Meta
	Colors Colors
	Pages  []Page
}

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

// Colors is the optional per-adventure brand palette. Any field left at
// the zero value means "fall back to the default theme". Values are stored
// as "#rrggbb" strings so they paste straight into CSS.
type Colors struct {
	Accent          string `json:"accent,omitempty"`
	AccentSecondary string `json:"accent_secondary,omitempty"`
	Background      string `json:"background,omitempty"`
	Text            string `json:"text,omitempty"`
	// SubmitBackground paints the sticky answer dock at the bottom of
	// /play. Independent of Background so a brand can keep a neutral
	// page body and still have a contrasting brand panel hosting the
	// submit / scan buttons. The play UI also reuses this value as
	// the buttons' text colour, producing the "punched-through"
	// effect the customisation copy describes.
	SubmitBackground string `json:"submit_bg,omitempty"`
}

// Empty reports whether no colours are set — handy for skipping the field
// entirely in JSON responses.
func (c Colors) Empty() bool {
	return c.Accent == "" && c.AccentSecondary == "" && c.Background == "" && c.Text == "" && c.SubmitBackground == ""
}

// Page is one puzzle in an adventure.
type Page struct {
	ID       int64
	Index    int
	Title    string
	Prompt   string
	Hints    []Hint
	Accept   []AcceptEntry
	Location Location
	Images   []Image
	Audio    []Audio
	Disabled bool
	Notes    string
}

// AcceptEntry is one accepted answer for a puzzle. Plain entries
// (Regex=false) match the player's input literally after the same
// case/whitespace normalisation applied to both sides — what the
// arranger types is what the player must type. Regex entries are
// interpreted as Go RE2 patterns, anchored to the entire normalised
// input and matched case-insensitively, so an arranger writing
// `(yes|ja|jo)` doesn't have to remember to wrap it themselves.
type AcceptEntry struct {
	Text  string `json:"text"`
	Regex bool   `json:"regex,omitempty"`
}

// Hint is one on-demand clue for a puzzle. DelaySeconds is how long
// after the team reached the puzzle the hint becomes revealable.
// Order is authoring-defined via the database idx column (zero-based);
// the Hints slice on Page preserves that order.
//
// Images are attached to a specific hint and only travel to a player
// after they reveal it — see ServeHintImage's visibility check. Empty
// for hints with nothing attached, so the player DTO can drop the
// field entirely (omitempty).
type Hint struct {
	ID           int64
	Text         string
	DelaySeconds int
	Images       []Image
	Audio        []Audio
}

// Image describes one attached image. Binary content is fetched separately
// via the image-serving endpoint; Page carries only the metadata needed to
// build URLs and render layout.
type Image struct {
	ID        int64
	Index     int
	MimeType  string
	SizeBytes int64
	// AltText is the author-provided text alternative. Empty means the
	// image is decorative and should render with alt="" so screen
	// readers skip it; non-empty values become the alt attribute
	// verbatim — no markdown, no HTML.
	AltText string
}

// Audio describes one attached audio clip. Same metadata-only shape
// as Image — the blob streams from the audio-serving endpoint and the
// player renders a native <audio controls> element.
//
// Description is the author-provided transcript or summary. Empty
// means the audio is decorative or self-explanatory; non-empty values
// can be surfaced near the player as a fallback for users who can't
// hear it.
type Audio struct {
	ID          int64
	Index       int
	MimeType    string
	SizeBytes   int64
	Description string
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
	var (
		adv                                  Adventure
		accent, accent2, bg, ink, submitBG   sql.NullString
	)
	err := l.db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color
		FROM adventures WHERE slug = $1
	`, slug).Scan(&adv.ID, &adv.Slug, &adv.Meta.Title, &adv.Meta.Description, &adv.Meta.Author,
		&adv.Meta.IntroText, &adv.Meta.OutroText,
		&accent, &accent2, &bg, &ink, &submitBG)
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
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color
		FROM adventures ORDER BY slug
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Adventure
	for rows.Next() {
		a := &Adventure{}
		var accent, accent2, bg, ink, submitBG sql.NullString
		if err := rows.Scan(&a.ID, &a.Slug, &a.Meta.Title, &a.Meta.Description, &a.Meta.Author,
			&a.Meta.IntroText, &a.Meta.OutroText,
			&accent, &accent2, &bg, &ink, &submitBG); err != nil {
			return nil, err
		}
		a.Colors = Colors{
			Accent:           accent.String,
			AccentSecondary:  accent2.String,
			Background:       bg.String,
			Text:             ink.String,
			SubmitBackground: submitBG.String,
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
		SELECT id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes
		FROM adventure_pages
		WHERE adventure_id = $1
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
			disabled int
		)
		if err := rows.Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &acceptJS,
			&p.Location.Lat, &p.Location.Lng, &radius, &disabled, &p.Notes); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
		if acceptJS != "" {
			parsed, perr := ParseAcceptJSON([]byte(acceptJS))
			if perr != nil {
				return nil, fmt.Errorf("page %d: bad accept JSON: %w", p.ID, perr)
			}
			p.Accept = parsed
		}
		if radius.Valid {
			v := radius.Float64
			p.Location.RadiusM = &v
		}
		pages = append(pages, p)
	}
	// Fill each page's images and hints. Both are per-page satellite
	// tables — cheap N+1 queries given the typical 3–20 puzzles per
	// adventure.
	for i := range pages {
		imgs, err := l.loadImages(ctx, pages[i].ID)
		if err != nil {
			return nil, err
		}
		pages[i].Images = imgs
		audio, err := l.loadAudio(ctx, pages[i].ID)
		if err != nil {
			return nil, err
		}
		pages[i].Audio = audio
		hints, err := l.loadHints(ctx, pages[i].ID)
		if err != nil {
			return nil, err
		}
		pages[i].Hints = hints
	}
	return pages, nil
}

func (l *Library) loadHints(ctx context.Context, pageID int64) ([]Hint, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, text, delay_seconds
		FROM adventure_page_hints
		WHERE page_id = $1
		ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hint
	for rows.Next() {
		var h Hint
		if err := rows.Scan(&h.ID, &h.Text, &h.DelaySeconds); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Attach images per hint. The N is small (≤10 hints per page in
	// practice) so the per-hint follow-up query is cheaper than a
	// JOIN-and-group dance.
	for i := range out {
		imgs, err := l.loadHintImages(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Images = imgs
		audio, err := l.loadHintAudio(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Audio = audio
	}
	return out, nil
}

func (l *Library) loadHintImages(ctx context.Context, hintID int64) ([]Image, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(alt_text, '')
		FROM adventure_page_hint_images WHERE hint_id = $1
		ORDER BY idx
	`, hintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Image
	for rows.Next() {
		var im Image
		if err := rows.Scan(&im.ID, &im.Index, &im.MimeType, &im.SizeBytes, &im.AltText); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, nil
}

func (l *Library) loadImages(ctx context.Context, pageID int64) ([]Image, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(alt_text, '')
		FROM adventure_page_images WHERE page_id = $1
		ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Image
	for rows.Next() {
		var im Image
		if err := rows.Scan(&im.ID, &im.Index, &im.MimeType, &im.SizeBytes, &im.AltText); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, nil
}

func (l *Library) loadAudio(ctx context.Context, pageID int64) ([]Audio, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(description, '')
		FROM adventure_page_audio WHERE page_id = $1
		ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audio
	for rows.Next() {
		var a Audio
		if err := rows.Scan(&a.ID, &a.Index, &a.MimeType, &a.SizeBytes, &a.Description); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (l *Library) loadHintAudio(ctx context.Context, hintID int64) ([]Audio, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(description, '')
		FROM adventure_page_hint_audio WHERE hint_id = $1
		ORDER BY idx
	`, hintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audio
	for rows.Next() {
		var a Audio
		if err := rows.Scan(&a.ID, &a.Index, &a.MimeType, &a.SizeBytes, &a.Description); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// CheckAnswer reports whether submitted matches any of the page's accepted
// answers. Both literal and regex entries are matched against the same
// normalisation of the submitted answer (trimmed, lowercased, runs of
// whitespace collapsed). Regex entries are wrapped with `\A...\z` so a
// pattern like `\d{4}` only fires when the entire input is four digits,
// not when four digits appear anywhere — saves arrangers from having
// to remember the anchors themselves. ValidateAcceptEntries (called at
// save time) rejects patterns that don't compile, so the runtime
// branch here just skips on the rare corruption case rather than
// crashing the player flow.
func (p Page) CheckAnswer(submitted string) bool {
	norm := normalise(submitted)
	if norm == "" {
		return false
	}
	for _, a := range p.Accept {
		if a.Regex {
			re, err := regexp.Compile(`(?i)\A(?:` + a.Text + `)\z`)
			if err != nil {
				continue
			}
			if re.MatchString(norm) {
				return true
			}
			continue
		}
		if normalise(a.Text) == norm {
			return true
		}
	}
	return false
}

// ParseAcceptJSON decodes the `accept` column into AcceptEntry slices,
// tolerant of both the modern object shape and the legacy string-array
// shape used before the regex flag shipped (commit "Add regex
// answers"). Old rows look like `["foo","bar"]`; new rows look like
// `[{"text":"foo"},{"text":"bar","regex":true}]`. Without this
// fallback, an old row would silently fail to unmarshal into
// []AcceptEntry and the puzzle would reject every answer.
//
// Empty input (nil or `[]`) returns a nil slice and no error so
// callers don't have to special-case it.
func ParseAcceptJSON(b []byte) ([]AcceptEntry, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var entries []AcceptEntry
	if err := json.Unmarshal(b, &entries); err == nil {
		return entries, nil
	}
	// Legacy fallback: try a string array. We don't lose data here —
	// strings are literal answers; the arranger never had a way to
	// mark them as regex back then.
	var strs []string
	if err := json.Unmarshal(b, &strs); err != nil {
		return nil, err
	}
	entries = make([]AcceptEntry, len(strs))
	for i, s := range strs {
		entries[i] = AcceptEntry{Text: s}
	}
	return entries, nil
}

// ValidateAcceptEntries returns the first regex compile error found
// among the entries, or nil if every regex entry compiles. The save
// handler calls this so the arranger gets immediate feedback on a bad
// pattern instead of discovering it via "no answer ever matches" at
// run time.
func ValidateAcceptEntries(entries []AcceptEntry) error {
	for i, a := range entries {
		if !a.Regex {
			continue
		}
		if _, err := regexp.Compile(`(?i)\A(?:` + a.Text + `)\z`); err != nil {
			return fmt.Errorf("accept entry %d: invalid regex: %w", i+1, err)
		}
	}
	return nil
}

func normalise(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}
