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

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/teamimport"
)

// ExportEventResults writes a CSV of the event's final standings. Columns:
// rank, team, members, puzzles_solved, puzzles_total, time. The order
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
	if uses, err := play.UsesGraph(r.Context(), h.DB, eventID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	} else if uses {
		h.exportGraphResults(w, r, eventID)
		return
	}

	var (
		slug      string
		startedAt string
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_slug, COALESCE(started_at,'') FROM events WHERE id = $1`, eventID).
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
	for _, p := range adv.Pages {
		if !p.Disabled {
			enabledCount++
		}
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, '')
		FROM teams t
		WHERE t.event_id = $1
		ORDER BY
		  (CASE WHEN t.completed_at IS NULL THEN 1 ELSE 0 END),
		  t.completed_at ASC,
		  t.current_page_index DESC,
		  COALESCE(
		    (SELECT MAX(created_at) FROM event_activity
		     WHERE event_id = t.event_id AND team_id = t.id AND kind = 'answer.right'),
		    t.created_at
		  ) ASC,
		  t.created_at ASC
	`, eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="run-%d-results.csv"`, eventID))
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
		// current_page_index is a monotonic count of advances (one per
		// accepted answer / unlock), so it doubles as the "puzzles
		// solved" tally. Cap at enabledCount to keep the column tidy
		// when an arranger toggled puzzles after teams advanced.
		solved := rawCursor
		if solved > enabledCount {
			solved = enabledCount
		}
		if solved < 0 {
			solved = 0
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
		// CSV-injection defang on the WAY OUT. Team names + member
		// names typed via the admin UI bypass the importer's defang
		// (which only covers file-uploaded rosters), so an
		// arranger-typed `=PWN()` would round-trip into Excel
		// here and execute on download. SanitizeCellValue prefixes
		// `=` / `+` / `-` / `@` / tab / `|` cells with a single
		// quote — Excel renders the literal text instead of
		// evaluating. Same OWASP-shaped helper used at import.
		_ = cw.Write([]string{
			strconv.Itoa(rank),
			teamimport.SanitizeCellValue(name),
			teamimport.SanitizeCellValue(members),
			strconv.Itoa(solved),
			strconv.Itoa(enabledCount),
			completedAt,
			elapsed,
		})
	}
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
	var started sql.NullTime
	if err := h.DB.QueryRowContext(ctx, `SELECT MIN(started_at) FROM team_graph_state WHERE event_id = $1`, eventID).Scan(&started); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	idle, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name FROM teams t
		WHERE t.event_id = $1 AND (t.withdrawn_at IS NOT NULL OR NOT EXISTS (SELECT 1 FROM team_graph_state s WHERE s.team_id = t.id))
		ORDER BY LOWER(t.name)`, eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	type unranked struct {
		id   int64
		name string
	}
	var rest []unranked
	for idle.Next() {
		var u unranked
		if err := idle.Scan(&u.id, &u.name); err != nil {
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
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="run-%d-results.csv"`, eventID))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"rank", "team", "members", "points", "tiebreak_guess", "tiebreak_off_by", "finished_at", "time"})
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
		finished, elapsed := "", ""
		if t.Done {
			finished = t.CompletedAt.UTC().Format(time.RFC3339)
			if started.Valid {
				elapsed = t.CompletedAt.Sub(started.Time).Round(time.Second).String()
			}
		}
		// Names are defanged on the way out, as in the lane export below.
		_ = cw.Write([]string{strconv.Itoa(rank), teamimport.SanitizeCellValue(t.Name), teamimport.SanitizeCellValue(members), num(t.Score), guess, off, finished, elapsed})
	}
	for _, u := range rest {
		members, _ := h.teamMemberNames(ctx, u.id)
		_ = cw.Write([]string{"", teamimport.SanitizeCellValue(u.name), teamimport.SanitizeCellValue(members), "", "", "", "", ""})
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
