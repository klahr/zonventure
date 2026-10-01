package play

import (
	"context"
	"database/sql"
	"errors"
)

// StartIfGraph starts an event on the graph engine when its adventure
// has a graph, and reports whether it did. An adventure without one is
// not an error: it runs on lanes, exactly as before.
//
// Every place that moves an event to running calls this — the arranger's
// start button, the scheduler, and a player opening a due event — so no
// path can start a graph adventure without pinning its graph.
func StartIfGraph(ctx context.Context, db *sql.DB, eventID int64) (bool, error) {
	err := New(db).StartEvent(ctx, eventID)
	if errors.Is(err, ErrNoGraph) {
		return false, nil
	}
	return err == nil, err
}

// UsesGraph reports whether an event is played on the graph engine.
//
// Once an event has started the answer is fixed: it is whatever was
// pinned at the start. Before that it follows the adventure, so a player
// who opens their link early already lands in the right app. An event
// that started on lanes stays on lanes even if its adventure gains a
// graph later — switching a running event's engine would strand every
// team mid-game.
func UsesGraph(ctx context.Context, q Querier, eventID int64) (bool, error) {
	var pinned, hasGraph bool
	var state string
	err := q.QueryRowContext(ctx, `
		SELECT e.graph_id IS NOT NULL, e.state,
		       EXISTS (SELECT 1 FROM adventures a
		               WHERE a.slug = e.adventure_slug AND a.published_graph_id IS NOT NULL)
		FROM events e WHERE e.id = $1
	`, eventID).Scan(&pinned, &state, &hasGraph)
	if err != nil {
		return false, err
	}
	return pinned || (state == "scheduled" && hasGraph), nil
}

// TeamBegun reports whether a team has made its first move: left a step
// other than a start, or finished. Until then its players may still
// name themselves and the team and choose its picture; after, those are
// fixed for them, so the leaderboard, the organisers' view and the recap
// keep one name throughout. Placement alone does not count — a
// self-guided team is placed the moment its link is first opened. A
// team on the lane engine has no graph state, so it never reads as begun.
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
