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
       │                          │  team finishing,   │
       │                          │  or end_event) ◄───┤
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
| (none) → scheduled     | create_event  | Adds a fresh row in state ` + "`scheduled`" + `. Pass ` + "`public_for_sale: true`" + ` to mark it as a Quest. |
| scheduled → running    | start_event   | Requires at least one team for regular events. **Quests bypass this guard** — they're started empty, with teams minted from ticket purchases at /store. |
| running → paused       | pause_event   | Players see a "paused" banner; answers/movement aren't recorded. For Quests, pausing also delists from /store (no new ticket sales) without ending the quest. |
| paused → running       | resume_event  | Same elapsed clock; team progress untouched.                           |
| running → completed    | (automatic)   | When the last non-withdrawn team finishes.                             |
| running / paused → completed | end_event | End now: unfinished teams keep their progress, results and recaps open, players are signed out. Requires the user's password. |
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

| Phase            | Tools |
|------------------|-------|
| Adventures       | list_adventures · get_adventure · create_adventure · update_adventure_meta · set_adventure_icon · remove_adventure_icon · list_org_themes · delete_adventure |
| Steps            | get_authoring_guide · get_adventure_graph · get_step · edit_adventure_graph · undo_adventure_graph_edit · save_adventure_graph · validate_adventure_graph · publish_adventure_graph · get_creator_position |
| With co-authors  | list_adventure_threads · comment_on_adventure · suggest_step_change · resolve_adventure_thread · set_step_status |
| Files            | get_upload_link (pictures, the icon, an event cover — from a phone too) · list_adventure_media · upload_adventure_media |
| Trying it        | create_dev_ticket · get_dev_screen · dev_go_to · dev_reset · simulate_adventure · playtest_start · playtest_act · playtest_end |
| Events           | list_events · get_event · create_event · update_event · start_event · pause_event · resume_event · end_event · cancel_event |
| Teams + players  | create_team · bulk_create_teams · update_team · delete_team · give_free_ticket · withdraw_team · unwithdraw_team · unlock_team · let_team_start_anywhere · add_slot · remove_slot · rotate_slot_token |
| Watching         | get_event_state_summary · list_activity · list_positions · list_tracks · send_event_message · list_event_messages · delete_event_message |
| Event passes     | list_purchases · get_purchase · create_purchase (simulated only) · confirm_purchase · grant_slots_to_event (admin) |
| Invitations      | send_event_invitations · send_team_invitation · get_event_invite_ics |
| Results          | export_event_results_csv · describe_event_lifecycle (this tool) |

## Security boundaries

- ` + "`end_event`" + `, ` + "`cancel_event`" + ` and ` + "`delete_team`" + ` require the caller's password (passed as ` + "`password`" + `, forwarded as X-Confirm-Password). The API token alone cannot cancel or destroy.
- ` + "`grant_slots_to_event`" + ` is admin-only and refused at the MCP layer when the token's user isn't a global admin.
- A global admin's token reaches only the operator tools (` + "`grant_slots_to_event`" + `, the purchase reads, this reference): authoring and running events belong to org users.
- ` + "`create_purchase`" + ` is hard-coded to provider="simulated" and refuses on Stripe-enabled deployments — real-money buys go through the web checkout.

## Self-guided adventures vs scheduled events

A self-guided adventure (a Quest) is an event with ` + "`public_for_sale: true`" + ` — set once at create_event, immutable thereafter. update_event refuses to flip the flag with a 409. Practical consequences for the LLM:

- A quest is listed at /store once it's in ` + "`running`" + ` or ` + "`paused`" + ` state, has a ticket price set and its listing is approved by a platform admin (` + "`listing_review`" + ` on get_event; a verified creator's are approved as saved, and an edit waits for review while the approved version stays up) (use update_event to set ` + "`ticket_price_cents`" + `, ` + "`ticket_currency`" + `, ` + "`ticket_team_size`" + `, ` + "`public_blurb`" + ` after create_event).
- Quests start with no teams. start_event won't 409 the way it does for regular events.
- To "delist" a quest temporarily, pause_event it. To kill it, cancel_event.
`

func (s *Server) registerLifecycleTool() {
	s.tools.register(ToolDescriptor{
		Name:        "describe_event_lifecycle",
		Audience:    audienceAny,
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
