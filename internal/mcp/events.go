package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/klahr/zonventure/internal/admin"
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
		Description: "Read full detail for one event: meta, every team, every team's player slots (with magic-link tokens), allowed next operations from the current state. print_materials, when its adventure has any, says how the event delivers each (delivery) beside the adventure's own (adventure_delivery); set_print_delivery changes it. Use before transitioning state so you can confirm `state` matches the action's precondition (start: scheduled, pause: running, resume: paused; end: running/paused; cancel: scheduled/running/paused).",
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
			"a scheduled event (public_for_sale false) for invited teams, on one of the organisation's own or shared adventures. Nothing is paid up front: each team is charged to the organisation as it starts, so its balance (the console's Billing page) must cover the teams. " +
			"attest_no_minors must be true: no players under 13, or with parental consent. public_for_sale cannot change later. Returns the event with its id.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"attest_no_minors"},
			"properties": map[string]any{
				"name":             map[string]any{"type": "string", "description": "Optional event name shown in the dashboard and webhooks."},
				"adventure_slug":   map[string]any{"type": "string", "description": "Adventure slug: one of the organisation's own adventures, or one shared with it."},
				"scheduled_at":     map[string]any{"type": "string", "description": "Optional ISO 8601 start time. Empty leaves it unscheduled (ad-hoc start)."},
				"attest_no_minors": map[string]any{"type": "boolean", "description": "Must be true. Confirms no under-13 players (or that you have parental consent)."},
				"public_for_sale":  map[string]any{"type": "boolean", "description": "When true, creates a Quest: listed at /store, ticket-purchased, always-on. Default false (regular event). Immutable after creation."},
			},
		},
	}, createEvent)

	s.tools.register(ToolDescriptor{
		Name:        "update_event",
		Scope:       scopeWrite,
		Description: "Change an event's settings: name, scheduled_at (only while scheduled), meeting_place, show_map, secret_roles, and for a self-guided event (public_for_sale) its store listing — ticket_price_cents, ticket_currency, ticket_team_size, public_blurb, listing_translations. A self-guided event is sold in the store once it is running, has a price and its listing is approved: a platform admin reviews each new or changed listing (name, price, team size, blurb, cover) unless the organisation is a verified creator, and until then the store keeps showing the last approved version. get_event's listing_review says where it is (pending, approved, rejected with listing_reject_reason). Only the keys you pass change.",
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
				"ticket_currency":    map[string]any{"type": "string", "description": "Self-guided: always the platform's currency, the one tickets are sold in; may be left out."},
				"ticket_team_size":   map[string]any{"type": "integer", "description": "Self-guided: players per ticket."},
				"public_blurb":       map[string]any{"type": "string", "description": "Self-guided: the line shown in the store."},
				"listing_translations": map[string]any{
					"type":        "object",
					"description": "Self-guided: the listing's name and blurb in the other languages the adventure is played in (get_event's languages), by language tag: {\"en\": {\"name\": \"…\", \"blurb\": \"…\"}}. Replaces them all; {} removes them. Reviewed with the listing. The adventure's own title, meeting point (meta:start_label) and area (meta:area) are translated in the adventure's graph.",
					"additionalProperties": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":  map[string]any{"type": "string"},
							"blurb": map[string]any{"type": "string"},
						},
					},
				},
				"sales_stopped": map[string]any{"type": "boolean", "description": "Self-guided: true takes it out of the store and refuses new tickets while teams that have one play on; false sells again."},
				"meeting_place": map[string]any{"type": "string", "description": "Where teams meet, e.g. \"By the fountain in Kungsträdgården\": said in invitations and the calendar entry. Empty string clears it."},
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
		Description: "Terminate an event before completion. Allowed from scheduled, running or paused. Discards live state (positions, leaderboard) without rollback. Ask the user first, then pass confirm: true.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "confirm"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to cancel."},
				"confirm":  map[string]any{"type": "boolean", "description": "true once the user has confirmed cancelling this event."},
			},
		},
	}, cancelEvent)

	s.tools.register(ToolDescriptor{
		Name:        "end_event",
		Scope:       scopeWrite,
		Description: "End a running or paused event now, before every team has finished. Teams still playing keep the progress and points they have; the event becomes completed, so results and recaps open and players are signed out. Cannot be undone. Refused for a self-guided adventure (public_for_sale), which never ends: stop selling tickets with update_event sales_stopped, or cancel it. Ask the user first, then pass confirm: true.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "confirm"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to end."},
				"confirm":  map[string]any{"type": "boolean", "description": "true once the user has confirmed ending this event."},
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
	if status == http.StatusOK {
		body = withPrintMaterials(ctx, s, id, body)
		body = withISOTimes(body)
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

// withPrintMaterials adds print_materials to an event's detail: how the
// event delivers each of its adventure's print materials, beside the
// adventure's own (set_print_delivery changes it). An event without any
// is left as it is, as is one whose materials can't be read.
func withPrintMaterials(ctx context.Context, s *Server, id string, body []byte) []byte {
	status, pb, err := callAdmin(ctx, s.Admin.EventPrintDelivery, "GET", "/api/admin/events/"+id+"/print-delivery",
		map[string]string{"eventID": id}, nil, nil)
	if err != nil || status != http.StatusOK {
		return body
	}
	var p struct {
		Materials []json.RawMessage `json:"materials"`
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(pb, &p) != nil || len(p.Materials) == 0 || json.Unmarshal(body, &m) != nil {
		return body
	}
	m["print_materials"], _ = json.Marshal(p.Materials)
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// utcISO turns an event timestamp as the console stores it, UTC
// "2006-01-02 15:04:05", into RFC 3339 as create_event takes it, so an
// assistant can't read a UTC time as local; anything else stays as it is.
func utcISO(v string) string {
	t, err := time.Parse("2006-01-02 15:04:05", v)
	if err != nil {
		return v
	}
	return t.UTC().Format(time.RFC3339)
}

// withISOTimes gives an event detail's event timestamps as utcISO does.
func withISOTimes(body []byte) []byte {
	var m map[string]json.RawMessage
	var ev map[string]any
	if json.Unmarshal(body, &m) != nil || json.Unmarshal(m["event"], &ev) != nil {
		return body
	}
	for _, k := range []string{"scheduled_at", "started_at", "completed_at", "cancelled_at", "paused_at"} {
		if v, ok := ev[k].(string); ok {
			ev[k] = utcISO(v)
		}
	}
	m["event"], _ = json.Marshal(ev)
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
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
	SalesStopped     *bool   `json:"sales_stopped,omitempty"`
	MeetingPlace     *string `json:"meeting_place,omitempty"`

	ListingTranslations *map[string]listingTexts `json:"listing_translations,omitempty"`
}

// listingTexts are a listing's name and blurb in one language.
type listingTexts struct {
	Name  string `json:"name,omitempty"`
	Blurb string `json:"blurb,omitempty"`
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
	EventID int64 `json:"event_id"`
	Confirm bool  `json:"confirm"`
}

func cancelEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a cancelEventArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if !a.Confirm {
		return errorResult("confirm required: cancel discards live state; ask the user, then pass confirm: true"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(admin.WithConfirmed(ctx), s.Admin.CancelEvent,
		"POST", "/api/admin/events/"+id+"/cancel",
		map[string]string{"eventID": id},
		nil, nil)
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
	if !a.Confirm {
		return errorResult("confirm required: ending signs every player out; ask the user, then pass confirm: true"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(admin.WithConfirmed(ctx), s.Admin.EndEvent,
		"POST", "/api/admin/events/"+id+"/end",
		map[string]string{"eventID": id},
		nil, nil)
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
