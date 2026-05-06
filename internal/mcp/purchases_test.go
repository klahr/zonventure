package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/auth"
)

func TestPurchaseTools_NilAdminGuard(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})
	// grant_slots_to_event runs an isAdmin check before the
	// nil-Admin guard, so the no-admin user gets the
	// "forbidden: ... admin-only" error first. Other tools fall
	// straight through to the misconfigured branch.
	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"list_purchases", listPurchases, `{}`, "misconfigured"},
		{"get_purchase", getPurchase, `{"purchase_id":1}`, "misconfigured"},
		{"create_purchase", createPurchase, `{"adventure_slug":"x","max_players":1}`, "misconfigured"},
		{"confirm_purchase", confirmPurchase, `{"purchase_id":1}`, "misconfigured"},
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

func TestPurchaseTools_ArgValidation(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"get_purchase no id", getPurchase, `{}`, "purchase_id required"},
		{"create_purchase no slug", createPurchase, `{"max_players":5}`, "adventure_slug required"},
		{"create_purchase bad max_players", createPurchase, `{"adventure_slug":"x","max_players":0}`, "max_players must be 1–500"},
		{"create_purchase too many players", createPurchase, `{"adventure_slug":"x","max_players":501}`, "max_players must be 1–500"},
		{"confirm_purchase no id", confirmPurchase, `{}`, "purchase_id required"},
		{"grant_slots no event", grantSlotsToEvent, `{"added_players":1}`, "event_id required"},
		{"grant_slots bad added", grantSlotsToEvent, `{"event_id":1,"added_players":0}`, "added_players must be 1–500"},
		{"grant_slots negative amount", grantSlotsToEvent, `{"event_id":1,"added_players":1,"amount_cents":-1}`, "amount_cents must be ≥ 0"},
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

// TestCreatePurchase_RefusesOnStripeEnabled locks in the Q1 gate:
// when both Stripe credentials are present the tool refuses without
// dispatching, regardless of how the caller phrased the request.
// On a Stripe-less deployment the same args fall through to the
// nil-Admin guard (admin handlers happen to be nil in this test),
// proving the gate is the only thing standing between the LLM and
// a real-money flow.
func TestCreatePurchase_RefusesOnStripeEnabled(t *testing.T) {
	stripeOn := &Server{Admin: &admin.Handlers{
		StripeSecretKey:     "sk_test_x",
		StripeWebhookSecret: "whsec_x",
	}}
	ctx := withScopes(context.Background(), []string{"read", "write"})
	res, err := createPurchase(ctx, stripeOn,
		json.RawMessage(`{"adventure_slug":"x","max_players":5}`))
	if err != nil {
		t.Fatalf("protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError on stripe-enabled, body=%q", textOf(res))
	}
	if !strings.Contains(textOf(res), "stripe is configured") {
		t.Errorf("body=%q, want 'stripe is configured'", textOf(res))
	}

	// Sanity: same args on a stripe-less server reach the next
	// gate (nil-Admin in this test), not the stripe refusal.
	stripeOff := &Server{Admin: nil}
	res, err = createPurchase(ctx, stripeOff,
		json.RawMessage(`{"adventure_slug":"x","max_players":5}`))
	if err != nil {
		t.Fatalf("protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError on nil-admin, body=%q", textOf(res))
	}
	if strings.Contains(textOf(res), "stripe is configured") {
		t.Errorf("stripe-less server hit the stripe refusal: body=%q", textOf(res))
	}
}

// TestGrantSlots_RefusesNonAdmin proves the admin-only gate fires
// before any handler dispatch — a non-admin token sees a clean
// 'forbidden' regardless of whether the rest of the server is
// wired.
func TestGrantSlots_RefusesNonAdmin(t *testing.T) {
	s := &Server{Admin: nil}
	ctx := auth.WithUser(context.Background(), auth.User{
		ID: 7, Username: "alice", Role: auth.RoleOrgAdmin,
	})
	ctx = withScopes(ctx, []string{"read", "write"})

	res, err := grantSlotsToEvent(ctx, s,
		json.RawMessage(`{"event_id":1,"added_players":5}`))
	if err != nil {
		t.Fatalf("protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError, body=%q", textOf(res))
	}
	if !strings.Contains(textOf(res), "admin-only") {
		t.Errorf("body=%q, want 'admin-only'", textOf(res))
	}

	// As an admin, the tool advances past the role gate and hits
	// the nil-Admin misconfigured branch instead.
	ctx = auth.WithUser(context.Background(), auth.User{
		ID: 1, Username: "root", IsAdmin: true, Role: auth.RoleAdmin,
	})
	ctx = withScopes(ctx, []string{"read", "write"})
	res, err = grantSlotsToEvent(ctx, s,
		json.RawMessage(`{"event_id":1,"added_players":5}`))
	if err != nil {
		t.Fatalf("protocol err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError on nil admin, body=%q", textOf(res))
	}
	if !strings.Contains(textOf(res), "misconfigured") {
		t.Errorf("admin path body=%q, want 'misconfigured'", textOf(res))
	}
}

func TestPurchaseTools_AreRegisteredWithCorrectScope(t *testing.T) {
	s := &Server{tools: newRegistry()}
	s.registerPurchaseTools()

	want := map[string]string{
		"list_purchases":       scopeRead,
		"get_purchase":         scopeRead,
		"create_purchase":      scopeWrite,
		"confirm_purchase":     scopeWrite,
		"grant_slots_to_event": scopeWrite,
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
