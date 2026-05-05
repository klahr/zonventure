package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// Maximum lane name length. Long enough for "Downtown puzzles" or
// "Final stretch" without giving the editor enough rope to push
// pixels around.
const maxLaneNameLen = 80

// SetLaneMode flips a single lane's mode between "linear" and
// "shuffled" by adventure id and lane idx. Used by the MCP
// set_lane_mode tool — the LLM-facing shape uses lane_idx (export
// shape) rather than the DB row id, so this entry point hides the
// id lookup.
//
// Caller is responsible for authorization
// (AuthorizeAdventureEdit). Returns an *importError on bad mode
// or unknown idx; wrapped DB error otherwise. Stamps the parent
// adventure's updated_at on success — same as the HTTP handler.
func SetLaneMode(ctx context.Context, db *sql.DB, advID int64, laneIdx int, mode string) error {
	m := adventure.LaneMode(mode)
	if m != adventure.LaneModeLinear && m != adventure.LaneModeShuffled {
		return badRequestf(`mode must be "linear" or "shuffled"`)
	}
	res, err := db.ExecContext(ctx,
		`UPDATE adventure_lanes SET mode = $1 WHERE adventure_id = $2 AND idx = $3`,
		string(m), advID, laneIdx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &importError{status: http.StatusNotFound,
			msg: "no lane with that idx in this adventure"}
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE adventures SET updated_at = now_text() WHERE id = $1`, advID); err != nil {
		return err
	}
	return nil
}

// laneIDFrom pulls the {laneID} URL parameter and returns it as an
// int64 plus an ok flag. Writes the 400 on parse failure so callers
// can early-return.
func laneIDFrom(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "laneID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad lane id")
		return 0, false
	}
	return id, true
}

// requireEditLane checks ownership of the lane's adventure and
// returns the resolved adventure_id so callers don't have to look
// it up again. Writes the HTTP error on failure.
func (h *Handlers) requireEditLane(w http.ResponseWriter, r *http.Request, laneID int64) (int64, bool) {
	var (
		advID  int64
		advOrg sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT l.adventure_id, a.org_id
		FROM adventure_lanes l
		JOIN adventures a ON a.id = l.adventure_id
		WHERE l.id = $1
	`, laneID).Scan(&advID, &advOrg)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "lane not found")
		return 0, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return 0, false
	}
	if !canAuthorRequest(r, advOrg) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return 0, false
	}
	return advID, true
}

// canAuthorRequest is the requireEditAdventure-style policy check
// against an already-loaded org_id, factored out so the lane
// handlers can authorise a lane id without re-querying the
// adventure.
func canAuthorRequest(r *http.Request, advOrg sql.NullInt64) bool {
	me, ok := auth.FromContext(r.Context())
	if !ok {
		return false
	}
	return canAuthor(me, advOrg)
}

// validateLaneName trims and length-caps a lane name. Returns the
// trimmed value plus an error message suitable for a 400 body.
// Empty is allowed — that's "fall back to 'Lane N' in the UI".
func validateLaneName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if len([]rune(name)) > maxLaneNameLen {
		return "", "name too long"
	}
	return name, ""
}

/* ---------------- Lane CRUD ---------------- */

// laneDTO is what the lane endpoints return — same shape the editor
// will iterate over to render lane sections.
type laneDTO struct {
	ID    int64  `json:"id"`
	Index int    `json:"idx"`
	Mode  string `json:"mode"`
	Name  string `json:"name"`
}

// loadAdventureLanes returns the adventure's lanes in idx order,
// shaped for the editor DTO. Used by both the lane CRUD response
// path and loadAdventureDTO so the editor and the granular create
// response carry the same shape.
func loadAdventureLanes(ctx context.Context, db *sql.DB, adventureID int64) ([]laneDTO, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mode, name
		FROM adventure_lanes
		WHERE adventure_id = $1
		ORDER BY idx
	`, adventureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []laneDTO{}
	for rows.Next() {
		var ln laneDTO
		if err := rows.Scan(&ln.ID, &ln.Index, &ln.Mode, &ln.Name); err != nil {
			return nil, err
		}
		out = append(out, ln)
	}
	return out, rows.Err()
}

type createLaneRequest struct {
	Mode string `json:"mode"`
	Name string `json:"name"`
}

// CreateLane appends a new lane to the adventure at the next idx.
// Mode defaults to "linear" when omitted; name is optional ("" means
// "fall back to 'Lane N' in the UI"). Returns the new lane's DTO so
// the editor can drop it straight into its rendered list.
//
// POST /api/admin/adventures/{slug}/lanes
func (h *Handlers) CreateLane(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	var req createLaneRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	mode := adventure.LaneMode(req.Mode)
	if mode == "" {
		mode = adventure.LaneModeLinear
	}
	if mode != adventure.LaneModeLinear && mode != adventure.LaneModeShuffled {
		httpx.Error(w, http.StatusBadRequest, `mode must be "linear" or "shuffled"`)
		return
	}
	name, nameErr := validateLaneName(req.Name)
	if nameErr != "" {
		httpx.Error(w, http.StatusBadRequest, "name "+nameErr)
		return
	}
	var advID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&advID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	// idx = max(existing) + 1, with 0 as the seed when the table is
	// empty for this adventure. COALESCE keeps the SQL portable
	// across "no rows yet" and "rows exist".
	var newIdx int
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(MAX(idx) + 1, 0) FROM adventure_lanes WHERE adventure_id = $1
	`, advID).Scan(&newIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next idx failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_lanes (adventure_id, idx, mode, name)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, advID, newIdx, string(mode), name).Scan(&id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create lane failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventures SET updated_at = now_text() WHERE id = $1`, advID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "stamp updated_at failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, laneDTO{
		ID:    id,
		Index: newIdx,
		Mode:  string(mode),
		Name:  name,
	})
}

type updateLaneRequest struct {
	Mode *string `json:"mode,omitempty"`
	Name *string `json:"name,omitempty"`
}

// UpdateLane patches a lane's mode and/or name. Both fields are
// optional pointers so the editor can rename without touching the
// mode and vice versa.
//
// PATCH /api/admin/adventures/{slug}/lanes/{laneID}
func (h *Handlers) UpdateLane(w http.ResponseWriter, r *http.Request) {
	laneID, ok := laneIDFrom(w, r)
	if !ok {
		return
	}
	advID, ok := h.requireEditLane(w, r, laneID)
	if !ok {
		return
	}
	var req updateLaneRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Mode == nil && req.Name == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	sets := []string{}
	args := []any{}
	if req.Mode != nil {
		mode := adventure.LaneMode(*req.Mode)
		if mode != adventure.LaneModeLinear && mode != adventure.LaneModeShuffled {
			httpx.Error(w, http.StatusBadRequest, `mode must be "linear" or "shuffled"`)
			return
		}
		args = append(args, string(mode))
		sets = append(sets, "mode = $"+strconv.Itoa(len(args)))
	}
	if req.Name != nil {
		name, nameErr := validateLaneName(*req.Name)
		if nameErr != "" {
			httpx.Error(w, http.StatusBadRequest, "name "+nameErr)
			return
		}
		args = append(args, name)
		sets = append(sets, "name = $"+strconv.Itoa(len(args)))
	}
	args = append(args, laneID)
	q := `UPDATE adventure_lanes SET ` + strings.Join(sets, ", ") + ` WHERE id = $` + strconv.Itoa(len(args))
	if _, err := h.DB.ExecContext(r.Context(), q, args...); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update lane failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventures SET updated_at = now_text() WHERE id = $1`, advID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "stamp updated_at failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteLane removes a lane. Refuses with 409 if the lane still
// owns any pages — the arranger has to drag puzzles out first.
// Auto-merging into a neighbour was considered but rejected for v1:
// quietly relocating someone's puzzles is a data-loss-shaped
// surprise we'd rather avoid.
//
// DELETE /api/admin/adventures/{slug}/lanes/{laneID}
func (h *Handlers) DeleteLane(w http.ResponseWriter, r *http.Request) {
	laneID, ok := laneIDFrom(w, r)
	if !ok {
		return
	}
	advID, ok := h.requireEditLane(w, r, laneID)
	if !ok {
		return
	}
	var pageCount int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM adventure_pages WHERE lane_id = $1`, laneID).Scan(&pageCount); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "count failed")
		return
	}
	if pageCount > 0 {
		httpx.Error(w, http.StatusConflict, "lane has puzzles — move them out first")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_lanes WHERE id = $1`, laneID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "delete lane failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventures SET updated_at = now_text() WHERE id = $1`, advID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "stamp updated_at failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

type reorderLanesRequest struct {
	LaneIDs []int64 `json:"lane_ids"`
}

// ReorderLanes assigns each lane in the supplied list a new idx in
// list order, parking and reassigning under the UNIQUE constraint
// the same way the page reorder does.
//
// POST /api/admin/adventures/{slug}/lanes/reorder
func (h *Handlers) ReorderLanes(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	var req reorderLanesRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	// Park every lane at a guaranteed-unique idx well above the
	// reassign range so the second pass can write 0..N-1 without
	// transient UNIQUE(adventure_id, idx) collisions. Same pattern
	// as ReorderPages.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE adventure_lanes SET idx = 1000000000 + id WHERE adventure_id = $1`, advID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "park failed", err)
		return
	}
	for i, id := range req.LaneIDs {
		res, err := tx.ExecContext(r.Context(),
			`UPDATE adventure_lanes SET idx = $1 WHERE id = $2 AND adventure_id = $3`,
			i, id, advID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "reorder failed", err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.Error(w, http.StatusBadRequest, "unknown lane id in list")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE adventures SET updated_at = now_text() WHERE id = $1`, advID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "stamp updated_at failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}
