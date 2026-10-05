package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The organisation's theme library: looks its authors keep for all its
// adventures. The assistant offers them when choosing a look.

func (s *Server) registerOrgThemeTools() {
	s.tools.register(ToolDescriptor{
		Name:  "list_org_themes",
		Scope: scopeRead,
		Description: "The caller's organisation's own themes: looks its authors keep for all its adventures (without pictures or sounds). " +
			"Offer them first when choosing an adventure's look. Each look takes the put_theme look fields; to make one the main look, pass it to update_adventure_meta with _color added to accent, accent_secondary, background, text and submit_bg.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, listOrgThemes)
}

func listOrgThemes(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.ListOrgThemes, "GET", "/api/admin/org-themes", nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var resp struct {
		Themes []struct {
			Name string          `json:"name"`
			Look json.RawMessage `json:"look"`
		} `json:"themes"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errorResult("decoding the themes: " + err.Error()), nil
	}
	if len(resp.Themes) == 0 {
		return ToolCallResult{Content: []ToolContent{{Type: "text", Text: "The organisation has no themes of its own yet. An org admin adds one in the designer's theme editor with Save for the organisation."}}}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The organisation's %d themes:\n", len(resp.Themes))
	for _, th := range resp.Themes {
		fmt.Fprintf(&b, "- %s: %s\n", th.Name, th.Look)
	}
	return ToolCallResult{Content: []ToolContent{{Type: "text", Text: b.String()}}}, nil
}
