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
	RunCompleted  = "run.completed"
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

// MaybeCompleteRun flips adventure_runs.state to 'completed' (stamping
// completed_at) when every team in the run has its own completed_at
// set. Returns (true, nil) when the transition happened — callers
// should publish a hub notification in that case so live/arrange
// subscribers re-render immediately.
//
// Semantics:
//
//   - A run with zero teams is never auto-completed; an empty run
//     has nothing to finish and doing so would be surprising.
//   - Runs in 'scheduled', 'completed', or unknown states are
//     untouched; only 'running' / 'paused' are promoted, so an
//     arranger who force-unlocks teams during a pause still gets the
//     expected finish.
//   - On a lost race (two callers observing "all done" at the same
//     instant) the UPDATE's state predicate ensures exactly one
//     transition fires; the loser returns (false, nil).
func MaybeCompleteRun(ctx context.Context, db *sql.DB, runID int64) (bool, error) {
	var total, done int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       SUM(CASE WHEN completed_at IS NULL THEN 0 ELSE 1 END)
		FROM teams WHERE run_id = ?
	`, runID).Scan(&total, &done); err != nil {
		return false, err
	}
	if total == 0 || done < total {
		return false, nil
	}
	res, err := db.ExecContext(ctx, `
		UPDATE adventure_runs
		SET state = 'completed', completed_at = datetime('now')
		WHERE id = ? AND state IN ('running','paused')
	`, runID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	Record(ctx, db, runID, nil, Actor{}, RunCompleted, nil)
	return true, nil
}
