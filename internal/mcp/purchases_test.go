package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/payments"
)

// stubProvider is the smallest payments.Provider that flips
// adminH.paymentsEnabled() / mcp.stripeConfigured() to true. Tests
// here only check the gate, never actually invoke the methods.
type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }
func (stubProvider) CreateCheckout(ctx context.Context, p payments.CheckoutParams) (payments.CheckoutSession, error) {
	return payments.CheckoutSession{}, nil
}
func (stubProvider) VerifyAndParseWebhook(body []byte, headers http.Header) (payments.Event, error) {
	return payments.Event{}, nil
}
func (stubProvider) Refund(ctx context.Context, p payments.RefundParams) (payments.Refund, error) {
	return payments.Refund{}, nil
}
func (stubProvider) CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error) {
	return "", nil
}

func TestPurchaseTools_NilAdminGuard(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	s := &Server{Admin: nil}
	ctx := withScopes(context.Background(), []string{"read", "write"})

	cases := []struct {
		name    string
		fn      toolHandler
		args    string
		wantSub string
	}{
		{"get_purchase no id", getPurchase, `{}`, "purchase_id required"},
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

func TestPurchaseTools_AreRegisteredWithCorrectScope(t *testing.T) {
	t.Parallel()
	s := &Server{tools: newRegistry()}
	s.registerPurchaseTools()

	want := map[string]string{
		"list_purchases": scopeRead,
		"get_purchase":   scopeRead,
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
