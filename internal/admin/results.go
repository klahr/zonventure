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
	"github.com/klahr/zonventure/internal/results"
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
	res, err := h.eventResults(r.Context(), eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "results failed", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d-results.csv"`, eventID))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"rank", "team", "members", "points", "tiebreak_guess", "tiebreak_off_by", "finished_at", "time", "status"})
	num := func(f *float64) string {
		if f == nil {
			return ""
		}
		return strconv.FormatFloat(*f, 'f', -1, 64)
	}
	for _, t := range res.Teams {
		rank, elapsed := "", ""
		if t.Rank > 0 {
			rank = strconv.Itoa(t.Rank)
		}
		if t.TimeSeconds != nil {
			elapsed = (time.Duration(*t.TimeSeconds) * time.Second).String()
		}
		// Names are defanged on the way out.
		_ = cw.Write([]string{rank, teamimport.SanitizeCellValue(t.Team), teamimport.SanitizeCellValue(strings.Join(t.Members, ", ")),
			num(t.Points), num(t.TiebreakGuess), num(t.TiebreakOffBy), t.FinishedAt, elapsed, t.Status})
	}
}

// ExportEventResultsJSON is the results export as JSON, for a program
// to read: the CSV's rows, with the event around them, each team's
// results receipt and, once the event is over, its results fingerprint.
//
//	GET /api/admin/events/{eventID}/results.json
func (h *Handlers) ExportEventResultsJSON(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	res, err := h.eventResults(r.Context(), eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "results failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d-results.json"`, eventID))
	httpx.JSON(w, http.StatusOK, res)
}

// resultsExport is an event's results as the exports give them.
type resultsExport struct {
	Event struct {
		ID             int64  `json:"id"`
		Name           string `json:"name,omitempty"`
		AdventureSlug  string `json:"adventure_slug"`
		AdventureTitle string `json:"adventure_title,omitempty"`
		State          string `json:"state"`
		StartedAt      string `json:"started_at,omitempty"`
		CompletedAt    string `json:"completed_at,omitempty"`
		// ResultsFingerprint is the hash that seals the event's results
		// record (internal/results), once the event is over.
		ResultsFingerprint string `json:"results_fingerprint,omitempty"`
	} `json:"event"`
	GeneratedAt string       `json:"generated_at"`
	Teams       []resultsRow `json:"teams"`
}

// resultsRow is one team. Rank is 0 for a team that withdrew or never
// started, which has no points either; teams on the same place share
// its number. Times are in the organisation's time zone, with its
// offset; TimeSeconds runs from the event's first start.
type resultsRow struct {
	Rank          int      `json:"rank,omitempty"`
	Team          string   `json:"team"`
	Members       []string `json:"members"`
	Points        *float64 `json:"points,omitempty"`
	TiebreakGuess *float64 `json:"tiebreak_guess,omitempty"`
	TiebreakOffBy *float64 `json:"tiebreak_off_by,omitempty"`
	FinishedAt    string   `json:"finished_at,omitempty"`
	TimeSeconds   *int64   `json:"time_seconds,omitempty"`
	Status        string   `json:"status"`
	// Receipt is the team's results receipt, the code its recap shows.
	Receipt string `json:"receipt,omitempty"`
}

// eventResults ranks a graph event's teams as the leaderboard and the
// standings rank them (play.Ahead): points, then the tie-breaker between
// finished teams, then the earlier finish. Teams that never started, or
// withdrew, come last, unranked.
func (h *Handlers) eventResults(ctx context.Context, eventID int64) (resultsExport, error) {
	var res resultsExport
	res.Teams = []resultsRow{}
	ranked, err := h.engine().EventRanking(ctx, eventID)
	if err != nil {
		return res, err
	}
	var (
		started            sql.NullTime
		tz                 string
		evStarted, evEnded sql.NullString
	)
	ev := &res.Event
	ev.ID = eventID
	if err := h.DB.QueryRowContext(ctx, `
		SELECT (SELECT MIN(started_at) FROM team_graph_state WHERE event_id = e.id), e.state, o.time_zone,
		       COALESCE(e.name, ''), e.adventure_slug, e.started_at, e.completed_at
		FROM events e JOIN organizations o ON o.id = e.org_id WHERE e.id = $1`, eventID).Scan(
		&started, &ev.State, &tz, &ev.Name, &ev.AdventureSlug, &evStarted, &evEnded); err != nil {
		return res, err
	}
	loc := emailtpl.Location(tz)
	stamp := func(raw sql.NullString) string {
		if t, ok := parseRecapTimestamp(raw.String); ok {
			return t.In(loc).Format(time.RFC3339)
		}
		return ""
	}
	ev.AdventureTitle = h.adventureTitle(ev.AdventureSlug)
	ev.StartedAt, ev.CompletedAt = stamp(evStarted), stamp(evEnded)
	ev.ResultsFingerprint = results.Sealed(ctx, h.DB, eventID)
	res.GeneratedAt = time.Now().In(loc).Format(time.RFC3339)
	receipts, err := h.teamReceipts(ctx, eventID)
	if err != nil {
		return res, err
	}
	unfinished := "did not finish"
	switch ev.State {
	case "scheduled", "running", "paused":
		unfinished = "playing"
	}
	for _, t := range ranked {
		row := resultsRow{Rank: 1, Team: t.Name, Status: unfinished, Receipt: receipts[t.TeamID]}
		for _, o := range ranked {
			if play.Ahead(o, t) {
				row.Rank++
			}
		}
		row.Members, _ = h.teamMembers(ctx, t.TeamID)
		row.Points = &t.Score
		if t.Guess != nil {
			miss := t.Miss
			row.TiebreakGuess, row.TiebreakOffBy = t.Guess, &miss
		}
		if t.Done {
			row.Status = "finished"
			row.FinishedAt = t.CompletedAt.In(loc).Format(time.RFC3339)
			if started.Valid {
				secs := int64(t.CompletedAt.Sub(started.Time).Round(time.Second) / time.Second)
				row.TimeSeconds = &secs
			}
		}
		res.Teams = append(res.Teams, row)
	}
	idle, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.withdrawn_at IS NOT NULL FROM teams t
		WHERE t.event_id = $1 AND (t.withdrawn_at IS NOT NULL OR NOT EXISTS (SELECT 1 FROM team_graph_state s WHERE s.team_id = t.id AND NOT s.cheated))
		ORDER BY LOWER(t.name)`, eventID)
	if err != nil {
		return res, err
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
			return res, err
		}
		rest = append(rest, u)
	}
	idle.Close()
	if err := idle.Err(); err != nil {
		return res, err
	}
	for _, u := range rest {
		row := resultsRow{Team: u.name, Status: "not started", Receipt: receipts[u.id]}
		if u.withdrawn {
			row.Status = "withdrawn"
		}
		row.Members, _ = h.teamMembers(ctx, u.id)
		res.Teams = append(res.Teams, row)
	}
	return res, nil
}

// teamReceipts is each finished team's results receipt: the hash of its
// latest finish entry.
func (h *Handlers) teamReceipts(ctx context.Context, eventID int64) (map[int64]string, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT team_id, entry_hash FROM event_result_log
		WHERE event_id = $1 AND kind = $2 ORDER BY seq`, eventID, results.TeamFinished)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var (
			id int64
			h  string
		)
		if err := rows.Scan(&id, &h); err != nil {
			return nil, err
		}
		out[id] = h
	}
	return out, rows.Err()
}

// teamMemberNames returns a comma-joined list of slot names for a team.
func (h *Handlers) teamMemberNames(ctx context.Context, teamID int64) (string, error) {
	names, err := h.teamMembers(ctx, teamID)
	return strings.Join(names, ", "), err
}

// teamMembers is a team's players' names.
func (h *Handlers) teamMembers(ctx context.Context, teamID int64) ([]string, error) {
	rows, err := h.DB.QueryContext(ctx,
		`SELECT name FROM player_slots WHERE team_id = $1 ORDER BY LOWER(name)`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
