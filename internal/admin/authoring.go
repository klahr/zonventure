package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
)

/* ---------------- Adventure authoring ---------------- */
//
// These endpoints let an org's admins create and edit adventures that
// live entirely in the database. Image uploads accept multipart/form-data
// and store the blob alongside the page. The org-side read endpoints go
// through the adventure.Library.

// maxImageBytes is the per-upload size cap. Defaults to 5 MiB; an
// operator can lower it for a small self-host box (or raise it for a
// dedicated arranger machine) by setting ZV_MAX_IMAGE_BYTES. Anything
// non-positive falls back to the default — the gate is "explicit
// override or sane default", never "no cap".
var maxImageBytes int64 = func() int64 {
	const def int64 = 5 * 1024 * 1024
	if s := os.Getenv("ZV_MAX_IMAGE_BYTES"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}()

// MaxImageBytes exposes the per-image upload cap to other packages
// (currently the MCP attach_image tool, which validates the decoded
// payload against the same ceiling the multipart UI enforces). Cheap
// accessor over the package-private var so callers don't have to
// re-parse ZV_MAX_IMAGE_BYTES.
func MaxImageBytes() int64 { return maxImageBytes }

type pageView struct {
	ID     int64  `json:"id"`
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	// PromptHTML is the goldmark-rendered sibling of Prompt — the same
	// HTML players see. Lets list views in arrange.html drop in the
	// pre-rendered result instead of calling /markdown/preview per card.
	PromptHTML string                  `json:"prompt_html,omitempty"`
	Hints      []hintView              `json:"hints"`
	Accept     []adventure.AcceptEntry `json:"accept"`
	Lat        float64                 `json:"lat"`
	Lng        float64                 `json:"lng"`
	RadiusM    *float64                `json:"radius_m,omitempty"`
	Gated      bool                    `json:"gated"`
	Disabled   bool                    `json:"disabled"`
	Notes      string                  `json:"notes"`
	Images     []imageView             `json:"images"`
	Audio      []audioView             `json:"audio"`
	// LaneID is the lane this page currently belongs to. Zero means
	// "unassigned" — fresh pages briefly between lane attachments
	// land here, and the editor renders them in an "Unsorted" bucket
	// or auto-files them into the default lane.
	LaneID int64 `json:"lane_id,omitempty"`
}

// hintView is a single hint row on the editor side. DelaySeconds is
// how many seconds after the team reaches this puzzle the hint
// becomes revealable; 0 means available immediately.
//
// ID round-trips with the editor: present on read, echoed back by the
// client on update so the upsert in pages.go can match existing rows
// and preserve their attached images. Omitting ID on a write is the
// signal that this row is new and the server should INSERT.
//
// Images is read-only — the wire shape is shared with the puzzle
// image flow so the front-end can render the per-hint image strip
// from the same DTO. Image uploads / edits / deletes go through the
// dedicated /api/admin/hints/{hintID}/images endpoints; the field is
// ignored on the way in.
type hintView struct {
	ID           *int64      `json:"id,omitempty"`
	Text         string      `json:"text"`
	DelaySeconds int         `json:"delay_seconds"`
	Images       []imageView `json:"images,omitempty"`
	Audio        []audioView `json:"audio,omitempty"`
}

type imageView struct {
	ID        int64  `json:"id"`
	Index     int    `json:"index"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	URL       string `json:"url"`
	AltText   string `json:"alt_text"`
}

// audioView is the wire shape for one attached audio clip on a puzzle
// or hint. Mirrors imageView but carries `description` (a transcript
// hint / summary) instead of an alt text — the column name reflects
// the WCAG fallback expected for audio.
type audioView struct {
	ID          int64  `json:"id"`
	Index       int    `json:"index"`
	MimeType    string `json:"mime_type"`
	SizeBytes   int64  `json:"size_bytes"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type adventureEditDTO struct {
	ID int64 `json:"id"`
	// GraphPublished says the adventure plays a published graph, so the
	// lane pages here are no longer played and their edits are refused
	// (LaneOnly). The editor says so.
	GraphPublished bool   `json:"graph_published,omitempty"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Author         string `json:"author"`
	IntroText      string `json:"intro_text"`
	OutroText      string `json:"outro_text"`
	// IntroHTML / OutroHTML are the goldmark-rendered siblings of
	// IntroText / OutroText — same shape pageView uses for prompt_html.
	// The arranger UI (and any future read surface) renders the HTML
	// instead of dumping the raw markdown source as plain text.
	IntroHTML            string `json:"intro_html,omitempty"`
	OutroHTML            string `json:"outro_html,omitempty"`
	AccentColor          string `json:"accent_color"`
	AccentSecondaryColor string `json:"accent_secondary_color"`
	BackgroundColor      string `json:"background_color"`
	TextColor            string `json:"text_color"`
	SubmitBgColor        string `json:"submit_bg_color"`
	Font                 string `json:"font"`
	HeadingFont          string `json:"heading_font"`
	Corners              string `json:"corners"`
	Cards                string `json:"cards"`
	CardOutline          bool   `json:"card_outline"`
	// Language is the BCP 47 tag of the puzzle content (e.g. "en",
	// "sv"). Defaults to "en" via the schema's column default.
	Language  string `json:"language"`
	AvatarURL string `json:"avatar_url,omitempty"`
	// OrgID identifies the owner. The publish flag is only load-bearing
	// for the catalog publisher's adventures (it gates cross-org
	// catalog visibility) and is set by the operator, not here.
	OrgID     *int64 `json:"org_id,omitempty"`
	Published bool   `json:"published"`
	// StrictAssets, when true, scopes player asset visibility (page
	// images + audio + manifest URLs) to the team's current page index.
	// Default false keeps the permissive prefetch-friendly behaviour.
	StrictAssets bool `json:"strict_assets"`
	// HasCheatPassword says a cheat password is set. The password itself
	// is never sent back.
	HasCheatPassword bool `json:"has_cheat_password"`
	// Starting location — optional. Both StartLat and StartLng are
	// pointers because they're meaningful only as a pair; an absent
	// start (one or both nil) means "fall back to the first gated
	// puzzle's location". StartLabel is a short free-text hint
	// surfaced alongside the coords ("vid bänken under trappan").
	StartLat   *float64 `json:"start_lat,omitempty"`
	StartLng   *float64 `json:"start_lng,omitempty"`
	StartLabel string   `json:"start_label,omitempty"`
	// Brief is what the adventure is meant to be, as agreed with its
	// author: the yardstick an assistant designs each step against.
	Brief  Brief       `json:"brief"`
	Shares []shareView `json:"shares"`
	Pages  []pageView  `json:"pages"`
	// Lanes lists the adventure's lanes in idx order. The editor
	// uses this to render its collapsible lane sections; Pages still
	// arrives flat (with each page tagged by lane_id) so the editor
	// can group them client-side without an extra round-trip.
	Lanes []laneDTO `json:"lanes"`
	// RunningEventCount lets the editor warn the author before saving
	// edits that will land on teams currently mid-event. Counts both
	// scheduled (about to start) and running events using this slug —
	// either is "live enough" that a content swap is risky.
	RunningEventCount int `json:"running_event_count"`
}

// shareView is one row from adventure_shares expanded with the org name,
// so the authoring UI can render a chip without a second lookup.
type shareView struct {
	OrgID     int64  `json:"org_id"`
	OrgName   string `json:"org_name"`
	GrantedAt string `json:"granted_at,omitempty"`
}

// LoadAdventureForEdit returns full adventure content (including accept
// lists and locations) for the authoring UI.
func (h *Handlers) LoadAdventureForEdit(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	dto, err := loadAdventureDTO(r.Context(), h.DB, slug)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "load failed", err)
		return
	}
	switch err := RefuseIfGraph(r.Context(), h.DB, dto.ID); {
	case errors.Is(err, ErrGraphAdventure):
		dto.GraphPublished = true
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "load failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

type createAdventureRequest struct {
	Slug                 string `json:"slug"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	Author               string `json:"author"`
	IntroText            string `json:"intro_text"`
	OutroText            string `json:"outro_text"`
	AccentColor          string `json:"accent_color"`
	AccentSecondaryColor string `json:"accent_secondary_color"`
	BackgroundColor      string `json:"background_color"`
	TextColor            string `json:"text_color"`
	SubmitBgColor        string `json:"submit_bg_color"`
	Language             string `json:"language,omitempty"` // empty defaults to "en" via column default
}

// validLanguageTag accepts a permissive subset of BCP 47: a primary
// language subtag (2–3 letters) optionally followed by region or
// script subtags joined with `-`. Examples: "en", "sv", "pt-BR",
// "zh-Hant". We don't enforce the IANA registry — authors writing
// in tlh / oc / minority codes shouldn't be blocked by an
// out-of-date allowlist, and the value is rendered as-is on the
// Catalog so a typo is visible to the author who set it.
func validLanguageTag(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 32 {
		return false
	}
	for i, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-' && i > 0 && i < len(s)-1:
		default:
			return false
		}
	}
	return true
}

// CreateAdventure inserts an empty adventure. Admin-only.
func (h *Handlers) CreateAdventure(w http.ResponseWriter, r *http.Request) {
	var req createAdventureRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Slug == "" {
		httpx.Error(w, http.StatusBadRequest, "slug and title required")
		return
	}
	title, err := validateName(req.Title, MaxNameLen)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Title = title
	if !validSlug(req.Slug) {
		httpx.Error(w, http.StatusBadRequest, "slug may only contain a-z 0-9 and hyphens")
		return
	}
	if _, err := validateBlurb(req.Description, MaxBlurbLen); err != nil {
		httpx.Error(w, http.StatusBadRequest, "description "+err.Error())
		return
	}
	if _, err := validateBlurb(req.Author, MaxAuthorLen); err != nil {
		httpx.Error(w, http.StatusBadRequest, "author "+err.Error())
		return
	}
	if _, err := validateBlurb(req.IntroText, MaxBlurbLen); err != nil {
		httpx.Error(w, http.StatusBadRequest, "intro_text "+err.Error())
		return
	}
	if _, err := validateBlurb(req.OutroText, MaxBlurbLen); err != nil {
		httpx.Error(w, http.StatusBadRequest, "outro_text "+err.Error())
		return
	}
	colorFields := []struct{ label, value string }{
		{"accent_color", req.AccentColor},
		{"accent_secondary_color", req.AccentSecondaryColor},
		{"background_color", req.BackgroundColor},
		{"text_color", req.TextColor},
		{"submit_bg_color", req.SubmitBgColor},
	}
	for _, f := range colorFields {
		if !validHexColor(f.value) {
			httpx.Error(w, http.StatusBadRequest, f.label+` must be empty or "#rrggbb"`)
			return
		}
	}
	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = "en"
	}
	if !validLanguageTag(language) {
		httpx.Error(w, http.StatusBadRequest,
			`language must be a BCP 47 tag (e.g. "en", "sv", "pt-BR")`)
		return
	}
	// Ownership: admins create global-catalog rows (org_id NULL); every
	// other authoring role stamps their own org onto the row.
	me, _ := auth.FromContext(r.Context())
	org := callerOrgID(me)
	if !canAuthor(me, org) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var orgArg any
	if org.Valid {
		orgArg = org.Int64
	}
	var id int64
	err = h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventures(slug, title, description, author, intro_text, outro_text,
		                       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
		                       language,
		                       org_id, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id
	`, req.Slug, req.Title, req.Description, req.Author, req.IntroText, req.OutroText,
		nullIfEmpty(req.AccentColor), nullIfEmpty(req.AccentSecondaryColor),
		nullIfEmpty(req.BackgroundColor), nullIfEmpty(req.TextColor), nullIfEmpty(req.SubmitBgColor),
		language,
		orgArg, me.ID).Scan(&id)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "slug already used")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id": id, "slug": req.Slug, "title": req.Title,
	})
}

// adventureOwner looks up the adventure's org_id by slug. Returns
// sql.ErrNoRows when the slug is unknown.
func (h *Handlers) adventureOwner(ctx context.Context, slug string) (sql.NullInt64, error) {
	var org sql.NullInt64
	err := h.DB.QueryRowContext(ctx, `SELECT org_id FROM adventures WHERE slug = $1`, slug).Scan(&org)
	return org, err
}

// requireEditAdventure centralises the load-owner + policy-check step
// every adventure-mutating endpoint performs. Writes the HTTP error on
// failure so callers can just early-return.
func (h *Handlers) requireEditAdventure(w http.ResponseWriter, r *http.Request, slug string) (sql.NullInt64, bool) {
	org, err := h.adventureOwner(r.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return sql.NullInt64{}, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return sql.NullInt64{}, false
	}
	me, _ := auth.FromContext(r.Context())
	if !canAuthor(me, org) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return sql.NullInt64{}, false
	}
	return org, true
}

type updateAdventureRequest struct {
	Title                *string `json:"title,omitempty"`
	Description          *string `json:"description,omitempty"`
	Author               *string `json:"author,omitempty"`
	Slug                 *string `json:"slug,omitempty"`
	IntroText            *string `json:"intro_text,omitempty"`
	OutroText            *string `json:"outro_text,omitempty"`
	AccentColor          *string `json:"accent_color,omitempty"`
	AccentSecondaryColor *string `json:"accent_secondary_color,omitempty"`
	BackgroundColor      *string `json:"background_color,omitempty"`
	TextColor            *string `json:"text_color,omitempty"`
	SubmitBgColor        *string `json:"submit_bg_color,omitempty"`
	Font                 *string `json:"font,omitempty"`
	HeadingFont          *string `json:"heading_font,omitempty"`
	Corners              *string `json:"corners,omitempty"`
	Cards                *string `json:"cards,omitempty"`
	CardOutline          *bool   `json:"card_outline,omitempty"`
	Language             *string `json:"language,omitempty"`
	// Published is refused: catalog listing is the operator's call
	// (PUT /api/admin/catalog/{slug}/published).
	Published    *bool `json:"published,omitempty"`
	StrictAssets *bool `json:"strict_assets,omitempty"`
	// CheatPassword sets the password that unlocks cheating on the
	// phone; empty removes it.
	CheatPassword *string `json:"cheat_password,omitempty"`
	// Starting location (optional). Pass both lat and lng pointing at
	// values to set; pass them with explicit null inside the JSON body
	// to clear ({"start_lat": null}) — the wrapper struct distinguishes
	// "not present" (no patch) from "present and null" (clear). The
	// label is independent and passes through verbatim.
	StartLat   *Nullable[float64] `json:"start_lat,omitempty"`
	StartLng   *Nullable[float64] `json:"start_lng,omitempty"`
	StartLabel *string            `json:"start_label,omitempty"`
	// Brief replaces the whole brief.
	Brief *Brief `json:"brief,omitempty"`
}

// Brief records an adventure's intent. Every field is free text, or a
// number where one is natural, because it is written in conversation
// and read back by people and assistants, never by the engine.
type Brief struct {
	Theme         string `json:"theme,omitempty"`
	Setting       string `json:"setting,omitempty"`
	Tone          string `json:"tone,omitempty"`
	Audience      string `json:"audience,omitempty"`
	AgeRange      string `json:"age_range,omitempty"`
	GroupSize     string `json:"group_size,omitempty"`
	Minutes       int    `json:"minutes,omitempty"`
	Difficulty    string `json:"difficulty,omitempty"`
	DistanceM     int    `json:"distance_m,omitempty"`
	Accessibility string `json:"accessibility,omitempty"`
	Premise       string `json:"premise,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

// maxBriefBytes bounds a stored brief; a real one is a few hundred bytes.
const maxBriefBytes = 16 << 10

// Nullable distinguishes "field absent in JSON" (the outer pointer is
// nil) from "field present with null" (the outer pointer is non-nil
// and Set is true with Value zero) and "field present with value".
// Used by patch-style endpoints where clearing a column is a real
// state transition the caller may want to express. UnmarshalJSON
// flips Set to true on any decoded input, so a caller writing
// `{"start_lat": null}` clears the column while an absent field is
// left unchanged.
type Nullable[T any] struct {
	Value T
	Valid bool // true when the JSON value was non-null
}

// UnmarshalJSON tracks whether the field was present at all (the
// containing struct has *Nullable; nil means absent) and whether the
// value was null vs a real T.
func (n *Nullable[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.Valid = false
		var zero T
		n.Value = zero
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	n.Value = v
	n.Valid = true
	return nil
}

// UpdateAdventure amends the metadata.
func (h *Handlers) UpdateAdventure(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	var req updateAdventureRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := applyAdventureMetaUpdate(r.Context(), h.DB, slug, req); err != nil {
		var ie *importError
		if errors.As(err, &ie) {
			httpx.Error(w, ie.status, ie.Error())
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// applyAdventureMetaUpdate is the shared core of UpdateAdventure (the
// HTTP handler) and UpdateAdventureMetaJSON (the MCP entry point).
// Builds the SET clause off whichever fields the caller patched,
// runs the full per-column validation pipeline, and either commits
// with a stamped updated_at or returns an *importError carrying
// the right HTTP status for the multipart handler to map back.
//
// "nothing to update" reads as 400 — empty patches are caller bugs,
// not no-ops. Slug uniqueness violations bubble up as 409 via the
// generic conflict mapping.
func applyAdventureMetaUpdate(ctx context.Context, db *sql.DB, slug string, req updateAdventureRequest) error {
	sets := []string{"updated_at = now_text()"}
	args := []any{}
	add := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)+1))
		args = append(args, val)
	}
	if req.Title != nil {
		title, err := validateName(*req.Title, MaxNameLen)
		if err != nil {
			return badRequestf("title %s", err.Error())
		}
		add("title", title)
	}
	if req.Description != nil {
		if _, err := validateBlurb(*req.Description, MaxBlurbLen); err != nil {
			return badRequestf("description %s", err.Error())
		}
		add("description", *req.Description)
	}
	if req.Author != nil {
		if _, err := validateBlurb(*req.Author, MaxAuthorLen); err != nil {
			return badRequestf("author %s", err.Error())
		}
		add("author", *req.Author)
	}
	if req.IntroText != nil {
		if _, err := validateBlurb(*req.IntroText, MaxBlurbLen); err != nil {
			return badRequestf("intro_text %s", err.Error())
		}
		add("intro_text", *req.IntroText)
	}
	if req.OutroText != nil {
		if _, err := validateBlurb(*req.OutroText, MaxBlurbLen); err != nil {
			return badRequestf("outro_text %s", err.Error())
		}
		add("outro_text", *req.OutroText)
	}
	if req.Slug != nil {
		newSlug := strings.TrimSpace(*req.Slug)
		if !validSlug(newSlug) {
			return badRequestf("slug may only contain a-z 0-9 and hyphens")
		}
		add("slug", newSlug)
	}
	colorPatches := []struct {
		col string
		val *string
	}{
		{"accent_color", req.AccentColor},
		{"accent_secondary_color", req.AccentSecondaryColor},
		{"background_color", req.BackgroundColor},
		{"text_color", req.TextColor},
		{"submit_bg_color", req.SubmitBgColor},
	}
	for _, p := range colorPatches {
		if p.val == nil {
			continue
		}
		if !validHexColor(*p.val) {
			return badRequestf(`%s must be empty or "#rrggbb"`, p.col)
		}
		add(p.col, nullIfEmpty(*p.val))
	}
	if req.Font != nil {
		if _, ok := adventure.Fonts[*req.Font]; !ok && *req.Font != "" {
			return badRequestf("font must be empty or one of the listed fonts")
		}
		add("font", *req.Font)
	}
	if req.HeadingFont != nil {
		if _, ok := adventure.Fonts[*req.HeadingFont]; !ok && *req.HeadingFont != "" {
			return badRequestf("heading_font must be empty or one of the listed fonts")
		}
		add("heading_font", *req.HeadingFont)
	}
	if req.Corners != nil {
		if !adventure.Corners[*req.Corners] && *req.Corners != "" {
			return badRequestf(`corners must be empty, "rounded", "soft" or "sharp"`)
		}
		add("corners", *req.Corners)
	}
	if req.Cards != nil {
		if !adventure.Cards[*req.Cards] && *req.Cards != "" {
			return badRequestf(`cards must be empty, "raised" or "flat"`)
		}
		add("cards", *req.Cards)
	}
	if req.CardOutline != nil {
		add("card_outline", *req.CardOutline)
	}
	if req.Language != nil {
		lang := strings.TrimSpace(*req.Language)
		if !validLanguageTag(lang) {
			return badRequestf(`language must be a BCP 47 tag (e.g. "en", "sv", "pt-BR")`)
		}
		add("language", lang)
	}
	if req.Published != nil {
		return badRequestf("published is set by the platform operator when curating the catalog")
	}
	if req.StrictAssets != nil {
		add("strict_assets", *req.StrictAssets)
	}
	if req.CheatPassword != nil {
		switch pw := *req.CheatPassword; {
		case pw == "":
			add("cheat_password_hash", nil)
		case len(pw) > MaxNameLen:
			return badRequestf("cheat_password is too long")
		default:
			hash, err := auth.HashPassword(pw)
			if err != nil {
				return err
			}
			add("cheat_password_hash", hash)
		}
	}
	if req.StartLat != nil {
		if req.StartLat.Valid {
			lat := req.StartLat.Value
			if lat < -90 || lat > 90 {
				return badRequestf("start_lat out of range")
			}
			add("start_lat", lat)
		} else {
			add("start_lat", nil)
		}
	}
	if req.StartLng != nil {
		if req.StartLng.Valid {
			lng := req.StartLng.Value
			if lng < -180 || lng > 180 {
				return badRequestf("start_lng out of range")
			}
			add("start_lng", lng)
		} else {
			add("start_lng", nil)
		}
	}
	if req.StartLabel != nil {
		label, err := validateBlurb(*req.StartLabel, MaxNameLen)
		if err != nil {
			return badRequestf("start_label %s", err.Error())
		}
		add("start_label", label)
	}
	if req.Brief != nil {
		b, _ := json.Marshal(req.Brief)
		if len(b) > maxBriefBytes {
			return badRequestf("brief is longer than %d bytes", maxBriefBytes)
		}
		add("brief", string(b))
	}
	if len(sets) == 1 {
		return badRequestf("nothing to update")
	}
	args = append(args, slug)
	q := fmt.Sprintf(`UPDATE adventures SET %s WHERE slug = $%d`, strings.Join(sets, ", "), len(args))
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return conflictf("update failed: %s", err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &importError{status: http.StatusNotFound, msg: "unknown adventure"}
	}
	return nil
}

// UpdateAdventureMetaJSON applies a partial-update patch to the
// named adventure. patchJSON shape matches updateAdventureRequest:
// every field is optional and only present keys are applied. The
// LLM-facing MCP tool calls this so the model can rename, recolor,
// or tweak intro/outro without rebuilding the whole envelope.
//
// Caller is responsible for authorization
// (AuthorizeAdventureEdit). Returns *importError carrying the
// HTTP status equivalent of any validation failure, or a wrapped
// DB error for infrastructure problems.
func UpdateAdventureMetaJSON(ctx context.Context, db *sql.DB, slug string, patchJSON []byte) error {
	var req updateAdventureRequest
	if err := json.Unmarshal(patchJSON, &req); err != nil {
		return badRequestf("patch parse: %s", err.Error())
	}
	return applyAdventureMetaUpdate(ctx, db, slug, req)
}

// DeleteAdventure removes an adventure (and all its pages + images).
// Adventures that still back runs or assignments can't be deleted.
func (h *Handlers) DeleteAdventure(w http.ResponseWriter, r *http.Request) {
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	if err := DeleteAdventureBySlug(r.Context(), h.DB, slug); err != nil {
		var ie *importError
		if errors.As(err, &ie) {
			httpx.Error(w, ie.status, ie.Error())
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "delete failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteAdventureBySlug is the non-HTTP core of DeleteAdventure. It
// blocks the delete if the adventure still backs runs or
// assignments (those need to be cleaned up first), then sweeps the
// shares table (no FK on slug — kept loose so slug renames don't
// break) and drops the adventure row. CASCADE on adventure_pages,
// adventure_lanes, etc. handles the rest.
//
// Used by the MCP delete_adventure tool. The password-confirm gate
// the HTTP handler enforces is intentionally NOT replicated here:
// the bearer token IS the credential, and the MCP tool surfaces
// its own confirm-flag UX before reaching this function.
//
// Caller is responsible for authorization
// (AuthorizeAdventureEdit). Returns *importError on
// "still has runs / assignments / unknown slug"; wrapped DB error
// otherwise.
func DeleteAdventureBySlug(ctx context.Context, db *sql.DB, slug string) error {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE adventure_slug = $1`, slug).Scan(&count); err != nil {
		return fmt.Errorf("runs lookup: %w", err)
	}
	if count > 0 {
		return conflictf("adventure still has runs; delete them first")
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM adventure_assignments WHERE adventure_slug = $1`, slug).Scan(&count); err != nil {
		return fmt.Errorf("assignments lookup: %w", err)
	}
	if count > 0 {
		return conflictf("adventure still has event passes; revoke them first")
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM adventure_shares WHERE adventure_slug = $1`, slug); err != nil {
		return fmt.Errorf("shares cleanup: %w", err)
	}
	res, err := db.ExecContext(ctx, `DELETE FROM adventures WHERE slug = $1`, slug)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &importError{status: http.StatusNotFound, msg: "unknown adventure"}
	}
	return nil
}

/* page CRUD lives in pages.go — types, validation, requireEditPage,
   CreatePage/UpdatePage/DeletePage/ReorderPages, replacePageHints. */
/* Image upload + serve + alt-text edit lives in images.go. */

/* ---------------- helpers ---------------- */

func adventureIDBySlug(ctx context.Context, db *sql.DB, slug string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&id)
	return id, err
}

func loadAdventureDTO(ctx context.Context, db *sql.DB, slug string) (*adventureEditDTO, error) {
	a := adventureEditDTO{Pages: []pageView{}, Shares: []shareView{}}
	var (
		accent, accent2, bg, ink, submitBG sql.NullString
		hasAvatar                          int
		published                          int
		orgID                              sql.NullInt64
		startLat, startLng                 sql.NullFloat64
		brief                              []byte
	)
	if err := db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color, font,
		       heading_font, corners, cards, card_outline,
		       language,
		       (avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       published, org_id, strict_assets, (cheat_password_hash IS NOT NULL),
		       start_lat, start_lng, start_label, brief
		FROM adventures WHERE slug = $1
	`, slug).Scan(&a.ID, &a.Slug, &a.Title, &a.Description, &a.Author,
		&a.IntroText, &a.OutroText,
		&accent, &accent2, &bg, &ink, &submitBG, &a.Font, &a.HeadingFont, &a.Corners, &a.Cards, &a.CardOutline, &a.Language, &hasAvatar, &published, &orgID, &a.StrictAssets, &a.HasCheatPassword,
		&startLat, &startLng, &a.StartLabel, &brief); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(brief, &a.Brief); err != nil {
		return nil, fmt.Errorf("decoding brief: %w", err)
	}
	if startLat.Valid {
		v := startLat.Float64
		a.StartLat = &v
	}
	if startLng.Valid {
		v := startLng.Float64
		a.StartLng = &v
	}
	a.AccentColor = accent.String
	a.AccentSecondaryColor = accent2.String
	a.BackgroundColor = bg.String
	a.TextColor = ink.String
	a.SubmitBgColor = submitBG.String
	a.Published = published != 0
	if orgID.Valid {
		v := orgID.Int64
		a.OrgID = &v
	}
	if hasAvatar != 0 {
		a.AvatarURL = adventureAvatarURL(a.Slug)
	}
	if a.IntroText != "" {
		a.IntroHTML = mdx.Render(a.IntroText)
	}
	if a.OutroText != "" {
		a.OutroHTML = mdx.Render(a.OutroText)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, lane_id
		FROM adventure_pages WHERE adventure_id = $1 ORDER BY idx
	`, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			p        pageView
			acceptJS string
			radius   sql.NullFloat64
			disabled int
			laneID   sql.NullInt64
		)
		if err := rows.Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &acceptJS,
			&p.Lat, &p.Lng, &radius, &disabled, &p.Notes, &laneID); err != nil {
			return nil, err
		}
		if laneID.Valid {
			p.LaneID = laneID.Int64
		}
		p.Disabled = disabled != 0
		p.PromptHTML = mdx.Render(p.Prompt)
		// Use the tolerant parser so legacy string-array rows
		// (pre "Add regex answers" commit) don't silently load as
		// empty — they'd otherwise reject every player answer.
		parsed, perr := adventure.ParseAcceptJSON([]byte(acceptJS))
		if perr == nil {
			p.Accept = parsed
		}
		if p.Accept == nil {
			p.Accept = []adventure.AcceptEntry{}
		}
		if radius.Valid {
			v := radius.Float64
			p.RadiusM = &v
			p.Gated = v > 0
		}
		p.Images = []imageView{}
		p.Audio = []audioView{}
		a.Pages = append(a.Pages, p)
	}
	for i := range a.Pages {
		imgs, err := loadPageImages(ctx, db, a.Pages[i].ID)
		if err != nil {
			return nil, err
		}
		a.Pages[i].Images = imgs
		audio, err := loadPageAudio(ctx, db, a.Pages[i].ID)
		if err != nil {
			return nil, err
		}
		a.Pages[i].Audio = audio
		hints, err := loadPageHints(ctx, db, a.Pages[i].ID)
		if err != nil {
			return nil, err
		}
		a.Pages[i].Hints = hints
	}
	shares, err := loadAdventureShares(ctx, db, a.Slug)
	if err != nil {
		return nil, err
	}
	a.Shares = shares
	// Lanes ride alongside pages in the editor DTO so the front-end
	// can render lane sections without a second round-trip.
	lanes, err := loadAdventureLanes(ctx, db, a.ID)
	if err != nil {
		return nil, err
	}
	a.Lanes = lanes
	// Count events currently running with this adventure. We treat
	// 'running' as "any change to a puzzle ships immediately to a real
	// team" — that's the case worth warning about. Scheduled events
	// haven't pushed state yet, so we leave them out.
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM events WHERE adventure_slug = $1 AND state = 'running'
	`, a.Slug).Scan(&a.RunningEventCount); err != nil {
		return nil, err
	}
	return &a, nil
}

// validHexColor accepts "" (meaning "use default") or exactly "#rrggbb"
// (case-insensitive). Rejecting "red"/shorthand/rgba keeps client output
// deterministic across browsers and makes the field safe to interpolate
// straight into a CSS variable.
func validHexColor(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// nullIfEmpty turns an empty string into a SQL NULL so the adventures row
// stores the explicit "use default" sentinel instead of a bogus "".
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func validSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}
