package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInvitationTools_NilAdminGuard(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name string
		fn   toolHandler
		args string
	}{
		{"send_event_invitations", sendEventInvitations, `{"event_id":1}`},
		{"send_team_invitation", sendTeamInvitation, `{"team_id":1}`},
		{"get_event_invite_ics", getEventInviteICS, `{"event_id":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.fn(ctx, s, json.RawMessage(c.args))
			if err != nil {
				t.Fatalf("protocol err: %v", err)
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

func TestInvitationTools_ArgValidation(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"send_event_invitations no id", sendEventInvitations, `{}`, "event_id required"},
		{"send_team_invitation no id", sendTeamInvitation, `{}`, "team_id required"},
		{"get_event_invite_ics no id", getEventInviteICS, `{}`, "event_id required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.fn(ctx, s, json.RawMessage(c.args))
			if err != nil {
				t.Fatalf("protocol err: %v", err)
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

func TestInvitationTools_AreRegisteredWithCorrectScope(t *testing.T) {
	s := &Server{tools: newRegistry()}
	s.registerInvitationTools()

	want := map[string]string{
		"send_event_invitations": scopeWrite,
		"send_team_invitation":   scopeWrite,
		"get_event_invite_ics":   scopeRead,
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
