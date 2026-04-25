package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
)

type eventDTO struct {
	ID        int64           `json:"id"`
	TeamID    *int64          `json:"team_id,omitempty"`
	TeamName  string          `json:"team_name,omitempty"`
	Actor     string          `json:"actor,omitempty"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

// buildEvents returns up to 500 newest events for a run, optionally
// filtered to one team.
func (h *Handlers) buildEvents(ctx context.Context, runID int64, teamFilter *int64) ([]eventDTO, error) {
	query := `
		SELECT e.id, e.team_id, COALESCE(t.name, ''), COALESCE(u.username, ''),
		       e.kind, e.payload, e.created_at
		FROM run_events e
		LEFT JOIN teams t ON t.id = e.team_id
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.run_id = $1`
	args := []any{runID}
	if teamFilter != nil {
		query += ` AND e.team_id = $1`
		args = append(args, *teamFilter)
	}
	query += ` ORDER BY e.created_at DESC, e.id DESC LIMIT 500`
	rows, err := h.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []eventDTO{}
	for rows.Next() {
		var (
			e       eventDTO
			teamID  sql.NullInt64
			payload string
		)
		if err := rows.Scan(&e.ID, &teamID, &e.TeamName, &e.Actor, &e.Kind, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		if teamID.Valid {
			v := teamID.Int64
			e.TeamID = &v
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, nil
}

// ListEvents returns the run's event feed, newest first.
func (h *Handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	var orgID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT org_id FROM adventure_runs WHERE id = $1`, runID).Scan(&orgID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !sameOrg(me, orgID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var teamFilter *int64
	if s := r.URL.Query().Get("team_id"); s != "" {
		tid, err := strconv.ParseInt(s, 10, 64)
		if err != nil || tid <= 0 {
			httpx.Error(w, http.StatusBadRequest, "bad team_id")
			return
		}
		teamFilter = &tid
	}
	out, err := h.buildEvents(r.Context(), runID, teamFilter)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}
