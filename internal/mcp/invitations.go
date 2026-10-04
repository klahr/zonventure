package mcp

import (
	"context"
	"encoding/json"
	"strconv"
)

// Phase 4: invitations + ICS export.
//
// Q2 invariant: invitation send is allowed via MCP under the
// existing org-level rate limit. The wrapper does not add per-token
// rate limiting today — operators worried about a runaway loop
// should keep an eye on the org's mail volume and revoke the
// MCP token if it misbehaves.
//
// All three tools delegate through the standard authoriseEvent
// gate at the admin layer, so a member of the wrong org gets the
// usual 403 envelope even before any email work.

func (s *Server) registerInvitationTools() {
	s.tools.register(ToolDescriptor{
		Name:        "send_event_invitations",
		Scope:       scopeWrite,
		Description: "Send the magic-link email to every team in the event that has a contact address set. Each email carries the team's per-slot tokens plus the event's .ics calendar attachment (when scheduled). Teams without an email are skipped, not failed. NOT idempotent — calling twice sends two emails per team. Returns {sent, skipped_no_email, failed} counts.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose teams should be notified."},
			},
		},
	}, sendEventInvitations)

	s.tools.register(ToolDescriptor{
		Name:        "send_team_invitation",
		Scope:       scopeWrite,
		Description: "Resend exactly one team's magic-link email — same payload send_event_invitations would mail this team, scoped to a single recipient. Use when a captain says they lost the message. Existing tokens are sent verbatim (no rotation); a player whose link is leaked should rotate via rotate_slot_token instead.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"team_id"},
			"properties": map[string]any{
				"team_id": map[string]any{"type": "integer", "description": "Team to resend to."},
			},
		},
	}, sendTeamInvitation)

	s.tools.register(ToolDescriptor{
		Name:  "give_free_ticket",
		Scope: scopeWrite,
		Description: "Give someone a ticket to an event without payment — a tester, a guest. Makes a team the size of the event's bought ticket and emails them " +
			"what a buyer gets: the team link and a voucher code to type at /play. Returns team_id, team_url, play_url, voucher_code and emailed " +
			"(false when the mail could not be queued; hand over the link or code instead). Use for self-guided events; for a scheduled event " +
			"with a roster, create_team and send_team_invitation fit better.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "email"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to give the ticket to."},
				"email":    map[string]any{"type": "string", "description": "Where the ticket is emailed; it is also the team's contact."},
			},
		},
	}, giveFreeTicket)

	s.tools.register(ToolDescriptor{
		Name:        "get_event_invite_ics",
		Scope:       scopeRead,
		Description: "Return the event's .ics (RFC 5545 iCalendar) bytes as a text result — same calendar attachment send_event_invitations carries. Use to populate the arranger's own calendar, or to forward to a single recipient out-of-band. Errors when the event has no scheduled time.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose calendar entry should be exported."},
			},
		},
	}, getEventInviteICS)
}

/* ---------------- handlers ---------------- */

func sendEventInvitations(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a eventIDArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.SendEventInvitations,
		"POST", "/api/admin/events/"+id+"/invitations/send",
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func sendTeamInvitation(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
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
	id := strconv.FormatInt(a.TeamID, 10)
	status, body, err := callAdmin(ctx, s.Admin.SendTeamInvitation,
		"POST", "/api/admin/teams/"+id+"/invitations/send",
		map[string]string{"teamID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func giveFreeTicket(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		EventID int64  `json:"event_id"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.GiveFreeTicket,
		"POST", "/api/admin/events/"+id+"/free-ticket",
		map[string]string{"eventID": id}, nil, map[string]string{"email": a.Email})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func getEventInviteICS(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a eventIDArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	// The admin handler writes text/calendar bytes directly to the
	// response — adminCallResult treats any 2xx body as a text result,
	// so the .ics travels through unchanged. Non-2xx falls back to
	// the structured error envelope.
	status, body, err := callAdmin(ctx, s.Admin.ExportEventInviteICS,
		"GET", "/api/admin/events/"+id+"/invite.ics",
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
