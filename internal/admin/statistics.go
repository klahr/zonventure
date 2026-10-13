package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
)

// EventStats is the event page's statistics: how the game went, across
// every team that played and never naming one. Teams that cheated are
// left out, as they are from the standings.
type EventStats struct {
	Teams          int           `json:"teams"`
	Finished       int           `json:"finished"`
	Failed         int           `json:"failed"`
	Guesses        int           `json:"guesses"`
	Wrong          int           `json:"wrong"`
	FirstTry       int           `json:"first_try"`
	Solved         int           `json:"solved"`
	Hints          int           `json:"hints"`
	Photos         int           `json:"photos"`
	MedianSeconds  *int          `json:"median_seconds,omitempty"`
	FastestSeconds *int          `json:"fastest_seconds,omitempty"`
	SlowestSeconds *int          `json:"slowest_seconds,omitempty"`
	MedianScore    *float64      `json:"median_score,omitempty"`
	TopScore       *float64      `json:"top_score,omitempty"`
	AvgDistanceM   *float64      `json:"avg_distance_m,omitempty"`
	Puzzles        []PuzzleStats `json:"puzzles"`
}

// PuzzleStats is one puzzle's numbers, in graph order. Wrong guesses
// are the commonest ones, case and surrounding space folded together.
type PuzzleStats struct {
	Node          string       `json:"node"`
	Title         string       `json:"title"`
	Reached       int          `json:"reached"`
	Solved        int          `json:"solved"`
	FirstTry      int          `json:"first_try"`
	Wrong         int          `json:"wrong"`
	Hints         int          `json:"hints"`
	MedianSeconds *int         `json:"median_seconds,omitempty"`
	WrongAnswers  []GuessCount `json:"wrong_answers"`
}

// GuessCount is how often a wrong guess was made, and by how many teams.
type GuessCount struct {
	Answer string `json:"answer"`
	Count  int    `json:"count"`
	Teams  int    `json:"teams"`
}

// topWrongAnswers caps each puzzle's list of wrong guesses.
const topWrongAnswers = 5

// statsTeams is the subquery of the teams the statistics count.
const statsTeams = `SELECT team_id FROM team_graph_state WHERE event_id = $1 AND NOT cheated`

// EventStatistics answers GET /events/{eventID}/statistics. A lane event
// has none of the graph state they are read from, and gets an empty set.
func (h *Handlers) EventStatistics(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	ctx := r.Context()
	g, err := h.engine().EventGraph(ctx, h.DB, eventID)
	if err != nil && !errors.Is(err, play.ErrNoGraph) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	st := EventStats{Puzzles: []PuzzleStats{}}
	if g != nil {
		if err := h.fillEventStats(ctx, eventID, g, &st); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "statistics failed", err)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, st)
}

func (h *Handlers) fillEventStats(ctx context.Context, eventID int64, g *graph.Graph, st *EventStats) error {
	var (
		durations []int
		scores    []float64
	)
	rows, err := h.DB.QueryContext(ctx, `
		SELECT started_at, completed_at, COALESCE(outcome, ''), score - penalty_score
		FROM team_graph_state WHERE event_id = $1 AND NOT cheated`, eventID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			started   time.Time
			completed sql.NullTime
			outcome   string
			score     float64
		)
		if err := rows.Scan(&started, &completed, &outcome, &score); err != nil {
			rows.Close()
			return err
		}
		st.Teams++
		scores = append(scores, score)
		if outcome == "fail" {
			st.Failed++
		} else if completed.Valid {
			st.Finished++
			durations = append(durations, int(completed.Time.Sub(started).Seconds()))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(durations) > 0 {
		slices.Sort(durations)
		st.MedianSeconds = new(median(durations))
		st.FastestSeconds = new(durations[0])
		st.SlowestSeconds = new(durations[len(durations)-1])
	}
	if len(scores) > 0 {
		slices.Sort(scores)
		st.MedianScore = new(median(scores))
		st.TopScore = new(scores[len(scores)-1])
	}

	// Puzzles in graph order; the per-team node rows fold into them.
	at := map[string]int{}
	for _, n := range g.Nodes {
		if n.Type != graph.Puzzle {
			continue
		}
		title := n.Title
		if title == "" {
			title = n.Slug
		}
		at[n.Slug] = len(st.Puzzles)
		st.Puzzles = append(st.Puzzles, PuzzleStats{Node: n.Slug, Title: title, WrongAnswers: []GuessCount{}})
	}
	solveTimes := make([][]int, len(st.Puzzles))
	rows, err = h.DB.QueryContext(ctx, `
		SELECT node, first_seen_at, solved_at, attempts, hints_revealed
		FROM team_node_state WHERE team_id IN (`+statsTeams+`)`, eventID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			node            string
			seen            time.Time
			solved          sql.NullTime
			attempts, hints int
		)
		if err := rows.Scan(&node, &seen, &solved, &attempts, &hints); err != nil {
			rows.Close()
			return err
		}
		i, ok := at[node]
		if !ok {
			continue
		}
		p := &st.Puzzles[i]
		p.Reached++
		p.Wrong += attempts
		p.Hints += hints
		if solved.Valid {
			p.Solved++
			if attempts == 0 {
				p.FirstTry++
			}
			solveTimes[i] = append(solveTimes[i], int(solved.Time.Sub(seen).Seconds()))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range st.Puzzles {
		p := &st.Puzzles[i]
		if len(solveTimes[i]) > 0 {
			slices.Sort(solveTimes[i])
			p.MedianSeconds = new(median(solveTimes[i]))
		}
		st.Wrong += p.Wrong
		st.Solved += p.Solved
		st.FirstTry += p.FirstTry
		st.Hints += p.Hints
	}
	st.Guesses = st.Wrong + st.Solved

	rows, err = h.DB.QueryContext(ctx, `
		SELECT node, answer, n, teams FROM (
		  SELECT p->>'node' AS node, LOWER(TRIM(p->>'submitted')) AS answer,
		         COUNT(*) AS n, COUNT(DISTINCT team_id) AS teams,
		         ROW_NUMBER() OVER (PARTITION BY p->>'node' ORDER BY COUNT(*) DESC, LOWER(TRIM(p->>'submitted'))) AS rk
		  FROM (SELECT team_id, payload::jsonb AS p FROM event_activity
		        WHERE event_id = $1 AND kind = 'answer.wrong' AND team_id IN (`+statsTeams+`)) a
		  WHERE TRIM(COALESCE(p->>'submitted', '')) <> ''
		  GROUP BY 1, 2
		) x WHERE rk <= $2
		ORDER BY node, rk`, eventID, topWrongAnswers)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			node string
			gc   GuessCount
		)
		if err := rows.Scan(&node, &gc.Answer, &gc.Count, &gc.Teams); err != nil {
			rows.Close()
			return err
		}
		if i, ok := at[node]; ok {
			st.Puzzles[i].WrongAnswers = append(st.Puzzles[i].WrongAnswers, gc)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM team_photos WHERE event_id = $1 AND team_id IN (`+statsTeams+`)`, eventID).Scan(&st.Photos); err != nil {
		return err
	}

	// Each team's walk is the haversine sum of its pings in order, as on
	// its recap, leaving out GPS jumps (a step faster than
	// play.MaxPingSpeedMS, judged over at least 5 s as the anti-teleport
	// rule is); the average is over the teams that sent any.
	var (
		dist  sql.NullFloat64
		walks int
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT SUM(d) FILTER (WHERE d <= $2 * GREATEST(secs, 5)),
		       COUNT(DISTINCT team_id)
		FROM (
		  SELECT team_id,
		         2 * 6371000 * ASIN(LEAST(1, SQRT(
		           POWER(SIN(RADIANS(lat - plat) / 2), 2) +
		           COS(RADIANS(lat)) * COS(RADIANS(plat)) * POWER(SIN(RADIANS(lng - plng) / 2), 2)))) AS d,
		         EXTRACT(EPOCH FROM server_ts::timestamp - pts::timestamp) AS secs
		  FROM (
		    SELECT team_id, lat, lng, server_ts, LAG(lat) OVER w AS plat, LAG(lng) OVER w AS plng, LAG(server_ts) OVER w AS pts
		    FROM location_pings
		    WHERE event_id = $1 AND team_id IN (`+statsTeams+`)
		    WINDOW w AS (PARTITION BY team_id ORDER BY server_ts, id)
		  ) x
		) y`, eventID, play.MaxPingSpeedMS).Scan(&dist, &walks); err != nil {
		return err
	}
	if walks > 0 {
		st.AvgDistanceM = new(dist.Float64 / float64(walks))
	}
	return nil
}

// median of a sorted, non-empty slice; the lower middle for an even count.
func median[T int | float64](s []T) T { return s[(len(s)-1)/2] }
