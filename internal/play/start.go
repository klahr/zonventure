package play

import (
	"context"
	"database/sql"
)

// Start starts an event: it pins the adventure's published graph and
// places every team. An adventure with nothing published is ErrNoGraph.
//
// Every place that moves an event to running calls this — the arranger's
// start button, the scheduler, and a player opening a due event — so no
// path can start an event without pinning its graph.
func Start(ctx context.Context, db *sql.DB, eventID int64) error {
	return New(db).StartEvent(ctx, eventID)
}

// TeamBegun reports whether a team has made its first move: left a step
// other than a start, or finished. Until then its players may still
// name themselves and the team and choose its picture; after, those are
// fixed for them, so the leaderboard, the organisers' view and the recap
// keep one name throughout. Placement alone does not count — a
// self-guided team is placed the moment its link is first opened.
func TeamBegun(ctx context.Context, q Querier, teamID int64) (bool, error) {
	var begun bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS (
		         SELECT 1 FROM team_graph_state WHERE team_id = $1 AND completed_at IS NOT NULL
		       ) OR EXISTS (
		         SELECT 1
		         FROM team_node_state n
		         JOIN team_graph_state s ON s.team_id = n.team_id
		         JOIN adventure_graphs g ON g.id = s.graph_id
		         WHERE n.team_id = $1 AND n.outcome IS NOT NULL
		           AND NOT EXISTS (
		             SELECT 1 FROM jsonb_array_elements(g.graph -> 'nodes') x
		             WHERE x ->> 'slug' = n.node AND x ->> 'type' = 'start'
		           )
		       )
	`, teamID).Scan(&begun)
	return begun, err
}
