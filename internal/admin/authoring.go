package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
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
	ID       int64       `json:"id"`
	Index    int         `json:"index"`
	Title    string      `json:"title"`
	Prompt   string      `json:"prompt"`
	Hint     string      `json:"hint,omitempty"`
	Accept   []string    `json:"accept"`
	Lat      float64     `json:"lat"`
	Lng      float64     `json:"lng"`
	RadiusM  *float64    `json:"radius_m,omitempty"`
	Gated    bool        `json:"gated"`
	Disabled bool        `json:"disabled"`
	Notes    string      `json:"notes"`
	Images   []imageView `json:"images"`
}

type imageView struct {
	ID        int64  `json:"id"`
	Index     int    `json:"index"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	URL       string `json:"url"`
}

type adventureEditDTO struct {
	ID                   int64      `json:"id"`
	Slug                 string     `json:"slug"`
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	Author               string     `json:"author"`
	AccentColor          string     `json:"accent_color"`
	AccentSecondaryColor string     `json:"accent_secondary_color"`
	BackgroundColor      string     `json:"background_color"`
	TextColor            string     `json:"text_color"`
	Pages                []pageView `json:"pages"`
}

// LoadAdventureForEdit returns full adventure content (including accept
// lists and locations) for the admin editor.
func (h *Handlers) LoadAdventureForEdit(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
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
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventures(slug, title, description, author,
		                       accent_color, accent_secondary_color, background_color, text_color)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, req.Slug, req.Title, req.Description, req.Author,
		nullIfEmpty(req.AccentColor), nullIfEmpty(req.AccentSecondaryColor),
		nullIfEmpty(req.BackgroundColor), nullIfEmpty(req.TextColor))
	if err != nil {
		httpx.Error(w, http.StatusConflict, "slug already used")
		return
	}
	id, _ := res.LastInsertId()
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id": id, "slug": req.Slug, "title": req.Title,
	})
}

type updateAdventureRequest struct {
	Title                *string `json:"title,omitempty"`
	Description          *string `json:"description,omitempty"`
	Author               *string `json:"author,omitempty"`
	Slug                 *string `json:"slug,omitempty"`
	AccentColor          *string `json:"accent_color,omitempty"`
	AccentSecondaryColor *string `json:"accent_secondary_color,omitempty"`
	BackgroundColor      *string `json:"background_color,omitempty"`
	TextColor            *string `json:"text_color,omitempty"`
}

// UpdateAdventure amends the metadata.
func (h *Handlers) UpdateAdventure(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var req updateAdventureRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	sets := []string{"updated_at = datetime('now')"}
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
	if len(sets) == 1 {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	args = append(args, slug)
	q := fmt.Sprintf(`UPDATE adventures SET %s WHERE slug = ?`, strings.Join(sets, ", "))
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
	var count int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM adventure_runs WHERE adventure_slug = ?`, slug).Scan(&count); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "runs lookup failed")
		return
	}
	if count > 0 {
		httpx.Error(w, http.StatusConflict, "adventure still has runs; delete them first")
		return
	}
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM adventure_assignments WHERE adventure_slug = ?`, slug).Scan(&count); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assignments lookup failed")
		return
	}
	if count > 0 {
		httpx.Error(w, http.StatusConflict, "adventure still has assignments; revoke them first")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `DELETE FROM adventures WHERE slug = ?`, slug)
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
	Title    string   `json:"title"`
	Prompt   string   `json:"prompt"`
	Hint     string   `json:"hint"`
	Notes    string   `json:"notes"`
	Accept   []string `json:"accept"`
	Lat      float64  `json:"lat"`
	Lng      float64  `json:"lng"`
	RadiusM  *float64 `json:"radius_m,omitempty"`
	Disabled bool     `json:"disabled"`
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
	return nil
}

// CreatePage appends a new page at the end of the adventure.
func (h *Handlers) CreatePage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
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
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_pages WHERE adventure_id = ?`, advID).
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
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, hint, accept, lat, lng, radius_m, disabled, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, advID, nextIdx, req.Title, req.Prompt, req.Hint, string(accept), req.Lat, req.Lng, radius, disabled, req.Notes)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "index": nextIdx})
}

// UpdatePage replaces the fields of one page (images handled separately).
func (h *Handlers) UpdatePage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
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
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventure_pages
		SET title = ?, prompt = ?, hint = ?, accept = ?, lat = ?, lng = ?, radius_m = ?, disabled = ?, notes = ?
		WHERE id = ?
	`, req.Title, req.Prompt, req.Hint, string(accept), req.Lat, req.Lng, radius, disabled, req.Notes, pageID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "page not found")
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
	var (
		advID sql.NullInt64
		idx   int
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_id, idx FROM adventure_pages WHERE id = ?`, pageID).Scan(&advID, &idx); err != nil {
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
		`DELETE FROM adventure_pages WHERE id = ?`, pageID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	// Library puzzles don't participate in adventure idx compaction —
	// their idx is ignored entirely (UNIQUE(adventure_id, idx) treats
	// each NULL as distinct).
	if advID.Valid {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE adventure_pages SET idx = idx - 1 WHERE adventure_id = ? AND idx > ?`,
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
	// Bump all idx values out of the unique-constrained space first so we
	// can reassign without transient collisions.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE adventure_pages SET idx = -idx - 1 WHERE adventure_id = ?`, advID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "prepare failed")
		return
	}
	for i, id := range req.PageIDs {
		res, err := tx.ExecContext(r.Context(),
			`UPDATE adventure_pages SET idx = ? WHERE id = ? AND adventure_id = ?`,
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

	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_page_images WHERE page_id = ?`, pageID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content)
		VALUES (?, ?, ?, ?, ?)
	`, pageID, nextIdx, mime, int64(len(data)), data)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
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
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_page_images WHERE id = ?`, imageID)
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
		FROM adventure_page_images WHERE id = ?
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
		`SELECT adventure_id, idx FROM adventure_pages WHERE id = ?`, pageID).Scan(&advID, &idx); err != nil {
		return false
	}
	// Match up: slot's team is on a run whose adventure_slug → advID, and
	// team.current_page_index == idx.
	var onPage int
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM teams t
		JOIN adventure_runs ar ON ar.id = t.run_id
		JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.id = ? AND a.id = ? AND t.current_page_index = ?
	`, slot.TeamID, advID, idx).Scan(&onPage)
	if err != nil {
		return false
	}
	return onPage > 0
}

/* ---------------- helpers ---------------- */

func adventureIDBySlug(ctx context.Context, db *sql.DB, slug string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = ?`, slug).Scan(&id)
	return id, err
}

func loadAdventureDTO(ctx context.Context, db *sql.DB, slug string) (*adventureEditDTO, error) {
	a := adventureEditDTO{Pages: []pageView{}}
	var accent, accent2, bg, ink sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, author,
		       accent_color, accent_secondary_color, background_color, text_color
		FROM adventures WHERE slug = ?
	`, slug).Scan(&a.ID, &a.Slug, &a.Title, &a.Description, &a.Author,
		&accent, &accent2, &bg, &ink); err != nil {
		return nil, err
	}
	a.AccentColor = accent.String
	a.AccentSecondaryColor = accent2.String
	a.BackgroundColor = bg.String
	a.TextColor = ink.String
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, title, prompt, hint, accept, lat, lng, radius_m, disabled, notes
		FROM adventure_pages WHERE adventure_id = ? ORDER BY idx
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
		if err := rows.Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &p.Hint, &acceptJS,
			&p.Lat, &p.Lng, &radius, &disabled, &p.Notes); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
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
	}
	return &a, nil
}

func loadPageImages(ctx context.Context, db *sql.DB, pageID int64) ([]imageView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes FROM adventure_page_images
		WHERE page_id = ? ORDER BY idx
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
