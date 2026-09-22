package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
)

// eventOp is the set of mutating operations the arranger surface
// exposes against an event or its teams. State-aware gating lives in
// allowedForState below; every mutating handler funnels through
// requireEventOp so the matrix is enforced in exactly one place.
type eventOp string

const (
	OpRescheduleEvent eventOp = "reschedule_event"
	OpCreateTeam      eventOp = "create_team"
	OpRenameTeam      eventOp = "rename_team"
	OpDeleteTeam      eventOp = "delete_team"
	OpSetTeamAvatar   eventOp = "set_team_avatar"
	OpAddSlot         eventOp = "add_slot"
	OpRemoveSlot      eventOp = "remove_slot"
	OpRotateSlotToken eventOp = "rotate_slot_token"
	OpUnlockTeam      eventOp = "unlock_team"
	// OpWithdrawTeam soft-flags a team that gave up mid-event so it
	// stops counting as "stuck" and gets its own "Withdrew" group on
	// the leaderboard + recap. Reversible via the same op (toggle).
	// Allowed only while a result-affecting state is in flight —
	// post-completion the standings are frozen for honest reporting.
	OpWithdrawTeam    eventOp = "withdraw_team"
	// OpEditTeamEmail is the captain-contact email edit. Distinct
	// from OpRenameTeam because email is purely arranger-side
	// metadata (never surfaced on the player share page) and an
	// arranger frequently needs to fill it in after the event has
	// completed — to send the recap link, follow up about a lost
	// item, etc. Allowed in every state including completed and
	// cancelled.
	OpEditTeamEmail eventOp = "edit_team_email"
)

// allOps is the canonical list — used to render allowed_ops on the
// wire so the client knows the universe of ops it can ask about.
var allOps = []eventOp{
	OpRescheduleEvent,
	OpCreateTeam,
	OpRenameTeam,
	OpDeleteTeam,
	OpSetTeamAvatar,
	OpAddSlot,
	OpRemoveSlot,
	OpRotateSlotToken,
	OpUnlockTeam,
	OpWithdrawTeam,
	OpEditTeamEmail,
}

// allowedForState is the single source of truth for what's editable
// at each lifecycle point. Keep this exhaustive: a missing (state, op)
// pair defaults to disallowed so a forgotten case fails closed.
//
//   scheduled — fully editable, nothing has happened yet.
//   running   — operational tweaks only; the structure is locked.
//   paused    — same as running.
//   completed — read-only by default. Renames of the *event* itself
//               and show_map happen via UpdateEvent which doesn't
//               consult this matrix; everything that would mutate the
//               leaderboard or team roster is blocked.
//   cancelled — terminal & read-only. Same posture as completed; kept
//               distinct so the UI and the activity log can show a
//               truthful label instead of pretending the event finished.
func allowedForState(state string, op eventOp) bool {
	// OpEditTeamEmail is the lone exception to the "terminal states
	// are read-only" rule — see the const doc-comment. Email is
	// arranger-only metadata; permitting late edits doesn't affect
	// any player-facing surface or audit-able outcome.
	if op == OpEditTeamEmail {
		return true
	}
	switch state {
	case "scheduled":
		switch op {
		case OpRescheduleEvent, OpCreateTeam, OpRenameTeam, OpDeleteTeam,
			OpSetTeamAvatar, OpAddSlot, OpRemoveSlot, OpRotateSlotToken:
			return true
		}
	case "running", "paused":
		switch op {
		case OpCreateTeam, OpRenameTeam, OpSetTeamAvatar,
			OpAddSlot, OpRotateSlotToken, OpUnlockTeam, OpWithdrawTeam:
			return true
		}
	case "completed", "cancelled":
		return false
	}
	return false
}

// allowedOpsForState lists every operation permitted at the given
// state, as wire-format strings. Surfaced in event-detail responses
// so the client can disable affordances that the server would 409.
func allowedOpsForState(state string) []string {
	out := make([]string, 0, len(allOps))
	for _, op := range allOps {
		if allowedForState(state, op) {
			out = append(out, string(op))
		}
	}
	return out
}

// reasonForState gives the wire-stable reason code returned with a
// 409. The frontend shouldn't be parsing the human text — this lets
// it map to a localized message without hardcoding strings.
func reasonForState(state string) string {
	switch state {
	case "running", "paused":
		return "event_active"
	case "completed":
		return "event_completed"
	case "cancelled":
		return "event_cancelled"
	}
	return "event_invalid_state"
}

// requireEventOp enforces the lifecycle matrix. Returns true when the
// op is allowed; otherwise writes a 409 with a stable reason field
// and returns false. Callers should `return` immediately on false.
func (h *Handlers) requireEventOp(w http.ResponseWriter, ctx context.Context, eventID int64, op eventOp) bool {
	var state string
	if err := h.DB.QueryRowContext(ctx,
		`SELECT state FROM events WHERE id = $1`, eventID).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "event not found")
			return false
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return false
	}
	if !allowedForState(state, op) {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":  "operation not allowed in state " + state,
			"reason": reasonForState(state),
			"state":  state,
			"op":     string(op),
		})
		return false
	}
	return true
}

// teamEventOp resolves the team's event id, then runs requireEventOp.
// Saves a roundtrip's worth of glue in every team-scoped handler.
func (h *Handlers) teamEventOp(w http.ResponseWriter, ctx context.Context, teamID int64, op eventOp) bool {
	var eventID int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT event_id FROM teams WHERE id = $1`, teamID).Scan(&eventID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "team not found")
			return false
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return false
	}
	return h.requireEventOp(w, ctx, eventID, op)
}
