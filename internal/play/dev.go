package play

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klahr/zonventure/internal/graph"
)

// A dev team plays the adventure's draft as it is edited (graphstore's
// dev row), for an author trying changes on site. It may be put on any
// step and started over: what a real team may never do.

// ErrNotDev refuses a dev move to a team in a real event.
var ErrNotDev = errors.New("not a dev team")

// ErrNoSuchStep refuses a dev move to a step the draft does not have, or
// one a team never sees.
var ErrNoSuchStep = errors.New("no such step to show")

// devEvent returns the event of a team, refusing one that is not a dev event.
func devEvent(ctx context.Context, q Querier, teamID int64) (int64, error) {
	var (
		eventID int64
		dev     bool
	)
	err := q.QueryRowContext(ctx, `SELECT ev.id, ev.dev FROM teams t JOIN events ev ON ev.id = t.event_id WHERE t.id = $1`, teamID).Scan(&eventID, &dev)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !dev {
		return 0, ErrNotDev
	}
	return eventID, err
}

// DevMoveTo puts a dev team on a step — back to one it has played, or on
// to one it has not reached — with that step's own progress forgotten,
// so it plays afresh: its attempts, hints and outcome.
func (e *Engine) DevMoveTo(ctx context.Context, teamID int64, node string) error {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := devEvent(ctx, tx, teamID); err != nil {
		return err
	}
	s, err := e.loadCore(ctx, tx, teamID, true)
	if err != nil {
		return err
	}
	n, ok := s.G.Node(node)
	if !ok || !Visible(n.Type) {
		return ErrNoSuchStep
	}
	now := e.now()
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_node_state WHERE team_id = $1 AND node = $2`, teamID, node); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE team_graph_state
		SET current_node = $2, node_entered_at = $3, completed_at = NULL, outcome = NULL, updated_at = $3
		WHERE team_id = $1
	`, teamID, node, now); err != nil {
		return err
	}
	return tx.Commit()
}

// devProgress lists the tables that hold a team's progress, which a
// start over clears. Its roster, its tokens and the event's log stay.
var devProgress = []string{
	"team_node_state", "team_hub_branches", "team_items", "team_item_holders", "item_gifts",
	"team_journal", "team_block_picks", "team_block_votes", "team_presses", "team_roles",
	"team_secrets", "team_photos", "answer_attempts", "graph_effect_outbox", "team_graph_state",
}

// DevReset starts a dev team over: its progress forgotten, placed again
// as at the start of an event — at the start screen, or on the first step.
func (e *Engine) DevReset(ctx context.Context, teamID int64) error {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	eventID, err := devEvent(ctx, tx, teamID)
	if err != nil {
		return err
	}
	for _, table := range devProgress {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE team_id = $1`, table), teamID); err != nil {
			return fmt.Errorf("clearing %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return e.StartEvent(ctx, eventID)
}

// Visible reports whether teams see a step of this type: the ones a dev
// team may be put on.
func Visible(t graph.Type) bool {
	return t == graph.Puzzle || t == graph.Story || t == graph.Hub || t == graph.End
}
