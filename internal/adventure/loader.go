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
	// Lanes are the ordered groupings of puzzles inside this adventure.
	// Linear lanes have every team visit their pages in idx order;
	// shuffled lanes give each team a deterministic cyclic-shifted
	// permutation of the lane's pages so teams spread out at the start
	// instead of piling up on puzzle 1. Lanes are sequenced — finishing
	// every page in a team's permutation of lane N unlocks lane N+1.
	// When every lane is exhausted the team is done; the winner is
	// whoever's completed_at lands first. There's no separate finale
	// puzzle — if an arranger wants a shared finish-line puzzle, they
	// make the last lane linear with one puzzle. Adventures pre-dating
	// the lanes feature load a single synthesised linear lane so the
	// legacy "everyone the same order" behaviour is preserved without
	// arrangers having to do anything.
	Lanes []Lane
	// StrictAssets, when true, scopes page-image / page-audio visibility
	// to the team's current_page_index — the manifest endpoint and the
	// per-asset gate both honour it. Default false keeps the permissive
	// "any team in the adventure" behaviour that suits flavour-image
	// hunts; opt-in for hunts where the photo IS the puzzle.
	StrictAssets bool
}

// Lane is one ordered grouping of puzzles within an adventure.
type Lane struct {
	ID    int64
	Index int
	Mode  LaneMode
	// Name is the optional human label the arranger picked for this
	// lane. Empty means "fall back to a 'Lane N' default in the UI"
	// — we don't auto-fill it server-side so a future rename to ""
	// can re-engage the default.
	Name string
}

// LaneMode controls how a lane orders its puzzles for each team.
type LaneMode string

const (
	// LaneModeLinear plays every page in idx order — the legacy
	// behaviour, identical for every team in the run.
	LaneModeLinear LaneMode = "linear"
	// LaneModeShuffled hands every team a deterministic cyclic shift
	// of the lane's pages so they spread out across stations instead
	// of piling up on the first puzzle.
	LaneModeShuffled LaneMode = "shuffled"
)

// LanePages returns the enabled pages that belong to lane laneID,
// preserving the adventure's idx ordering. Used as the "canonical"
// ordering of a lane — shuffled-lane permutations are cyclic shifts
// of this slice. Pages with no lane assignment (library puzzles, or
// pages mid-edit) are excluded.
func (a *Adventure) LanePages(laneID int64) []Page {
	out := make([]Page, 0, len(a.Pages))
	for _, p := range a.Pages {
		if p.LaneID == laneID && !p.Disabled {
			out = append(out, p)
		}
	}
	return out
}

// LaneByID returns a pointer to the lane with the given id, or nil
// if the adventure has no such lane.
func (a *Adventure) LaneByID(laneID int64) *Lane {
	for i := range a.Lanes {
		if a.Lanes[i].ID == laneID {
			return &a.Lanes[i]
		}
	}
	return nil
}

// FirstLane returns the lane every team enters first (idx=0), or nil
// if the adventure has no lanes — the latter shouldn't happen after
// the lanes migration but the runtime tolerates it.
func (a *Adventure) FirstLane() *Lane {
	for i := range a.Lanes {
		if a.Lanes[i].Index == 0 {
			return &a.Lanes[i]
		}
	}
	if len(a.Lanes) > 0 {
		return &a.Lanes[0]
	}
	return nil
}

// NextLane returns the lane immediately after the given one in idx
// order, or nil when the given lane is the last one.
func (a *Adventure) NextLane(after *Lane) *Lane {
	if after == nil {
		return nil
	}
	var best *Lane
	for i := range a.Lanes {
		l := &a.Lanes[i]
		if l.Index <= after.Index {
			continue
		}
		if best == nil || l.Index < best.Index {
			best = l
		}
	}
	return best
}

// PageByID returns the page with the given id, or nil if the
// adventure has no such page. O(N) — fine for the small page counts
// adventures actually have.
func (a *Adventure) PageByID(pageID int64) *Page {
	for i := range a.Pages {
		if a.Pages[i].ID == pageID {
			return &a.Pages[i]
		}
	}
	return nil
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
	// LaneID points at the lane this page belongs to. Zero on library
	// puzzles (which never join a lane) and on pages briefly between
	// lane assignments during admin edits — the runtime tolerates
	// both by treating such pages as unrouteable.
	LaneID int64
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
		adv                                Adventure
		accent, accent2, bg, ink, submitBG sql.NullString
	)
	err := l.db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
		       strict_assets
		FROM adventures WHERE slug = $1
	`, slug).Scan(&adv.ID, &adv.Slug, &adv.Meta.Title, &adv.Meta.Description, &adv.Meta.Author,
		&adv.Meta.IntroText, &adv.Meta.OutroText,
		&accent, &accent2, &bg, &ink, &submitBG,
		&adv.StrictAssets)
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
	lanes, err := l.loadLanes(ctx, adv.ID)
	if err != nil {
		return nil, err
	}
	adv.Lanes = lanes
	pages, err := l.loadPages(ctx, adv.ID)
	if err != nil {
		return nil, err
	}
	adv.Pages = pages
	synthesizeDefaultLane(&adv)
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
		       strict_assets
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
			&accent, &accent2, &bg, &ink, &submitBG,
			&a.StrictAssets); err != nil {
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
	// Fill lanes and pages for each (small N * small N — acceptable).
	for _, a := range out {
		lanes, err := l.loadLanes(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		a.Lanes = lanes
		pages, err := l.loadPages(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		a.Pages = pages
		synthesizeDefaultLane(a)
	}
	return out, nil
}

// SynthesizedLaneID is the sentinel id used for the in-memory implicit
// lane the loader synthesizes for adventures that pre-date the lanes
// migration (or that the migration left untouched). It's negative so it
// can never collide with a real BIGSERIAL row, and the lanes runtime
// special-cases it so writes to teams.current_lane_id collapse to SQL
// NULL — the FK column won't accept a synthesized value.
const SynthesizedLaneID = int64(-1)

// IsSynthesizedLane reports whether laneID is the loader's synthesized
// implicit-lane sentinel, meaning the lane has no database row and
// downstream writers should treat it as "no lane assignment".
func IsSynthesizedLane(laneID int64) bool { return laneID < 0 }

// synthesizeDefaultLane gives an adventure that has pages but no
// adventure_lanes rows an implicit single linear lane in memory, and
// drops every orphan page (lane_id IS NULL in the DB) into it. Lets
// the runtime treat pre-lanes adventures and freshly-created
// adventures (no default lane row yet) as if they already had the
// migration's default lane structure, so progression code doesn't
// need to special-case "no lanes" alongside "lanes exist".
func synthesizeDefaultLane(adv *Adventure) {
	if adv == nil {
		return
	}
	if len(adv.Lanes) > 0 {
		return
	}
	hasAttachedPage := false
	for _, p := range adv.Pages {
		if !p.Disabled {
			hasAttachedPage = true
			break
		}
	}
	if !hasAttachedPage {
		return
	}
	adv.Lanes = []Lane{{ID: SynthesizedLaneID, Index: 0, Mode: LaneModeLinear}}
	for i := range adv.Pages {
		if adv.Pages[i].LaneID == 0 {
			adv.Pages[i].LaneID = SynthesizedLaneID
		}
	}
}

func (l *Library) loadPages(ctx context.Context, adventureID int64) ([]Page, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, lane_id
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
			laneID   sql.NullInt64
		)
		if err := rows.Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &acceptJS,
			&p.Location.Lat, &p.Location.Lng, &radius, &disabled, &p.Notes, &laneID); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
		if laneID.Valid {
			p.LaneID = laneID.Int64
		}
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

func (l *Library) loadLanes(ctx context.Context, adventureID int64) ([]Lane, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, idx, mode, name
		FROM adventure_lanes
		WHERE adventure_id = $1
		ORDER BY idx
	`, adventureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lane
	for rows.Next() {
		var (
			ln   Lane
			mode string
		)
		if err := rows.Scan(&ln.ID, &ln.Index, &mode, &ln.Name); err != nil {
			return nil, err
		}
		ln.Mode = LaneMode(mode)
		out = append(out, ln)
	}
	return out, rows.Err()
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
