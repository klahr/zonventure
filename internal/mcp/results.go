package mcp

import (
	"context"
	"encoding/json"
	"strconv"
)

// Slice 3 polish: post-event exports and a discovery aid for the
// LLM. Kept thin — every line of code here saves the model a round
// trip the existing surface couldn't avoid.

func (s *Server) registerResultsTools() {
	s.tools.register(ToolDescriptor{
		Name:        "export_event_results_csv",
		Scope:       scopeRead,
		Description: "Return the event's final-standings CSV as a text result. Columns: rank, team, members, points, tiebreak_guess, tiebreak_off_by, finished_at (RFC 3339), time (from the event's first start to the team's finish). Order and ranks match the public leaderboard; teams that never started or withdrew come last with no rank. Rows are still emitted for not-yet-finished teams (with empty finished_at and time) so an arranger can pull a snapshot mid-event. Recap PDF stays web-only — see /arrange.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose results to export."},
			},
		},
	}, exportEventResultsCSV)
}

func exportEventResultsCSV(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
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
	// CSV body travels through adminCallResult unchanged — 2xx →
	// textResult(string(body)), so the model receives the file
	// content verbatim. Non-2xx falls back to the structured error
	// envelope.
	status, body, err := callAdmin(ctx, s.Admin.ExportEventResults,
		"GET", "/api/admin/events/"+id+"/results.csv",
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
