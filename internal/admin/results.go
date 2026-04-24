package admin

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/httpx"
)

// ExportRunResults writes a CSV of the run's final standings. Columns:
// rank, team, members, puzzles_solved, puzzles_total, time. The order
// and ranking rules mirror the public leaderboard's so a TV shot and
// the exported file agree.
//
// Filename is "run-{id}-results.csv" so the arranger can drop several
// into a folder without renaming.
func (h *Handlers) ExportRunResults(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	if _, ok := h.authorizeRun(w, r, runID); !ok {
		return
	}

	var (
		slug      string
		startedAt string
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_slug, COALESCE(started_at,'') FROM adventure_runs WHERE id = ?`, runID).
		Scan(&slug, &startedAt); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	adv := h.Lib.Get(slug)
	if adv == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded")
		return
	}
	// Enabled-only count — disabled puzzles never count toward a team's
	// progress, so they shouldn't pad the "x of N" total on results.
	enabledCount := 0
	enabled := make([]int, 0, len(adv.Pages))
	for _, p := range adv.Pages {
		if !p.Disabled {
			enabled = append(enabled, p.Index)
			enabledCount++
		}
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, '')
		FROM teams t
		WHERE t.run_id = ?
		ORDER BY
		  (CASE WHEN t.completed_at IS NULL THEN 1 ELSE 0 END),
		  t.completed_at ASC,
		  t.current_page_index DESC,
		  COALESCE(
		    (SELECT MAX(created_at) FROM run_events
		     WHERE run_id = t.run_id AND team_id = t.id AND kind = 'answer.right'),
		    t.created_at
		  ) ASC,
		  t.created_at ASC
	`, runID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="run-%d-results.csv"`, runID))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"rank", "team", "members", "puzzles_solved", "puzzles_total", "finished_at", "time"})

	startedTime, _ := time.Parse("2006-01-02 15:04:05", startedAt)
	if startedTime.IsZero() {
		startedTime, _ = time.Parse(time.RFC3339, startedAt)
	}

	rank := 0
	for rows.Next() {
		var (
			teamID      int64
			name        string
			rawCursor   int
			completedAt string
		)
		if err := rows.Scan(&teamID, &name, &rawCursor, &completedAt); err != nil {
			return
		}
		rank++
		members, _ := h.teamMemberNames(r.Context(), teamID)
		// Enabled-solved count: count enabled pages whose idx < rawCursor.
		solved := 0
		for _, idx := range enabled {
			if idx < rawCursor {
				solved++
			}
		}
		if solved > enabledCount {
			solved = enabledCount
		}
		elapsed := ""
		if completedAt != "" && !startedTime.IsZero() {
			finishedTime, perr := time.Parse("2006-01-02 15:04:05", completedAt)
			if perr != nil {
				finishedTime, _ = time.Parse(time.RFC3339, completedAt)
			}
			if !finishedTime.IsZero() {
				d := finishedTime.Sub(startedTime).Round(time.Second)
				elapsed = d.String()
			}
		}
		_ = cw.Write([]string{
			strconv.Itoa(rank),
			name,
			members,
			strconv.Itoa(solved),
			strconv.Itoa(enabledCount),
			completedAt,
			elapsed,
		})
	}
}

// teamMemberNames returns a comma-joined list of slot names for a team.
func (h *Handlers) teamMemberNames(ctx context.Context, teamID int64) (string, error) {
	rows, err := h.DB.QueryContext(ctx,
		`SELECT name FROM player_slots WHERE team_id = ? ORDER BY name COLLATE NOCASE`, teamID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", err
		}
		names = append(names, n)
	}
	return strings.Join(names, ", "), nil
}
