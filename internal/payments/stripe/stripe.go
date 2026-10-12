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

	"github.com/klahr/zonventure/internal/payments"

	stripego "github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/account"
	"github.com/stripe/stripe-go/v87/accountlink"
	portalsession "github.com/stripe/stripe-go/v87/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v87/checkout/session"
	"github.com/stripe/stripe-go/v87/loginlink"
	"github.com/stripe/stripe-go/v87/refund"
	"github.com/stripe/stripe-go/v87/webhook"
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

// IsTestKey reports whether key is a Stripe test-mode secret or
// restricted key.
func IsTestKey(key string) bool {
	return strings.HasPrefix(key, "sk_test_") || strings.HasPrefix(key, "rk_test_")
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
		// Our prices are shown and stored gross, so tax comes out of
		// the amount rather than on top: the buyer pays AmountCents.
		csParams.LineItems[0].PriceData.TaxBehavior = stripego.String(
			string(stripego.PriceTaxBehaviorInclusive))
	}
	// Stripe refuses automatic tax / tax id collection for an existing
	// customer unless Checkout may write the collected address and
	// name back onto it.
	if params.CustomerID != "" && (params.AutoTax || params.CollectTaxID) {
		csParams.CustomerUpdate = &stripego.CheckoutSessionCustomerUpdateParams{
			Address: stripego.String("auto"),
			Name:    stripego.String("auto"),
		}
	}
	if params.CollectAddress {
		csParams.BillingAddressCollection = stripego.String(
			string(stripego.CheckoutSessionBillingAddressCollectionRequired))
	}
	if len(params.ShipCountries) > 0 {
		csParams.ShippingAddressCollection = &stripego.CheckoutSessionShippingAddressCollectionParams{
			AllowedCountries: stripego.StringSlice(params.ShipCountries),
		}
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
	// connected Express account, with the platform's application
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
	case "checkout.session.completed",
		"checkout.session.async_payment_succeeded",
		"checkout.session.async_payment_failed":
		var sess stripego.CheckoutSession
		if err := json.Unmarshal(ev.Data.Raw, &sess); err != nil {
			return payments.Event{}, fmt.Errorf("decode checkout.session: %w", err)
		}
		// async_payment_succeeded carries payment_status="paid" and
		// fulfils through the same path as a synchronous completion.
		out.Type = payments.EventCheckoutCompleted
		if ev.Type == "checkout.session.async_payment_failed" {
			out.Type = payments.EventCheckoutFailed
		}
		out.Metadata = sess.Metadata
		detail := payments.CheckoutDetail{
			SessionID:      sess.ID,
			AmountTotal:    sess.AmountTotal,
			AmountSubtotal: sess.AmountSubtotal,
			Currency:       string(sess.Currency),
			PaymentStatus:  payments.PaymentStatus(sess.PaymentStatus),
		}
		if sess.Customer != nil {
			detail.CustomerID = sess.Customer.ID
		}
		// Webhook payloads carry payment_intent and invoice as bare ids,
		// so the charge id (what Refund and charge.refunded key on) and
		// the hosted invoice URL need the session re-read with those
		// expanded. Only for a paid session: that's the one we fulfil.
		if detail.PaymentStatus == payments.PaymentStatusPaid && needsExpand(&sess) {
			if err := p.expandSession(&sess); err != nil {
				return payments.Event{}, fmt.Errorf("expand checkout.session %s: %w", sess.ID, err)
			}
		}
		if sess.PaymentIntent != nil && sess.PaymentIntent.LatestCharge != nil {
			detail.ChargeID = sess.PaymentIntent.LatestCharge.ID
		}
		if sess.Invoice != nil {
			detail.InvoiceURL = sess.Invoice.HostedInvoiceURL
		}
		if ci := sess.CollectedInformation; ci != nil && ci.ShippingDetails != nil && ci.ShippingDetails.Address != nil {
			a := ci.ShippingDetails.Address
			detail.Shipping = &payments.Shipping{Name: ci.ShippingDetails.Name, Line1: a.Line1, Line2: a.Line2,
				PostalCode: a.PostalCode, City: a.City, Country: a.Country}
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
		// Rows fulfilled before the charge id was recorded can only be
		// matched through their checkout session, so name it.
		if ch.PaymentIntent != nil && ch.PaymentIntent.ID != "" {
			sessID, err := p.sessionForPaymentIntent(ch.PaymentIntent.ID)
			if err != nil {
				return payments.Event{}, fmt.Errorf("checkout session for %s: %w", ch.PaymentIntent.ID, err)
			}
			detail.CheckoutSessionID = sessID
		}
		out.Charge = &detail

	case "account.updated":
		// Connected accounts emit account.updated through
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

// webhookAPITimeout bounds the Stripe reads made while parsing a
// webhook; a timeout fails the delivery and Stripe redelivers.
const webhookAPITimeout = 10 * time.Second

// needsExpand reports whether a webhook's checkout session names a
// payment intent or invoice without the fields we record from them.
func needsExpand(sess *stripego.CheckoutSession) bool {
	return (sess.PaymentIntent != nil && sess.PaymentIntent.ID != "" && sess.PaymentIntent.LatestCharge == nil) ||
		(sess.Invoice != nil && sess.Invoice.ID != "" && sess.Invoice.HostedInvoiceURL == "")
}

// expandSession replaces sess's payment intent and invoice with the
// expanded objects from a fresh read of the session.
func (p *Provider) expandSession(sess *stripego.CheckoutSession) error {
	stripego.Key = p.SecretKey
	ctx, cancel := context.WithTimeout(context.Background(), webhookAPITimeout)
	defer cancel()
	params := &stripego.CheckoutSessionParams{}
	params.Context = ctx
	params.AddExpand("payment_intent")
	params.AddExpand("invoice")
	full, err := checkoutsession.Get(sess.ID, params)
	if err != nil {
		return err
	}
	sess.PaymentIntent = full.PaymentIntent
	sess.Invoice = full.Invoice
	return nil
}

// sessionForPaymentIntent returns the id of the checkout session that
// created the payment intent, or "" when it wasn't one of ours.
func (p *Provider) sessionForPaymentIntent(paymentIntentID string) (string, error) {
	stripego.Key = p.SecretKey
	ctx, cancel := context.WithTimeout(context.Background(), webhookAPITimeout)
	defer cancel()
	params := &stripego.CheckoutSessionListParams{PaymentIntent: stripego.String(paymentIntentID)}
	params.Context = ctx
	params.Limit = stripego.Int64(1)
	it := checkoutsession.List(params)
	if it.Next() {
		return it.CheckoutSession().ID, nil
	}
	return "", it.Err()
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

// CreateConnectedAccount creates an Express Connect account. Stripe
// runs the KYC during hosted onboarding; the platform owns the
// customer relationship: charges are destination-charged on the
// platform account (which carries refunds and disputes), and the
// seller sees only the Express dashboard (balance, payouts) via
// CreateLoginLink. Stripe bills the platform per active account
// and per payout.
//
// card_payments + transfers are both requested: transfers is what
// destination charges need, card_payments lets Stripe settle card
// charges on the account's behalf should on_behalf_of come later.
//
// Payouts run monthly, on the 1st: the platform pays Stripe per
// payout, so one payout a month per org keeps that cost down.
// Express sellers can't change the schedule themselves.
//
// Country is ISO 3166-1 alpha-2; an empty value means "let Stripe
// infer from the platform's default", which works for most
// single-country deployments.
func (p *Provider) CreateConnectedAccount(ctx context.Context, params payments.ConnectedAccountParams) (string, error) {
	stripego.Key = p.SecretKey

	ap := &stripego.AccountParams{
		Type: stripego.String(string(stripego.AccountTypeExpress)),
		Capabilities: &stripego.AccountCapabilitiesParams{
			CardPayments: &stripego.AccountCapabilitiesCardPaymentsParams{Requested: stripego.Bool(true)},
			Transfers:    &stripego.AccountCapabilitiesTransfersParams{Requested: stripego.Bool(true)},
		},
		Settings: &stripego.AccountSettingsParams{
			Payouts: &stripego.AccountSettingsPayoutsParams{
				Schedule: &stripego.AccountSettingsPayoutsScheduleParams{
					Interval:      stripego.String(string(stripego.AccountSettingsPayoutsScheduleIntervalMonthly)),
					MonthlyAnchor: stripego.Int64(1),
				},
			},
		},
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

// CreateLoginLink mints a single-use URL into the connected
// account's Express dashboard, where the seller sees their balance
// and payouts. Stripe refuses it for accounts without the Express
// dashboard (orgs onboarded as Standard before the switch); those
// sellers sign in at dashboard.stripe.com instead.
func (p *Provider) CreateLoginLink(ctx context.Context, accountID string) (string, error) {
	if accountID == "" {
		return "", errors.New("stripe: missing account id")
	}
	stripego.Key = p.SecretKey

	llParams := &stripego.LoginLinkParams{
		Account: stripego.String(accountID),
	}
	llParams.Context = ctx
	link, err := loginlink.New(llParams)
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
