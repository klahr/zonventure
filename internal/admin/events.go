package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
)

// eventDTO is the JSON shape returned by ListEvents.
type eventDTO struct {
	ID        int64           `json:"id"`
	TeamID    *int64          `json:"team_id,omitempty"`
	TeamName  string          `json:"team_name,omitempty"`
	Actor     string          `json:"actor,omitempty"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

// ListEvents returns the run's event feed, newest first.
func (h *Handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	arr, _ := auth.FromContext(r.Context())
	var owner int64
	err := h.DB.QueryRowContext(r.Context(), `SELECT arranger_user_id FROM adventure_runs WHERE id = ?`, runID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if arr.Role != auth.RoleRoot && owner != arr.ID {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	// Optional ?team_id filter — when present, only include events tied to
	// that team. Note: run-level events (run.created, run.started, …) have
	// a NULL team_id and so are excluded under the filter, which is the
	// behaviour a per-team feed wants.
	query := `
		SELECT e.id, e.team_id, COALESCE(t.name, ''), COALESCE(u.username, ''),
		       e.kind, e.payload, e.created_at
		FROM run_events e
		LEFT JOIN teams t ON t.id = e.team_id
		LEFT JOIN users u ON u.id = e.actor_id
		WHERE e.run_id = ?`
	args := []any{runID}
	if s := r.URL.Query().Get("team_id"); s != "" {
		tid, err := strconv.ParseInt(s, 10, 64)
		if err != nil || tid <= 0 {
			httpx.Error(w, http.StatusBadRequest, "bad team_id")
			return
		}
		query += ` AND e.team_id = ?`
		args = append(args, tid)
	}
	query += ` ORDER BY e.created_at DESC, e.id DESC LIMIT 500`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
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
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if teamID.Valid {
			v := teamID.Int64
			e.TeamID = &v
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}
