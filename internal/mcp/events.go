package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Phase 1: event lifecycle. Each tool is a thin shim over an admin
// handler — all policy, validation, audit, webhook and WebSocket
// publish work runs inside the admin layer exactly as it would for
// a /api/admin caller. The MCP tool's only job is to translate the
// LLM-friendly args into the request shape the handler expects and
// re-package the response into a tool envelope.
//
// Tool ordering invariant: validate the LLM's args first, THEN
// check server state (Admin nil-guard). Bad args are the caller's
// problem and the error should be the same whether the server is
// fully wired or not.

func (s *Server) registerEventTools() {
	s.tools.register(ToolDescriptor{
		Name:        "list_events",
		Scope:       scopeRead,
		Description: "List events visible to the caller. Returns id, name, adventure_slug, state, scheduled_at, started_at, completed_at, cancelled_at, org_id, assignment_id, max_players, teams_count, and player_slots_count for each event. Use this to discover an event id before calling get_event / start_event / etc.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, listEvents)

	s.tools.register(ToolDescriptor{
		Name:        "get_event",
		Scope:       scopeRead,
		Description: "Read full detail for one event: meta, every team, every team's player slots (with magic-link tokens), allowed next operations from the current state. Use before transitioning state so you can confirm `state` matches the action's precondition (start: scheduled, pause: running, resume: paused; end: running/paused; cancel: scheduled/running/paused).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{
					"type":        "integer",
					"description": "Numeric id of the event. Use list_events to discover it.",
				},
			},
		},
	}, getEvent)

	s.tools.register(ToolDescriptor{
		Name:  "create_event",
		Scope: scopeWrite,
		Description: "Create an event for an adventure, which must be published first (publish_adventure_graph). Two kinds: " +
			"a self-guided adventure (public_for_sale: true, adventure_slug of one of the organisation's own adventures) is sold in the store and played whenever buyers like — set its price with update_event, then start_event to open it, empty of teams; " +
			"a scheduled event (public_for_sale false) for invited teams needs an event pass: an unused assignment_id from list_purchases (on a shared adventure, adventure_slug instead). Without a pass the reply links to where the user buys one. " +
			"attest_no_minors must be true: no players under 13, or with parental consent. public_for_sale cannot change later. Returns the event with its id.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"attest_no_minors"},
			"properties": map[string]any{
				"name":             map[string]any{"type": "string", "description": "Optional event name shown in the dashboard and webhooks."},
				"assignment_id":    map[string]any{"type": "integer", "description": "Unused adventure_assignments row to consume. Mutually exclusive with adventure_slug."},
				"adventure_slug":   map[string]any{"type": "string", "description": "Adventure slug (only valid for share-commissioned content)."},
				"scheduled_at":     map[string]any{"type": "string", "description": "Optional ISO 8601 start time. Empty leaves it unscheduled (ad-hoc start)."},
				"attest_no_minors": map[string]any{"type": "boolean", "description": "Must be true. Confirms no under-13 players (or that you have parental consent)."},
				"public_for_sale":  map[string]any{"type": "boolean", "description": "When true, creates a Quest: listed at /store, ticket-purchased, always-on. Default false (regular event). Immutable after creation."},
			},
		},
	}, createEvent)

	s.tools.register(ToolDescriptor{
		Name:        "update_event",
		Scope:       scopeWrite,
		Description: "Change an event's settings: name, scheduled_at (only while scheduled), show_map, secret_roles, and for a self-guided event (public_for_sale) its store listing — ticket_price_cents, ticket_currency, ticket_team_size, public_blurb. A self-guided event is sold in the store once it is running and has a price. Only the keys you pass change.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id":           map[string]any{"type": "integer", "description": "Event to update."},
				"name":               map[string]any{"type": "string", "description": "New event name. Empty string clears it."},
				"scheduled_at":       map[string]any{"type": "string", "description": "New ISO 8601 start time, or empty string to clear the schedule."},
				"show_map":           map[string]any{"type": "boolean", "description": "Whether the audience /live/{token} page renders team positions."},
				"secret_roles":       map[string]any{"type": "boolean", "description": "Deal the adventure's secret saboteur role in this event."},
				"ticket_price_cents": map[string]any{"type": "integer", "description": "Self-guided: price per ticket in cents, e.g. 19900 for 199.00."},
				"ticket_currency":    map[string]any{"type": "string", "description": "Self-guided: always SEK, the one currency tickets are sold in; may be left out."},
				"ticket_team_size":   map[string]any{"type": "integer", "description": "Self-guided: players per ticket."},
				"public_blurb":       map[string]any{"type": "string", "description": "Self-guided: the line shown in the store."},
			},
		},
	}, updateEvent)

	s.tools.register(ToolDescriptor{
		Name:        "start_event",
		Scope:       scopeWrite,
		Description: "Transition a scheduled event into running. Requires the event to be in state='scheduled' AND have at least one team. Pins the adventure's published graph, places every team, and emits the event.started webhook. Refused when the adventure has nothing published.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to start."},
			},
		},
	}, startEvent)

	s.tools.register(ToolDescriptor{
		Name:        "pause_event",
		Scope:       scopeWrite,
		Description: "Freeze a running event. Players see a 'paused' banner and answers/movement are not recorded. Use during an emergency or when the arranger wants to hold the field.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to pause."},
			},
		},
	}, pauseEvent)

	s.tools.register(ToolDescriptor{
		Name:        "resume_event",
		Scope:       scopeWrite,
		Description: "Resume a paused event. Returns it to running with the same elapsed clock and team progress.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to resume."},
			},
		},
	}, resumeEvent)

	s.tools.register(ToolDescriptor{
		Name:        "cancel_event",
		Scope:       scopeWrite,
		Description: "Terminate an event before completion. Allowed from scheduled, running or paused. Discards live state (positions, leaderboard) without rollback. Requires the caller's password — pass it as 'password' so the admin layer's confirm-gate can verify it. The API token alone cannot cancel.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "password"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to cancel."},
				"password": map[string]any{"type": "string", "description": "Caller's account password. Forwarded to the admin layer as the X-Confirm-Password header."},
			},
		},
	}, cancelEvent)

	s.tools.register(ToolDescriptor{
		Name:        "end_event",
		Scope:       scopeWrite,
		Description: "End a running or paused event now, before every team has finished. Teams still playing keep the progress and points they have; the event becomes completed, so results and recaps open and players are signed out. Cannot be undone. Refused for a self-guided adventure (public_for_sale), which never ends: pause it to stop selling tickets, or cancel it. Requires the caller's password — pass it as 'password' so the admin layer's confirm-gate can verify it.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "password"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to end."},
				"password": map[string]any{"type": "string", "description": "Caller's account password. Forwarded to the admin layer as the X-Confirm-Password header."},
			},
		},
	}, endEvent)
}

/* ---------------- handlers ---------------- */

// adminConfigured is the shared "is the admin handler set wired?"
// guard. Returning errorResult lets each tool fail consistently
// when the parent process forgot to pass an *admin.Handlers into
// New — see internal/server/server.go for the canonical wiring.
func adminConfigured(s *Server) *ToolCallResult {
	if s.Admin == nil {
		r := errorResult("server misconfigured: admin handlers not wired")
		return &r
	}
	return nil
}

func listEvents(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.ListEvents,
		"GET", "/api/admin/events", nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type eventIDArgs struct {
	EventID int64 `json:"event_id"`
}

func getEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
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
	status, body, err := callAdmin(ctx, s.Admin.EventDetail,
		"GET", "/api/admin/events/"+id, map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	// A read-scoped token must stay read-only. The event detail carries
	// capability tokens — each team's share + recap token and every
	// player's magic-link token — which let the holder act as those
	// teams/players. Strip them unless the token also holds write scope.
	if status == http.StatusOK && !scopesAllow(scopesFromContext(ctx), scopeWrite) {
		body = redactEventCapabilityTokens(body)
	}
	return adminCallResult(status, body), nil
}

// redactEventCapabilityTokens blanks the per-team and per-slot
// capability tokens in an event-detail JSON body. Best-effort: on any
// parse/marshal error it returns the input unchanged — a working
// response without redaction is no worse than the pre-existing
// behaviour, and the scope gate on mutating tools is the real control.
func redactEventCapabilityTokens(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	teams, ok := m["teams"].([]any)
	if !ok {
		return body
	}
	for _, t := range teams {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		delete(tm, "token")
		delete(tm, "recap_token")
		members, ok := tm["members"].([]any)
		if !ok {
			continue
		}
		for _, mem := range members {
			if md, ok := mem.(map[string]any); ok {
				delete(md, "token")
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// createEventArgs mirrors admin.createEventRequest but uses the
// public field names the LLM sees in the schema. Translated 1:1
// when assembling the body so the admin handler decodes its native
// shape.
type createEventArgs struct {
	Name           string `json:"name"`
	AssignmentID   int64  `json:"assignment_id"`
	AdventureSlug  string `json:"adventure_slug"`
	ScheduledAt    string `json:"scheduled_at"`
	AttestNoMinors bool   `json:"attest_no_minors"`
	// PublicForSale flips the event into a Quest at creation time —
	// listed at /store, ticket-purchased, always-on. Immutable after
	// creation. False (default) produces a regular event.
	PublicForSale bool `json:"public_for_sale"`
}

func createEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a createEventArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	body := map[string]any{
		"name":             a.Name,
		"assignment_id":    a.AssignmentID,
		"adventure_slug":   a.AdventureSlug,
		"scheduled_at":     a.ScheduledAt,
		"attest_no_minors": a.AttestNoMinors,
		"public_for_sale":  a.PublicForSale,
	}
	status, respBody, err := callAdmin(ctx, s.Admin.CreateEvent,
		"POST", "/api/admin/events", nil, nil, body)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if bytes.Contains(respBody, []byte(`"assignment_required"`)) {
		msg := "A scheduled event on this adventure needs an event pass, which is bought on the website"
		if base := strings.TrimRight(s.Admin.PublicURL, "/"); base != "" && a.AdventureSlug != "" {
			msg += ": " + base + "/app/arrange/adventures#/adventures/" + a.AdventureSlug + " (Schedule event)"
		}
		return errorResult(msg + ". Then list_purchases shows its assignment_id; call create_event with it. A self-guided adventure (public_for_sale: true) needs no pass."), nil
	}
	return adminCallResult(status, respBody), nil
}

type updateEventArgs struct {
	EventID          int64   `json:"event_id"`
	Name             *string `json:"name,omitempty"`
	ScheduledAt      *string `json:"scheduled_at,omitempty"`
	ShowMap          *bool   `json:"show_map,omitempty"`
	SecretRoles      *bool   `json:"secret_roles,omitempty"`
	TicketPriceCents *int    `json:"ticket_price_cents,omitempty"`
	TicketCurrency   *string `json:"ticket_currency,omitempty"`
	TicketTeamSize   *int    `json:"ticket_team_size,omitempty"`
	PublicBlurb      *string `json:"public_blurb,omitempty"`
}

func updateEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a updateEventArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	// The admin request has the same keys, each left out when absent.
	patch := a
	patch.EventID = 0
	if b, _ := json.Marshal(patch); string(b) == `{"event_id":0}` {
		return errorResult("nothing to update: pass at least one setting"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, respBody, err := callAdmin(ctx, s.Admin.UpdateEvent,
		"PATCH", "/api/admin/events/"+id,
		map[string]string{"eventID": id}, nil, updateEventBody(patch))
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, respBody), nil
}

func startEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTransition(ctx, s, raw, "start", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.StartEvent
	})
}

func pauseEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTransition(ctx, s, raw, "pause", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.PauseEvent
	})
}

func resumeEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return runTransition(ctx, s, raw, "resume", func() http.HandlerFunc {
		if s.Admin == nil {
			return nil
		}
		return s.Admin.ResumeEvent
	})
}

// runTransition factors the start / pause / resume bodies. Args
// validate first; admin nil-guard happens after, so a malformed id
// surfaces the same error regardless of server state.
//
// pickHandler is a thunk so we can defer dereferencing s.Admin
// until after the nil-check — taking s.Admin.StartEvent at the
// call site would nil-deref before we got the chance to error
// cleanly.
func runTransition(ctx context.Context, s *Server, raw json.RawMessage, action string, pickHandler func() http.HandlerFunc) (ToolCallResult, error) {
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
	handler := pickHandler()
	if handler == nil {
		return errorResult("server misconfigured: admin handler missing for " + action), nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, handler,
		"POST", fmt.Sprintf("/api/admin/events/%s/%s", id, action),
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type cancelEventArgs struct {
	EventID  int64  `json:"event_id"`
	Password string `json:"password"`
}

func cancelEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a cancelEventArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if a.Password == "" {
		return errorResult("password required: cancel discards live state and the admin layer requires re-confirmation"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.CancelEvent,
		"POST", "/api/admin/events/"+id+"/cancel",
		map[string]string{"eventID": id},
		map[string]string{"X-Confirm-Password": a.Password}, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func endEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a cancelEventArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if a.Password == "" {
		return errorResult("password required: ending signs every player out and the admin layer requires re-confirmation"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.EndEvent,
		"POST", "/api/admin/events/"+id+"/end",
		map[string]string{"eventID": id},
		map[string]string{"X-Confirm-Password": a.Password}, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

// updateEventBody is the admin request: the args without the event id.
func updateEventBody(a updateEventArgs) map[string]any {
	b, _ := json.Marshal(a)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	delete(m, "event_id")
	return m
}
