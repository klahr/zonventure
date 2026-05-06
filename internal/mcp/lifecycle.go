package mcp

import (
	"context"
	"encoding/json"
)

// describe_event_lifecycle is a self-contained reference document
// the model can fetch once at the start of an event-running task to
// learn the state machine and which MCP tool drives which arrow.
// Static — no DB lookup, no admin call. Cheap to call repeatedly.
//
// The text mirrors internal/admin/eventstate.go::allowedForState,
// which is the canonical state matrix. If you change that matrix,
// keep this string in sync — there is a registration-level test
// that asserts every state name and every transition tool appears.

const eventLifecycleDoc = `# Event lifecycle

An event moves through five named states. Most authoring happens in **scheduled**; the live action is in **running**; **paused** is a held breath; **completed** and **cancelled** are terminal.

` + "```" + `
                  start_event           pause_event
   scheduled ────────────────► running ◄──────────► paused
       │                          │                    │
       │                          │ (auto on last      │
       │                          │  team finishing)   │
       │                          ▼                    │
       │                      completed                │
       │                                               │
       └────────── cancel_event ───┬───────────────────┘
                                   ▼
                              cancelled
` + "```" + `

## Tools that drive the transitions

| From → To              | Tool          | Notes                                                                  |
|------------------------|---------------|------------------------------------------------------------------------|
| (none) → scheduled     | create_event  | Adds a fresh row in state ` + "`scheduled`" + `.                                  |
| scheduled → running    | start_event   | Requires at least one team. Initialises lane permutations.             |
| running → paused       | pause_event   | Players see a "paused" banner; answers/movement aren't recorded.       |
| paused → running       | resume_event  | Same elapsed clock; team progress untouched.                           |
| running → completed    | (automatic)   | When the last non-withdrawn team finishes its lane.                    |
| any non-terminal → cancelled | cancel_event | Allowed from scheduled, running, paused. Requires the user's password. |

## What's editable in each state

Everything below is enforced inside the admin layer at the relevant tool — MCP just surfaces the same 409 envelope the web UI sees.

- **scheduled**: full edit — reschedule, create / rename / delete teams, add / remove / rotate slots, set avatars.
- **running** / **paused**: operational tweaks only — create / rename teams, set avatars, add / rotate (NOT remove) slots, unlock_team, withdraw_team. No reschedule, no team deletion.
- **completed** / **cancelled**: read-only by default. The single exception is editing a team's contact email (so the recap email can still go out).

## Observation while running

The MCP surface has no streaming. To watch a live event, poll:

- ` + "`get_event_state_summary`" + ` every 30–60s as a heartbeat (state, completed_count, top-5 leaderboard).
- ` + "`list_activity`" + ` after a state change to see what happened (newest first).
- ` + "`list_positions`" + ` every 10–30s if you need GPS detail (only meaningful while running).
- ` + "`list_tracks`" + ` sparingly — payload grows linearly with event duration.

## Full tool index by phase

| Phase            | Tools                                                                                                   |
|------------------|----------------------------------------------------------------------------------------------------------|
| Authoring        | list_adventures · get_adventure · validate_adventure · create_adventure · update_adventure_meta · delete_adventure · {add,update,delete}_puzzle · move_puzzle · {create,update,delete,reorder}_lane · set_lane_mode · {attach,detach}_image · {attach,detach}_audio · update_image_alt · detach_avatar · {add,update,delete}_hint |
| Lifecycle        | list_events · get_event · create_event · update_event · start_event · pause_event · resume_event · cancel_event |
| Teams + slots    | create_team · bulk_create_teams · update_team · delete_team · withdraw_team · unwithdraw_team · unlock_team · add_slot · remove_slot · rotate_slot_token |
| Observation      | list_positions · list_activity · list_tracks · get_event_state_summary                                  |
| Billing          | list_purchases · get_purchase · create_purchase (simulated only) · confirm_purchase · grant_slots_to_event (admin) |
| Invitations      | send_event_invitations · send_team_invitation · get_event_invite_ics                                    |
| Results          | export_event_results_csv (CSV inline) · describe_event_lifecycle (this tool)                            |

## Security boundaries

- ` + "`cancel_event`" + ` and ` + "`delete_team`" + ` require the caller's password (passed as ` + "`password`" + `, forwarded as X-Confirm-Password). The MCP token alone cannot cancel or destroy.
- ` + "`grant_slots_to_event`" + ` is admin-only and refused at the MCP layer when the token's user isn't a global admin.
- ` + "`create_purchase`" + ` is hard-coded to provider="simulated" and refuses on Stripe-enabled deployments — real-money buys go through the web checkout.
`

func (s *Server) registerLifecycleTool() {
	s.tools.register(ToolDescriptor{
		Name:        "describe_event_lifecycle",
		Scope:       scopeRead,
		Description: "Return a Markdown reference covering the event state machine, which MCP tool drives each transition, what's editable in each state, observation polling cadence, and the full tool index by phase. Useful as a one-shot fetch at the start of an event-running task so the model can plan transitions without trial-and-error.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, describeEventLifecycle)
}

func describeEventLifecycle(_ context.Context, _ *Server, _ json.RawMessage) (ToolCallResult, error) {
	return textResult(eventLifecycleDoc), nil
}
