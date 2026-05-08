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
	"github.com/stripe/stripe-go/v82/account"
	"github.com/stripe/stripe-go/v82/accountlink"
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

// Compile-time assertion that *Provider satisfies both interfaces.
// Catches signature drift if either side changes shape.
var (
	_ payments.Provider            = (*Provider)(nil)
	_ payments.MarketplaceProvider = (*Provider)(nil)
)

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

	// Marketplace path: the charge is destination-charged into a
	// connected Standard account, with the platform's application
	// fee captured off the top. Both fields must be set together —
	// a non-empty ConnectedAccountID with no fee would produce a
	// "free passthrough" charge that benefits nobody, and a fee
	// without a destination has nowhere to send the rest of the
	// money. Caller is expected to honour both-or-neither; we
	// defensively branch on the destination.
	if params.ConnectedAccountID != "" {
		csParams.PaymentIntentData = &stripego.CheckoutSessionPaymentIntentDataParams{
			ApplicationFeeAmount: stripego.Int64(params.ApplicationFeeCents),
			TransferData: &stripego.CheckoutSessionPaymentIntentDataTransferDataParams{
				Destination: stripego.String(params.ConnectedAccountID),
			},
		}
	}

	// Plumb ctx onto the embedded stripe.Params so the underlying
	// HTTP request is bound to the caller's lifetime. A browser-close
	// or upstream timeout now aborts the in-flight Stripe call rather
	// than letting it run to completion against a vanished caller.
	csParams.Context = ctx
	if params.IdempotencyKey != "" {
		csParams.IdempotencyKey = stripego.String(params.IdempotencyKey)
	}

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
			SessionID:     sess.ID,
			AmountTotal:   sess.AmountTotal,
			Currency:      string(sess.Currency),
			PaymentStatus: payments.PaymentStatus(sess.PaymentStatus),
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
			ID:             ch.ID,
			AmountCents:    ch.Amount,
			AmountRefunded: ch.AmountRefunded,
			Refunded:       ch.Refunded,
			Currency:       string(ch.Currency),
		}
		if ch.Customer != nil {
			detail.CustomerID = ch.Customer.ID
		}
		out.Charge = &detail

	case "account.updated":
		// Connect Standard accounts emit account.updated through
		// the platform webhook endpoint when their verification or
		// capability state changes. The event body is the account
		// itself; the load-bearing fields are charges_enabled and
		// payouts_enabled, which the admin layer caches into the
		// org row so the listing query stays cheap.
		var a stripego.Account
		if err := json.Unmarshal(ev.Data.Raw, &a); err != nil {
			return payments.Event{}, fmt.Errorf("decode account: %w", err)
		}
		out.Type = payments.EventConnectedAccountUpdated
		out.ConnectedAccount = &payments.ConnectedAccount{
			ID:             a.ID,
			ChargesEnabled: a.ChargesEnabled,
			PayoutsEnabled: a.PayoutsEnabled,
		}
	}
	return out, nil
}

// Refund issues a full refund against the named charge. ReasonRequestedByCustomer
// is the default — the more accurate ReasonFraudulent / ReasonDuplicate
// distinctions live in the Stripe dashboard and don't have a
// generic counterpart in payments.RefundParams yet.
//
// For destination-charged refunds (RefundApplicationFee=true), we
// set ReverseTransfer so the connected account's transfer is
// undone proportionally and RefundApplicationFee so the platform
// returns its cut. The original charge always lives on the
// platform account (that's where destination charges originate),
// so the refund call doesn't need a Stripe-Account header.
func (p *Provider) Refund(ctx context.Context, params payments.RefundParams) (payments.Refund, error) {
	if params.ChargeID == "" {
		return payments.Refund{}, errors.New("stripe: missing charge id")
	}
	stripego.Key = p.SecretKey

	rp := &stripego.RefundParams{
		Charge: stripego.String(params.ChargeID),
	}
	rp.Context = ctx
	if params.RefundApplicationFee {
		rp.RefundApplicationFee = stripego.Bool(true)
		rp.ReverseTransfer = stripego.Bool(true)
	}
	if params.IdempotencyKey != "" {
		rp.IdempotencyKey = stripego.String(params.IdempotencyKey)
	}
	rf, err := refund.New(rp)
	if err != nil {
		return payments.Refund{}, err
	}
	return payments.Refund{
		ID:     rf.ID,
		Status: string(rf.Status),
	}, nil
}

/* ---------------- MarketplaceProvider ---------------- */

// CreateConnectedAccount creates a Standard Connect account. The
// arranger keeps full control of the resulting Stripe account —
// they accept Stripe's TOS during onboarding, see Stripe's brand
// in their dashboard, and pay normal Stripe processing fees on
// their side. The platform pays nothing extra per account.
//
// Country is ISO 3166-1 alpha-2; an empty value means "let Stripe
// infer from the platform's default", which works for most
// single-country deployments.
func (p *Provider) CreateConnectedAccount(ctx context.Context, params payments.ConnectedAccountParams) (string, error) {
	stripego.Key = p.SecretKey

	ap := &stripego.AccountParams{
		Type: stripego.String(string(stripego.AccountTypeStandard)),
	}
	ap.Context = ctx
	if params.Email != "" {
		ap.Email = stripego.String(params.Email)
	}
	if params.Country != "" {
		ap.Country = stripego.String(strings.ToUpper(params.Country))
	}
	if params.DisplayName != "" {
		ap.BusinessProfile = &stripego.AccountBusinessProfileParams{
			Name: stripego.String(params.DisplayName),
		}
	}
	if params.IdempotencyKey != "" {
		ap.IdempotencyKey = stripego.String(params.IdempotencyKey)
	}
	a, err := account.New(ap)
	if err != nil {
		return "", err
	}
	return a.ID, nil
}

// CreateAccountLink mints a hosted onboarding URL the arranger
// follows to complete (or resume) Connect onboarding. The link is
// single-use — once the user clicks it, follow-up retries need a
// fresh link generated server-side.
func (p *Provider) CreateAccountLink(ctx context.Context, params payments.AccountLinkParams) (string, error) {
	if params.AccountID == "" {
		return "", errors.New("stripe: missing account id")
	}
	if params.RefreshURL == "" || params.ReturnURL == "" {
		return "", errors.New("stripe: missing refresh/return URL")
	}
	stripego.Key = p.SecretKey

	alParams := &stripego.AccountLinkParams{
		Account:    stripego.String(params.AccountID),
		RefreshURL: stripego.String(params.RefreshURL),
		ReturnURL:  stripego.String(params.ReturnURL),
		Type:       stripego.String("account_onboarding"),
	}
	alParams.Context = ctx
	link, err := accountlink.New(alParams)
	if err != nil {
		return "", err
	}
	return link.URL, nil
}

// GetConnectedAccount fetches the live status flags from Stripe.
// Called from the manual "refresh" affordance on the Connect
// settings page and (eventually) from the account.updated webhook
// handler. Returns ChargesEnabled / PayoutsEnabled which the
// caller writes back into the local DB cache so subsequent
// requests don't round-trip.
func (p *Provider) GetConnectedAccount(ctx context.Context, accountID string) (payments.ConnectedAccount, error) {
	if accountID == "" {
		return payments.ConnectedAccount{}, errors.New("stripe: missing account id")
	}
	stripego.Key = p.SecretKey

	getParams := &stripego.AccountParams{}
	getParams.Context = ctx
	a, err := account.GetByID(accountID, getParams)
	if err != nil {
		return payments.ConnectedAccount{}, err
	}
	return payments.ConnectedAccount{
		ID:             a.ID,
		ChargesEnabled: a.ChargesEnabled,
		PayoutsEnabled: a.PayoutsEnabled,
	}, nil
}

// CustomerPortal mints a Stripe-hosted self-service portal session
// for the given customer and returns the URL.
func (p *Provider) CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error) {
	if customerID == "" {
		return "", errors.New("stripe: missing customer id")
	}
	stripego.Key = p.SecretKey

	psParams := &stripego.BillingPortalSessionParams{
		Customer:  stripego.String(customerID),
		ReturnURL: stripego.String(returnURL),
	}
	psParams.Context = ctx
	sess, err := portalsession.New(psParams)
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}
