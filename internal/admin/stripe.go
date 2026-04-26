package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/metrics"
	"git.sr.ht/~klahr/zv/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/refund"
	"github.com/stripe/stripe-go/v82/webhook"
)

// stripeEnabled reports whether the operator has wired Stripe credentials.
// Both the secret key and the webhook secret must be present — without
// the webhook secret we'd accept unauthenticated "purchase paid" claims.
func (h *Handlers) stripeEnabled() bool {
	return h.StripeSecretKey != "" && h.StripeWebhookSecret != ""
}

// createStripeCheckoutSession builds a Checkout Session for a pending
// purchase and returns its hosted URL + session id. The price is
// inlined via PriceData (no Stripe Price catalog entry to maintain),
// metadata carries our purchase id so the webhook can find the row,
// and success/cancel both bounce back to /arrange.
func (h *Handlers) createStripeCheckoutSession(ctx context.Context, purchaseID int64,
	advSlug, advTitle string, maxTeams, amountCents int, customerEmail string,
) (string, string, error) {
	stripe.Key = h.StripeSecretKey

	publicURL := strings.TrimRight(h.PublicURL, "/")
	if publicURL == "" {
		// Stripe needs an absolute URL; bail loudly rather than building
		// a Checkout Session that would land users somewhere broken.
		return "", "", errors.New("stripe checkout requires PublicURL to be configured")
	}

	productName := advTitle
	if productName == "" {
		productName = advSlug
	}
	productName = fmt.Sprintf("%s — %d team slot(s)", productName, maxTeams)

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency:   stripe.String("eur"),
					UnitAmount: stripe.Int64(int64(amountCents)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(productName),
					},
				},
			},
		},
		// Both URLs return to the arrange app. The query param tells
		// the front-end which toast to flash; the actual paid-state
		// flip happens via the webhook, not via the redirect.
		SuccessURL: stripe.String(fmt.Sprintf("%s/arrange#purchase-success/%d", publicURL, purchaseID)),
		CancelURL:  stripe.String(fmt.Sprintf("%s/arrange#purchase-cancelled/%d", publicURL, purchaseID)),
		// Metadata is what binds the Stripe-side session to our row.
		// CheckoutSessionMetadata is the field on every Stripe-created
		// resource we can stash arbitrary key/value pairs in; we read
		// it back from webhook payloads.
	}
	params.AddMetadata("zv_purchase_id", fmt.Sprintf("%d", purchaseID))
	if customerEmail != "" {
		params.CustomerEmail = stripe.String(customerEmail)
	}
	// Auto-collect tax via Stripe Tax — once the operator's account is
	// configured, this single boolean covers VAT/GST/sales tax for
	// every checkout. No-op when Stripe Tax is off.
	params.AutomaticTax = &stripe.CheckoutSessionAutomaticTaxParams{
		Enabled: stripe.Bool(true),
	}

	s, err := session.New(params)
	if err != nil {
		return "", "", err
	}
	return s.URL, s.ID, nil
}

// StripeWebhook receives Stripe events. Verifies the HMAC signature
// using the configured webhook secret, dedupes by event id, and
// dispatches on the event type. Always responds 200 on a valid event
// (even ones we don't handle) — Stripe redrives on any non-2xx, and
// we don't want unhandled types to feed an infinite retry loop.
func (h *Handlers) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.stripeEnabled() {
		http.Error(w, "stripe not configured", http.StatusServiceUnavailable)
		return
	}
	// Cap the read so a misbehaving caller can't OOM us. Stripe events
	// are typically <50 KB; 1 MB is generous headroom.
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	if len(body) > maxBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	sig := r.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(body, sig, h.StripeWebhookSecret)
	if err != nil {
		// Don't surface details — a forged-signature attacker
		// shouldn't learn what passes the parser and what doesn't.
		logx.Warn("stripe.webhook.invalid_sig", "err", err.Error())
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}

	// Idempotency. Stripe redrives on non-2xx and on transient
	// failures even when the original delivery succeeded; we record
	// every event id we've acted on so a redelivery is a no-op.
	already, err := h.alreadyProcessed(r.Context(), event.ID)
	if err != nil {
		logx.Error("stripe.webhook.dedupe_lookup", "event_id", event.ID, "err", err.Error())
		http.Error(w, "dedupe lookup failed", http.StatusInternalServerError)
		return
	}
	if already {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		if err := h.handleCheckoutCompleted(r.Context(), event); err != nil {
			logx.Error("stripe.webhook.checkout_completed", "event_id", event.ID, "err", err.Error())
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
	case "charge.refunded":
		if err := h.handleChargeRefunded(r.Context(), event); err != nil {
			logx.Error("stripe.webhook.charge_refunded", "event_id", event.ID, "err", err.Error())
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
	default:
		logx.Debug("stripe.webhook.unhandled", "event_id", event.ID, "type", event.Type)
	}

	if err := h.markProcessed(r.Context(), event.ID); err != nil {
		// Mark-failure is the most dangerous race here: a webhook
		// processed but not recorded leads to a duplicate
		// fulfilment on redelivery. The action handlers above use
		// state-checks that already guard against double-fulfilment
		// (via the existing 'paid' state), so log + 200 is safe.
		logx.Warn("stripe.webhook.mark_failed", "event_id", event.ID, "err", err.Error())
	}
	w.WriteHeader(http.StatusOK)
}

// handleCheckoutCompleted finds the purchase by metadata.zv_purchase_id,
// creates the assignment, and flips the purchase to paid. The bulk of
// this is an "in-process equivalent of ConfirmPurchase" — we don't
// reuse that handler directly because it requires an HTTP request +
// authenticated user.
func (h *Handlers) handleCheckoutCompleted(ctx context.Context, event stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := decodeEvent(event, &sess); err != nil {
		return fmt.Errorf("decode session: %w", err)
	}
	idStr := sess.Metadata["zv_purchase_id"]
	if idStr == "" {
		// Not one of ours (e.g. a Stripe sandbox event from a different
		// integration sharing the same webhook secret). Skip silently.
		return nil
	}
	var purchaseID int64
	if _, err := fmt.Sscanf(idStr, "%d", &purchaseID); err != nil {
		return fmt.Errorf("metadata parse: %w", err)
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		orgID         int64
		advSlug       string
		maxTeams      int
		state         string
		existingAssn  sql.NullInt64
	)
	// FOR UPDATE serialises against ConfirmPurchase and against a
	// concurrent redelivery of this same webhook event. Without it the
	// dedupe in alreadyProcessed/markProcessed leaves a window where
	// two deliveries can both observe state='pending' and each insert
	// an assignment row.
	err = tx.QueryRowContext(ctx, `
		SELECT org_id, adventure_slug, max_teams, state, assignment_id
		FROM purchases WHERE id = $1 FOR UPDATE
	`, purchaseID).Scan(&orgID, &advSlug, &maxTeams, &state, &existingAssn)
	if errors.Is(err, sql.ErrNoRows) {
		// Purchase row was deleted between checkout creation and
		// payment success. Nothing to fulfil.
		return nil
	}
	if err != nil {
		return err
	}
	if state == "paid" {
		// Already fulfilled by a prior delivery of this event or by
		// the simulated /confirm flow during dev. Idempotent — refresh
		// the stripe metadata in case the charge id arrived later.
		return updatePurchaseStripeMetadata(ctx, tx, purchaseID, sess)
	}
	if state != "pending" {
		// refunded / failed — don't reopen.
		return nil
	}

	var assID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams)
		VALUES ($1, $2, $3) RETURNING id
	`, orgID, advSlug, maxTeams).Scan(&assID); err != nil {
		return fmt.Errorf("assignment insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE purchases
		   SET state = 'paid', paid_at = now_text(),
		       assignment_id = $1, provider_session_id = $2
		 WHERE id = $3
	`, assID, sess.ID, purchaseID); err != nil {
		return fmt.Errorf("purchase update: %w", err)
	}
	if err := updatePurchaseStripeMetadata(ctx, tx, purchaseID, sess); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	metrics.PurchaseEvents.WithLabelValues("confirmed_stripe").Inc()
	logx.Info("stripe.purchase.paid", "purchase_id", purchaseID, "assignment_id", assID)
	// Webhook out: customer-side billing/PO systems that want their
	// own copy of the paid event. Most won't subscribe (they
	// reconcile via Stripe directly) but ship for completeness.
	webhooks.TryPublish(ctx, h.DB, orgID, webhooks.KindPurchasePaid, map[string]any{
		"purchase_id":     purchaseID,
		"assignment_id":   assID,
		"adventure_slug":  advSlug,
		"max_teams":       maxTeams,
		"amount_cents":    sess.AmountTotal,
		"currency":        string(sess.Currency),
		"stripe_session":  sess.ID,
	})
	return nil
}

// RefundPurchase initiates a Stripe refund for a paid purchase. The
// actual state flip from "paid" → "refunded" happens via the
// charge.refunded webhook so the same code path runs whether the
// refund originates here or in the Stripe Dashboard. We just pull
// the trigger.
//
// Admin-only: refunds touch real money, so org_admins can't do it
// from their own UI. They contact us, we click; the audit log on the
// Stripe side records who initiated.
func (h *Handlers) RefundPurchase(w http.ResponseWriter, r *http.Request) {
	if !h.stripeEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "stripe not configured")
		return
	}
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	var (
		state    string
		chargeID sql.NullString
	)
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT state, stripe_charge_id FROM purchases WHERE id = $1`, purchaseID).Scan(&state, &chargeID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "purchase not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if state != "paid" {
		httpx.Error(w, http.StatusConflict, "purchase is "+state+"; only paid purchases can be refunded")
		return
	}
	if !chargeID.Valid || chargeID.String == "" {
		httpx.Error(w, http.StatusConflict, "no charge id on file — webhook hasn't delivered yet")
		return
	}

	stripe.Key = h.StripeSecretKey
	rp := &stripe.RefundParams{Charge: stripe.String(chargeID.String)}
	rp.AddMetadata("zv_purchase_id", strconv.FormatInt(purchaseID, 10))
	rf, err := refund.New(rp)
	if err != nil {
		logx.Error("stripe.refund.failed", "purchase_id", purchaseID, "err", err.Error())
		httpx.Error(w, http.StatusBadGateway, "stripe refund failed: "+err.Error())
		return
	}
	logx.Info("stripe.refund.initiated", "purchase_id", purchaseID, "charge_id", chargeID.String, "refund_id", rf.ID)
	// Don't flip the state here; wait for the webhook. 202 signals
	// "we kicked it off, watch for the state change to land".
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"purchase_id": purchaseID,
		"refund_id":   rf.ID,
		"status":      string(rf.Status),
	})
}

// handleChargeRefunded marks the matching purchase refunded so the UI
// can show the chip + the org loses the assignment. Matched by
// stripe_charge_id which we recorded at checkout completion.
func (h *Handlers) handleChargeRefunded(ctx context.Context, event stripe.Event) error {
	var charge stripe.Charge
	if err := decodeEvent(event, &charge); err != nil {
		return fmt.Errorf("decode charge: %w", err)
	}
	// Capture the affected rows' org + purchase ids in the same
	// statement that flips the state — a separate SELECT after the
	// UPDATE could race with a parallel deletion.
	rows, err := h.DB.QueryContext(ctx, `
		UPDATE purchases SET state = 'refunded'
		 WHERE stripe_charge_id = $1 AND state = 'paid'
		 RETURNING id, org_id
	`, charge.ID)
	if err != nil {
		return err
	}
	type refundedRow struct {
		purchaseID int64
		orgID      int64
	}
	var refunded []refundedRow
	for rows.Next() {
		var rr refundedRow
		if err := rows.Scan(&rr.purchaseID, &rr.orgID); err != nil {
			rows.Close()
			return err
		}
		refunded = append(refunded, rr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(refunded) > 0 {
		logx.Info("stripe.purchase.refunded", "charge_id", charge.ID, "rows", len(refunded))
		for range refunded {
			metrics.PurchaseEvents.WithLabelValues("refunded").Inc()
		}
	}
	for _, rr := range refunded {
		webhooks.TryPublish(ctx, h.DB, rr.orgID, webhooks.KindPurchaseRefunded, map[string]any{
			"purchase_id":      rr.purchaseID,
			"stripe_charge_id": charge.ID,
		})
	}
	return nil
}

// updatePurchaseStripeMetadata stamps the charge id + invoice URL onto
// the purchase row when present in the session. PaymentIntent's
// LatestCharge gives us the charge id; the hosted invoice URL only
// shows up when Stripe Tax / invoicing is enabled.
func updatePurchaseStripeMetadata(ctx context.Context, tx *sql.Tx, purchaseID int64, sess stripe.CheckoutSession) error {
	var (
		chargeID   string
		invoiceURL string
	)
	if sess.PaymentIntent != nil && sess.PaymentIntent.LatestCharge != nil {
		chargeID = sess.PaymentIntent.LatestCharge.ID
	}
	if sess.Invoice != nil {
		invoiceURL = sess.Invoice.HostedInvoiceURL
	}
	if chargeID == "" && invoiceURL == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE purchases
		   SET stripe_charge_id   = COALESCE(NULLIF($1,''), stripe_charge_id),
		       stripe_invoice_url = COALESCE(NULLIF($2,''), stripe_invoice_url)
		 WHERE id = $3
	`, chargeID, invoiceURL, purchaseID)
	return err
}

func (h *Handlers) alreadyProcessed(ctx context.Context, eventID string) (bool, error) {
	var n int
	err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM processed_webhook_events WHERE event_id = $1`, eventID).Scan(&n)
	return n > 0, err
}

func (h *Handlers) markProcessed(ctx context.Context, eventID string) error {
	_, err := h.DB.ExecContext(ctx,
		`INSERT INTO processed_webhook_events(event_id) VALUES ($1) ON CONFLICT(event_id) DO NOTHING`, eventID)
	return err
}

// decodeEvent unmarshals event.Data.Raw (the Stripe event payload) into
// a typed struct. Wrapped so callers don't need to remember which
// field holds the bytes — Stripe parses the envelope, but typed
// structs come from the raw object body via plain JSON unmarshalling.
func decodeEvent(event stripe.Event, into any) error {
	return json.Unmarshal(event.Data.Raw, into)
}
