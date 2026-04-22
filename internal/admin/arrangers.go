package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

type assignmentDTO struct {
	ID             int64  `json:"id"`
	AdventureSlug  string `json:"adventure_slug"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	MaxTeams       int    `json:"max_teams"`
	CreatedAt      string `json:"created_at"`
	RunID          *int64 `json:"run_id,omitempty"`
}

type arrangerDTO struct {
	ID           int64           `json:"id"`
	Username     string          `json:"username"`
	Assignments  []assignmentDTO `json:"assignments"`
	RunsTotal    int             `json:"runs_total"`
	RunsRunning  int             `json:"runs_running"`
	PlayersTotal int             `json:"players_total"`
}

// ListArrangers returns every arranger with their assignments and activity
// counts. Root-only.
func (h *Handlers) ListArrangers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT u.id, u.username,
		       (SELECT COUNT(*) FROM adventure_runs ar WHERE ar.arranger_user_id = u.id),
		       (SELECT COUNT(*) FROM adventure_runs ar WHERE ar.arranger_user_id = u.id AND ar.state = 'running'),
		       (SELECT COUNT(*) FROM users p WHERE p.role = 'player' AND p.created_by_user_id = u.id)
		FROM users u
		WHERE u.role = 'arranger'
		ORDER BY u.username COLLATE NOCASE
	`)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []arrangerDTO{}
	for rows.Next() {
		a := arrangerDTO{Assignments: []assignmentDTO{}}
		if err := rows.Scan(&a.ID, &a.Username, &a.RunsTotal, &a.RunsRunning, &a.PlayersTotal); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, a)
	}
	// Second pass for assignments (left joined to the run that consumed each).
	for i := range out {
		if err := h.loadAssignments(r, &out[i]); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "assignments failed")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"arrangers": out})
}

func (h *Handlers) loadAssignments(r *http.Request, a *arrangerDTO) error {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT aa.id, aa.adventure_slug, aa.max_teams, aa.created_at, ar.id
		FROM adventure_assignments aa
		LEFT JOIN adventure_runs ar ON ar.assignment_id = aa.id
		WHERE aa.arranger_user_id = ?
		ORDER BY aa.created_at DESC
	`, a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			as       assignmentDTO
			runID    sql.NullInt64
		)
		if err := rows.Scan(&as.ID, &as.AdventureSlug, &as.MaxTeams, &as.CreatedAt, &runID); err != nil {
			return err
		}
		if runID.Valid {
			v := runID.Int64
			as.RunID = &v
		}
		if adv := h.Lib.Get(as.AdventureSlug); adv != nil {
			as.AdventureTitle = adv.Meta.Title
		}
		a.Assignments = append(a.Assignments, as)
	}
	return nil
}

type createAssignmentRequest struct {
	AdventureSlug string `json:"adventure_slug"`
	MaxTeams      int    `json:"max_teams"`
}

// CreateAssignment grants an arranger one more run of an adventure with a
// fixed team cap. Root-only.
func (h *Handlers) CreateAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var req createAssignmentRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if h.Lib.Get(req.AdventureSlug) == nil {
		httpx.Error(w, http.StatusBadRequest, "unknown adventure")
		return
	}
	if req.MaxTeams <= 0 {
		httpx.Error(w, http.StatusBadRequest, "max_teams must be > 0")
		return
	}
	var role string
	err := h.DB.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id = ?`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if role != auth.RoleArranger {
		httpx.Error(w, http.StatusBadRequest, "target user is not an arranger")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_assignments(arranger_user_id, adventure_slug, max_teams)
		VALUES (?, ?, ?)
	`, id, req.AdventureSlug, req.MaxTeams)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assign failed")
		return
	}
	assignmentID, _ := res.LastInsertId()
	httpx.JSON(w, http.StatusCreated, assignmentDTO{
		ID: assignmentID, AdventureSlug: req.AdventureSlug, MaxTeams: req.MaxTeams,
	})
}

// DeleteAssignment revokes an unused assignment. Consumed assignments can't
// be deleted — they're tied to a run. Root-only.
func (h *Handlers) DeleteAssignment(w http.ResponseWriter, r *http.Request) {
	arrangerID, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	assignmentID, err := strconv.ParseInt(chi.URLParam(r, "assignmentID"), 10, 64)
	if err != nil || assignmentID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad assignment id")
		return
	}
	// Existence + ownership check combined with "is it already consumed?"
	var (
		owner   int64
		runID   sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT aa.arranger_user_id, ar.id
		FROM adventure_assignments aa
		LEFT JOIN adventure_runs ar ON ar.assignment_id = aa.id
		WHERE aa.id = ?
	`, assignmentID).Scan(&owner, &runID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "assignment not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owner != arrangerID {
		httpx.Error(w, http.StatusNotFound, "assignment not found")
		return
	}
	if runID.Valid {
		httpx.Error(w, http.StatusConflict, "assignment is already in use by a run")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_assignments WHERE id = ?`, assignmentID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ListMyAssignments returns the caller's own assignments (arranger-facing).
// Used by the arranger's Adventures page to show what they're allowed to
// run and how many teams each allows.
func (h *Handlers) ListMyAssignments(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	dto := arrangerDTO{ID: me.ID, Assignments: []assignmentDTO{}}
	if err := h.loadAssignments(r, &dto); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assignments failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"assignments": dto.Assignments})
}
