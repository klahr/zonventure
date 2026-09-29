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
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Rescue(node) })
}
