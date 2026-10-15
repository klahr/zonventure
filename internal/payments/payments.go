// Package payments is the seam between the rest of the app and the
// payment provider that processes ticket / purchase money. The
// `Provider` interface is deliberately small — checkout, refund,
// webhook ingest, customer portal — and its types are framed in
// generic e-commerce vocabulary so impls behind it don't leak SDK
// shapes into the call sites.
//
// The shipping impl is `internal/payments/stripe`. Future Mollie /
// Paddle / Adyen impls drop in next to it; any one of them satisfies
// `Provider`. Self-hosters who don't take payments at all leave the
// provider nil — call sites guard with `Enabled` (a nil-safe helper)
// before reaching in.
//
// Naming: "checkout session" is universal in the hosted-checkout
// world (Stripe Checkout, Mollie Checkout, etc.) so the type names
// match the industry term rather than picking a synonym.
package payments

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ErrNotSupported is returned when a Provider doesn't implement a
// particular optional feature (most commonly CustomerPortal — Stripe
// has a hosted self-service portal, others don't). Callers translate
// this into a 501-or-410-shaped error response rather than treating
// it as a server-side fault.
var ErrNotSupported = errors.New("payments: feature not supported by provider")

// Provider is the seam wired into admin handlers. One impl, one
// configured set of credentials, per running server.
type Provider interface {
	// Name identifies the impl in logs and metrics ("stripe",
	// "mollie", …). Stable across restarts.
	Name() string

	// CreateCheckout opens a hosted checkout session for the buyer.
	// On success, returns the URL to redirect to and the provider's
	// own session id (we persist it for redelivery dedupe). The
	// metadata bag round-trips through the provider unchanged and
	// shows up on the matching webhook event.
	CreateCheckout(ctx context.Context, p CheckoutParams) (CheckoutSession, error)

	// VerifyAndParseWebhook validates the body against the configured
	// signing secret and decodes it into a domain Event. The
	// `headers` argument carries the provider-specific signature
	// header; impls pluck what they need.
	//
	// Returns one of the canonical EventType values plus the typed
	// detail block for that flavour (Checkout / Charge). EventOther
	// is returned for shapes we don't act on; callers ack and
	// move on.
	VerifyAndParseWebhook(body []byte, headers http.Header) (Event, error)

	// Refund issues a full refund against a previously-captured
	// charge. The actual purchase-state flip happens via the
	// charge.refunded webhook (so a refund initiated in the
	// provider's dashboard takes the same code path as one
	// initiated here); this method just pulls the trigger.
	Refund(ctx context.Context, p RefundParams) (Refund, error)

	// CustomerPortal returns a hosted self-service URL where the
	// customer can update their card, view invoices, etc. Optional
	// — return ErrNotSupported when the underlying provider has
	// no equivalent surface, or when the operator hasn't enabled
	// it. Callers translate ErrNotSupported into a clear "feature
	// not enabled" message rather than a generic 5xx.
	CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error)
}

// CheckoutParams describes a one-off hosted checkout. Currency is
// ISO 4217 lower-case, amount is minor units (cents). Metadata is
// the bag the webhook handler reads back to find our row — keep
// keys short and provider-agnostic.
type CheckoutParams struct {
	AmountCents int64
	Currency    string
	ProductName string
	SuccessURL  string
	CancelURL   string

	// Buyer identification: pass either CustomerID (for repeat
	// buyers we already have on file) or CustomerEmail (for first-
	// time anonymous purchases). Some providers reject both at
	// once — the impl resolves the conflict.
	CustomerID    string
	CustomerEmail string

	// Metadata round-trips through the provider; the webhook
	// handler reads this back to find our purchase row.
	Metadata map[string]string

	// Stripe-style tax / invoice / address collection knobs. Other
	// providers may ignore. CollectTaxID + CollectAddress are
	// mostly for B2B-ish flows where the buyer needs a real
	// invoice; consumer ticket sales typically leave them off.
	// AutoTax treats AmountCents as tax-inclusive: tax is computed
	// out of it, so the buyer is charged exactly AmountCents.
	CollectTaxID    bool
	CollectAddress  bool
	AutoTax         bool
	InvoiceCreation bool

	// ShipCountries asks for a postal address to post something to,
	// in one of these ISO 3166-1 alpha-2 countries; none asks for
	// none. CheckoutDetail.Shipping carries it back. CollectAddress
	// is the billing address for an invoice, a different thing.
	ShipCountries []string

	// Marketplace fields — set together when the charge should be
	// destination-charged: money lands in the connected account
	// (ConnectedAccountID) first, with ApplicationFeeCents captured
	// by the platform. Both empty/zero leaves the charge on the
	// platform account directly (the regular flow). Providers that
	// don't satisfy MarketplaceProvider ignore these.
	ConnectedAccountID  string
	ApplicationFeeCents int64

	// IdempotencyKey deduplicates retried CreateCheckout calls. A
	// transport hiccup or browser double-click that re-runs the
	// purchase handler must not create two checkout sessions for
	// the same buyer; with the same key, Stripe returns the original
	// session. Callers derive a stable key from the local row id
	// (e.g. "checkout-purchase-42") so the dedupe is durable across
	// process restarts. Empty string disables idempotency, which
	// callers should avoid for state-changing requests.
	IdempotencyKey string
}

// CheckoutSession is what CreateCheckout returns. The buyer is
// redirected to URL; SessionID lets us correlate webhooks back to
// the in-flight session.
type CheckoutSession struct {
	URL       string
	SessionID string
	// CustomerID is the customer a card setup made, when it made one.
	CustomerID string
}

// EventType is the canonical event family — every provider speaks
// at least these two shapes. EventOther covers everything else
// (subscription lifecycle, dispute notifications, etc.) which we
// ack but don't act on.
type EventType string

const (
	EventCheckoutCompleted EventType = "checkout.completed"
	// EventCheckoutFailed fires when an async payment method (ACH,
	// BACS, SEPA debit, …) that left a completed session "unpaid"
	// is finally declined. Carries the same Checkout detail.
	EventCheckoutFailed EventType = "checkout.failed"
	EventChargeRefunded EventType = "charge.refunded"
	// EventConnectedAccountUpdated fires when a connected
	// marketplace account's verification / capability state
	// changes. The webhook handler uses it to refresh the
	// stripe_connect_charges_enabled cache without waiting for
	// the next manual refresh. Stripe Connect-specific; non-
	// marketplace providers never emit it.
	EventConnectedAccountUpdated EventType = "account.updated"
	// EventCardSaved fires when a card setup (CardBiller) completes:
	// Card holds the card, Metadata the setup's.
	EventCardSaved EventType = "card.saved"
	EventOther     EventType = "other"
)

// Event is the parsed shape of an incoming webhook. Provider names
// the impl that produced it (audit / cross-tenant guard); ID is the
// dedupe key written to processed_webhook_events; Created powers
// the redelivery reconcile window.
type Event struct {
	Provider string
	Type     EventType
	ID       string
	Created  time.Time
	Metadata map[string]string
	// Account is the connected account the event happened on: a store
	// sale charged on an organisation's own account. Empty for the
	// platform's own.
	Account string

	// Exactly one of these is populated based on Type. EventOther
	// leaves them all nil.
	Checkout         *CheckoutDetail
	Charge           *ChargeDetail
	ConnectedAccount *ConnectedAccount
	Card             *SavedCard
}

// CheckoutDetail is the post-payment view of a CheckoutSession.
// AmountTotal / Currency reflect what was actually charged after
// any provider-side coupons / tax. ChargeID is what Refund() and
// the charge.refunded path key on. CustomerID is what we persist
// on the org row for portal sessions; InvoiceURL surfaces in the
// admin UI when the provider issued one.
//
// PaymentStatus distinguishes a session that's truly paid from one
// where the buyer has only committed (async methods like ACH/PIX/BACS
// land checkout.session.completed before the funds actually clear).
// Fulfilment must gate on PaymentStatusPaid or PaymentStatusNoPayment;
// PaymentStatusUnpaid means "wait for the async-payment follow-up".
type CheckoutDetail struct {
	SessionID   string
	AmountTotal int64
	// AmountSubtotal is the line-item total before discounts and any
	// tax added on top — the figure that matches our stored price.
	AmountSubtotal int64
	Currency       string
	CustomerID     string
	ChargeID       string
	InvoiceURL     string
	PaymentStatus  PaymentStatus
	// Shipping is the postal address the buyer gave, when the checkout
	// asked for one (CheckoutParams.ShipCountries).
	Shipping *Shipping
}

// Shipping is a postal address collected at checkout.
type Shipping struct {
	Name, Line1, Line2, PostalCode, City, Country string
}

// PaymentStatus mirrors Stripe's `payment_status` on a Checkout
// Session: "paid" (synchronous, funds captured), "unpaid" (async
// method in flight), or "no_payment_required" (€0 session, e.g.
// 100% coupon). Other providers map onto the same three.
type PaymentStatus string

const (
	PaymentStatusPaid      PaymentStatus = "paid"
	PaymentStatusUnpaid    PaymentStatus = "unpaid"
	PaymentStatusNoPayment PaymentStatus = "no_payment_required"
)

// FulfilmentReady reports whether a checkout session is in a state
// where it's safe to grant the goods. Async-method sessions that are
// still PaymentStatusUnpaid land on the same checkout.session.completed
// event but must not trigger fulfilment until the async-payment
// follow-up (Stripe: checkout.session.async_payment_succeeded).
func (s PaymentStatus) FulfilmentReady() bool {
	return s == PaymentStatusPaid || s == PaymentStatusNoPayment
}

// ChargeDetail is the per-charge slice of a charge.refunded event.
// Currency + AmountCents come along for refund accounting.
//
// Refunded vs AmountRefunded distinguish a full refund from a partial:
// a charge.refunded webhook fires for every refund delta, including
// €5 off a €100 charge. Fulfilment-reversal logic (state='refunded',
// withdraw team) must only run when the full amount is back —
// otherwise an arranger initiating a partial refund from the Stripe
// dashboard would lose the team. FullyRefunded is the helper that
// captures the gate.
type ChargeDetail struct {
	ID             string
	AmountCents    int64
	AmountRefunded int64
	Refunded       bool
	Currency       string
	CustomerID     string
	// CheckoutSessionID names the checkout session that took the
	// charge, when the provider can tell. It matches rows whose
	// charge id was never recorded.
	CheckoutSessionID string
}

// FullyRefunded reports whether the charge has been refunded to zero
// (i.e. amount_refunded equals the original amount and Stripe has set
// the `refunded` flag). Only then should the matching purchase row
// flip to state='refunded'.
func (c ChargeDetail) FullyRefunded() bool {
	return c.Refunded && c.AmountRefunded >= c.AmountCents && c.AmountCents > 0
}

// RefundParams names a charge by id. Partial refunds aren't
// modelled — every refund we issue is full-amount today.
type RefundParams struct {
	ChargeID string

	// RefundApplicationFee, when true, gives back the platform's
	// application fee with the refund: the organisation then gets
	// back what it paid the platform for the sale. Marketplace-
	// specific; providers that don't satisfy MarketplaceProvider
	// ignore it.
	RefundApplicationFee bool

	// ConnectedAccountID names the organisation's account a store
	// sale was charged on (a direct charge): the refund is made
	// there. Empty for a charge on the platform's own account.
	ConnectedAccountID string

	// IdempotencyKey deduplicates retried Refund calls. A double-
	// click on the admin "refund" button must not cut two refund
	// objects against the same charge. Callers derive a stable key
	// from the local row id (e.g. "refund-purchase-42"). The key
	// must NOT be reused across genuinely-distinct refunds (full
	// after a partial, etc.); embedding the local row id and the
	// intended amount in the key avoids that.
	IdempotencyKey string
}

// Refund is the post-call view of a refund. Status is provider-
// specific ("succeeded", "pending", …) and surfaces in the admin
// UI verbatim.
type Refund struct {
	ID     string
	Status string
}

// Enabled is a nil-safe helper that returns true when the caller
// has a usable provider. Lets `if payments.Enabled(h.Provider)`
// replace the older `if h.stripeEnabled()` style without forcing
// a nil-check at every call site.
func Enabled(p Provider) bool { return p != nil }
