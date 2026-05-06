// Package stripe is the Stripe concrete impl of payments.Provider.
// All Stripe SDK imports live in this package so the rest of the
// codebase consumes only the abstract `payments` types — replacing
// Stripe with a different provider is a matter of writing another
// package next to this one and wiring it from cmd/server.
//
// Operator config: Stripe is enabled when both SecretKey and
// WebhookSecret are non-empty. The constructor's `New` returns nil
// in that case so call sites that test `payments.Enabled(p)` stay
// honest about whether payments are configured.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/payments"

	stripego "github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/refund"
	"github.com/stripe/stripe-go/v82/webhook"
)

// Provider implements payments.Provider against Stripe's API.
type Provider struct {
	SecretKey     string
	WebhookSecret string
}

// Compile-time assertion that *Provider satisfies the interface.
// Catches signature drift if either side changes shape.
var _ payments.Provider = (*Provider)(nil)

// New returns a configured Stripe provider, or nil when either
// credential is missing. Returning nil rather than an error lets
// the server boot in self-host mode without complaining: payments
// are simply off, and callers that route through `payments.Enabled`
// degrade gracefully.
func New(secretKey, webhookSecret string) *Provider {
	if secretKey == "" || webhookSecret == "" {
		return nil
	}
	return &Provider{
		SecretKey:     secretKey,
		WebhookSecret: webhookSecret,
	}
}

// Name identifies this impl in logs / metrics.
func (p *Provider) Name() string { return "stripe" }

// CreateCheckout opens a Stripe Checkout session for the given
// params. Mirrors the shape of the previous in-package helper:
// price inlined via PriceData (no Price catalog to maintain),
// metadata round-tripped, optional auto-tax / address / tax-id /
// invoice-creation knobs.
func (p *Provider) CreateCheckout(ctx context.Context, params payments.CheckoutParams) (payments.CheckoutSession, error) {
	if params.SuccessURL == "" || params.CancelURL == "" {
		return payments.CheckoutSession{}, errors.New("stripe: missing success/cancel URL")
	}
	stripego.Key = p.SecretKey

	csParams := &stripego.CheckoutSessionParams{
		Mode: stripego.String(string(stripego.CheckoutSessionModePayment)),
		LineItems: []*stripego.CheckoutSessionLineItemParams{{
			Quantity: stripego.Int64(1),
			PriceData: &stripego.CheckoutSessionLineItemPriceDataParams{
				Currency:   stripego.String(strings.ToLower(params.Currency)),
				UnitAmount: stripego.Int64(params.AmountCents),
				ProductData: &stripego.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripego.String(params.ProductName),
				},
			},
		}},
		SuccessURL: stripego.String(params.SuccessURL),
		CancelURL:  stripego.String(params.CancelURL),
	}
	for k, v := range params.Metadata {
		csParams.AddMetadata(k, v)
	}
	// Stripe rejects CustomerEmail when Customer is set, so the
	// resolution is one-or-the-other (Customer wins).
	switch {
	case params.CustomerID != "":
		csParams.Customer = stripego.String(params.CustomerID)
	case params.CustomerEmail != "":
		csParams.CustomerEmail = stripego.String(params.CustomerEmail)
	}
	if params.AutoTax {
		csParams.AutomaticTax = &stripego.CheckoutSessionAutomaticTaxParams{
			Enabled: stripego.Bool(true),
		}
	}
	if params.CollectAddress {
		csParams.BillingAddressCollection = stripego.String(
			string(stripego.CheckoutSessionBillingAddressCollectionRequired))
	}
	if params.CollectTaxID {
		csParams.TaxIDCollection = &stripego.CheckoutSessionTaxIDCollectionParams{
			Enabled: stripego.Bool(true),
		}
	}
	if params.InvoiceCreation {
		csParams.InvoiceCreation = &stripego.CheckoutSessionInvoiceCreationParams{
			Enabled: stripego.Bool(true),
		}
	}

	// stripe-go doesn't accept ctx on CheckoutSession.New; deadline
	// is enforced by the package-level HTTP client. Caller-supplied
	// cancellation is the right fix for the SDK to expose, but for
	// now we accept the parameter to keep the interface consistent.
	_ = ctx

	s, err := checkoutsession.New(csParams)
	if err != nil {
		return payments.CheckoutSession{}, err
	}
	return payments.CheckoutSession{
		URL:       s.URL,
		SessionID: s.ID,
	}, nil
}

// VerifyAndParseWebhook validates the Stripe-Signature HMAC and
// decodes the event into the abstract Event shape. EventOther
// covers types we don't act on (subscription lifecycle, etc.) so
// callers can ack-and-move-on without a switch over Stripe-specific
// strings.
func (p *Provider) VerifyAndParseWebhook(body []byte, headers http.Header) (payments.Event, error) {
	sig := headers.Get("Stripe-Signature")
	ev, err := webhook.ConstructEvent(body, sig, p.WebhookSecret)
	if err != nil {
		return payments.Event{}, err
	}
	out := payments.Event{
		Provider: "stripe",
		ID:       ev.ID,
		Created:  time.Unix(ev.Created, 0),
		Type:     payments.EventOther,
	}
	switch ev.Type {
	case "checkout.session.completed":
		var sess stripego.CheckoutSession
		if err := json.Unmarshal(ev.Data.Raw, &sess); err != nil {
			return payments.Event{}, fmt.Errorf("decode checkout.session: %w", err)
		}
		out.Type = payments.EventCheckoutCompleted
		out.Metadata = sess.Metadata
		detail := payments.CheckoutDetail{
			SessionID:   sess.ID,
			AmountTotal: sess.AmountTotal,
			Currency:    string(sess.Currency),
		}
		if sess.Customer != nil {
			detail.CustomerID = sess.Customer.ID
		}
		if sess.PaymentIntent != nil && sess.PaymentIntent.LatestCharge != nil {
			detail.ChargeID = sess.PaymentIntent.LatestCharge.ID
		}
		if sess.Invoice != nil {
			detail.InvoiceURL = sess.Invoice.HostedInvoiceURL
		}
		out.Checkout = &detail

	case "charge.refunded":
		var ch stripego.Charge
		if err := json.Unmarshal(ev.Data.Raw, &ch); err != nil {
			return payments.Event{}, fmt.Errorf("decode charge: %w", err)
		}
		out.Type = payments.EventChargeRefunded
		detail := payments.ChargeDetail{
			ID:          ch.ID,
			AmountCents: ch.Amount,
			Currency:    string(ch.Currency),
		}
		if ch.Customer != nil {
			detail.CustomerID = ch.Customer.ID
		}
		out.Charge = &detail
	}
	return out, nil
}

// Refund issues a full refund against the named charge. ReasonRequestedByCustomer
// is the default — the more accurate ReasonFraudulent / ReasonDuplicate
// distinctions live in the Stripe dashboard and don't have a
// generic counterpart in payments.RefundParams yet.
func (p *Provider) Refund(ctx context.Context, params payments.RefundParams) (payments.Refund, error) {
	if params.ChargeID == "" {
		return payments.Refund{}, errors.New("stripe: missing charge id")
	}
	stripego.Key = p.SecretKey
	_ = ctx

	rf, err := refund.New(&stripego.RefundParams{
		Charge: stripego.String(params.ChargeID),
	})
	if err != nil {
		return payments.Refund{}, err
	}
	return payments.Refund{
		ID:     rf.ID,
		Status: string(rf.Status),
	}, nil
}

// CustomerPortal mints a Stripe-hosted self-service portal session
// for the given customer and returns the URL.
func (p *Provider) CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error) {
	if customerID == "" {
		return "", errors.New("stripe: missing customer id")
	}
	stripego.Key = p.SecretKey
	_ = ctx

	sess, err := portalsession.New(&stripego.BillingPortalSessionParams{
		Customer:  stripego.String(customerID),
		ReturnURL: stripego.String(returnURL),
	})
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}
