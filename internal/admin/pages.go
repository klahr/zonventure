package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
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
	Title   string                  `json:"title"`
	Prompt  string                  `json:"prompt"`
	Hints   []hintView              `json:"hints"`
	Notes   string                  `json:"notes"`
	Accept  []adventure.AcceptEntry `json:"accept"`
	Lat     float64                 `json:"lat"`
	Lng     float64                 `json:"lng"`
	RadiusM *float64                `json:"radius_m,omitempty"`
	Disabled bool                   `json:"disabled"`
	// LaneID, when set on CreatePage, places the new page in that
	// specific lane (used by the per-lane "Add puzzle" button in
	// the editor). Zero or omitted falls back to the adventure's
	// default lane (idx=0). Ignored on UpdatePage — moving a page
	// between lanes goes through the reorder endpoint so the new
	// idx and lane assignment land in one transaction.
	LaneID int64 `json:"lane_id,omitempty"`
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
	if _, err := validateBlurb(req.Title, MaxNameLen); err != nil {
		return fmt.Errorf("title %s", err.Error())
	}
	if _, err := validateBlurb(req.Prompt, MaxPromptLen); err != nil {
		return fmt.Errorf("prompt %s", err.Error())
	}
	if _, err := validateBlurb(req.Notes, MaxBlurbLen); err != nil {
		return fmt.Errorf("notes %s", err.Error())
	}
	for i, h := range req.Hints {
		if h.DelaySeconds < 0 {
			return fmt.Errorf("hint %d: delay_seconds must be >= 0", i+1)
		}
		if _, err := validateBlurb(h.Text, MaxHintTextLen); err != nil {
			return fmt.Errorf("hint %d text %s", i+1, err.Error())
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

// resolveCreateLane returns the lane id a freshly created page should
// land in. Explicit non-zero requestedLaneID is validated against the
// adventure (must belong to it). Zero / omitted requestedLaneID falls
// back to the idx=0 lane, creating it lazily if the adventure has no
// lanes yet — keeps the legacy "just call CreatePage with no lane
// awareness" client path working.
func resolveCreateLane(ctx context.Context, db *sql.DB, advID, requestedLaneID int64) (int64, error) {
	if requestedLaneID > 0 {
		var owner int64
		if err := db.QueryRowContext(ctx,
			`SELECT adventure_id FROM adventure_lanes WHERE id = $1`, requestedLaneID).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, errors.New("unknown lane id")
			}
			return 0, err
		}
		if owner != advID {
			return 0, errors.New("lane belongs to another adventure")
		}
		return requestedLaneID, nil
	}
	var laneID int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM adventure_lanes WHERE adventure_id = $1 AND idx = 0`, advID).Scan(&laneID)
	if err == nil {
		return laneID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO adventure_lanes (adventure_id, idx, mode)
		VALUES ($1, 0, 'linear')
		RETURNING id
	`, advID).Scan(&laneID); err != nil {
		return 0, err
	}
	return laneID, nil
}

// AddPageFromExport inserts a single puzzle into the named adventure
// from an exportPage-shaped JSON blob (the same shape ImportEnvelope
// nests under `pages[]`). Used by the MCP add_puzzle tool so the
// LLM can grow an adventure incrementally instead of replacing it
// wholesale on every edit.
//
// Lane resolution is by idx, not id (the LLM only sees the export
// shape, which never mentions DB ids): a non-negative LaneIdx is
// matched against the adventure's own lanes; missing matches fall
// back to the idx=0 lane, lazily created if absent. The page's
// own Idx field is ignored — new pages always append. Image / audio
// references on the page are rejected (assets need to be attached
// via the arranger UI).
//
// Caller is responsible for authorizing the slug first
// (AuthorizeAdventureEdit).
func AddPageFromExport(ctx context.Context, db *sql.DB, advID int64, pageJSON []byte) (int64, error) {
	dec := json.NewDecoder(bytes.NewReader(pageJSON))
	dec.DisallowUnknownFields()
	var p exportPage
	if err := dec.Decode(&p); err != nil {
		return 0, fmt.Errorf("page parse: %w", err)
	}
	if len(p.Images) > 0 || len(p.Audio) > 0 {
		return 0, errors.New("page assets unsupported via this entry — attach via arranger UI")
	}
	for i, hint := range p.Hints {
		if len(hint.Images) > 0 || len(hint.Audio) > 0 {
			return 0, fmt.Errorf("hint %d assets unsupported via this entry", i+1)
		}
	}
	// Title / prompt / hints reuse the same validators CreatePage
	// runs so the model gets identical error messages whether it
	// authors via add_puzzle or via the create_adventure full-blob
	// path.
	if _, err := validateBlurb(p.Title, MaxNameLen); err != nil {
		return 0, fmt.Errorf("title %s", err.Error())
	}
	if _, err := validateBlurb(p.Prompt, MaxPromptLen); err != nil {
		return 0, fmt.Errorf("prompt %s", err.Error())
	}
	if _, err := validateBlurb(p.Notes, MaxBlurbLen); err != nil {
		return 0, fmt.Errorf("notes %s", err.Error())
	}
	if p.RadiusM != nil && *p.RadiusM <= 0 {
		return 0, errors.New("radius_m must be > 0")
	}
	for i, hint := range p.Hints {
		if hint.DelaySeconds < 0 {
			return 0, fmt.Errorf("hint %d: delay_seconds must be >= 0", i+1)
		}
		if _, err := validateBlurb(hint.Text, MaxHintTextLen); err != nil {
			return 0, fmt.Errorf("hint %d text %s", i+1, err.Error())
		}
	}
	if err := adventure.ValidateAcceptEntries(p.Accept); err != nil {
		return 0, err
	}

	// Resolve lane by idx, falling back to (and lazily creating)
	// idx=0 — same lenient defaulting resolveCreateLane offers when
	// requestedLaneID is zero.
	var laneID int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM adventure_lanes WHERE adventure_id = $1 AND idx = $2`,
		advID, p.LaneIdx).Scan(&laneID)
	if errors.Is(err, sql.ErrNoRows) {
		// Unknown idx: fall back to idx=0 (creating it if needed).
		laneID, err = resolveCreateLane(ctx, db, advID, 0)
		if err != nil {
			return 0, fmt.Errorf("resolve lane: %w", err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("resolve lane: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	var nextIdx int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_pages WHERE adventure_id = $1`,
		advID).Scan(&nextIdx); err != nil {
		return 0, fmt.Errorf("next idx: %w", err)
	}
	acceptJ, _ := json.Marshal(p.Accept)
	var radius any
	if p.RadiusM != nil {
		radius = *p.RadiusM
	}
	disabled := 0
	if p.Disabled {
		disabled = 1
	}
	var pageID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, lane_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, advID, nextIdx, p.Title, p.Prompt, string(acceptJ), p.Lat, p.Lng, radius, disabled, p.Notes, laneID).Scan(&pageID); err != nil {
		return 0, fmt.Errorf("insert page: %w", err)
	}
	for i, hint := range p.Hints {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
			VALUES ($1, $2, $3, $4)
		`, pageID, i, hint.Text, hint.DelaySeconds); err != nil {
			return 0, fmt.Errorf("insert hint %d: %w", i+1, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return pageID, nil
}

// UpdatePageFromExport replaces the content of the page at
// (advID, pageIdx) with the fields from a per-page export object —
// same shape AddPageFromExport accepts. Used by the MCP
// update_puzzle tool so an LLM can rewrite an existing puzzle (e.g.
// translate prompts) without going through the multipart UI.
//
// Validation runs the same gauntlet as AddPageFromExport so the
// model gets identical error messages whether it's authoring or
// editing. Image / audio fields on the input are rejected — assets
// are managed by attach_image, never by puzzle content edits.
//
// lane_idx in the input is honoured when it points at an existing
// lane on the adventure; an unknown idx is rejected (no silent
// lane-zero fallback on update — silent re-laning is more
// surprising on edit than on create).
//
// Hints are replace-all: existing rows for the page are deleted,
// the input slice is inserted from idx 0. Same shape the UI's
// PUT handler uses via upsertPageHints.
//
// Caller is responsible for authorizing the slug first
// (AuthorizeAdventureEdit).
func UpdatePageFromExport(ctx context.Context, db *sql.DB, advID int64, pageIdx int, pageJSON []byte) error {
	dec := json.NewDecoder(bytes.NewReader(pageJSON))
	dec.DisallowUnknownFields()
	var p exportPage
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("page parse: %w", err)
	}
	if len(p.Images) > 0 || len(p.Audio) > 0 {
		return errors.New("page assets unsupported via this entry — attach via attach_image")
	}
	for i, hint := range p.Hints {
		if len(hint.Images) > 0 || len(hint.Audio) > 0 {
			return fmt.Errorf("hint %d assets unsupported via this entry", i+1)
		}
	}
	if _, err := validateBlurb(p.Title, MaxNameLen); err != nil {
		return fmt.Errorf("title %s", err.Error())
	}
	if _, err := validateBlurb(p.Prompt, MaxPromptLen); err != nil {
		return fmt.Errorf("prompt %s", err.Error())
	}
	if _, err := validateBlurb(p.Notes, MaxBlurbLen); err != nil {
		return fmt.Errorf("notes %s", err.Error())
	}
	if p.RadiusM != nil && *p.RadiusM <= 0 {
		return errors.New("radius_m must be > 0")
	}
	for i, hint := range p.Hints {
		if hint.DelaySeconds < 0 {
			return fmt.Errorf("hint %d: delay_seconds must be >= 0", i+1)
		}
		if _, err := validateBlurb(hint.Text, MaxHintTextLen); err != nil {
			return fmt.Errorf("hint %d text %s", i+1, err.Error())
		}
	}
	if err := adventure.ValidateAcceptEntries(p.Accept); err != nil {
		return err
	}

	var pageID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM adventure_pages WHERE adventure_id = $1 AND idx = $2`,
		advID, pageIdx).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no puzzle at idx %d in this adventure", pageIdx)
		}
		return fmt.Errorf("page lookup: %w", err)
	}
	var laneID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM adventure_lanes WHERE adventure_id = $1 AND idx = $2`,
		advID, p.LaneIdx).Scan(&laneID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no lane at idx %d in this adventure", p.LaneIdx)
		}
		return fmt.Errorf("resolve lane: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	acceptJ, _ := json.Marshal(p.Accept)
	var radius any
	if p.RadiusM != nil {
		radius = *p.RadiusM
	}
	disabled := 0
	if p.Disabled {
		disabled = 1
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE adventure_pages
		SET title = $1, prompt = $2, accept = $3, lat = $4, lng = $5,
		    radius_m = $6, disabled = $7, notes = $8, lane_id = $9
		WHERE id = $10
	`, p.Title, p.Prompt, string(acceptJ), p.Lat, p.Lng,
		radius, disabled, p.Notes, laneID, pageID); err != nil {
		return fmt.Errorf("update page: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM adventure_page_hints WHERE page_id = $1`, pageID); err != nil {
		return fmt.Errorf("clear hints: %w", err)
	}
	for i, hint := range p.Hints {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
			VALUES ($1, $2, $3, $4)
		`, pageID, i, hint.Text, hint.DelaySeconds); err != nil {
			return fmt.Errorf("insert hint %d: %w", i+1, err)
		}
	}
	return tx.Commit()
}

// DeletePageByIdx removes the puzzle at (advID, pageIdx) and
// compacts subsequent idx values so the global ordering stays
// gap-free. Same semantics as the multipart DELETE handler, just
// addressed by export-shape idx instead of internal pageID — used
// by the MCP delete_puzzle tool.
//
// Caller authorises the slug first (AuthorizeAdventureEdit).
func DeletePageByIdx(ctx context.Context, db *sql.DB, advID int64, pageIdx int) error {
	var pageID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM adventure_pages WHERE adventure_id = $1 AND idx = $2`,
		advID, pageIdx).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no puzzle at idx %d in this adventure", pageIdx)
		}
		return fmt.Errorf("page lookup: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM adventure_pages WHERE id = $1`, pageID); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE adventure_pages SET idx = idx - 1 WHERE adventure_id = $1 AND idx > $2`,
		advID, pageIdx); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	return tx.Commit()
}

// MovePage moves the puzzle at fromIdx to toIdx within the
// adventure, optionally reassigning it to a different lane in the
// same transaction. Park-then-renumber under the
// UNIQUE(adventure_id, idx) constraint mirrors ReorderPages — same
// reason, same trick: stage every row at id+10^9 and then write
// 0..N-1 in the desired order without transient collisions.
//
// toIdx is clamped to [0, len(pages)-1] so callers can pass a
// large value to mean "move to end" without needing the count.
// newLaneIdx == nil leaves the lane unchanged; non-nil resolves
// against (advID, lane idx) and rejects an unknown lane.
func MovePage(ctx context.Context, db *sql.DB, advID int64, fromIdx, toIdx int, newLaneIdx *int) error {
	if fromIdx < 0 || toIdx < 0 {
		return errors.New("from_idx and to_idx must be >= 0")
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM adventure_pages WHERE adventure_id = $1 ORDER BY idx`, advID)
	if err != nil {
		return fmt.Errorf("load pages: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}
	if len(ids) == 0 {
		return errors.New("adventure has no puzzles")
	}
	if fromIdx >= len(ids) {
		return fmt.Errorf("from_idx %d is past the last puzzle (idx %d)", fromIdx, len(ids)-1)
	}
	if toIdx >= len(ids) {
		toIdx = len(ids) - 1
	}
	noopMove := fromIdx == toIdx && newLaneIdx == nil
	if noopMove {
		return nil
	}
	moved := ids[fromIdx]
	ids = append(ids[:fromIdx], ids[fromIdx+1:]...)
	ids = append(ids[:toIdx], append([]int64{moved}, ids[toIdx:]...)...)

	var newLaneID int64
	if newLaneIdx != nil {
		if err := db.QueryRowContext(ctx,
			`SELECT id FROM adventure_lanes WHERE adventure_id = $1 AND idx = $2`,
			advID, *newLaneIdx).Scan(&newLaneID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("no lane at idx %d in this adventure", *newLaneIdx)
			}
			return fmt.Errorf("resolve lane: %w", err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE adventure_pages SET idx = 1000000000 + id WHERE adventure_id = $1`, advID); err != nil {
		return fmt.Errorf("park: %w", err)
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE adventure_pages SET idx = $1 WHERE id = $2`, i, id); err != nil {
			return fmt.Errorf("renumber idx %d: %w", i, err)
		}
	}
	if newLaneIdx != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE adventure_pages SET lane_id = $1 WHERE id = $2`, newLaneID, moved); err != nil {
			return fmt.Errorf("relane: %w", err)
		}
	}
	return tx.Commit()
}

// AddHintAtEnd appends one hint to the puzzle at (advID, pageIdx)
// at the next idx, returning the new hint's idx so the caller can
// echo it back. Validation matches CreatePage's hint loop —
// non-negative delay, length-capped text — so error messages stay
// identical between full-puzzle authoring and per-hint additions.
func AddHintAtEnd(ctx context.Context, db *sql.DB, advID int64, pageIdx int, text string, delaySeconds int) (int, error) {
	if delaySeconds < 0 {
		return 0, errors.New("delay_seconds must be >= 0")
	}
	if _, err := validateBlurb(text, MaxHintTextLen); err != nil {
		return 0, fmt.Errorf("text %s", err.Error())
	}
	var pageID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM adventure_pages WHERE adventure_id = $1 AND idx = $2`,
		advID, pageIdx).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("no puzzle at idx %d in this adventure", pageIdx)
		}
		return 0, fmt.Errorf("page lookup: %w", err)
	}
	// Atomic next-idx + INSERT in a single statement so two concurrent
	// adds to the same page can't compute the same idx.
	var newIdx int
	if err := db.QueryRowContext(ctx, `
		INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
		SELECT $1, COALESCE(MAX(idx)+1, 0), $2, $3 FROM adventure_page_hints WHERE page_id = $1
		RETURNING idx
	`, pageID, text, delaySeconds).Scan(&newIdx); err != nil {
		return 0, fmt.Errorf("insert: %w", err)
	}
	return newIdx, nil
}

// UpdateHintByIdx patches the text and/or delay_seconds of a hint
// addressed by (advID, pageIdx, hintIdx). Either field may be nil
// to leave it alone; both nil is a 400-shape error. Used by the
// MCP update_hint tool — addressed by export-shape idx so the
// model doesn't need to know DB rowids.
func UpdateHintByIdx(ctx context.Context, db *sql.DB, advID int64, pageIdx, hintIdx int, text *string, delaySeconds *int) error {
	if text == nil && delaySeconds == nil {
		return errors.New("nothing to update")
	}
	if delaySeconds != nil && *delaySeconds < 0 {
		return errors.New("delay_seconds must be >= 0")
	}
	if text != nil {
		if _, err := validateBlurb(*text, MaxHintTextLen); err != nil {
			return fmt.Errorf("text %s", err.Error())
		}
	}
	var hintID int64
	if err := db.QueryRowContext(ctx, `
		SELECT h.id FROM adventure_page_hints h
		JOIN adventure_pages p ON p.id = h.page_id
		WHERE p.adventure_id = $1 AND p.idx = $2 AND h.idx = $3
	`, advID, pageIdx, hintIdx).Scan(&hintID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no hint at idx %d on puzzle %d", hintIdx, pageIdx)
		}
		return fmt.Errorf("hint lookup: %w", err)
	}
	sets := []string{}
	args := []any{}
	if text != nil {
		args = append(args, *text)
		sets = append(sets, fmt.Sprintf("text = $%d", len(args)))
	}
	if delaySeconds != nil {
		args = append(args, *delaySeconds)
		sets = append(sets, fmt.Sprintf("delay_seconds = $%d", len(args)))
	}
	args = append(args, hintID)
	q := `UPDATE adventure_page_hints SET ` + strings.Join(sets, ", ") +
		fmt.Sprintf(" WHERE id = $%d", len(args))
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

// DeleteHintByIdx removes one hint and compacts subsequent idx
// values so the per-page hint sequence stays gap-free. Same
// reasoning as DeletePageByIdx for puzzles: gappy idx breaks the
// player UI's reveal-in-order assumption.
func DeleteHintByIdx(ctx context.Context, db *sql.DB, advID int64, pageIdx, hintIdx int) error {
	var pageID, hintID int64
	if err := db.QueryRowContext(ctx, `
		SELECT p.id, h.id FROM adventure_page_hints h
		JOIN adventure_pages p ON p.id = h.page_id
		WHERE p.adventure_id = $1 AND p.idx = $2 AND h.idx = $3
	`, advID, pageIdx, hintIdx).Scan(&pageID, &hintID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no hint at idx %d on puzzle %d", hintIdx, pageIdx)
		}
		return fmt.Errorf("hint lookup: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM adventure_page_hints WHERE id = $1`, hintID); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE adventure_page_hints SET idx = idx - 1 WHERE page_id = $1 AND idx > $2`,
		pageID, hintIdx); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	return tx.Commit()
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
	// Resolve the lane to attach this page to. Explicit lane_id wins;
	// otherwise fall back to the adventure's idx=0 lane (creating it
	// if the adventure has no lanes yet — that happens for adventures
	// authored fresh on a clean DB before any lane has been touched).
	laneID, err := resolveCreateLane(r.Context(), h.DB, advID, req.LaneID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
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
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, lane_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, advID, nextIdx, req.Title, req.Prompt, string(accept), req.Lat, req.Lng, radius, disabled, req.Notes, laneID).Scan(&id)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "insert failed", err)
		return
	}
	if err := upsertPageHints(r.Context(), tx, id, req.Hints); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "hints insert failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "commit failed", err)
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
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "page not found")
		return
	}
	if err := upsertPageHints(r.Context(), tx, pageID, req.Hints); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "hints update failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "commit failed", err)
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

// ReorderPages assigns each page in the supplied list a new idx in
// list order. Two wire shapes are accepted, and both can travel in
// the same body — clients send whichever fits their interaction:
//
//   - "page_ids": [id, id, ...] — legacy flat reorder. Each page
//     keeps its current lane assignment. Used by clients that
//     haven't been updated for the multi-lane editor yet.
//   - "pages": [{"id":id,"lane_id":id}, ...] — lane-aware reorder.
//     Each entry sets both position and lane in one round-trip so a
//     drag-between-lanes commits atomically. lane_id == 0 (or
//     omitted) keeps the page's current lane.
//
// Mixing the two is allowed: a client can send "pages" alone, or
// "page_ids" alone. Behaviour is the same either way — the new idx
// is the index in whichever array the client supplied.
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
		PageIDs []int64 `json:"page_ids,omitempty"`
		Pages   []struct {
			ID     int64 `json:"id"`
			LaneID int64 `json:"lane_id,omitempty"`
		} `json:"pages,omitempty"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	type entry struct {
		id     int64
		laneID int64 // 0 = leave alone
	}
	var entries []entry
	switch {
	case len(req.Pages) > 0:
		entries = make([]entry, len(req.Pages))
		for i, p := range req.Pages {
			entries[i] = entry{id: p.ID, laneID: p.LaneID}
		}
	case len(req.PageIDs) > 0:
		entries = make([]entry, len(req.PageIDs))
		for i, id := range req.PageIDs {
			entries[i] = entry{id: id}
		}
	default:
		httpx.Error(w, http.StatusBadRequest, "supply pages or page_ids")
		return
	}
	// Validate every supplied lane belongs to this adventure before
	// touching any rows. A bad lane id should 400 cleanly, not crash
	// halfway through the reorder and leave parked rows behind.
	laneSeen := map[int64]bool{}
	for _, e := range entries {
		if e.laneID == 0 || laneSeen[e.laneID] {
			continue
		}
		laneSeen[e.laneID] = true
		var owner int64
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT adventure_id FROM adventure_lanes WHERE id = $1`, e.laneID).Scan(&owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				httpx.Error(w, http.StatusBadRequest, "unknown lane id in list")
				return
			}
			httpx.Error(w, http.StatusInternalServerError, "lane check failed")
			return
		}
		if owner != advID {
			httpx.Error(w, http.StatusBadRequest, "lane belongs to another adventure")
			return
		}
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
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
		httpx.ServerErr(w, http.StatusInternalServerError, "prepare failed", err)
		return
	}
	for i, e := range entries {
		var (
			res sql.Result
			err error
		)
		if e.laneID > 0 {
			res, err = tx.ExecContext(r.Context(), `
				UPDATE adventure_pages
				SET idx = $1, lane_id = $2
				WHERE id = $3 AND adventure_id = $4
			`, i, e.laneID, e.id, advID)
		} else {
			res, err = tx.ExecContext(r.Context(), `
				UPDATE adventure_pages
				SET idx = $1
				WHERE id = $2 AND adventure_id = $3
			`, i, e.id, advID)
		}
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "reorder failed", err)
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
