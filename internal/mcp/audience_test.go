package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
)

// TestRegistry_AudienceGate: a platform admin's token reaches only
// operator (and shared) tools, an org user's only org (and shared)
// ones — both in tools/list and at dispatch.
func TestRegistry_AudienceGate(t *testing.T) {
	t.Parallel()
	r := newRegistry()
	ok := func(_ context.Context, _ *Server, _ json.RawMessage) (ToolCallResult, error) {
		return textResult("ok"), nil
	}
	for _, d := range []ToolDescriptor{
		{Name: "org_tool", Scope: scopeRead},
		{Name: "admin_tool", Scope: scopeRead, Audience: audienceAdmin},
		{Name: "any_tool", Scope: scopeRead, Audience: audienceAny},
	} {
		r.register(d, ok)
	}
	orgID := int64(1)
	for _, c := range []struct {
		who  string
		user auth.User
		want map[string]bool
	}{
		{"admin", auth.User{Role: auth.RoleAdmin, IsAdmin: true},
			map[string]bool{"org_tool": false, "admin_tool": true, "any_tool": true}},
		{"org_admin", auth.User{Role: auth.RoleOrgAdmin, OrgID: &orgID},
			map[string]bool{"org_tool": true, "admin_tool": false, "any_tool": true}},
	} {
		ctx := withScopes(auth.WithUser(context.Background(), c.user), []string{scopeRead})
		listed := map[string]bool{}
		for _, d := range r.list(ctx) {
			listed[d.Name] = true
		}
		for name, want := range c.want {
			if listed[name] != want {
				t.Errorf("%s: tools/list has %s = %v, want %v", c.who, name, listed[name], want)
			}
			res, err := r.call(ctx, nil, name, nil)
			if err != nil {
				t.Fatalf("%s: call %s: %v", c.who, name, err)
			}
			refused := res.IsError && strings.Contains(textOf(res), "forbidden_audience")
			if refused == want {
				t.Errorf("%s: call %s refused = %v, want %v", c.who, name, refused, !want)
			}
		}
	}
}
