package play

import (
	"context"

	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// RescueGroup moves a team past the step it is stuck on, for the
// organisers — or, given group, that group of a split team. node guards
// against a team that moved on while they were deciding. See
// session.Team.RescueGroup.
func (e *Engine) RescueGroup(ctx context.Context, teamID int64, node, group string) (Result, error) {
	return e.act(ctx, teamID, action{"rescue", node, group}, func(t *session.Team) (Result, *walk.Step, error) { return t.RescueGroup(node, group) })
}
