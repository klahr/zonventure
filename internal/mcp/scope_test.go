package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestScopesAllow exercises the membership predicate the registry
// uses to gate dispatch. No DB / HTTP — just the pure function.
func TestScopesAllow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		have []string
		need string
		want bool
	}{
		{[]string{"read", "write"}, "read", true},
		{[]string{"read", "write"}, "write", true},
		{[]string{"read"}, "read", true},
		{[]string{"read"}, "write", false},
		{[]string{"write"}, "read", false},
		{[]string{}, "read", false},
		{nil, "write", false},
	}
	for _, c := range cases {
		got := scopesAllow(c.have, c.need)
		if got != c.want {
			t.Errorf("scopesAllow(%v, %q) = %v, want %v", c.have, c.need, got, c.want)
		}
	}
}

// TestRegistryCall_ScopeGate confirms the registry refuses a tool
// call when the token's scope set doesn't include what the tool
// requires, and produces an in-band tool error rather than a JSON-RPC
// protocol error (so the model can see the message and self-correct).
func TestRegistryCall_ScopeGate(t *testing.T) {
	t.Parallel()
	r := newRegistry()
	called := 0
	r.register(ToolDescriptor{
		Name:        "fake_read",
		Scope:       scopeRead,
		Audience:    audienceAny,
		Description: "read fixture",
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, _ *Server, _ json.RawMessage) (ToolCallResult, error) {
		called++
		return textResult("ok"), nil
	})
	r.register(ToolDescriptor{
		Name:        "fake_write",
		Scope:       scopeWrite,
		Audience:    audienceAny,
		Description: "write fixture",
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, _ *Server, _ json.RawMessage) (ToolCallResult, error) {
		called++
		return textResult("ok"), nil
	})

	// read-only token: read tool runs, write tool refused and not listed.
	ctx := withScopes(context.Background(), []string{"read"})
	if l := r.list(ctx); len(l) != 1 || l[0].Name != "fake_read" {
		t.Errorf("read-only tools/list = %v, want only fake_read", l)
	}

	res, err := r.call(ctx, nil, "fake_read", nil)
	if err != nil {
		t.Fatalf("read with read scope: unexpected err %v", err)
	}
	if res.IsError {
		t.Fatalf("read with read scope: got IsError, body=%q", textOf(res))
	}

	res, err = r.call(ctx, nil, "fake_write", nil)
	if err != nil {
		t.Fatalf("write with read scope: unexpected protocol err %v", err)
	}
	if !res.IsError {
		t.Fatalf("write with read scope: expected IsError, got success %q", textOf(res))
	}
	if !strings.Contains(textOf(res), "forbidden_scope") {
		t.Errorf("expected forbidden_scope in error, got %q", textOf(res))
	}
	if called != 1 {
		t.Errorf("handler ran %d times, want 1 (only fake_read)", called)
	}

	// Full token: both tools run.
	ctx = withScopes(context.Background(), []string{"read", "write"})
	if _, err := r.call(ctx, nil, "fake_read", nil); err != nil {
		t.Fatalf("read with full scope: %v", err)
	}
	if _, err := r.call(ctx, nil, "fake_write", nil); err != nil {
		t.Fatalf("write with full scope: %v", err)
	}
	if called != 3 {
		t.Errorf("handler ran %d times, want 3", called)
	}
}

// TestRegistryRegister_PanicsOnMissingScope locks in the startup-time
// safety net: a contributor adding a new tool without a scope hits
// a panic in registerTools rather than silently shipping an
// unscoped (and therefore ungated) tool.
func TestRegistryRegister_PanicsOnMissingScope(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty scope, got none")
		}
	}()
	r := newRegistry()
	r.register(ToolDescriptor{
		Name:        "no_scope",
		Description: "should panic",
		InputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *Server, json.RawMessage) (ToolCallResult, error) {
		return ToolCallResult{}, nil
	})
}

func textOf(r ToolCallResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}
