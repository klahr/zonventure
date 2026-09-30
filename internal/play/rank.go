package play

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/klahr/zonventure/internal/graph"
)

// Ranked is one team in its event's ranking.
type Ranked struct {
	TeamID int64
	Name   string
	Score  float64
	// Done is whether the team has finished, and CompletedAt when.
	Done        bool
	CompletedAt time.Time
	// Guess is the team's tie-breaker answer, Miss how far off it is:
	// +Inf without one.
	Guess *float64
	Miss  float64
}

// Ahead is the one ranking rule, for the standings block, the live
// leaderboard and the results export alike: more points first; between
// finished teams on the same points, the closer tie-break guess, then the
// earlier finish. Unfinished teams on the same points are level — a
// tie-break used mid-event would hint at the answer.
func Ahead(a, b Ranked) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if !a.Done || !b.Done {
		return false
	}
	if a.Miss != b.Miss {
		return a.Miss < b.Miss
	}
	return a.CompletedAt.Before(b.CompletedAt)
}

// SortRanking lists teams in Ahead's order. Ahead leaves teams level,
// which a sort cannot use as it stands — level is not transitive — so
// on the same points finished teams list first, then by Ahead's
// tie-breaks, then by name.
func SortRanking(rs []Ranked) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		switch {
		case a.Score != b.Score:
			return a.Score > b.Score
		case a.Done != b.Done:
			return a.Done
		case a.Done && a.Miss != b.Miss:
			return a.Miss < b.Miss
		case a.Done && !a.CompletedAt.Equal(b.CompletedAt):
			return a.CompletedAt.Before(b.CompletedAt)
		}
		return a.Name < b.Name
	})
}

// EventRanking ranks a graph event's teams that have started, leaving
// out withdrawn ones.
func (e *Engine) EventRanking(ctx context.Context, eventID int64) ([]Ranked, error) {
	var graphID sql.NullInt64
	if err := e.DB.QueryRowContext(ctx, `SELECT graph_id FROM events WHERE id = $1`, eventID).Scan(&graphID); err != nil {
		return nil, err
	}
	var g *graph.Graph
	if graphID.Valid {
		var err error
		if g, err = e.graph(ctx, e.DB, graphID.Int64); err != nil {
			return nil, err
		}
	}
	return ranking(ctx, e.DB, g, eventID)
}

func ranking(ctx context.Context, q Querier, g *graph.Graph, eventID int64) ([]Ranked, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT t.id, t.name, s.score - s.penalty_score, s.completed_at
		FROM team_graph_state s JOIN teams t ON t.id = s.team_id
		WHERE s.event_id = $1 AND t.withdrawn_at IS NULL AND NOT s.cheated`, eventID)
	if err != nil {
		return nil, fmt.Errorf("reading scores: %w", err)
	}
	defer rows.Close()
	var out []Ranked
	for rows.Next() {
		r := Ranked{Miss: math.Inf(1)}
		var completed sql.NullTime
		if err := rows.Scan(&r.TeamID, &r.Name, &r.Score, &completed); err != nil {
			return nil, err
		}
		r.Done, r.CompletedAt = completed.Valid, completed.Time
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if g != nil {
		if node, block, target, ok := tiebreakOf(g); ok {
			guesses, err := tiebreakGuesses(ctx, q, eventID, node, block)
			if err != nil {
				return nil, err
			}
			for i := range out {
				if f, ok := guesses[out[i].TeamID]; ok {
					out[i].Guess, out[i].Miss = &f, math.Abs(f-target)
				}
			}
		}
	}
	SortRanking(out)
	return out, nil
}

// tiebreakOf is the adventure's tie-breaker, if it has one.
func tiebreakOf(g *graph.Graph) (node, block string, target float64, ok bool) {
	for _, n := range g.Nodes {
		for _, b := range n.Blocks() {
			if b.Kind == graph.BlockTiebreak && b.Target != nil {
				return n.Slug, b.ID, *b.Target, true
			}
		}
	}
	return "", "", 0, false
}

// tiebreakGuesses is each team's tie-breaker answer.
func tiebreakGuesses(ctx context.Context, q Querier, eventID int64, node, block string) (map[int64]float64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT p.team_id, p.choice FROM team_block_picks p JOIN teams t ON t.id = p.team_id
		WHERE t.event_id = $1 AND p.node = $2 AND p.block_id = $3`, eventID, node, block)
	if err != nil {
		return nil, fmt.Errorf("reading tie-breaks: %w", err)
	}
	defer rows.Close()
	out := map[int64]float64{}
	for rows.Next() {
		var id int64
		var choice string
		if err := rows.Scan(&id, &choice); err != nil {
			return nil, err
		}
		if f, err := strconv.ParseFloat(choice, 64); err == nil {
			out[id] = f
		}
	}
	return out, rows.Err()
}
