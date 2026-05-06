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
	CollectTaxID    bool
	CollectAddress  bool
	AutoTax         bool
	InvoiceCreation bool
}

// CheckoutSession is what CreateCheckout returns. The buyer is
// redirected to URL; SessionID lets us correlate webhooks back to
// the in-flight session.
type CheckoutSession struct {
	URL       string
	SessionID string
}

// EventType is the canonical event family — every provider speaks
// at least these two shapes. EventOther covers everything else
// (subscription lifecycle, dispute notifications, etc.) which we
// ack but don't act on.
type EventType string

const (
	EventCheckoutCompleted EventType = "checkout.completed"
	EventChargeRefunded    EventType = "charge.refunded"
	EventOther             EventType = "other"
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

	// Exactly one of these is populated based on Type. EventOther
	// leaves both nil.
	Checkout *CheckoutDetail
	Charge   *ChargeDetail
}

// CheckoutDetail is the post-payment view of a CheckoutSession.
// AmountTotal / Currency reflect what was actually charged after
// any provider-side coupons / tax. ChargeID is what Refund() and
// the charge.refunded path key on. CustomerID is what we persist
// on the org row for portal sessions; InvoiceURL surfaces in the
// admin UI when the provider issued one.
type CheckoutDetail struct {
	SessionID   string
	AmountTotal int64
	Currency    string
	CustomerID  string
	ChargeID    string
	InvoiceURL  string
}

// ChargeDetail is the per-charge slice of a charge.refunded event.
// Currency + AmountCents come along for refund accounting.
type ChargeDetail struct {
	ID          string
	AmountCents int64
	Currency    string
	CustomerID  string
}

// RefundParams names a charge by id. Partial refunds aren't
// modelled — every refund we issue is full-amount today.
type RefundParams struct {
	ChargeID string
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
