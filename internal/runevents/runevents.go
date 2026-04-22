// Package runevents is a tiny helper shared by admin and player handlers
// for appending rows to the run_events activity feed.
package runevents

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Event kind strings. Centralised so handlers stay consistent and the
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

// Actor picks which actor column to write. Exactly one of User/Slot should
// be non-nil; both nil means "no actor recorded".
type Actor struct {
	User *int64
	Slot *int64
}

// UserActor is a shorthand helper.
func UserActor(id int64) Actor { return Actor{User: &id} }

// SlotActor is a shorthand helper.
func SlotActor(id int64) Actor { return Actor{Slot: &id} }

// Record appends one row. Errors are swallowed — telemetry should never
// fail a user action.
func Record(ctx context.Context, db *sql.DB, runID int64, teamID *int64, actor Actor, kind string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil || payload == nil {
		b = []byte(`{}`)
	}
	var tid, uid, sid any
	if teamID != nil {
		tid = *teamID
	}
	if actor.User != nil {
		uid = *actor.User
	}
	if actor.Slot != nil {
		sid = *actor.Slot
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO run_events(run_id, team_id, actor_user_id, actor_slot_id, kind, payload)
		VALUES (?, ?, ?, ?, ?, ?)
	`, runID, tid, uid, sid, kind, string(b))
}
