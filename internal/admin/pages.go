package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Pages (CRUD + reorder) ---------------- */

// requireEditPage runs the standard ownership check on the page's
// adventure (or, for library puzzles, the page's own org_id).
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
	if !canAuthor(me, owner) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type pageWriteRequest struct {
	Title    string                  `json:"title"`
	Prompt   string                  `json:"prompt"`
	Hints    []hintView              `json:"hints"`
	Notes    string                  `json:"notes"`
	Accept   []adventure.AcceptEntry `json:"accept"`
	Lat      float64                 `json:"lat"`
	Lng      float64                 `json:"lng"`
	RadiusM  *float64                `json:"radius_m,omitempty"`
	Disabled bool                    `json:"disabled"`
}

// validate checks only the constraints that are ALWAYS required,
// regardless of whether the puzzle is attached to an adventure yet.
// Prompt and accept are intentionally *not* required — an author may
// save a library stub with nothing but a location and fill in the rest
// later. The event-side code already handles empty-accept gracefully (no
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
	if err := adventure.ValidateAcceptEntries(req.Accept); err != nil {
		return err
	}
	return nil
}

// upsertPageHints diffs the incoming hint list against the page's
// existing rows and applies the minimum DML to converge: known IDs
// get an UPDATE, new rows (no ID supplied) get an INSERT, and any
// stored ID not in the incoming list is DELETEd — cascading its
// attached adventure_page_hint_images rows along with it.
//
// Stable hint IDs are the whole reason for this dance: a wipe-and-
// reinsert would orphan every image on every save, which makes
// per-hint image attachments useless. The server is the only place
// that mints hint IDs; the authoring UI echoes them back unchanged.
//
// The two-pass idx parking (1 000 000 000 + id, then 0..N-1) avoids
// transient UNIQUE(page_id, idx) collisions when an author reorders
// rows that already exist — the same trick the page-reorder
// endpoint uses for the same reason.
func upsertPageHints(ctx context.Context, tx *sql.Tx, pageID int64, hints []hintView) error {
	existing := map[int64]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM adventure_page_hints WHERE page_id = $1`, pageID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// Park every existing row well above the reassign range so the
	// pass below can write 0..N-1 without colliding. id is unique per
	// row, so the parked values are guaranteed disjoint from each
	// other and from any sane idx (including the target range).
	if _, err := tx.ExecContext(ctx,
		`UPDATE adventure_page_hints SET idx = 1000000000 + id WHERE page_id = $1`, pageID); err != nil {
		return err
	}

	keep := map[int64]bool{}
	for i, h := range hints {
		if h.ID != nil && existing[*h.ID] {
			if _, err := tx.ExecContext(ctx, `
				UPDATE adventure_page_hints
				SET idx = $1, text = $2, delay_seconds = $3
				WHERE id = $4 AND page_id = $5
			`, i, h.Text, h.DelaySeconds, *h.ID, pageID); err != nil {
				return err
			}
			keep[*h.ID] = true
			continue
		}
		// Either no ID was supplied (new hint) or the ID came from a
		// stale tab editing a hint we've since removed. Either way:
		// insert a fresh row. We deliberately don't error on a stale
		// ID — the author's text + delay are still valid content; the
		// only consequence is that any attempt to PATCH /hint-images/
		// against the dropped ID has already 404'd.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
			VALUES ($1, $2, $3, $4)
		`, pageID, i, h.Text, h.DelaySeconds); err != nil {
			return err
		}
	}
	// Drop any existing row the author removed from the editor list.
	// CASCADE on adventure_page_hint_images.hint_id sweeps the blobs.
	for id := range existing {
		if !keep[id] {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM adventure_page_hints WHERE id = $1 AND page_id = $2`, id, pageID); err != nil {
				return err
			}
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
	if err := upsertPageHints(r.Context(), tx, id, req.Hints); err != nil {
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
	if err := upsertPageHints(r.Context(), tx, pageID, req.Hints); err != nil {
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

// loadPageHints returns the author's hint list for a page in display
// order. Empty slice when the page has no hints, so clients can
// JSON-encode as [] rather than null and render a safe default.
//
// Each hint carries its ID (so the editor can echo it back on save)
// and its attached images (so the editor's per-hint image strip
// renders without an extra round-trip).
func loadPageHints(ctx context.Context, db *sql.DB, pageID int64) ([]hintView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, text, delay_seconds FROM adventure_page_hints
		WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hintView{}
	for rows.Next() {
		var (
			id int64
			hv hintView
		)
		if err := rows.Scan(&id, &hv.Text, &hv.DelaySeconds); err != nil {
			return nil, err
		}
		hv.ID = &id
		out = append(out, hv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		imgs, err := loadHintImages(ctx, db, *out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Images = imgs
		audio, err := loadHintAudio(ctx, db, *out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Audio = audio
	}
	return out, nil
}

func pageIDFromURL(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "pageID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
