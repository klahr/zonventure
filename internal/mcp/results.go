package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/klahr/zonventure/internal/admin"
)

// Slice 3 polish: post-event exports and a discovery aid for the
// LLM. Kept thin — every line of code here saves the model a round
// trip the existing surface couldn't avoid.

func (s *Server) registerResultsTools() {
	s.tools.register(ToolDescriptor{
		Name:        "export_event_results_csv",
		Scope:       scopeRead,
		Description: "Return the event's final-standings CSV as a text result. Columns: rank, team, members, points, tiebreak_guess, tiebreak_off_by, finished_at (RFC 3339), time (from the event's first start to the team's finish). Order and ranks match the public leaderboard; teams that never started or withdrew come last with no rank. Rows are still emitted for not-yet-finished teams (with empty finished_at and time) so an arranger can pull a snapshot mid-event. For a program to read, export_event_results_json has the same rows with each team's results receipt. The printable standings poster and the recap report (PDF) stay web-only: the event page in /arrange.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose results to export."},
			},
		},
	}, exportEventResultsCSV)
	s.tools.register(ToolDescriptor{
		Name:        "export_event_results_json",
		Scope:       scopeRead,
		Description: "Return the event's results as JSON: {event: {id, name, adventure_slug, adventure_title, state, started_at, completed_at, results_fingerprint}, generated_at, teams: [{rank, team, members[], points, tiebreak_guess, tiebreak_off_by, finished_at, time_seconds, status, receipt}]}. Same rows, order and ranks as export_event_results_csv; teams on the same place share its rank, and teams that never started or withdrew come last with no rank or points. status is finished, playing, did not finish, withdrawn or not started. receipt is a finished team's results receipt, the code its recap shows; results_fingerprint seals the event's results record once it is over (absent before). Times are RFC 3339 in the organisation's time zone; time_seconds runs from the event's first start.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose results to export."},
			},
		},
	}, exportEventResultsJSON)
	s.tools.register(ToolDescriptor{
		Name:        "verify_event_results",
		Scope:       scopeRead,
		Description: "Check the event's tamper-evident results record, as \"Check results\" on the event page does: recompute every entry's hash and link, and compare each team's result now with the last one recorded. Returns {ok, entries, head, fingerprint, sealed, problems: [{seq, team_id, problem}], teams: [{team_id, name, receipt, recorded, current, matches}]}. ok false means results were changed outside the game: name the problems to the user. fingerprint is the hash the organisation's admins were emailed when the event ended; if it no longer matches that email, the record was rewritten.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose results record to check."},
			},
		},
	}, verifyEventResults)
}

func exportEventResultsCSV(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return eventGet(ctx, s, raw, func(h *admin.Handlers) http.HandlerFunc { return h.ExportEventResults }, "/results.csv")
}

func exportEventResultsJSON(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return eventGet(ctx, s, raw, func(h *admin.Handlers) http.HandlerFunc { return h.ExportEventResultsJSON }, "/results.json")
}

func verifyEventResults(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return eventGet(ctx, s, raw, func(h *admin.Handlers) http.HandlerFunc { return h.VerifyEventResults }, "/results/verify")
}

// eventGet is a GET of one of an event's admin endpoints, under
// /api/admin/events/{eventID}, its body handed back as is.
func eventGet(ctx context.Context, s *Server, raw json.RawMessage, handler func(*admin.Handlers) http.HandlerFunc, suffix string) (ToolCallResult, error) {
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
	// The body travels through adminCallResult unchanged — 2xx →
	// textResult(string(body)), so the model receives the file
	// content verbatim. Non-2xx falls back to the structured error
	// envelope.
	status, body, err := callAdmin(ctx, handler(s.Admin),
		"GET", "/api/admin/events/"+id+suffix,
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
