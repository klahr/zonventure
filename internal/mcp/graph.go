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
					"The graph has nodes (start, puzzle, story, hub, gate, effect, switch, end), edges from a node's " +
					"output port to another node, and zones/items/counters/journal referenced by slug. " +
					"Get one to edit with get_adventure_graph.",
			},
			"note": map[string]any{"type": "string", "description": "Saving only: " + noteDescription},
		},
	}

	s.tools.register(ToolDescriptor{
		Name:     "get_adventure_graph",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "Load an adventure's draft graph (or its published one if there is no draft), with a fresh validation report, " +
			"the published version and whether the draft has unpublished changes. " +
			"Returns 404 with code graph_not_found if the adventure has no graph yet — build one and save it with save_adventure_graph.",
		InputSchema: slugOnly,
	}, getAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:     "validate_adventure_graph",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "Check a graph without saving it. Reports every problem at once — unreachable nodes, nodes that cannot reach an end, " +
			"unconnected outputs, references to missing nodes/zones/items/counters, regex that does not compile. Run this before save_adventure_graph.",
		InputSchema: withEnvelope,
	}, validateAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:     "save_adventure_graph",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: "Save a graph as the adventure's draft. Drafts may have errors; the returned report lists them. " +
			"Nothing reaches players until publish_adventure_graph.",
		InputSchema: withEnvelope,
	}, saveAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:     "publish_adventure_graph",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: "Publish the saved draft as the adventure's next version; events that start afterwards play it. " +
			"Refused with code graph_invalid and the full report if the draft has errors. Events already running keep their version.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug":  map[string]any{"type": "string", "description": "Adventure slug."},
				"label": map[string]any{"type": "string", "description": "Optional release name."},
			},
		},
	}, publishAdventureGraph)
}

type graphArgs struct {
	Slug     string          `json:"slug"`
	Envelope json.RawMessage `json:"envelope,omitempty"`
	Label    string          `json:"label,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// noteDescription is the note an assistant may give with an edit.
const noteDescription = "A few words on what you are doing (\"tightening the hints on the riddle\"), shown live to the people watching the adventure in the designer."

// noteHeader passes an edit's note on to the designers watching.
func noteHeader(headers map[string]string, note string) map[string]string {
	if note == "" {
		return headers
	}
	if headers == nil {
		headers = map[string]string{}
	}
	headers["X-Edit-Note"] = note
	return headers
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
	} else if a.Label != "" {
		body = map[string]string{"label": a.Label}
	}
	status, resp, err := callAdmin(ctx, pick(s), method, "/api/admin/adventures/"+a.Slug+"/graph"+suffix,
		map[string]string{"slug": a.Slug}, noteHeader(nil, a.Note), body)
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

func saveAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return graphCall(ctx, s, "PUT", "", raw, true, func(s *Server) http.HandlerFunc { return s.Admin.PutAdventureGraph })
}

func publishAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	return graphCall(ctx, s, "POST", "/publish", raw, false, func(s *Server) http.HandlerFunc { return s.Admin.PublishAdventureGraph })
}
