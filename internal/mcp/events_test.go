package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestEventTools_NilAdminGuard confirms each Phase 1 tool
// short-circuits with a clean error when the server was constructed
// without an admin handler set. The MCP package would otherwise
// nil-deref inside the adapter, which the LLM has no way to
// recover from.
//
// Each tool is called with valid args so the failure mode under
// test is the nil-Admin guard, not arg validation.
func TestEventTools_NilAdminGuard(t *testing.T) {
	t.Parallel()
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name string
		fn   toolHandler
		args string
	}{
		{"list_events", listEvents, `{}`},
		{"get_event", getEvent, `{"event_id":1}`},
		{"create_event", createEvent, `{"attest_no_minors":true,"adventure_slug":"x"}`},
		{"update_event", updateEvent, `{"event_id":1,"name":"x"}`},
		{"start_event", startEvent, `{"event_id":1}`},
		{"pause_event", pauseEvent, `{"event_id":1}`},
		{"resume_event", resumeEvent, `{"event_id":1}`},
		{"cancel_event", cancelEvent, `{"event_id":1,"confirm":true}`},
		{"end_event", endEvent, `{"event_id":1,"confirm":true}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.fn(ctx, s, json.RawMessage(c.args))
			if err != nil {
				t.Fatalf("unexpected protocol error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError, got success body=%q", textOf(res))
			}
			if !strings.Contains(textOf(res), "misconfigured") {
				t.Errorf("error body=%q, want 'misconfigured'", textOf(res))
			}
		})
	}
}

// TestEventTools_ArgValidation locks in the LLM-friendly errors
// each tool emits when an obviously bad shape arrives. These
// validate-first paths fire BEFORE the nil-Admin guard so a bad
// id surfaces a useful error regardless of server state.
func TestEventTools_ArgValidation(t *testing.T) {
	t.Parallel()
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"get_event without id", getEvent, `{}`, "event_id required"},
		{"get_event negative id", getEvent, `{"event_id":-1}`, "must be positive"},
		{"update_event no patch fields", updateEvent, `{"event_id":1}`, "nothing to update"},
		{"cancel_event without confirm", cancelEvent, `{"event_id":1}`, "confirm required"},
		{"end_event without confirm", endEvent, `{"event_id":1}`, "confirm required"},
		{"start_event without id", startEvent, `{}`, "event_id required"},
		{"pause_event without id", pauseEvent, `{}`, "event_id required"},
		{"resume_event without id", resumeEvent, `{}`, "event_id required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.fn(ctx, s, json.RawMessage(c.args))
			if err != nil {
				t.Fatalf("unexpected protocol error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError, got success body=%q", textOf(res))
			}
			if !strings.Contains(textOf(res), c.wantSub) {
				t.Errorf("error body=%q, want substring %q", textOf(res), c.wantSub)
			}
		})
	}
}

// TestEventTools_AreRegisteredWithCorrectScope guards against a
// future contributor adding a new event tool with the wrong scope.
// The 8 Phase 1 tools split 2 read / 6 write — list_events and
// get_event are read-side; everything else mutates state.
func TestEventTools_AreRegisteredWithCorrectScope(t *testing.T) {
	t.Parallel()
	s := &Server{tools: newRegistry()}
	s.registerEventTools()

	want := map[string]string{
		"list_events":  scopeRead,
		"get_event":    scopeRead,
		"create_event": scopeWrite,
		"update_event": scopeWrite,
		"start_event":  scopeWrite,
		"pause_event":  scopeWrite,
		"resume_event": scopeWrite,
		"cancel_event": scopeWrite,
		"end_event":    scopeWrite,
	}
	for name, w := range want {
		got, ok := s.tools.scopes[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if got != w {
			t.Errorf("tool %q registered with scope %q, want %q", name, got, w)
		}
	}
	if len(s.tools.scopes) != len(want) {
		t.Errorf("registry has %d tools, expected exactly %d Phase 1 tools",
			len(s.tools.scopes), len(want))
	}
}
