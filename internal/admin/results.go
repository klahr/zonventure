package admin

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/teamimport"
)

// ExportEventResults writes a CSV of the event's final standings. Columns:
// rank, team, members, points, tiebreak_guess, tiebreak_off_by,
// finished_at (in the organisation's time zone, with its offset), time,
// status (finished, playing, did not finish, withdrawn, not started). The order
// and ranking rules mirror the public leaderboard's so a TV shot and
// the exported file agree.
//
// Filename is "event-{id}-results.csv" so the arranger can drop several
// into a folder without renaming.
func (h *Handlers) ExportEventResults(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	h.exportGraphResults(w, r, eventID)
}

// exportGraphResults is the results CSV of a graph event, ranked as the
// leaderboard and the standings rank it (play.Ahead): points, then the
// tie-breaker between finished teams, then the earlier finish. Teams on
// the same place share its number. Teams that never started, or
// withdrew, are listed last, unranked.
func (h *Handlers) exportGraphResults(w http.ResponseWriter, r *http.Request, eventID int64) {
	ctx := r.Context()
	e := h.Engine
	if e == nil {
		e = play.New(h.DB)
	}
	ranked, err := e.EventRanking(ctx, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "ranking failed", err)
		return
	}
	var (
		started   sql.NullTime
		state, tz string
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT (SELECT MIN(started_at) FROM team_graph_state WHERE event_id = e.id), e.state, o.time_zone
		FROM events e JOIN organizations o ON o.id = e.org_id WHERE e.id = $1`, eventID).Scan(&started, &state, &tz); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	loc := emailtpl.Location(tz)
	unfinished := "did not finish"
	switch state {
	case "scheduled", "running", "paused":
		unfinished = "playing"
	}
	idle, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.withdrawn_at IS NOT NULL FROM teams t
		WHERE t.event_id = $1 AND (t.withdrawn_at IS NOT NULL OR NOT EXISTS (SELECT 1 FROM team_graph_state s WHERE s.team_id = t.id AND NOT s.cheated))
		ORDER BY LOWER(t.name)`, eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	type unranked struct {
		id        int64
		name      string
		withdrawn bool
	}
	var rest []unranked
	for idle.Next() {
		var u unranked
		if err := idle.Scan(&u.id, &u.name, &u.withdrawn); err != nil {
			idle.Close()
			return
		}
		rest = append(rest, u)
	}
	idle.Close()
	if err := idle.Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d-results.csv"`, eventID))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"rank", "team", "members", "points", "tiebreak_guess", "tiebreak_off_by", "finished_at", "time", "status"})
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	for _, t := range ranked {
		rank := 1
		for _, o := range ranked {
			if play.Ahead(o, t) {
				rank++
			}
		}
		members, _ := h.teamMemberNames(ctx, t.TeamID)
		guess, off := "", ""
		if t.Guess != nil {
			guess, off = num(*t.Guess), num(t.Miss)
		}
		finished, elapsed, status := "", "", unfinished
		if t.Done {
			status = "finished"
			finished = t.CompletedAt.In(loc).Format(time.RFC3339)
			if started.Valid {
				elapsed = t.CompletedAt.Sub(started.Time).Round(time.Second).String()
			}
		}
		// Names are defanged on the way out.
		_ = cw.Write([]string{strconv.Itoa(rank), teamimport.SanitizeCellValue(t.Name), teamimport.SanitizeCellValue(members), num(t.Score), guess, off, finished, elapsed, status})
	}
	for _, u := range rest {
		members, _ := h.teamMemberNames(ctx, u.id)
		status := "not started"
		if u.withdrawn {
			status = "withdrawn"
		}
		_ = cw.Write([]string{"", teamimport.SanitizeCellValue(u.name), teamimport.SanitizeCellValue(members), "", "", "", "", "", status})
	}
}

// teamMemberNames returns a comma-joined list of slot names for a team.
func (h *Handlers) teamMemberNames(ctx context.Context, teamID int64) (string, error) {
	rows, err := h.DB.QueryContext(ctx,
		`SELECT name FROM player_slots WHERE team_id = $1 ORDER BY LOWER(name)`, teamID)
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
