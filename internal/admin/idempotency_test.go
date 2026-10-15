package admin_test

import (
	"context"
	"net/http"

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

func (recordingProvider) RefundApplicationFee(ctx context.Context, accountID, chargeID string) error {
	return nil
}

func (recordingProvider) GetConnectedAccount(ctx context.Context, accountID string) (payments.ConnectedAccount, error) {
	return payments.ConnectedAccount{ID: accountID, ChargesEnabled: true, PayoutsEnabled: true}, nil
}
