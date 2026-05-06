package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestObservationTools_NilAdminGuard(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name string
		fn   toolHandler
		args string
	}{
		{"list_positions", listPositions, `{"event_id":1}`},
		{"list_activity", listActivity, `{"event_id":1}`},
		{"list_tracks", listTracks, `{"event_id":1}`},
		{"get_event_state_summary", getEventStateSummary, `{"event_id":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.fn(ctx, s, json.RawMessage(c.args))
			if err != nil {
				t.Fatalf("unexpected protocol err: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError, body=%q", textOf(res))
			}
			if !strings.Contains(textOf(res), "misconfigured") {
				t.Errorf("body=%q, want 'misconfigured'", textOf(res))
			}
		})
	}
}

func TestObservationTools_ArgValidation(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read"})

	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"list_positions no id", listPositions, `{}`, "event_id required"},
		{"list_activity no id", listActivity, `{}`, "event_id required"},
		{"list_tracks no id", listTracks, `{}`, "event_id required"},
		{"summary no id", getEventStateSummary, `{}`, "event_id required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.fn(ctx, s, json.RawMessage(c.args))
			if err != nil {
				t.Fatalf("unexpected protocol err: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError, body=%q", textOf(res))
			}
			if !strings.Contains(textOf(res), c.wantSub) {
				t.Errorf("body=%q, want substring %q", textOf(res), c.wantSub)
			}
		})
	}
}

func TestObservationTools_AreRegisteredWithCorrectScope(t *testing.T) {
	s := &Server{tools: newRegistry()}
	s.registerObservationTools()

	want := map[string]string{
		"list_positions":          scopeRead,
		"list_activity":           scopeRead,
		"list_tracks":             scopeRead,
		"get_event_state_summary": scopeRead,
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

// TestSummariseDetail_LeaderboardOrdering exercises the projection
// that's specific to get_event_state_summary — the leaderboard
// sort: completed first, then by page_index desc, then withdrawn
// sunk to the bottom regardless. Stable order between equal
// entries matters because the LLM diffs successive polls.
func TestSummariseDetail_LeaderboardOrdering(t *testing.T) {
	d := eventDetailForSummary{
		State:      "running",
		AllowedOps: []string{"pause", "cancel"},
		TeamsCount: 6,
		Teams: []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			PageIndex   int    `json:"page_index"`
			CompletedAt string `json:"completed_at,omitempty"`
			WithdrawnAt string `json:"withdrawn_at,omitempty"`
		}{
			{ID: 1, Name: "Slow", PageIndex: 1},
			{ID: 2, Name: "Mid", PageIndex: 5},
			{ID: 3, Name: "Done", PageIndex: 8, CompletedAt: "2026-05-06 10:00:00"},
			{ID: 4, Name: "Quit", PageIndex: 3, WithdrawnAt: "2026-05-06 09:30:00"},
			{ID: 5, Name: "Lead", PageIndex: 7},
			{ID: 6, Name: "Tail", PageIndex: 2},
		},
	}
	s := summariseDetail(d)
	if s.CompletedCount != 1 {
		t.Errorf("completed_count=%d, want 1", s.CompletedCount)
	}
	if s.WithdrawnCount != 1 {
		t.Errorf("withdrawn_count=%d, want 1", s.WithdrawnCount)
	}
	if len(s.Leaderboard) != 5 {
		t.Fatalf("leaderboard size=%d, want 5", len(s.Leaderboard))
	}
	// Expected order: Done (completed) → Lead (page 7) → Mid (5) →
	// Tail (2) → Slow (1). Quit is withdrawn so it's beyond top-5.
	wantOrder := []string{"Done", "Lead", "Mid", "Tail", "Slow"}
	for i, want := range wantOrder {
		if got := s.Leaderboard[i].Name; got != want {
			t.Errorf("leaderboard[%d]=%q, want %q (full=%v)", i, got, want, s.Leaderboard)
		}
	}
}
