// Package activity is a tiny helper shared by admin and player handlers
// for appending rows to the event_activity feed (per-event lifecycle and
// gameplay log).
package activity

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Execer is the read+write subset of *sql.DB that Record and
// MaybeCompleteEvent need. Both *sql.DB and *sql.Tx satisfy it, so a
// caller can pass an open transaction to make telemetry +
// event-completion checks part of the same atomic unit as the rest of
// their writes — the typical pattern in player.applyAnswer where
// answer_attempts INSERT and teams UPDATE must commit together with
// their activity rows.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Activity kind strings. Centralised so handlers stay consistent and
// the frontend can reliably switch on them. The "Event*" prefix here
// refers to the top-level scheduled-adventure entity (the Event); not
// to be confused with webhook *delivery* events, which is a separate
// concept layered on top.
const (
	EventCreated   = "event.created"
	EventStarted   = "event.started"
	EventPaused    = "event.paused"
	EventResumed   = "event.resumed"
	EventCompleted = "event.completed"
	EventCancelled = "event.cancelled"
	TeamCreated    = "team.created"
	TeamRenamed    = "team.renamed"
	TeamDeleted    = "team.deleted"
	MemberAdded    = "team.member.added"
	MemberRemoved  = "team.member.removed"
	AnswerWrong    = "answer.wrong"
	AnswerRight    = "answer.right"
	TeamCompleted  = "team.completed"
	TeamUnlocked   = "team.unlocked"
	GateReached    = "gate.reached"
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
// fail a user action. db may be a *sql.DB or a *sql.Tx; callers that
// already hold a transaction should pass it so the activity row commits
// or rolls back with the rest of their writes.
func Record(ctx context.Context, db Execer, eventID int64, teamID *int64, actor Actor, kind string, payload any) {
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
		INSERT INTO event_activity(event_id, team_id, actor_user_id, actor_slot_id, kind, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, eventID, tid, uid, sid, kind, string(b))
}

// MaybeCompleteEvent flips events.state to 'completed' (stamping
// completed_at) when every team in the event has its own completed_at
// set. Returns (true, nil) when the transition happened — callers
// should publish a hub notification in that case so live/arrange
// subscribers re-render immediately.
//
// Semantics:
//
//   - An event with zero teams is never auto-completed; an empty
//     event has nothing to finish and doing so would be surprising.
//   - Events in 'scheduled', 'completed', or unknown states are
//     untouched; only 'running' / 'paused' are promoted, so an
//     arranger who force-unlocks teams during a pause still gets the
//     expected finish.
//   - On a lost race (two callers observing "all done" at the same
//     instant) the UPDATE's state predicate ensures exactly one
//     transition fires; the loser returns (false, nil).
func MaybeCompleteEvent(ctx context.Context, db Execer, eventID int64) (bool, error) {
	var total, done int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       SUM(CASE WHEN completed_at IS NULL THEN 0 ELSE 1 END)
		FROM teams WHERE event_id = $1
	`, eventID).Scan(&total, &done); err != nil {
		return false, err
	}
	if total == 0 || done < total {
		return false, nil
	}
	res, err := db.ExecContext(ctx, `
		UPDATE events
		SET state = 'completed', completed_at = now_text()
		WHERE id = $1 AND state IN ('running','paused')
	`, eventID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	Record(ctx, db, eventID, nil, Actor{}, EventCompleted, nil)
	return true, nil
}
