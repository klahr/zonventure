package mcp

// Graph authoring tools. Each dispatches to the same admin handler the
// HTTP API uses (internal/admin/graph.go) through callAdmin, so
// authorisation, validation and error codes are identical rather than
// reimplemented — an assistant can do exactly what the editor can, and
// nothing more.

import (
	"context"
	"encoding/json"
	"net/http"
)

func (s *Server) registerGraphTools() {
	slugOnly := map[string]any{
		"type":     "object",
		"required": []string{"slug"},
		"properties": map[string]any{
			"slug": map[string]any{"type": "string", "description": "Adventure slug."},
		},
	}
	withEnvelope := map[string]any{
		"type":     "object",
		"required": []string{"slug", "envelope"},
		"properties": map[string]any{
			"slug": map[string]any{"type": "string", "description": "Adventure slug."},
			"envelope": map[string]any{
				"type": "object",
				"description": `A graph envelope: {"format":"zonventure-adventure-v2","graph":{...}}. ` +
					"The graph has nodes (start, puzzle, story, hub, gate, effect, end), edges from a node's " +
					"output port to another node, and zones/items/counters/journal referenced by slug. " +
					"Get one to edit with get_adventure_graph, or build one from lanes with convert_adventure_graph.",
			},
		},
	}

	s.tools.register(ToolDescriptor{
		Name:  "get_adventure_graph",
		Scope: scopeRead,
		Description: "Load an adventure's latest saved graph, with a fresh validation report. " +
			"Returns 404 with code graph_not_found if the adventure has no graph yet — use convert_adventure_graph to start one from its lanes.",
		InputSchema: slugOnly,
	}, getAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:  "validate_adventure_graph",
		Scope: scopeRead,
		Description: "Check a graph without saving it. Reports every problem at once — unreachable nodes, nodes that cannot reach an end, " +
			"unconnected outputs, references to missing nodes/zones/items/counters, regex that does not compile. Run this before save_adventure_graph.",
		InputSchema: withEnvelope,
	}, validateAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:  "convert_adventure_graph",
		Scope: scopeRead,
		Description: "Build a graph from the adventure's current lanes and pages. Nothing is saved: inspect or edit the result, then save it with save_adventure_graph. " +
			"Linear lanes become chains; shuffled lanes become hubs that play in the same per-team order as today.",
		InputSchema: slugOnly,
	}, convertAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:  "save_adventure_graph",
		Scope: scopeWrite,
		Description: "Save a graph as the adventure's next version. Refused with code graph_invalid and the full report if it has errors; nothing is written. " +
			"Events already running keep the version they started with.",
		InputSchema: withEnvelope,
	}, saveAdventureGraph)
}

type graphArgs struct {
	Slug     string          `json:"slug"`
	Envelope json.RawMessage `json:"envelope,omitempty"`
}

func parseGraphArgs(raw json.RawMessage, needEnvelope bool) (graphArgs, *ToolCallResult) {
	var a graphArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		r := errorResult("bad arguments: " + err.Error())
		return a, &r
	}
	if a.Slug == "" {
		r := errorResult("slug is required")
		return a, &r
	}
	if needEnvelope && len(a.Envelope) == 0 {
		r := errorResult("envelope is required")
		return a, &r
	}
	return a, nil
}

func graphCall(ctx context.Context, s *Server, method, suffix string, raw json.RawMessage, needEnvelope bool,
	pick func(*Server) http.HandlerFunc) (ToolCallResult, error) {
	a, bad := parseGraphArgs(raw, needEnvelope)
	if bad != nil {
		return *bad, nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	var body any
	if needEnvelope {
		body = a.Envelope
	}
	status, resp, err := callAdmin(ctx, pick(s), method, "/api/admin/adventures/"+a.Slug+"/graph"+suffix,
		map[string]string{"slug": a.Slug}, nil, body)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, resp), nil
}

func getAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return graphCall(ctx, s, "GET", "", raw, false, func(s *Server) http.HandlerFunc { return s.Admin.GetAdventureGraph })
}

func validateAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return graphCall(ctx, s, "POST", "/validate", raw, true, func(s *Server) http.HandlerFunc { return s.Admin.ValidateAdventureGraph })
}

func convertAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return graphCall(ctx, s, "POST", "/convert", raw, false, func(s *Server) http.HandlerFunc { return s.Admin.ConvertAdventureGraph })
}

func saveAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return graphCall(ctx, s, "PUT", "", raw, true, func(s *Server) http.HandlerFunc { return s.Admin.PutAdventureGraph })
}
