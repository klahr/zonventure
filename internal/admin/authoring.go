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

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/mdx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Adventure authoring (admin-only) ---------------- */
//
// These endpoints let the global admin create and edit adventures that
// live entirely in the database. Image uploads accept multipart/form-data
// and store the blob alongside the page. Everything is admin-only — the
// org-side endpoints read adventures through the adventure.Library.

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
}

type imageView struct {
	ID        int64  `json:"id"`
	Index     int    `json:"index"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	URL       string `json:"url"`
	AltText   string `json:"alt_text"`
}

type adventureEditDTO struct {
	ID                   int64       `json:"id"`
	Slug                 string      `json:"slug"`
	Title                string      `json:"title"`
	Description          string      `json:"description"`
	Author               string      `json:"author"`
	IntroText            string      `json:"intro_text"`
	OutroText            string      `json:"outro_text"`
	AccentColor          string      `json:"accent_color"`
	AccentSecondaryColor string      `json:"accent_secondary_color"`
	BackgroundColor      string      `json:"background_color"`
	TextColor            string      `json:"text_color"`
	AvatarURL            string      `json:"avatar_url,omitempty"`
	// OrgID identifies the owner — nil for admin-owned global-catalog
	// adventures, set for org-owned ones. The publish flag is only
	// load-bearing for the nil case (it gates cross-org catalog
	// visibility); the UI uses this to hide the toggle for org-owned
	// rows where flipping it has no effect.
	OrgID                *int64      `json:"org_id,omitempty"`
	Published            bool        `json:"published"`
	Shares               []shareView `json:"shares"`
	Pages                []pageView  `json:"pages"`
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
		httpx.Error(w, http.StatusInternalServerError, "load failed: "+err.Error())
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
}

// CreateAdventure inserts an empty adventure. Admin-only.
func (h *Handlers) CreateAdventure(w http.ResponseWriter, r *http.Request) {
	var req createAdventureRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Title = strings.TrimSpace(req.Title)
	if req.Slug == "" || req.Title == "" {
		httpx.Error(w, http.StatusBadRequest, "slug and title required")
		return
	}
	if !validSlug(req.Slug) {
		httpx.Error(w, http.StatusBadRequest, "slug may only contain a-z 0-9 and hyphens")
		return
	}
	colorFields := []struct{ label, value string }{
		{"accent_color", req.AccentColor},
		{"accent_secondary_color", req.AccentSecondaryColor},
		{"background_color", req.BackgroundColor},
		{"text_color", req.TextColor},
	}
	for _, f := range colorFields {
		if !validHexColor(f.value) {
			httpx.Error(w, http.StatusBadRequest, f.label+` must be empty or "#rrggbb"`)
			return
		}
	}
	// Ownership: admins create global-catalog rows (org_id NULL); every
	// other authoring role stamps their own org onto the row.
	me, _ := auth.FromContext(r.Context())
	org := callerOrgID(me)
	var orgArg any
	if org.Valid {
		orgArg = org.Int64
	}
	var id int64
	err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventures(slug, title, description, author, intro_text, outro_text,
		                       accent_color, accent_secondary_color, background_color, text_color,
		                       org_id, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`, req.Slug, req.Title, req.Description, req.Author, req.IntroText, req.OutroText,
		nullIfEmpty(req.AccentColor), nullIfEmpty(req.AccentSecondaryColor),
		nullIfEmpty(req.BackgroundColor), nullIfEmpty(req.TextColor),
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
	if !canEditOwned(me, org) {
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
	Published            *bool   `json:"published,omitempty"`
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
	sets := []string{"updated_at = now_text()"}
	args := []any{}
	// Postgres uses `$N` placeholders, not `?`. Each `add` mints the
	// next index off the current arg count so SET clauses stay in sync
	// with their values regardless of which optional fields the caller
	// patched.
	add := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)+1))
		args = append(args, val)
	}
	if req.Title != nil {
		add("title", strings.TrimSpace(*req.Title))
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if req.Author != nil {
		add("author", *req.Author)
	}
	if req.IntroText != nil {
		add("intro_text", *req.IntroText)
	}
	if req.OutroText != nil {
		add("outro_text", *req.OutroText)
	}
	if req.Slug != nil {
		newSlug := strings.TrimSpace(*req.Slug)
		if !validSlug(newSlug) {
			httpx.Error(w, http.StatusBadRequest, "slug may only contain a-z 0-9 and hyphens")
			return
		}
		add("slug", newSlug)
	}
	// Colour fields: accept empty string as "clear to default". Pointer
	// semantics let callers patch one colour at a time without touching
	// the others.
	colorPatches := []struct {
		col string
		val *string
	}{
		{"accent_color", req.AccentColor},
		{"accent_secondary_color", req.AccentSecondaryColor},
		{"background_color", req.BackgroundColor},
		{"text_color", req.TextColor},
	}
	for _, p := range colorPatches {
		if p.val == nil {
			continue
		}
		if !validHexColor(*p.val) {
			httpx.Error(w, http.StatusBadRequest, p.col+` must be empty or "#rrggbb"`)
			return
		}
		add(p.col, nullIfEmpty(*p.val))
	}
	if req.Published != nil {
		if *req.Published {
			add("published", 1)
		} else {
			add("published", 0)
		}
	}
	if len(sets) == 1 {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	args = append(args, slug)
	q := fmt.Sprintf(`UPDATE adventures SET %s WHERE slug = $%d`, strings.Join(sets, ", "), len(args))
	res, err := h.DB.ExecContext(r.Context(), q, args...)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "update failed: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
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
	var count int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM events WHERE adventure_slug = $1`, slug).Scan(&count); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "runs lookup failed")
		return
	}
	if count > 0 {
		httpx.Error(w, http.StatusConflict, "adventure still has runs; delete them first")
		return
	}
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM adventure_assignments WHERE adventure_slug = $1`, slug).Scan(&count); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assignments lookup failed")
		return
	}
	if count > 0 {
		httpx.Error(w, http.StatusConflict, "adventure still has assignments; revoke them first")
		return
	}
	// Shares have no FK to adventures.slug (kept loose so slug renames
	// don't break), so clean them up explicitly on delete.
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_shares WHERE adventure_slug = $1`, slug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "shares cleanup failed")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `DELETE FROM adventures WHERE slug = $1`, slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
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
		accent, accent2, bg, ink sql.NullString
		hasAvatar                int
		published                int
		orgID                    sql.NullInt64
	)
	if err := db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color,
		       (avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       published, org_id
		FROM adventures WHERE slug = $1
	`, slug).Scan(&a.ID, &a.Slug, &a.Title, &a.Description, &a.Author,
		&a.IntroText, &a.OutroText,
		&accent, &accent2, &bg, &ink, &hasAvatar, &published, &orgID); err != nil {
		return nil, err
	}
	a.AccentColor = accent.String
	a.AccentSecondaryColor = accent2.String
	a.BackgroundColor = bg.String
	a.TextColor = ink.String
	a.Published = published != 0
	if orgID.Valid {
		v := orgID.Int64
		a.OrgID = &v
	}
	if hasAvatar != 0 {
		a.AvatarURL = adventureAvatarURL(a.Slug)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes
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
		)
		if err := rows.Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &acceptJS,
			&p.Lat, &p.Lng, &radius, &disabled, &p.Notes); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
		p.PromptHTML = mdx.Render(p.Prompt)
		_ = json.Unmarshal([]byte(acceptJS), &p.Accept)
		if p.Accept == nil {
			p.Accept = []adventure.AcceptEntry{}
		}
		if radius.Valid {
			v := radius.Float64
			p.RadiusM = &v
			p.Gated = v > 0
		}
		p.Images = []imageView{}
		a.Pages = append(a.Pages, p)
	}
	for i := range a.Pages {
		imgs, err := loadPageImages(ctx, db, a.Pages[i].ID)
		if err != nil {
			return nil, err
		}
		a.Pages[i].Images = imgs
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


