package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/graph/session"
)

// Dev tickets: the author plays the adventure's draft on a phone, on
// site, while the assistant edits it — every save shows on the phone at
// once, nothing published, no event restarted. The assistant can see
// what the phone shows, move it to a step, and start it over.

func (s *Server) registerDevTools() {
	slugOnly := map[string]any{
		"type":       "object",
		"required":   []string{"slug"},
		"properties": map[string]any{"slug": map[string]any{"type": "string", "description": "Adventure slug."}},
	}
	s.tools.register(ToolDescriptor{
		Name:  "create_dev_ticket",
		Scope: scopeWrite,
		Description: "Get the author's dev ticket for an adventure: a link to open on their phone that plays the saved DRAFT, live. " +
			"Every change you save (edit_adventure_graph, save_adventure_graph, the designer) shows on the phone within a second — nothing to publish, no event to restart. " +
			"Write the link in the chat for the author to open on the phone. The same ticket comes back each time. Dev tickets are free, never listed among events, never ranked.",
		InputSchema: slugOnly,
	}, createDevTicket)
	s.tools.register(ToolDescriptor{
		Name:  "get_dev_screen",
		Scope: scopeRead,
		Description: "What the author's dev phone shows right now: the step (its slug) and its screen — text, pictures, the answer box. " +
			"Use it to know what \"this\" means when the author, standing there, says \"change this text\".",
		InputSchema: slugOnly,
	}, getDevScreen)
	s.tools.register(ToolDescriptor{
		Name:  "dev_go_to",
		Scope: scopeWrite,
		Description: "Move the author's dev phone to a step of the draft — back to one already played, or on to one not reached — and play it afresh (its attempts, hints and outcome forgotten). " +
			"Name a puzzle, story, hub or end by its slug.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "node"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug."},
				"node": map[string]any{"type": "string", "description": "Slug of the step to show."},
			},
		},
	}, devGoTo)
	s.tools.register(ToolDescriptor{
		Name:        "dev_reset",
		Scope:       scopeWrite,
		Description: "Start the author's dev phone over from the beginning of the draft, its progress forgotten.",
		InputSchema: slugOnly,
	}, devReset)
}

func devSlug(raw json.RawMessage) (string, *ToolCallResult) {
	var a struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		r := errorResult("bad arguments: " + err.Error())
		return "", &r
	}
	if a.Slug == "" {
		r := errorResult("slug is required")
		return "", &r
	}
	return a.Slug, nil
}

func createDevTicket(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	slug, bad := devSlug(raw)
	if bad != nil {
		return *bad, nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.CreateDevTicket, "POST", "/api/admin/adventures/"+slug+"/dev-ticket", map[string]string{"slug": slug}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var t struct {
		TeamURL     string `json:"team_url"`
		PlayURL     string `json:"play_url"`
		VoucherCode string `json:"voucher_code"`
	}
	_ = json.Unmarshal(body, &t)
	text := fmt.Sprintf("Dev ticket for %q. Open on the phone: %s", slug, t.TeamURL)
	if t.VoucherCode != "" {
		text += fmt.Sprintf("\nOr type the code %s at %s", t.VoucherCode, strings.SplitN(t.PlayURL, "?", 2)[0])
	}
	text += "\nIt plays the draft: every change you save shows on the phone within a second."
	return ToolCallResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func getDevScreen(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	slug, bad := devSlug(raw)
	if bad != nil {
		return *bad, nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.DevScreen, "GET", "/api/admin/adventures/"+slug+"/dev-screen", map[string]string{"slug": slug}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var resp struct {
		View session.View `json:"view"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errorResult("decoding the dev screen: " + err.Error()), nil
	}
	g, bad := s.loadDraftForEdit(ctx, slug)
	if bad != nil {
		return *bad, nil
	}
	// The playtest's own screen, of the dev phone's view.
	clockState, _ := json.Marshal(map[string]any{"started_at": resp.View.Now, "now": resp.View.Now})
	ps := &playSession{slug: slug, g: g, view: resp.View, team: clockState}
	note := "The author's dev phone shows:"
	if resp.View.Node != "" {
		note = fmt.Sprintf("The author's dev phone is on step %q:", resp.View.Node)
	}
	return ps.screen(ctx, s, note, true), nil
}

func devGoTo(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug string `json:"slug"`
		Node string `json:"node"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if a.Slug == "" || a.Node == "" {
		return errorResult("slug and node are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.DevGoTo, "POST", "/api/admin/adventures/"+a.Slug+"/dev-goto", map[string]string{"slug": a.Slug}, nil, map[string]string{"node": a.Node})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	return ToolCallResult{Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("The dev phone is on %q now, played afresh.", a.Node)}}}, nil
}

func devReset(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	slug, bad := devSlug(raw)
	if bad != nil {
		return *bad, nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.DevReset, "POST", "/api/admin/adventures/"+slug+"/dev-reset", map[string]string{"slug": slug}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	return ToolCallResult{Content: []ToolContent{{Type: "text", Text: "The dev phone starts the draft over."}}}, nil
}
