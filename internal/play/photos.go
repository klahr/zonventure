package play

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/klahr/zonventure/internal/results"

	"github.com/klahr/zonventure/internal/graph"
)

// EventGraph is the graph an event runs on, as pinned when it started.
// ErrNoGraph before the start.
func (e *Engine) EventGraph(ctx context.Context, q Querier, eventID int64) (*graph.Graph, error) {
	var id sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT graph_id FROM events WHERE id = $1`, eventID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !id.Valid {
		return nil, ErrNoGraph
	}
	if err != nil {
		return nil, err
	}
	return e.graph(ctx, q, id.Int64)
}

// AdjustScore adds delta to a team's score from outside the game: the
// points an organiser's verdict on a photo gives or takes back. The
// UPDATE takes the same row lock an action's load does, so it lands
// before or after a concurrent action, never inside one.
func AdjustScore(ctx context.Context, tx *sql.Tx, teamID int64, delta float64) error {
	if delta == 0 {
		return nil
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE team_graph_state SET score = score + $1, updated_at = now() WHERE team_id = $2`, delta, teamID)
	if err != nil {
		return fmt.Errorf("adjusting score: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoGraph
	}
	_, err = results.AppendTeam(ctx, tx, teamID, results.ScoreAdjusted, map[string]any{"delta": delta, "reason": "photo verdict"})
	return err
}
