package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExportResultsCSV_NilAdminGuard(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read"})
	res, err := exportEventResultsCSV(ctx, s, json.RawMessage(`{"event_id":1}`))
	if err != nil {
		t.Fatalf("protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError, body=%q", textOf(res))
	}
	if !strings.Contains(textOf(res), "misconfigured") {
		t.Errorf("body=%q, want 'misconfigured'", textOf(res))
	}
}

func TestExportResultsCSV_ArgValidation(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read"})
	res, err := exportEventResultsCSV(ctx, s, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError, body=%q", textOf(res))
	}
	if !strings.Contains(textOf(res), "event_id required") {
		t.Errorf("body=%q, want 'event_id required'", textOf(res))
	}
}

// TestDescribeEventLifecycle_Content asserts the reference doc
// covers the load-bearing strings: every state name and every tool
// that drives a transition. If a future contributor renames a tool
// or adds a state without updating the doc, this test catches it
// before the model starts giving stale advice.
func TestDescribeEventLifecycle_Content(t *testing.T) {
	res, err := describeEventLifecycle(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %q", textOf(res))
	}
	body := textOf(res)
	for _, want := range []string{
		// states
		"scheduled", "running", "paused", "completed", "cancelled",
		// transition tools
		"create_event", "start_event", "pause_event", "resume_event", "end_event", "cancel_event",
		// observation tools (cadence section)
		"list_positions", "list_activity", "list_tracks", "get_event_state_summary",
		// security gates
		"password", "admin-only", "simulated",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lifecycle doc missing %q", want)
		}
	}
}

func TestSlice3Tools_AreRegisteredWithCorrectScope(t *testing.T) {
	s := &Server{tools: newRegistry()}
	s.registerResultsTools()
	s.registerLifecycleTool()

	want := map[string]string{
		"export_event_results_csv": scopeRead,
		"describe_event_lifecycle": scopeRead,
	}
	for name, w := range want {
		got, ok := s.tools.scopes[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if got != w {
			t.Errorf("tool %q scope=%q, want %q", name, got, w)
		}
	}
	if len(s.tools.scopes) != len(want) {
		t.Errorf("registry has %d tools, expected %d", len(s.tools.scopes), len(want))
	}
}
