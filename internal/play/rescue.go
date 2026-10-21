package play

import (
	"context"

	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// Rescue moves a team past the puzzle it is stuck on, for the arranger.
// See session.Team.Rescue; node guards against a team that moved on
// while the arranger was deciding.
func (e *Engine) Rescue(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.RescueGroup(ctx, teamID, node, "")
}

// RescueGroup moves one group of a split team on — the group with key,
// standing on node. See session.Team.RescueGroup.
func (e *Engine) RescueGroup(ctx context.Context, teamID int64, node, group string) (Result, error) {
	return e.act(ctx, teamID, action{"rescue", node, group}, func(t *session.Team) (Result, *walk.Step, error) { return t.RescueGroup(node, group) })
}
