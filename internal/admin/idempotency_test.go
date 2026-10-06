package admin_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/payments"
)

// recordingProvider captures the params handed to each Provider call
// so tests can assert deterministic IdempotencyKey derivation. C4
// regression guard: an admin handler retried under transport error
// must hit Stripe with the same IdempotencyKey rather than minting a
// duplicate session / refund / connected account.
type recordingProvider struct {
	checkout      payments.CheckoutParams
	refund        payments.RefundParams
	connectedAcct payments.ConnectedAccountParams
	accountLink   payments.AccountLinkParams
}

func (recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) CreateCheckout(ctx context.Context, params payments.CheckoutParams) (payments.CheckoutSession, error) {
	p.checkout = params
	return payments.CheckoutSession{URL: "https://x/y", SessionID: "cs_rec"}, nil
}

func (recordingProvider) VerifyAndParseWebhook(body []byte, headers http.Header) (payments.Event, error) {
	return payments.Event{}, nil
}

func (p *recordingProvider) Refund(ctx context.Context, params payments.RefundParams) (payments.Refund, error) {
	p.refund = params
	return payments.Refund{ID: "re_rec", Status: "succeeded"}, nil
}

func (recordingProvider) CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error) {
	return "https://x/portal", nil
}

func (p *recordingProvider) CreateConnectedAccount(ctx context.Context, params payments.ConnectedAccountParams) (string, error) {
	p.connectedAcct = params
	return "acct_rec", nil
}

func (p *recordingProvider) CreateAccountLink(ctx context.Context, params payments.AccountLinkParams) (string, error) {
	p.accountLink = params
	return "https://x/link", nil
}

func (recordingProvider) CreateLoginLink(ctx context.Context, accountID string) (string, error) {
	return "https://x/express", nil
}

func (recordingProvider) GetConnectedAccount(ctx context.Context, accountID string) (payments.ConnectedAccount, error) {
	return payments.ConnectedAccount{ID: accountID, ChargesEnabled: true, PayoutsEnabled: true}, nil
}

// TestPurchaseCheckout_IdempotencyKey verifies the org-purchase
// checkout flow stamps a stable, purchase-bound key on the
// CheckoutParams. The exact format ("checkout-purchase-N") is part
// of the contract: changing it across deploys would defeat dedup
// for an in-flight retry.
func TestPurchaseCheckout_IdempotencyKey(t *testing.T) {
	t.Parallel()
	p := &recordingProvider{}
	h := &admin.Handlers{Provider: p, PublicURL: "https://zv.example.com"}

	if _, _, err := admin.ExportedCreatePurchaseCheckoutSession(
		h, context.Background(), 42, "demo", "Demo", 4, 2500, "buyer@example.com", "",
	); err != nil {
		t.Fatalf("CreatePurchaseCheckout: %v", err)
	}
	if got, want := p.checkout.IdempotencyKey, "checkout-purchase-42"; got != want {
		t.Errorf("IdempotencyKey = %q, want %q", got, want)
	}

	// A second invocation for the same purchase must produce the
	// same key (the whole point of "idempotency").
	if _, _, err := admin.ExportedCreatePurchaseCheckoutSession(
		h, context.Background(), 42, "demo", "Demo", 4, 2500, "buyer@example.com", "",
	); err != nil {
		t.Fatalf("CreatePurchaseCheckout (retry): %v", err)
	}
	if p.checkout.IdempotencyKey != "checkout-purchase-42" {
		t.Errorf("retry produced different key %q", p.checkout.IdempotencyKey)
	}

	// A different purchase id must yield a different key.
	if _, _, err := admin.ExportedCreatePurchaseCheckoutSession(
		h, context.Background(), 43, "demo", "Demo", 4, 2500, "buyer@example.com", "",
	); err != nil {
		t.Fatalf("CreatePurchaseCheckout (purchase 43): %v", err)
	}
	if p.checkout.IdempotencyKey == "checkout-purchase-42" {
		t.Errorf("purchase 43 reused key %q from purchase 42", p.checkout.IdempotencyKey)
	}
}
