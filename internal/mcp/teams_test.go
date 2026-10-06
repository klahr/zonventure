package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestTeamTools_NilAdminGuard mirrors the Phase 1 guard test: with
// no admin handler set, every team tool short-circuits to a clean
// 'misconfigured' error rather than nil-derefing inside the
// adapter.
func TestTeamTools_NilAdminGuard(t *testing.T) {
	t.Parallel()
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name string
		fn   toolHandler
		args string
	}{
		{"create_team", createTeam, `{"event_id":1,"name":"A"}`},
		{"bulk_create_teams", bulkCreateTeams, `{"event_id":1,"teams":[{"name":"A"}]}`},
		{"update_team", updateTeam, `{"team_id":1,"name":"X"}`},
		{"delete_team", deleteTeam, `{"team_id":1,"password":"x"}`},
		{"withdraw_team", withdrawTeam, `{"team_id":1}`},
		{"unwithdraw_team", unwithdrawTeam, `{"team_id":1}`},
		{"unlock_team", unlockTeam, `{"team_id":1}`},
		{"let_team_start_anywhere", letTeamStartAnywhere, `{"team_id":1}`},
		{"add_slot", addSlot, `{"team_id":1,"name":"Alice"}`},
		{"remove_slot", removeSlot, `{"team_id":1,"slot_id":2}`},
		{"rotate_slot_token", rotateSlotToken, `{"team_id":1,"slot_id":2}`},
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

// TestTeamTools_ArgValidation locks in the LLM-friendly error
// strings so the model can self-correct on bad shapes without
// needing to read source.
func TestTeamTools_ArgValidation(t *testing.T) {
	t.Parallel()
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"create_team without event_id", createTeam, `{"name":"A"}`, "event_id required"},
		{"create_team without name", createTeam, `{"event_id":1}`, "name required"},
		{"bulk_create_teams without teams", bulkCreateTeams, `{"event_id":1}`, "teams: at least one entry required"},
		{"update_team no patch", updateTeam, `{"team_id":1}`, "nothing to update"},
		{"delete_team without password", deleteTeam, `{"team_id":1}`, "password required"},
		{"withdraw_team without team_id", withdrawTeam, `{}`, "team_id required"},
		{"add_slot without name", addSlot, `{"team_id":1}`, "name required"},
		{"remove_slot without slot_id", removeSlot, `{"team_id":1}`, "team_id and slot_id required"},
		{"rotate_slot_token without slot_id", rotateSlotToken, `{"team_id":1}`, "team_id and slot_id required"},
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

func TestTeamTools_AreRegisteredWithCorrectScope(t *testing.T) {
	t.Parallel()
	s := &Server{tools: newRegistry()}
	s.registerTeamTools()

	want := map[string]string{
		"create_team":             scopeWrite,
		"bulk_create_teams":       scopeWrite,
		"update_team":             scopeWrite,
		"delete_team":             scopeWrite,
		"withdraw_team":           scopeWrite,
		"unwithdraw_team":         scopeWrite,
		"unlock_team":             scopeWrite,
		"let_team_start_anywhere": scopeWrite,
		"add_slot":                scopeWrite,
		"remove_slot":             scopeWrite,
		"rotate_slot_token":       scopeWrite,
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
