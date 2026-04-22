// Package runevents is a tiny helper shared by admin and player handlers
// for appending rows to the run_events activity feed.
package runevents

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Event kind strings. Kept centralised so handlers stay consistent and the
// frontend can reliably switch on them.
const (
	RunCreated    = "run.created"
	RunStarted    = "run.started"
	RunPaused     = "run.paused"
	RunResumed    = "run.resumed"
	TeamCreated   = "team.created"
	TeamRenamed   = "team.renamed"
	TeamDeleted   = "team.deleted"
	MemberAdded   = "team.member.added"
	MemberRemoved = "team.member.removed"
	AnswerWrong   = "answer.wrong"
	AnswerRight   = "answer.right"
	TeamCompleted = "team.completed"
	TeamUnlocked  = "team.unlocked"
	GateReached   = "gate.reached"
)

// Record appends one row to run_events. Errors are swallowed — activity
// telemetry should never fail the user's action.
func Record(ctx context.Context, db *sql.DB, runID int64, teamID *int64, actorID *int64, kind string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil || payload == nil {
		b = []byte(`{}`)
	}
	var tid, aid any
	if teamID != nil {
		tid = *teamID
	}
	if actorID != nil {
		aid = *actorID
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO run_events(run_id, team_id, actor_id, kind, payload)
		VALUES (?, ?, ?, ?, ?)
	`, runID, tid, aid, kind, string(b))
}
