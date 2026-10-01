package mcp

// Working with the people on an adventure: reading and joining the
// conversations on its steps, picking up what they asked the assistant
// for (threads that @assistant), and saying where a step is in the work.
// Each tool calls the admin handlers in admin/collab.go, so the same
// rules and live updates apply as in the designer.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) registerCollabTools() {
	s.tools.register(ToolDescriptor{
		Name:  "list_adventure_threads",
		Scope: scopeRead,
		Description: "The conversations the authors have on an adventure's steps: each thread's step (node, '' for the whole adventure), " +
			"optional block, whether it is resolved, and its comments in order. A thread with for_assistant: true is a task the authors gave you " +
			"by writing @assistant — do what it asks, reply on the thread with what you did, then resolve it. " +
			"Also returns each step's status (idea, draft, review, ready) and owner, and the people who can be mentioned by @name.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug":       map[string]any{"type": "string", "description": "Adventure slug."},
				"open_only":  map[string]any{"type": "boolean", "description": "Leave out resolved threads. Default true."},
				"tasks_only": map[string]any{"type": "boolean", "description": "Only threads that ask the assistant (for_assistant)."},
				"node":       map[string]any{"type": "string", "description": "Only threads on this step."},
			},
		},
	}, listAdventureThreads)

	s.tools.register(ToolDescriptor{
		Name:  "comment_on_adventure",
		Scope: scopeWrite,
		Description: "Write in the authors' conversation on an adventure. With thread_id, reply on that thread (a resolved thread opens again). " +
			"Without, start a thread on a step (node), on one block of it (node and block_id), or on the whole adventure (no node). " +
			"Use it to report what you did about a task, to ask before a change you are unsure of, and for review notes: one thread per issue, " +
			"on the step it is about, saying what is wrong and what you suggest. @name mentions an author, who is told by email. " +
			"The authors see your comments live in the designer, marked as the assistant's.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "body"},
			"properties": map[string]any{
				"slug":      map[string]any{"type": "string", "description": "Adventure slug."},
				"body":      map[string]any{"type": "string", "description": "The comment, plain text with **Markdown** emphasis; at most 4000 characters."},
				"thread_id": map[string]any{"type": "integer", "description": "Reply on this thread."},
				"node":      map[string]any{"type": "string", "description": "New thread: the step's slug."},
				"block_id":  map[string]any{"type": "string", "description": "New thread: the block of that step it is about."},
			},
		},
	}, commentOnAdventure)

	s.tools.register(ToolDescriptor{
		Name:  "suggest_step_change",
		Scope: scopeWrite,
		Description: "Propose a change to a step instead of making it: the authors see it as a thread with the change shown field by field, " +
			"and accept (merging it into the step) or reject it. Send the whole step as you would have it (same shape as put_node) and a short note " +
			"saying why. Prefer this to edit_adventure_graph for review fixes, and for a step an author is working on — " +
			"list_adventure_threads shows each step's status and owner.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "node"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug."},
				"node": map[string]any{"type": "object", "description": "The whole step as proposed; its slug says which step."},
				"note": map[string]any{"type": "string", "description": "Why, in a sentence or two. @name mentions an author."},
			},
		},
	}, suggestStepChange)

	s.tools.register(ToolDescriptor{
		Name:        "resolve_adventure_thread",
		Scope:       scopeWrite,
		Description: "Resolve a thread once what it asked is done (or open it again with resolved: false). Reply first saying what you did.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "thread_id"},
			"properties": map[string]any{
				"slug":      map[string]any{"type": "string", "description": "Adventure slug."},
				"thread_id": map[string]any{"type": "integer"},
				"resolved":  map[string]any{"type": "boolean", "description": "Default true."},
			},
		},
	}, resolveAdventureThread)

	s.tools.register(ToolDescriptor{
		Name:  "set_step_status",
		Scope: scopeWrite,
		Description: "Say where a step is in the work: status idea, draft, review (ready for someone to look at) or ready, " +
			"and who has it (owner, an author's username; empty to take it from whoever had it). Fields left out stay as they are. " +
			"Set review on a step you finished for an author to check; do not mark steps ready yourself unless asked.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "node"},
			"properties": map[string]any{
				"slug":   map[string]any{"type": "string", "description": "Adventure slug."},
				"node":   map[string]any{"type": "string", "description": "The step's slug."},
				"status": map[string]any{"type": "string", "enum": []string{"", "idea", "draft", "review", "ready"}},
				"owner":  map[string]any{"type": "string", "description": "An author's username, or empty for nobody."},
			},
		},
	}, setStepStatus)
}

// collabPeople is the people of admin's collab payload: who can own a step.
type collabPeople struct {
	People []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"people"`
}

func loadCollab(ctx context.Context, s *Server, slug string) (int, []byte, error) {
	return callAdmin(ctx, s.Admin.GetCollab, "GET", "/api/admin/adventures/"+slug+"/collab", map[string]string{"slug": slug}, nil, nil)
}

func listAdventureThreads(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug      string `json:"slug"`
		OpenOnly  *bool  `json:"open_only"`
		TasksOnly bool   `json:"tasks_only"`
		Node      string `json:"node"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := loadCollab(ctx, s, a.Slug)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	// Filter the threads as whole JSON objects, so the model sees each
	// one exactly as the designer does.
	var full struct {
		Threads []json.RawMessage `json:"threads"`
		Steps   json.RawMessage   `json:"steps"`
		People  json.RawMessage   `json:"people"`
	}
	if err := json.Unmarshal(body, &full); err != nil {
		return errorResult("decoding: " + err.Error()), nil
	}
	openOnly := a.OpenOnly == nil || *a.OpenOnly
	kept := []json.RawMessage{}
	for _, t := range full.Threads {
		var h struct {
			Node         string  `json:"node"`
			ResolvedAt   *string `json:"resolved_at"`
			ForAssistant bool    `json:"for_assistant"`
		}
		if json.Unmarshal(t, &h) != nil {
			continue
		}
		if (openOnly && h.ResolvedAt != nil) || (a.TasksOnly && !h.ForAssistant) || (a.Node != "" && h.Node != a.Node) {
			continue
		}
		kept = append(kept, t)
	}
	out, _ := json.Marshal(map[string]any{"threads": kept, "steps": full.Steps, "people": full.People})
	return textResult(string(out)), nil
}

func commentOnAdventure(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug     string `json:"slug"`
		Body     string `json:"body"`
		ThreadID int64  `json:"thread_id"`
		Node     string `json:"node"`
		BlockID  string `json:"block_id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" {
		return errorResult("slug and body are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	params := map[string]string{"slug": a.Slug}
	var (
		status int
		body   []byte
		err    error
	)
	if a.ThreadID > 0 {
		params["threadID"] = fmt.Sprint(a.ThreadID)
		status, body, err = callAdmin(ctx, s.Admin.ReplyToThread, "POST",
			fmt.Sprintf("/api/admin/adventures/%s/threads/%d/comments", a.Slug, a.ThreadID), params, nil, map[string]string{"body": a.Body})
	} else {
		status, body, err = callAdmin(ctx, s.Admin.CreateThread, "POST", "/api/admin/adventures/"+a.Slug+"/threads", params, nil,
			map[string]string{"node": a.Node, "block_id": a.BlockID, "body": a.Body})
	}
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func resolveAdventureThread(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug     string `json:"slug"`
		ThreadID int64  `json:"thread_id"`
		Resolved *bool  `json:"resolved"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" || a.ThreadID <= 0 {
		return errorResult("slug and thread_id are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	resolved := a.Resolved == nil || *a.Resolved
	status, body, err := callAdmin(ctx, s.Admin.UpdateThread, "PATCH", fmt.Sprintf("/api/admin/adventures/%s/threads/%d", a.Slug, a.ThreadID),
		map[string]string{"slug": a.Slug, "threadID": fmt.Sprint(a.ThreadID)}, nil, map[string]bool{"resolved": resolved})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func setStepStatus(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug   string  `json:"slug"`
		Node   string  `json:"node"`
		Status *string `json:"status"`
		Owner  *string `json:"owner"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" || a.Node == "" {
		return errorResult("slug and node are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	req := map[string]any{}
	if a.Status != nil {
		req["status"] = *a.Status
	}
	if a.Owner != nil {
		owner := strings.TrimPrefix(strings.TrimSpace(*a.Owner), "@")
		if owner == "" {
			req["owner_id"] = 0
		} else {
			status, body, err := loadCollab(ctx, s, a.Slug)
			if err != nil {
				return errorResult("call: " + err.Error()), nil
			}
			if status != http.StatusOK {
				return adminCallResult(status, body), nil
			}
			var c collabPeople
			if err := json.Unmarshal(body, &c); err != nil {
				return errorResult("decoding: " + err.Error()), nil
			}
			var names []string
			for _, p := range c.People {
				names = append(names, p.Name)
				if strings.EqualFold(p.Name, owner) {
					req["owner_id"] = p.ID
				}
			}
			if _, ok := req["owner_id"]; !ok {
				return errorResult(fmt.Sprintf("%q is not one of the adventure's authors: %s", owner, strings.Join(names, ", "))), nil
			}
		}
	}
	status, body, err := callAdmin(ctx, s.Admin.SetStepStatus, "PUT", "/api/admin/adventures/"+a.Slug+"/steps/"+a.Node+"/status",
		map[string]string{"slug": a.Slug, "node": a.Node}, nil, req)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func suggestStepChange(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug string          `json:"slug"`
		Node json.RawMessage `json:"node"`
		Note string          `json:"note"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" || len(a.Node) == 0 {
		return errorResult("slug and node are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	var proposed struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(a.Node, &proposed); err != nil || proposed.Slug == "" {
		return errorResult("node must be a step object with its slug"), nil
	}
	// The base is the step as the draft has it now: what the authors
	// will see the proposal against.
	status, body, err := callAdmin(ctx, s.Admin.GetAdventureGraph, "GET", "/api/admin/adventures/"+a.Slug+"/graph", map[string]string{"slug": a.Slug}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var state struct {
		Envelope struct {
			Graph struct {
				Nodes []json.RawMessage `json:"nodes"`
			} `json:"graph"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return errorResult("decoding the draft: " + err.Error()), nil
	}
	var base json.RawMessage
	for _, n := range state.Envelope.Graph.Nodes {
		var h struct {
			Slug string `json:"slug"`
		}
		if json.Unmarshal(n, &h) == nil && h.Slug == proposed.Slug {
			base = n
		}
	}
	if base == nil {
		return errorResult(fmt.Sprintf("the draft has no step %q; add new steps with edit_adventure_graph", proposed.Slug)), nil
	}
	status, body, err = callAdmin(ctx, s.Admin.CreateThread, "POST", "/api/admin/adventures/"+a.Slug+"/threads", map[string]string{"slug": a.Slug}, nil,
		map[string]any{"node": proposed.Slug, "body": a.Note, "suggestion": map[string]json.RawMessage{"base": base, "proposed": a.Node}})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
