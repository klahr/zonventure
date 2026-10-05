package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// Phase 2: teams + slots. Same thin-shim pattern as the event tools
// in events.go — every method dispatches into admin handlers via
// callAdmin so policy, audit, webhook and WS publish work runs in
// exactly one place.
//
// Skipped intentionally:
//   - import_teams (multipart roster file with rich formats: ODS, XLSX,
//     IOF XML). Multipart over MCP is a hassle and the rich formats are
//     a per-tournament-once flow. bulk_create_teams covers the JSON
//     path the LLM can synthesise from any source.
//   - team avatar attach/detach. Not part of the "run an event" core
//     path; an arranger that wants avatars can use the web UI.
//
// Read-side `list_teams` / `get_team` aren't here either — get_event
// already returns the team list with members, recap tokens and
// progress per team; the model uses that as the team source of truth.

func (s *Server) registerTeamTools() {
	s.tools.register(ToolDescriptor{
		Name:        "create_team",
		Scope:       scopeWrite,
		Description: "Create one team on an event together with its player slots (one slot per member name). Returns the team id, name, magic-link tokens for each slot, and the team's share token. Use bulk_create_teams when adding more than two or three teams in one go.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "name"},
			"properties": map[string]any{
				"event_id":     map[string]any{"type": "integer", "description": "Event the team joins."},
				"name":         map[string]any{"type": "string", "description": "Team display name. Must be unique within the event."},
				"member_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Player names; one slot is minted per name with a fresh magic-link token."},
				"email":        map[string]any{"type": "string", "description": "Optional contact address used for invitations and the recap email. Hidden from the player share page."},
			},
		},
	}, createTeam)

	s.tools.register(ToolDescriptor{
		Name:        "bulk_create_teams",
		Scope:       scopeWrite,
		Description: "Create up to 200 teams in one request. Per-team failures (duplicate name, slot-cap exceeded) come back as items in the results array — the call itself returns 200 with `created` / `failed` counts. Use after create_event when you have a roster ready.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "teams"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event the teams join."},
				"teams": map[string]any{
					"type":        "array",
					"description": "List of {name, member_names[]} entries. Each becomes one team with one slot per member.",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"name"},
						"properties": map[string]any{
							"name":         map[string]any{"type": "string"},
							"member_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
					},
				},
			},
		},
	}, bulkCreateTeams)

	s.tools.register(ToolDescriptor{
		Name:        "update_team",
		Scope:       scopeWrite,
		Description: "Patch a team's name and/or email. Only present fields are applied. Renaming is gated on the event's state — once the event is completed/cancelled, only email-only edits are allowed (so the recap email can still be sent to a freshly-discovered address).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team to update."},
				"name":    map[string]any{"type": "string", "description": "New team name."},
				"email":   map[string]any{"type": "string", "description": "New contact email; empty string clears it."},
			},
		},
	}, updateTeam)

	s.tools.register(ToolDescriptor{
		Name:        "delete_team",
		Scope:       scopeWrite,
		Description: "Remove a team and every slot it owns. Password-gated for the same reason cancel_event is — irreversible mid-event destruction shouldn't be possible from the API token alone.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id", "password"},
			"properties": map[string]any{
				"team_id":  map[string]any{"type": "integer", "description": "Team to delete."},
				"password": map[string]any{"type": "string", "description": "Caller's account password; forwarded as X-Confirm-Password."},
			},
		},
	}, deleteTeam)

	s.tools.register(ToolDescriptor{
		Name:        "withdraw_team",
		Scope:       scopeWrite,
		Description: "Soft-flag a team as withdrawn — they appear in the recap as 'didn't finish — withdrew' but their progress and history stay intact. Idempotent: a second call on an already-withdrawn team is a no-op.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team to withdraw."},
			},
		},
	}, withdrawTeam)

	s.tools.register(ToolDescriptor{
		Name:        "unwithdraw_team",
		Scope:       scopeWrite,
		Description: "Reverse a withdraw_team. Clears the withdrawn_at flag so the team competes again. Idempotent on a not-withdrawn team.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team to un-withdraw."},
			},
		},
	}, unwithdrawTeam)

	s.tools.register(ToolDescriptor{
		Name:        "unlock_team",
		Scope:       scopeWrite,
		Description: "Advance a stuck team past its current puzzle to the next one (or to completion if it was on the final puzzle). Used during a live event when a team can't progress and the arranger wants to keep them moving. Records an arranger-assist activity entry.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team to advance."},
			},
		},
	}, unlockTeam)

	s.tools.register(ToolDescriptor{
		Name:        "let_team_start_anywhere",
		Scope:       scopeWrite,
		Description: "Let a team begin the adventure wherever it stands, without being at the adventure's required starting point — for a team whose phone's GPS will not settle, or when the meeting point moved on the day. Works before the team opens its link or while it waits at the start screen; refused once it has finished. Records an arranger-assist activity entry.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team to let start."},
			},
		},
	}, letTeamStartAnywhere)

	s.tools.register(ToolDescriptor{
		Name:        "add_slot",
		Scope:       scopeWrite,
		Description: "Add a player to a team: a new personal link (magic-link token) for them to play on their own phone. The event's player cap counts every team's players.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id", "name"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team the slot joins."},
				"name":    map[string]any{"type": "string", "description": "Slot/member display name."},
			},
		},
	}, addSlot)

	s.tools.register(ToolDescriptor{
		Name:        "remove_slot",
		Scope:       scopeWrite,
		Description: "Remove a player (slot_id, from get_event) from a team; their link stops working at once. Not while the event runs.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id", "slot_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team owning the slot."},
				"slot_id": map[string]any{"type": "integer", "description": "Slot to delete."},
			},
		},
	}, removeSlot)

	s.tools.register(ToolDescriptor{
		Name:        "rotate_slot_token",
		Scope:       scopeWrite,
		Description: "Give a player (slot_id, from get_event) a new personal link, the old one stopping at once — when a link leaked or a player changed phones.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id", "slot_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team owning the slot."},
				"slot_id": map[string]any{"type": "integer", "description": "Slot to rotate."},
			},
		},
	}, rotateSlotToken)
}

/* ---------------- handlers ---------------- */

type teamIDArgs struct {
	TeamID int64 `json:"team_id"`
}

type slotArgs struct {
	TeamID int64 `json:"team_id"`
	SlotID int64 `json:"slot_id"`
}

type createTeamArgs struct {
	EventID     int64    `json:"event_id"`
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
	Email       string   `json:"email"`
}

func createTeam(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a createTeamArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if a.Name == "" {
		return errorResult("name required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	body := map[string]any{
		"name":         a.Name,
		"member_names": a.MemberNames,
		"email":        a.Email,
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, respBody, err := callAdmin(ctx, s.Admin.CreateTeam,
		"POST", "/api/admin/events/"+id+"/teams",
		map[string]string{"eventID": id}, nil, body)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, respBody), nil
}

type bulkTeam struct {
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
}

type bulkCreateTeamsArgs struct {
	EventID int64      `json:"event_id"`
	Teams   []bulkTeam `json:"teams"`
}

func bulkCreateTeams(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a bulkCreateTeamsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if len(a.Teams) == 0 {
		return errorResult("teams: at least one entry required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	// roster.JSONImporter expects {"teams":[{name, member_names[]}]}.
	body := map[string]any{"teams": a.Teams}
	id := strconv.FormatInt(a.EventID, 10)
	status, respBody, err := callAdmin(ctx, s.Admin.BulkCreateTeams,
		"POST", "/api/admin/events/"+id+"/teams/bulk",
		map[string]string{"eventID": id}, nil, body)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, respBody), nil
}

type updateTeamArgs struct {
	TeamID int64   `json:"team_id"`
	Name   *string `json:"name,omitempty"`
	Email  *string `json:"email,omitempty"`
}

func updateTeam(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a updateTeamArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.TeamID <= 0 {
		return errorResult("team_id required and must be positive"), nil
	}
	if a.Name == nil && a.Email == nil {
		return errorResult("nothing to update — pass at least one of name, email"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	patch := map[string]any{}
	if a.Name != nil {
		patch["name"] = *a.Name
	}
	if a.Email != nil {
		patch["email"] = *a.Email
	}
	id := strconv.FormatInt(a.TeamID, 10)
	status, respBody, err := callAdmin(ctx, s.Admin.UpdateTeam,
		"PATCH", "/api/admin/teams/"+id,
		map[string]string{"teamID": id}, nil, patch)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, respBody), nil
}

type deleteTeamArgs struct {
	TeamID   int64  `json:"team_id"`
	Password string `json:"password"`
}

func deleteTeam(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a deleteTeamArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.TeamID <= 0 {
		return errorResult("team_id required and must be positive"), nil
	}
	if a.Password == "" {
		return errorResult("password required: delete is irreversible and the admin layer requires re-confirmation"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.TeamID, 10)
	status, body, err := callAdmin(ctx, s.Admin.DeleteTeam,
		"DELETE", "/api/admin/teams/"+id,
		map[string]string{"teamID": id},
		map[string]string{"X-Confirm-Password": a.Password}, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func withdrawTeam(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTeamPost(ctx, s, raw, "withdraw", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.WithdrawTeam
	})
}

func unwithdrawTeam(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTeamPost(ctx, s, raw, "unwithdraw", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.UnwithdrawTeam
	})
}

func unlockTeam(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTeamPost(ctx, s, raw, "unlock", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.UnlockTeam
	})
}

func letTeamStartAnywhere(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTeamPost(ctx, s, raw, "start-anywhere", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.LetTeamStartAnywhere
	})
}

// runTeamPost factors the body of withdraw / unwithdraw / unlock —
// each is POST /api/admin/teams/{teamID}/{action} with no body.
// Args validate before the nil-Admin check, same invariant as the
// event-side runTransition.
func runTeamPost(ctx context.Context, s *Server, raw json.RawMessage, action string, pickHandler func() http.HandlerFunc) (ToolCallResult, error) {
	var a teamIDArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.TeamID <= 0 {
		return errorResult("team_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	handler := pickHandler()
	if handler == nil {
		return errorResult("server misconfigured: admin handler missing for " + action), nil
	}
	id := strconv.FormatInt(a.TeamID, 10)
	status, body, err := callAdmin(ctx, handler,
		"POST", "/api/admin/teams/"+id+"/"+action,
		map[string]string{"teamID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type addSlotArgs struct {
	TeamID int64  `json:"team_id"`
	Name   string `json:"name"`
}

func addSlot(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a addSlotArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.TeamID <= 0 {
		return errorResult("team_id required and must be positive"), nil
	}
	if a.Name == "" {
		return errorResult("name required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.TeamID, 10)
	status, body, err := callAdmin(ctx, s.Admin.AddSlot,
		"POST", "/api/admin/teams/"+id+"/slots",
		map[string]string{"teamID": id}, nil,
		map[string]any{"name": a.Name})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func removeSlot(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a slotArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.TeamID <= 0 || a.SlotID <= 0 {
		return errorResult("team_id and slot_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	tid := strconv.FormatInt(a.TeamID, 10)
	sid := strconv.FormatInt(a.SlotID, 10)
	status, body, err := callAdmin(ctx, s.Admin.RemoveSlot,
		"DELETE", "/api/admin/teams/"+tid+"/slots/"+sid,
		map[string]string{"teamID": tid, "slotID": sid}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func rotateSlotToken(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a slotArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.TeamID <= 0 || a.SlotID <= 0 {
		return errorResult("team_id and slot_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	tid := strconv.FormatInt(a.TeamID, 10)
	sid := strconv.FormatInt(a.SlotID, 10)
	status, body, err := callAdmin(ctx, s.Admin.RotateSlotToken,
		"POST", "/api/admin/teams/"+tid+"/slots/"+sid+"/rotate",
		map[string]string{"teamID": tid, "slotID": sid}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
