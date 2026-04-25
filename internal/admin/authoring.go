package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"git.sr.ht/~klahr/zv/internal/mdx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Adventure authoring (admin-only) ---------------- */
//
// These endpoints let the global admin create and edit adventures that
// live entirely in the database. Image uploads accept multipart/form-data
// and store the blob alongside the page. Everything is admin-only — the
// org-side endpoints read adventures through the adventure.Library.

// maxImageBytes is the per-upload size cap. Larger uploads are rejected.
const maxImageBytes = 5 * 1024 * 1024

type pageView struct {
	ID     int64  `json:"id"`
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	// PromptHTML is the goldmark-rendered sibling of Prompt — the same
	// HTML players see. Lets list views in arrange.html drop in the
	// pre-rendered result instead of calling /markdown/preview per card.
	PromptHTML string      `json:"prompt_html,omitempty"`
	Hints      []hintView  `json:"hints"`
	Accept     []string    `json:"accept"`
	Lat        float64     `json:"lat"`
	Lng        float64     `json:"lng"`
	RadiusM    *float64    `json:"radius_m,omitempty"`
	Gated      bool        `json:"gated"`
	Disabled   bool        `json:"disabled"`
	Notes      string      `json:"notes"`
	Images     []imageView `json:"images"`
}

// hintView is a single hint row on the editor side. DelaySeconds is
// how many seconds after the team reaches this puzzle the hint
// becomes revealable; 0 means available immediately.
type hintView struct {
	Text         string `json:"text"`
	DelaySeconds int    `json:"delay_seconds"`
}

type imageView struct {
	ID        int64  `json:"id"`
	Index     int    `json:"index"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	URL       string `json:"url"`
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
	Published            bool        `json:"published"`
	Shares               []shareView `json:"shares"`
	Pages                []pageView  `json:"pages"`
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

// requireEditPage is the page-level analogue of requireEditAdventure.
// Ownership comes from the attached adventure when there is one;
// orphan library pages fall back to the page's own org_id column.
func (h *Handlers) requireEditPage(w http.ResponseWriter, r *http.Request, pageID int64) bool {
	var (
		advID   sql.NullInt64
		pageOrg sql.NullInt64
		advOrg  sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT p.adventure_id, p.org_id, a.org_id
		FROM adventure_pages p
		LEFT JOIN adventures a ON a.id = p.adventure_id
		WHERE p.id = $1
	`, pageID).Scan(&advID, &pageOrg, &advOrg)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "page not found")
		return false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return false
	}
	owner := pageOrg
	if advID.Valid {
		owner = advOrg
	}
	me, _ := auth.FromContext(r.Context())
	if !canEditOwned(me, owner) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
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
	if req.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, strings.TrimSpace(*req.Title))
	}
	if req.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *req.Description)
	}
	if req.Author != nil {
		sets = append(sets, "author = ?")
		args = append(args, *req.Author)
	}
	if req.IntroText != nil {
		sets = append(sets, "intro_text = ?")
		args = append(args, *req.IntroText)
	}
	if req.OutroText != nil {
		sets = append(sets, "outro_text = ?")
		args = append(args, *req.OutroText)
	}
	if req.Slug != nil {
		newSlug := strings.TrimSpace(*req.Slug)
		if !validSlug(newSlug) {
			httpx.Error(w, http.StatusBadRequest, "slug may only contain a-z 0-9 and hyphens")
			return
		}
		sets = append(sets, "slug = ?")
		args = append(args, newSlug)
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
		sets = append(sets, p.col+" = ?")
		args = append(args, nullIfEmpty(*p.val))
	}
	if req.Published != nil {
		sets = append(sets, "published = ?")
		if *req.Published {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if len(sets) == 1 {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	args = append(args, slug)
	q := fmt.Sprintf(`UPDATE adventures SET %s WHERE slug = $1`, strings.Join(sets, ", "))
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
		`SELECT COUNT(*) FROM adventure_runs WHERE adventure_slug = $1`, slug).Scan(&count); err != nil {
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

type pageWriteRequest struct {
	Title    string     `json:"title"`
	Prompt   string     `json:"prompt"`
	Hints    []hintView `json:"hints"`
	Notes    string     `json:"notes"`
	Accept   []string   `json:"accept"`
	Lat      float64    `json:"lat"`
	Lng      float64    `json:"lng"`
	RadiusM  *float64   `json:"radius_m,omitempty"`
	Disabled bool       `json:"disabled"`
}

// validate checks only the constraints that are ALWAYS required,
// regardless of whether the puzzle is attached to an adventure yet.
// Prompt and accept are intentionally *not* required — an author may
// save a library stub with nothing but a location and fill in the rest
// later. The run-side code already handles empty-accept gracefully (no
// answer ever matches).
func (req pageWriteRequest) validate() error {
	if req.RadiusM != nil && *req.RadiusM <= 0 {
		return errors.New("radius_m must be > 0")
	}
	for i, h := range req.Hints {
		if h.DelaySeconds < 0 {
			return fmt.Errorf("hint %d: delay_seconds must be >= 0", i+1)
		}
	}
	return nil
}

// replacePageHints clears and rewrites the hints for a single page
// inside the supplied transaction. Separate helper because both
// CreatePage and UpdatePage need the same wipe-and-reinsert semantics —
// partial updates would need stable hint ids, which the authoring UI
// doesn't currently track.
func replacePageHints(ctx context.Context, tx *sql.Tx, pageID int64, hints []hintView) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM adventure_page_hints WHERE page_id = $1`, pageID); err != nil {
		return err
	}
	for i, h := range hints {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
			VALUES ($1, $2, $3, $4)
		`, pageID, i, h.Text, h.DelaySeconds); err != nil {
			return err
		}
	}
	return nil
}

// CreatePage appends a new page at the end of the adventure.
func (h *Handlers) CreatePage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	var req pageWriteRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_pages WHERE adventure_id = $1`, advID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	accept, _ := json.Marshal(req.Accept)
	var radius any
	if req.RadiusM != nil {
		radius = *req.RadiusM
	}
	disabled := 0
	if req.Disabled {
		disabled = 1
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, advID, nextIdx, req.Title, req.Prompt, string(accept), req.Lat, req.Lng, radius, disabled, req.Notes).Scan(&id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	if err := replacePageHints(r.Context(), tx, id, req.Hints); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hints insert failed: "+err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "index": nextIdx})
}

// UpdatePage replaces the fields of one page (images handled separately).
func (h *Handlers) UpdatePage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	var req pageWriteRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	accept, _ := json.Marshal(req.Accept)
	var radius any
	if req.RadiusM != nil {
		radius = *req.RadiusM
	}
	disabled := 0
	if req.Disabled {
		disabled = 1
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `
		UPDATE adventure_pages
		SET title = $1, prompt = $2, accept = $3, lat = $4, lng = $5, radius_m = $6, disabled = $7, notes = $8
		WHERE id = $9
	`, req.Title, req.Prompt, string(accept), req.Lat, req.Lng, radius, disabled, req.Notes, pageID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "page not found")
		return
	}
	if err := replacePageHints(r.Context(), tx, pageID, req.Hints); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hints update failed: "+err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeletePage removes a page. Subsequent pages keep their ids but the
// server compacts idx values so there are no gaps.
func (h *Handlers) DeletePage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	var (
		advID sql.NullInt64
		idx   int
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_id, idx FROM adventure_pages WHERE id = $1`, pageID).Scan(&advID, &idx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "page not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM adventure_pages WHERE id = $1`, pageID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	// Library puzzles don't participate in adventure idx compaction —
	// their idx is ignored entirely (UNIQUE(adventure_id, idx) treats
	// each NULL as distinct).
	if advID.Valid {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE adventure_pages SET idx = idx - 1 WHERE adventure_id = $1 AND idx > $2`,
			advID.Int64, idx); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "renumber failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ReorderPages takes a list of page ids in the desired new order.
func (h *Handlers) ReorderPages(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	var req struct {
		PageIDs []int64 `json:"page_ids"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()
	// Park every row at a guaranteed-unique idx well above the reassign
	// range so the second pass can write 0..N-1 without transient
	// UNIQUE(adventure_id, idx) collisions. id is the row's surrogate
	// key (unique per row), so the new values can never clash with
	// each other; the 10^9 offset keeps them disjoint from both the
	// target range and any sane existing idx (including the negative
	// values a previously crashed reorder could have left stuck — the
	// old scheme `-idx - 1` would flip those back into the positive
	// range and re-collide).
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE adventure_pages SET idx = 1000000000 + id WHERE adventure_id = $1`, advID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "prepare failed: "+err.Error())
		return
	}
	for i, id := range req.PageIDs {
		res, err := tx.ExecContext(r.Context(),
			`UPDATE adventure_pages SET idx = $1 WHERE id = $2 AND adventure_id = $3`,
			i, id, advID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "reorder failed: "+err.Error())
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.Error(w, http.StatusBadRequest, "unknown page id in list")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// AddPageImage reads the multipart "file" form field and stores the blob.
func (h *Handlers) AddPageImage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "image/") {
		httpx.Error(w, http.StatusBadRequest, "only image uploads are accepted")
		return
	}
	// Cap both dimensions to 512 px before storing — the player client
	// renders images at phone-width so there's no point storing camera
	// originals; this also keeps the SQLite row small.
	shrunk, newMIME, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = shrunk, newMIME

	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_page_images WHERE page_id = $1`, pageID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, pageID, nextIdx, mime, int64(len(data)), data).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, imageView{
		ID: id, Index: nextIdx, MimeType: mime, SizeBytes: int64(len(data)),
		URL: fmt.Sprintf("/adventure-assets/image/%d", id),
	})
}

// DeletePageImage drops one image.
func (h *Handlers) DeletePageImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad image id")
		return
	}
	// Resolve the owning page so we can run the standard policy check.
	var pageID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT page_id FROM adventure_page_images WHERE id = $1`, imageID).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_page_images WHERE id = $1`, imageID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "image not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Image serving ---------------- */

// ServeImage writes the binary image to the response. Admins may fetch
// any image; slots may fetch images for their current page only.
func (h *Handlers) ServeImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		http.Error(w, "bad image id", http.StatusBadRequest)
		return
	}
	var (
		pageID   int64
		mime     string
		size     int64
		content  []byte
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT page_id, mime_type, size_bytes, content
		FROM adventure_page_images WHERE id = $1
	`, imageID).Scan(&pageID, &mime, &size, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if !h.canViewPageImage(r, pageID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(content)
}

// canViewPageImage allows any staff user and players whose team is
// currently on the page owning the image.
func (h *Handlers) canViewPageImage(r *http.Request, pageID int64) bool {
	if u, ok := auth.FromContext(r.Context()); ok {
		_ = u
		return true
	}
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		return false
	}
	var (
		advID int64
		idx   int
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_id, idx FROM adventure_pages WHERE id = $1`, pageID).Scan(&advID, &idx); err != nil {
		return false
	}
	// Match up: slot's team is on a run whose adventure_slug → advID, and
	// team.current_page_index == idx.
	var onPage int
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM teams t
		JOIN adventure_runs ar ON ar.id = t.run_id
		JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.id = $1 AND a.id = $2 AND t.current_page_index = $3
	`, slot.TeamID, advID, idx).Scan(&onPage)
	if err != nil {
		return false
	}
	return onPage > 0
}

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
	)
	if err := db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color,
		       (avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       published
		FROM adventures WHERE slug = $1
	`, slug).Scan(&a.ID, &a.Slug, &a.Title, &a.Description, &a.Author,
		&a.IntroText, &a.OutroText,
		&accent, &accent2, &bg, &ink, &hasAvatar, &published); err != nil {
		return nil, err
	}
	a.AccentColor = accent.String
	a.AccentSecondaryColor = accent2.String
	a.BackgroundColor = bg.String
	a.TextColor = ink.String
	a.Published = published != 0
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
			p.Accept = []string{}
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
	return &a, nil
}

// loadAdventureShares returns which orgs can see this adventure via a
// commissioned share, ordered by name for stable UI rendering.
func loadAdventureShares(ctx context.Context, db *sql.DB, slug string) ([]shareView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.org_id, o.name, s.granted_at
		FROM adventure_shares s
		JOIN organizations o ON o.id = s.org_id
		WHERE s.adventure_slug = $1
		ORDER BY LOWER(o.name)
	`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []shareView{}
	for rows.Next() {
		var v shareView
		if err := rows.Scan(&v.OrgID, &v.OrgName, &v.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func loadPageImages(ctx context.Context, db *sql.DB, pageID int64) ([]imageView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes FROM adventure_page_images
		WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []imageView{}
	for rows.Next() {
		var im imageView
		if err := rows.Scan(&im.ID, &im.Index, &im.MimeType, &im.SizeBytes); err != nil {
			return nil, err
		}
		im.URL = fmt.Sprintf("/adventure-assets/image/%d", im.ID)
		out = append(out, im)
	}
	return out, nil
}

// loadPageHints returns the author's hint list for a page in display
// order. Empty slice when the page has no hints, so clients can
// JSON-encode as [] rather than null and render a safe default.
func loadPageHints(ctx context.Context, db *sql.DB, pageID int64) ([]hintView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT text, delay_seconds FROM adventure_page_hints
		WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hintView{}
	for rows.Next() {
		var hv hintView
		if err := rows.Scan(&hv.Text, &hv.DelaySeconds); err != nil {
			return nil, err
		}
		out = append(out, hv)
	}
	return out, nil
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

func pageIDFromURL(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "pageID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

/* ---------------- Adventure avatar ---------------- */

// adventureAvatarURL is the canonical serve path. Keyed by slug so
// callers in DTOs don't need the numeric id.
func adventureAvatarURL(slug string) string {
	return "/adventure-assets/avatar/" + url.PathEscape(slug)
}

// SetAdventureAvatar stores the uploaded cover picture inline on the
// adventures row. Same contract as SetTeamAvatar — multipart `file`
// field, 5 MB cap, image/* only. Authorised via requireEditAdventure.
func (h *Handlers) SetAdventureAvatar(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "image/") {
		httpx.Error(w, http.StatusBadRequest, "only image uploads are accepted")
		return
	}
	shrunk, newMIME, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = shrunk, newMIME
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventures
		   SET avatar_mime_type = $1, avatar_size_bytes = $2, avatar_content = $3
		 WHERE slug = $4
	`, mime, int64(len(data)), data, slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "store failed: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "adventure not found")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  adventureAvatarURL(slug),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteAdventureAvatar clears the cover picture so the client falls
// back to the initials-on-accent tile.
func (h *Handlers) DeleteAdventureAvatar(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventures
		   SET avatar_mime_type = NULL, avatar_size_bytes = NULL, avatar_content = NULL
		 WHERE slug = $1
	`, slug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeAdventureAvatar writes the stored blob. No auth: cover art is
// safe public read, same reasoning as team avatars — the slug is the
// identifier that's already in the URL.
func (h *Handlers) ServeAdventureAvatar(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT avatar_mime_type, avatar_size_bytes, avatar_content
		FROM adventures WHERE slug = $1
	`, slug).Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime.String)
	w.Header().Set("Content-Length", strconv.FormatInt(size.Int64, 10))
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(content)
}

/* ---------------- Adventure shares (commission an adventure for a specific org) ---------------- */

// ListAdventureShares returns the orgs that currently have a share for
// this adventure. Admin-only; the UI uses it to populate the manager
// section on the library edit page.
func (h *Handlers) ListAdventureShares(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	shares, err := loadAdventureShares(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "load failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"shares": shares})
}

type grantShareRequest struct {
	OrgID int64 `json:"org_id"`
}

// GrantAdventureShare adds (or keeps) a share row for the given org.
// Idempotent: re-granting to the same org is a no-op, so the UI can
// repost without checking first.
func (h *Handlers) GrantAdventureShare(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	var req grantShareRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.OrgID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "org_id required")
		return
	}
	var orgExists int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM organizations WHERE id = $1`, req.OrgID).Scan(&orgExists); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if orgExists == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_shares(adventure_slug, org_id, granted_by_user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT(adventure_slug, org_id) DO NOTHING
	`, slug, req.OrgID, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "grant failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RevokeAdventureShare removes the share for a given org. Runs already
// created from the share are unaffected — revoking only stops new runs.
func (h *Handlers) RevokeAdventureShare(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	orgID, err := strconv.ParseInt(chi.URLParam(r, "orgID"), 10, 64)
	if err != nil || orgID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_shares WHERE adventure_slug = $1 AND org_id = $2`,
		slug, orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}
