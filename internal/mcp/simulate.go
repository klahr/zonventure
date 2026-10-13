package mcp

// simulate_adventure: play the saved draft through a list of actions
// with the real rules, so an assistant can test what it built — does
// the right answer move on, does the hint arrive, does every path end —
// before telling the author it is ready. Each action goes through the
// designer's simulator endpoint (internal/admin/simulate.go), threading
// the team state from one call to the next as the designer does.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/klahr/zonventure/internal/graph"
)

// maxSimulateActions bounds one call; a playthrough of a real adventure
// is tens of actions.
const maxSimulateActions = 200

func (s *Server) registerSimulateTools() {
	s.tools.register(ToolDescriptor{
		Name:     "simulate_adventure",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "Play the adventure's saved draft through a list of actions with the same rules a real event runs, and return what happened at each. " +
			"Nothing is stored. Start with {kind: start}. A team that must choose a start, or begin at the adventure's starting point, then waits at the start: " +
			"move there and begin {port: <start node slug>}. Then: answer {node, guess}; continue {node}; choose {node, port} at a hub; skip {node}; hint {node}; " +
			"pick {node, block, choice} for quiz blocks; wait {seconds} jumps the clock; move {zone} puts the team in a zone (or nowhere when empty), " +
			"move {lat, lng} at a point; language {lang} switches to a translation. Stops at the first refused action and says how many after it were not run; a hint before its delay is refused, so wait {seconds} first. Each step reports the node the team is now at, " +
			"whether an answer was accepted, the port it left by, points, wrong-answer response and hint text.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "actions"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure slug."},
				"anywhere": map[string]any{"type": "boolean", "description": "Begin wherever the team is, skipping the adventure's required starting point. Default false."},
				"actions": map[string]any{
					"type":     "array",
					"minItems": 1,
					"items": map[string]any{
						"type":     "object",
						"required": []string{"kind"},
						"properties": map[string]any{
							"kind":    map[string]any{"type": "string"},
							"node":    map[string]any{"type": "string"},
							"guess":   map[string]any{"type": "string"},
							"port":    map[string]any{"type": "string"},
							"index":   map[string]any{"type": "integer"},
							"seconds": map[string]any{"type": "integer"},
							"zone":    map[string]any{"type": "string"},
							"lang":    map[string]any{"type": "string"},
							"block":   map[string]any{"type": "string"},
							"choice":  map[string]any{"type": "string"},
							"lat":     map[string]any{"type": "number"},
							"lng":     map[string]any{"type": "number"},
						},
					},
				},
			},
		},
	}, simulateAdventure)
}

// simStep is the model's view of one simulated action: the outcome, not
// the whole player view, which is mostly rendering detail.
type simStep struct {
	Action   json.RawMessage `json:"action"`
	At       string          `json:"at,omitempty"`
	Finished bool            `json:"finished,omitempty"`
	Accepted bool            `json:"accepted,omitempty"`
	Port     string          `json:"port,omitempty"`
	Points   float64         `json:"points,omitempty"`
	Response string          `json:"response,omitempty"`
	Hint     string          `json:"hint,omitempty"`
	TimeUp   bool            `json:"time_up,omitempty"`
	Passed   []string        `json:"passed,omitempty"`
	Refusal  json.RawMessage `json:"refusal,omitempty"`
}

func simulateAdventure(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug     string            `json:"slug"`
		Actions  []json.RawMessage `json:"actions"`
		Anywhere bool              `json:"anywhere"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if a.Slug == "" || len(a.Actions) == 0 {
		return errorResult("slug and actions are required"), nil
	}
	if len(a.Actions) > maxSimulateActions {
		return errorResult("too many actions for one call; split the playthrough"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	g, bad := s.loadDraftForEdit(ctx, a.Slug)
	if bad != nil {
		return *bad, nil
	}
	env, err := graph.MarshalEnvelope(g)
	if err != nil {
		return errorResult("encoding draft: " + err.Error()), nil
	}

	var team json.RawMessage
	steps := []simStep{}
	for _, act := range a.Actions {
		status, body, err := callAdmin(ctx, s.Admin.SimulateAdventureGraph, "POST", "/api/admin/adventures/"+a.Slug+"/simulate",
			map[string]string{"slug": a.Slug}, nil, map[string]any{"envelope": json.RawMessage(env), "team": team, "action": act, "any_place": a.Anywhere})
		if err != nil {
			return errorResult("call: " + err.Error()), nil
		}
		if status != http.StatusOK {
			return adminCallResult(status, body), nil
		}
		var resp struct {
			Team   json.RawMessage `json:"team"`
			Passed []string        `json:"passed"`
			Refuse json.RawMessage `json:"refusal"`
			Result struct {
				Accepted bool    `json:"accepted"`
				Port     string  `json:"port"`
				Points   float64 `json:"points"`
				Response string  `json:"response"`
				Hint     string  `json:"hint"`
				TimeUp   bool    `json:"time_up"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return errorResult("decoding simulation: " + err.Error()), nil
		}
		var where struct {
			Current   string `json:"current"`
			Completed bool   `json:"completed"`
		}
		_ = json.Unmarshal(resp.Team, &where)
		steps = append(steps, simStep{
			Action: act, At: where.Current, Finished: where.Completed,
			Accepted: resp.Result.Accepted, Port: resp.Result.Port, Points: resp.Result.Points,
			Response: resp.Result.Response, Hint: resp.Result.Hint, TimeUp: resp.Result.TimeUp,
			Passed: resp.Passed, Refusal: resp.Refuse,
		})
		if len(resp.Refuse) > 0 && string(resp.Refuse) != "null" {
			break
		}
		if len(resp.Team) > 0 && string(resp.Team) != "null" {
			team = resp.Team
		}
	}
	res := map[string]any{"steps": steps}
	// Say so when a refusal left actions unplayed, rather than dropping them quietly.
	if n := len(a.Actions) - len(steps); n > 0 {
		res["not_run"] = n
		res["note"] = fmt.Sprintf("stopped at the refused action; the %d action(s) after it were not run", n)
	}
	out, _ := json.Marshal(res)
	return textResult(string(out)), nil
}
