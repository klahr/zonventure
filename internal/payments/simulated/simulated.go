// Package simulated is a payments.Provider impl with no real
// payment processor behind it — checkouts auto-confirm in-process,
// "refunds" succeed without spending money, webhooks are
// synthesised locally. Two purposes:
//
//  1. Validates the Provider abstraction: switching the operator
//     env from `stripe` to `simulated` exercises a non-Stripe
//     `payments.Event` shape end-to-end and confirms the rest of
//     the app stays provider-agnostic.
//
//  2. Useful for dev / CI flows that need to walk through a
//     ticket purchase without real Stripe credentials.
//
// Strictly NOT for production: nothing is verified, nothing is
// charged, and the webhook bypass means anyone hitting the dev
// route can fulfil any pending purchase. The wiring layer in
// internal/server refuses to instantiate this provider unless
// ZV_ENV is "dev"/"development" — operators who want this in a
// staging environment have to deliberately opt in via that knob.
package simulated

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/payments"

	"github.com/go-chi/chi/v5"
)

// session is the in-memory record of a pending checkout. Created
// by CreateCheckout, deleted on confirm/cancel. The map is keyed
// by session id; lock protects concurrent fulfilment + race
// between the buyer's "pay" click and a stale browser tab.
type session struct {
	params  payments.CheckoutParams
	created time.Time
}

// Provider implements payments.Provider with a fake processor. Self-
// contained — no SDK, no network — so a build with this provider
// wired needs no external service for `go run` to walk the buy
// flow.
type Provider struct {
	mu       sync.Mutex
	sessions map[string]*session

	// hooks is the path of the local /api/webhooks/* route the
	// dev confirm handler POSTs the synthetic event to. Empty
	// disables the in-process delivery; the operator can still
	// drive the bypass directly via curl.
	WebhookURL string

	// HTTPClient is the client used to deliver synthetic webhook
	// events. Substituted in tests so the auto-confirm flow can be
	// exercised without standing up a real http server.
	HTTPClient *http.Client

	// secret is a per-process random value the simulated provider
	// stamps on its outbound webhook deliveries (X-Simulated-Secret)
	// and demands on every inbound VerifyAndParseWebhook call. It
	// turns the "no signature check" gap into a defence-in-depth
	// posture: a forgotten ZV_ENV that leaves the simulated route
	// mounted in staging no longer accepts unauthenticated POSTs
	// from the public internet.
	secret string
}

// Compile-time interface assertion — same posture as the Stripe
// impl, catches signature drift.
var _ payments.Provider = (*Provider)(nil)

// New returns a configured simulated provider. WebhookURL is the
// absolute URL of the local /api/webhooks/stripe (or whichever
// route the operator mounts at) that the dev confirm handler
// posts synthesised events to. Empty means "no auto-confirm" —
// useful for tests that drive the webhook handler directly.
func New(webhookURL string) *Provider {
	return &Provider{
		sessions:   make(map[string]*session),
		WebhookURL: webhookURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		secret:     newSessionID(),
	}
}

// Name identifies this impl in logs / metrics.
func (p *Provider) Name() string { return "simulated" }

// Secret returns the per-process X-Simulated-Secret value. Exposed
// for tests that drive VerifyAndParseWebhook directly without going
// through deliver(). Production handlers never reach for it.
func (p *Provider) Secret() string { return p.secret }

// CreateCheckout stores the params under a fresh session id and
// returns a URL pointing at the dev confirm route. The buyer's
// browser navigates to that URL, sees a tiny "Pay now / Cancel"
// page, and one click later the rest of the system sees a
// genuine checkout.completed event.
func (p *Provider) CreateCheckout(ctx context.Context, params payments.CheckoutParams) (payments.CheckoutSession, error) {
	if params.SuccessURL == "" || params.CancelURL == "" {
		return payments.CheckoutSession{}, errors.New("simulated: missing success/cancel URL")
	}
	id := newSessionID()
	p.mu.Lock()
	p.sessions[id] = &session{params: params, created: time.Now()}
	p.mu.Unlock()
	// The dev URL lives on the same origin as the buyer's flow
	// (the Stripe-equivalent of a hosted checkout). The buyer's
	// success/cancel URLs are kept verbatim — they round-trip
	// through the confirm handler.
	return payments.CheckoutSession{
		URL:       "/dev/payments/checkout/" + id,
		SessionID: id,
	}, nil
}

// VerifyAndParseWebhook parses a JSON body emitted by the dev
// confirm handler. The header X-Simulated-Secret must match the
// per-process secret stamped on outbound deliveries — a defence in
// depth on top of the ZV_ENV gate. The body itself is trusted (this
// provider is dev-only) but the secret check stops a forgotten
// ZV_ENV from turning the route into a public fulfilment buffet.
func (p *Provider) VerifyAndParseWebhook(body []byte, headers http.Header) (payments.Event, error) {
	if got := headers.Get("X-Simulated-Secret"); got == "" || got != p.secret {
		return payments.Event{}, errors.New("simulated: shared secret mismatch")
	}
	var ev payments.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return payments.Event{}, fmt.Errorf("simulated: parse: %w", err)
	}
	ev.Provider = "simulated"
	if ev.ID == "" {
		ev.ID = "sim_" + newSessionID()
	}
	if ev.Created.IsZero() {
		ev.Created = time.Now()
	}
	return ev, nil
}

// Refund is a no-op that returns success. Pairs with the
// charge.refunded webhook event the dev confirm handler can
// synthesise for testing the refund path.
func (p *Provider) Refund(ctx context.Context, params payments.RefundParams) (payments.Refund, error) {
	if params.ChargeID == "" {
		return payments.Refund{}, errors.New("simulated: missing charge id")
	}
	return payments.Refund{
		ID:     "sim_re_" + newSessionID(),
		Status: "succeeded",
	}, nil
}

// CustomerPortal is unsupported — no real customer state to portal
// into. Surfaces ErrNotSupported so the admin UI shows the
// "feature not enabled" path instead of a 5xx.
func (p *Provider) CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error) {
	return "", payments.ErrNotSupported
}

// MountDevRoutes attaches the dev-only checkout + confirm + cancel
// routes to the given chi router. Safe to call from server.New
// when the operator selects the simulated provider; never wire on
// production deployments.
func (p *Provider) MountDevRoutes(r chi.Router) {
	r.Get("/dev/payments/checkout/{sid}", p.devCheckoutPage)
	r.Post("/dev/payments/checkout/{sid}/confirm", p.devConfirm)
	r.Post("/dev/payments/checkout/{sid}/cancel", p.devCancel)
}

// devCheckoutPage renders a minimal HTML page summarising the
// pending purchase + offering Pay / Cancel buttons. Auto-confirm
// would skip the page, but a real button click matches what a
// buyer experiences on Stripe-hosted checkout and gives a chance
// to inspect the params during development.
func (p *Provider) devCheckoutPage(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	p.mu.Lock()
	s, ok := p.sessions[sid]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	page := `<!doctype html>
<html><head><meta charset="utf-8"><title>Simulated checkout — ` + html.EscapeString(s.params.ProductName) + `</title>
<style>
body{font-family:system-ui,sans-serif;max-width:30rem;margin:3rem auto;padding:0 1rem;color:#1f3349}
h1{font-size:1.4rem}
.row{display:flex;justify-content:space-between;padding:.5rem 0;border-bottom:1px solid #e2e7ef}
.banner{background:#fef3c7;border:1px solid #fcd34d;color:#78350f;padding:.75rem 1rem;border-radius:8px;margin:1rem 0;font-size:.9rem}
form{display:inline}
button{padding:.7rem 1.4rem;border-radius:8px;border:1px solid #cbd5e1;background:#fff;font:inherit;cursor:pointer;margin-right:.5rem}
button.pay{background:linear-gradient(135deg,#ad1357,#e24080);color:#fff;border:none}
</style></head><body>
<div class="banner">⚠️ This is a SIMULATED payment provider. No real money moves. Do not deploy in production.</div>
<h1>` + html.EscapeString(s.params.ProductName) + `</h1>
<div class="row"><span>Amount</span><strong>` + formatPrice(s.params.AmountCents, s.params.Currency) + `</strong></div>
<div class="row"><span>Buyer</span><span>` + html.EscapeString(s.params.CustomerEmail) + `</span></div>
<div class="row"><span>Session id</span><code>` + html.EscapeString(sid) + `</code></div>
<div style="margin-top:1.5rem">
  <form action="/dev/payments/checkout/` + html.EscapeString(sid) + `/confirm" method="post"><button class="pay" type="submit">Pay (simulate)</button></form>
  <form action="/dev/payments/checkout/` + html.EscapeString(sid) + `/cancel" method="post"><button type="submit">Cancel</button></form>
</div>
</body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, page)
}

// devConfirm fires a synthetic checkout.completed event into the
// configured webhook URL, then redirects the buyer to the original
// SuccessURL. Idempotent: a second click sees the session deleted
// and falls through to a 404, the same shape Stripe gives for a
// stale session.
func (p *Provider) devConfirm(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	p.mu.Lock()
	s, ok := p.sessions[sid]
	if ok {
		delete(p.sessions, sid)
	}
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Synthesise a charge id alongside the session id so the
	// downstream charge.refunded path can find the row by
	// stripe_charge_id later.
	chargeID := "sim_ch_" + newSessionID()
	ev := payments.Event{
		Provider: "simulated",
		Type:     payments.EventCheckoutCompleted,
		ID:       "sim_evt_" + newSessionID(),
		Created:  time.Now(),
		Metadata: s.params.Metadata,
		Checkout: &payments.CheckoutDetail{
			SessionID:      sid,
			AmountTotal:    s.params.AmountCents,
			AmountSubtotal: s.params.AmountCents,
			Currency:       strings.ToLower(s.params.Currency),
			ChargeID:       chargeID,
			PaymentStatus:  payments.PaymentStatusPaid,
		},
	}
	if err := p.deliver(r.Context(), ev); err != nil {
		http.Error(w, "simulated webhook delivery failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Stripe substitutes {CHECKOUT_SESSION_ID} at redirect time;
	// the simulated provider mirrors that so callers get the same
	// query-string shape on both backends. The /store post-
	// purchase poll relies on it.
	successURL := strings.ReplaceAll(s.params.SuccessURL, "{CHECKOUT_SESSION_ID}", sid)
	http.Redirect(w, r, successURL, http.StatusSeeOther)
}

// devCancel drops the pending session and redirects to the
// buyer's CancelURL. No webhook event is fired — that matches
// Stripe's posture, where cancelled sessions silently expire.
func (p *Provider) devCancel(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	p.mu.Lock()
	s, ok := p.sessions[sid]
	if ok {
		delete(p.sessions, sid)
	}
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, s.params.CancelURL, http.StatusSeeOther)
}

// deliver POSTs the synthesised event to the configured webhook
// URL. We go through the real HTTP path rather than calling the
// admin handler directly so the existing dedupe + transaction
// logic exercises end-to-end.
func (p *Provider) deliver(ctx context.Context, ev payments.Event) error {
	if p.WebhookURL == "" {
		return errors.New("WebhookURL not configured — cannot auto-deliver")
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", p.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Simulated-Secret", p.secret)
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webhook %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	return nil
}

// newSessionID returns a 16-byte hex-encoded random id. Same
// entropy as a Stripe session id, distinct namespace via the
// `sim_` prefix on emitted records.
func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a fixed string; tests will still see
		// distinct ids because the address differs per session.
		return fmt.Sprintf("fallback_%p", &b)
	}
	return hex.EncodeToString(b)
}

// formatPrice mirrors the JS helper on the listing page so the
// dev-only checkout page reads consistently with /store. Best-
// effort, English-only.
func formatPrice(cents int64, currency string) string {
	c := strings.ToLower(currency)
	amount := fmt.Sprintf("%.2f", float64(cents)/100)
	amount = strings.TrimSuffix(amount, ".00")
	switch c {
	case "eur":
		return "€" + amount
	case "usd":
		return "$" + amount
	case "gbp":
		return "£" + amount
	case "sek", "dkk", "nok":
		return amount + " kr"
	default:
		return amount + " " + strings.ToUpper(c)
	}
}
