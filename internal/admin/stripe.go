package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/metrics"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/webhooks"

	"github.com/go-chi/chi/v5"
)

// paymentsEnabled reports whether the operator has configured a
// payment provider (Stripe today, others in future). Both the
// secret key and the webhook secret must be present for the
// constructor in cmd/server to wire a non-nil provider, so this
// check is a thin nil-test against `h.Provider`.
func (h *Handlers) paymentsEnabled() bool { return payments.Enabled(h.Provider) }

// createPurchaseCheckoutSession opens a hosted checkout session for
// an org purchase (one slot pack tied to an adventure). The price
// is inlined via the params bag, metadata carries our purchase id
// so the webhook can find the row, and success/cancel both bounce
// back to /arrange.
//
// B2B-flavoured: tax id + billing address are collected so the
// resulting invoice is deductible for company buyers. Consumer
// ticket sales (createTicketCheckoutSession in store.go) leave
// those off.
func (h *Handlers) createPurchaseCheckoutSession(ctx context.Context, purchaseID int64,
	advSlug, advTitle string, maxPlayers, amountCents int, customerEmail, existingCustomerID string,
) (string, string, error) {
	publicURL := strings.TrimRight(h.PublicURL, "/")
	if publicURL == "" {
		// Provider needs absolute URLs; bail loudly rather than building
		// a Checkout Session that would land users somewhere broken.
		return "", "", errors.New("payments checkout requires PublicURL to be configured")
	}

	productName := advTitle
	if productName == "" {
		productName = advSlug
	}
	productName = fmt.Sprintf("%s — %d player slot(s)", productName, maxPlayers)

	sess, err := h.Provider.CreateCheckout(ctx, payments.CheckoutParams{
		AmountCents: int64(amountCents),
		Currency:    "eur",
		ProductName: productName,
		// Both URLs return to the arrange app. The query param tells
		// the front-end which toast to flash; the actual paid-state
		// flip happens via the webhook, not via the redirect.
		SuccessURL:    fmt.Sprintf("%s/arrange#purchase-success/%d", publicURL, purchaseID),
		CancelURL:     fmt.Sprintf("%s/arrange#purchase-cancelled/%d", publicURL, purchaseID),
		CustomerID:    existingCustomerID,
		CustomerEmail: customerEmail,
		Metadata: map[string]string{
			"zv_purchase_id": fmt.Sprintf("%d", purchaseID),
		},
		// Auto-tax + tax id + billing address + invoice creation are
		// the B2B-friendly bundle: VAT-deductible PDF invoices for
		// company buyers, the customer's tax id printed on the bill-
		// to block. No-ops when Stripe Tax is off in the dashboard.
		AutoTax:         true,
		CollectTaxID:    true,
		CollectAddress:  true,
		InvoiceCreation: true,
		// Stable, derived from the local row id: a retried handler
		// (transport timeout, browser double-submit) hits Stripe with
		// the same key and gets the original session back instead of
		// minting a duplicate.
		IdempotencyKey: fmt.Sprintf("checkout-purchase-%d", purchaseID),
	})
	if err != nil {
		return "", "", err
	}
	return sess.URL, sess.SessionID, nil
}

// PaymentsWebhook receives provider webhooks. The provider impl
// validates the signature against its configured secret and parses
// the payload into a domain Event; we dedupe by event id and
// dispatch on the canonical event type. Always responds 200 on a
// valid event (even ones we don't act on) — providers redrive on
// any non-2xx, and we don't want unhandled types to feed an
// infinite retry loop.
//
// Mounted at the historical `/api/webhooks/stripe` path for
// backwards compatibility with operator-configured Stripe webhook
// URLs; future providers keep their own routes (e.g.
// `/api/webhooks/mollie`) so a multi-provider deploy can take
// either traffic.
func (h *Handlers) PaymentsWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		http.Error(w, "payments not configured", http.StatusServiceUnavailable)
		return
	}
	// Cap the read so a misbehaving caller can't OOM us. Provider
	// events are typically <50 KB; 1 MB is generous headroom.
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

	event, err := h.Provider.VerifyAndParseWebhook(body, r.Header)
	if err != nil {
		// Don't surface details — a forged-signature attacker
		// shouldn't learn what passes the parser and what doesn't.
		logx.Warn("payments.webhook.invalid_sig",
			"provider", h.Provider.Name(), "err", err.Error())
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}

	// Idempotency. Providers redrive on non-2xx and on transient
	// failures even when the original delivery succeeded; we record
	// every event id we've acted on so a redelivery is a no-op.
	already, err := h.alreadyProcessed(r.Context(), event.ID)
	if err != nil {
		logx.Error("payments.webhook.dedupe_lookup", "event_id", event.ID, "err", err.Error())
		http.Error(w, "dedupe lookup failed", http.StatusInternalServerError)
		return
	}
	if already {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch event.Type {
	case payments.EventCheckoutCompleted:
		// Two flavours of session land on the same webhook: org-side
		// adventure purchases (zv_purchase_id metadata) and public
		// ticket purchases (zv_ticket_purchase_id). Each handler
		// short-circuits when its metadata key is absent, so dispatch
		// just calls both in turn.
		if err := h.handleCheckoutCompleted(r.Context(), event); err != nil {
			logx.Error("payments.webhook.checkout_completed", "event_id", event.ID, "err", err.Error())
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		if err := h.handleTicketCheckoutCompleted(r.Context(), event); err != nil {
			logx.Error("payments.webhook.ticket_checkout_completed", "event_id", event.ID, "err", err.Error())
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
	case payments.EventChargeRefunded:
		if err := h.handleChargeRefunded(r.Context(), event); err != nil {
			logx.Error("payments.webhook.charge_refunded", "event_id", event.ID, "err", err.Error())
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
	case payments.EventConnectedAccountUpdated:
		// Marketplace-only: the connected account's verification
		// state changed (charges_enabled, payouts_enabled, etc.).
		// Sync the cache so the listing query and the quest-detail
		// banner reflect Stripe's reality without waiting for the
		// arranger to hit the manual refresh button.
		if err := h.handleConnectedAccountUpdated(r.Context(), event); err != nil {
			logx.Error("payments.webhook.account_updated", "event_id", event.ID, "err", err.Error())
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
	default:
		logx.Debug("payments.webhook.unhandled", "event_id", event.ID, "provider", event.Provider)
	}

	if err := h.markProcessed(r.Context(), event.ID); err != nil {
		// Mark-failure is the most dangerous race here: a webhook
		// processed but not recorded leads to a duplicate
		// fulfilment on redelivery. The action handlers above use
		// state-checks that already guard against double-fulfilment
		// (via the existing 'paid' state), so log + 200 is safe.
		logx.Warn("payments.webhook.mark_failed", "event_id", event.ID, "err", err.Error())
	}
	w.WriteHeader(http.StatusOK)
}

// handleCheckoutCompleted finds the purchase by metadata.zv_purchase_id,
// creates the assignment, and flips the purchase to paid. Reads the
// abstract payments.Event shape — `event.Checkout` carries the
// post-payment session details, `event.Metadata` the round-tripped
// bag we set when opening the session.
func (h *Handlers) handleCheckoutCompleted(ctx context.Context, event payments.Event) error {
	if event.Checkout == nil {
		return nil
	}
	idStr := event.Metadata["zv_purchase_id"]
	if idStr == "" {
		// Not one of ours (e.g. a sandbox event from a different
		// integration sharing the same webhook secret). Skip silently.
		return nil
	}
	// Async payment methods (ACH, BACS, PIX, …) land
	// checkout.session.completed before the funds actually clear. The
	// session reports payment_status="unpaid" until the matching
	// payment_intent.succeeded follow-up. Fulfilling here would grant
	// the goods on a not-yet-paid order. Fail closed; an operator who
	// turns on async methods must wire payment_intent.succeeded too.
	if !event.Checkout.PaymentStatus.FulfilmentReady() {
		logx.Warn("payments.checkout.unpaid_skipped",
			"purchase_id", idStr, "status", string(event.Checkout.PaymentStatus))
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
		maxPlayers    int
		state         string
		existingAssn  sql.NullInt64
		topUpID       sql.NullInt64
	)
	// FOR UPDATE serialises against ConfirmPurchase and against a
	// concurrent redelivery of this same webhook event. Without it the
	// dedupe in alreadyProcessed/markProcessed leaves a window where
	// two deliveries can both observe state='pending' and each insert
	// an assignment row.
	err = tx.QueryRowContext(ctx, `
		SELECT org_id, adventure_slug, max_players, state, assignment_id, top_up_assignment_id
		FROM purchases WHERE id = $1 FOR UPDATE
	`, purchaseID).Scan(&orgID, &advSlug, &maxPlayers, &state, &existingAssn, &topUpID)
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
		// the metadata in case the charge id arrived later.
		return updatePurchaseStripeMetadata(ctx, tx, purchaseID, *event.Checkout)
	}
	if state != "pending" {
		// refunded / failed — don't reopen.
		return nil
	}

	var assID int64
	if topUpID.Valid {
		// Top-up: pin the existing assignment row, bump its cap. The
		// purchase row's assignment_id will end up equal to the
		// top-up target so downstream listings link to the same
		// assignment a fresh-buy would have created.
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM adventure_assignments WHERE id = $1 AND org_id = $2 FOR UPDATE`,
			topUpID.Int64, orgID).Scan(&assID); err != nil {
			return fmt.Errorf("top-up lookup: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE adventure_assignments SET max_players = max_players + $1 WHERE id = $2`,
			maxPlayers, assID); err != nil {
			return fmt.Errorf("top-up bump: %w", err)
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
			VALUES ($1, $2, $3) RETURNING id
		`, orgID, advSlug, maxPlayers).Scan(&assID); err != nil {
			return fmt.Errorf("assignment insert: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE purchases
		   SET state = 'paid', paid_at = now_text(),
		       assignment_id = $1, provider_session_id = $2
		 WHERE id = $3
	`, assID, event.Checkout.SessionID, purchaseID); err != nil {
		return fmt.Errorf("purchase update: %w", err)
	}
	if err := updatePurchaseStripeMetadata(ctx, tx, purchaseID, *event.Checkout); err != nil {
		return err
	}
	// Capture the provider customer ID on the org so subsequent
	// "Update payment method" / portal-session calls have something
	// to target. Only fill in when the column is currently NULL —
	// once an org is linked, we don't repoint it under them via a
	// later checkout. Subsequent purchases by the same org will
	// already reuse that customer record on the provider's side anyway.
	if event.Checkout.CustomerID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE organizations
			   SET stripe_customer_id = $1
			 WHERE id = $2 AND stripe_customer_id IS NULL
		`, event.Checkout.CustomerID, orgID); err != nil {
			return fmt.Errorf("org customer link: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	metrics.PurchaseEvents.WithLabelValues("confirmed_stripe").Inc()
	logx.Info("payments.purchase.paid", "purchase_id", purchaseID, "assignment_id", assID)
	// Webhook out: customer-side billing/PO systems that want their
	// own copy of the paid event. Most won't subscribe (they
	// reconcile via Stripe directly) but ship for completeness.
	webhooks.TryPublish(ctx, h.DB, orgID, webhooks.KindPurchasePaid, map[string]any{
		"purchase_id":     purchaseID,
		"assignment_id":   assID,
		"adventure_slug":  advSlug,
		"max_players":     maxPlayers,
		"amount_cents":    event.Checkout.AmountTotal,
		"currency":        event.Checkout.Currency,
		"stripe_session":  event.Checkout.SessionID,
	})
	return nil
}

// RefundPurchase initiates a refund for a paid purchase. The actual
// state flip from "paid" → "refunded" happens via the
// charge.refunded webhook so the same code path runs whether the
// refund originates here or in the provider's dashboard. We just
// pull the trigger.
//
// Admin-only: refunds touch real money, so org_admins can't do it
// from their own UI. They contact us, we click; the audit log on
// the provider side records who initiated.
func (h *Handlers) RefundPurchase(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "payments not configured")
		return
	}
	if me, _ := auth.FromContext(r.Context()); me.Role != auth.RoleAdmin {
		httpx.Error(w, http.StatusForbidden, "admin only")
		return
	}
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}

	var (
		state        string
		chargeID     sql.NullString
		preMigration bool
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT state, stripe_charge_id, pre_migration FROM purchases WHERE id = $1`,
		purchaseID).Scan(&state, &chargeID, &preMigration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "purchase not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if state != "paid" {
		httpx.Error(w, http.StatusConflict, "purchase not in paid state: "+state)
		return
	}
	if !chargeID.Valid || chargeID.String == "" {
		httpx.Error(w, http.StatusConflict, "purchase has no recorded charge id")
		return
	}
	if preMigration {
		// Imported from another zv deployment; the recorded charge
		// id is on the source instance's Stripe account, not ours.
		// Calling Refund here would 404 against our credentials —
		// or worse, match an unrelated charge. Refuse with a clear
		// pointer back to the legacy dashboard.
		httpx.Error(w, http.StatusConflict,
			"this purchase pre-dates the migration to this instance — refund it through the pre-migration Stripe dashboard, not the in-app button")
		return
	}

	rf, err := h.Provider.Refund(r.Context(), payments.RefundParams{
		ChargeID: chargeID.String,
		// Stable per-purchase: a double-click on the admin Refund
		// button retries with the same key and Stripe returns the
		// original refund object rather than cutting a duplicate.
		IdempotencyKey: fmt.Sprintf("refund-purchase-%d", purchaseID),
	})
	if err != nil {
		logx.Error("payments.refund.new_failed",
			"purchase_id", purchaseID, "charge_id", chargeID.String, "err", err.Error())
		httpx.Error(w, http.StatusBadGateway, "refund failed")
		return
	}
	logx.Info("payments.refund.created",
		"purchase_id", purchaseID, "charge_id", chargeID.String,
		"refund_id", rf.ID, "status", rf.Status)
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"purchase_id": purchaseID,
		"refund_id":   rf.ID,
		"status":      rf.Status,
	})
}

// stripeRefundReconcileWindow caps how long handleChargeRefunded
// will return errors to force redelivery when the charge doesn't
// yet match a paid purchase. After the window we ack the event so
// an unrelated charge (manual dashboard action, other tenant on
// the same account) doesn't loop forever. 24 hours is well below
// Stripe's redelivery horizon (~3 days) so a legitimately out-of-
// order charge.refunded vs checkout.session.completed pair gets
// reconciled before the cutoff.
const stripeRefundReconcileWindow = 24 * time.Hour

// handleChargeRefunded marks the matching purchase refunded so the UI
// can show the chip + the org loses the assignment. Matched by
// stripe_charge_id which we recorded at checkout completion.
//
// Out-of-order delivery: providers don't guarantee that
// checkout.session.completed lands before charge.refunded. When the
// refund event arrives first, our purchases row has no charge_id
// yet (handleCheckoutCompleted is what writes it), so the UPDATE
// matches 0 rows. Without the reconcile gate below, the refund
// event would be marked processed and lost — the later
// session.completed would then mark the row 'paid' and the
// customer would be in a paid-but-refunded state forever. We bias
// toward "redeliver" for fresh events; non-zv charges age out and
// get acked after the reconcile window.
func (h *Handlers) handleChargeRefunded(ctx context.Context, event payments.Event) error {
	if event.Charge == nil {
		return nil
	}
	chargeID := event.Charge.ID
	// charge.refunded fires for every refund delta. A €5 partial
	// refund on a €100 ticket arrives in this handler the same shape
	// as a full €100 refund. Flipping the purchase to 'refunded' on
	// a partial would withdraw the team and revoke their slots —
	// catastrophic for the buyer, and recoverable only by a manual
	// undo. Skip non-full refunds; the partial is still visible on
	// the Stripe dashboard, and the operator can complete the full
	// refund there if that was the intent.
	if !event.Charge.FullyRefunded() {
		logx.Info("payments.refund.partial_skipped",
			"charge_id", chargeID,
			"amount_cents", event.Charge.AmountCents,
			"amount_refunded", event.Charge.AmountRefunded,
			"refunded_flag", event.Charge.Refunded)
		return nil
	}
	// Capture the affected rows' org + purchase ids in the same
	// statement that flips the state — a separate SELECT after the
	// UPDATE could race with a parallel deletion.
	rows, err := h.DB.QueryContext(ctx, `
		UPDATE purchases SET state = 'refunded'
		 WHERE stripe_charge_id = $1 AND state = 'paid'
		 RETURNING id, org_id
	`, chargeID)
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

	// Ticket purchases share the same charge.refunded webhook as the
	// org-purchases flow. Flip any matching ticket_purchases row,
	// then withdraw the team it created so a refunded buyer can't
	// keep playing — same posture as the admin "withdraw team"
	// action; magic-link sessions stay valid but actions return
	// withdrawn errors.
	ticketRows, err := h.DB.QueryContext(ctx, `
		UPDATE ticket_purchases
		   SET state = 'refunded', refunded_at = now_text()
		 WHERE stripe_charge_id = $1 AND state = 'paid'
		 RETURNING id, COALESCE(team_id, 0)
	`, chargeID)
	if err != nil {
		return err
	}
	type refundedTicket struct {
		purchaseID int64
		teamID     int64
	}
	var tickets []refundedTicket
	for ticketRows.Next() {
		var rr refundedTicket
		if err := ticketRows.Scan(&rr.purchaseID, &rr.teamID); err != nil {
			ticketRows.Close()
			return err
		}
		tickets = append(tickets, rr)
	}
	ticketRows.Close()
	if err := ticketRows.Err(); err != nil {
		return err
	}
	for _, rr := range tickets {
		if rr.teamID == 0 {
			continue
		}
		// Best-effort withdraw: idempotent (the WHERE clause keeps
		// already-withdrawn rows untouched) and never fatal — a
		// failed withdraw still leaves the purchase refund recorded
		// for accounting.
		if _, werr := h.DB.ExecContext(ctx, `
			UPDATE teams SET withdrawn_at = now_text()
			 WHERE id = $1 AND withdrawn_at IS NULL
		`, rr.teamID); werr != nil {
			logx.Warn("payments.ticket.refund.withdraw",
				"purchase_id", rr.purchaseID, "team_id", rr.teamID, "err", werr.Error())
		}
	}

	if len(refunded) == 0 && len(tickets) == 0 {
		// Either out-of-order (the matching paid row hasn't been
		// written yet) or not our charge at all. We can't tell from
		// the Charge object alone, so bias toward the recoverable
		// case: return an error → provider redelivers → eventually
		// session.completed lands and the next attempt succeeds.
		// After the reconcile window, accept the loss and ack.
		eventAge := time.Since(event.Created)
		if eventAge < stripeRefundReconcileWindow {
			logx.Warn("payments.refund.no_match.redeliver",
				"charge_id", chargeID, "event_age_seconds", int(eventAge.Seconds()))
			return fmt.Errorf("no paid purchase matches charge %s yet — request redelivery", chargeID)
		}
		logx.Warn("payments.refund.no_match.aged_out",
			"charge_id", chargeID, "event_age_hours", eventAge.Hours())
		return nil
	}
	if len(refunded) > 0 {
		logx.Info("payments.purchase.refunded", "charge_id", chargeID, "rows", len(refunded))
	}
	if len(tickets) > 0 {
		logx.Info("payments.ticket.refunded", "charge_id", chargeID, "rows", len(tickets))
	}
	for range refunded {
		metrics.PurchaseEvents.WithLabelValues("refunded").Inc()
	}
	for range tickets {
		// Same metric label so dashboards aggregate uniformly; the
		// purchase_id label on the webhook fan-out below distinguishes
		// the two flavours.
		metrics.PurchaseEvents.WithLabelValues("ticket_refunded").Inc()
	}
	for _, rr := range refunded {
		webhooks.TryPublish(ctx, h.DB, rr.orgID, webhooks.KindPurchaseRefunded, map[string]any{
			"purchase_id":      rr.purchaseID,
			"stripe_charge_id": chargeID,
		})
	}
	return nil
}

// CreatePortalSession mints a hosted self-service portal session
// for the caller's org. Surfaces ErrNotSupported as a clear "feature
// not enabled" rather than a generic 5xx for providers that don't
// expose one.
//
// 409 with code "no_customer" when the org has never completed a
// real checkout, since there's nothing to portal into yet. 503
// when payments aren't configured at all.
func (h *Handlers) CreatePortalSession(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "payments not configured")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "no organization")
		return
	}
	var customer sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT stripe_customer_id FROM organizations WHERE id = $1`,
		*me.OrgID).Scan(&customer); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !customer.Valid || customer.String == "" {
		httpx.Error(w, http.StatusConflict, "no_customer")
		return
	}
	publicURL := strings.TrimRight(h.PublicURL, "/")
	if publicURL == "" {
		httpx.Error(w, http.StatusInternalServerError, "PublicURL is not configured")
		return
	}
	url, err := h.Provider.CustomerPortal(r.Context(), customer.String, publicURL+"/arrange#purchases")
	if errors.Is(err, payments.ErrNotSupported) {
		httpx.Error(w, http.StatusNotImplemented, "customer portal not supported by configured provider")
		return
	}
	if err != nil {
		logx.Error("payments.portal.new_failed", "org_id", *me.OrgID, "err", err.Error())
		httpx.Error(w, http.StatusBadGateway, "portal session failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"url": url})
}

// updatePurchaseStripeMetadata stamps the charge id + invoice URL onto
// the purchase row when present in the checkout detail. Column name
// kept as `stripe_charge_id` for back-compat with existing rows;
// future providers can either widen the column meaning ("payment
// charge id") or migrate to a provider-keyed alias.
func updatePurchaseStripeMetadata(ctx context.Context, tx *sql.Tx, purchaseID int64, c payments.CheckoutDetail) error {
	if c.ChargeID == "" && c.InvoiceURL == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE purchases
		   SET stripe_charge_id   = COALESCE(NULLIF($1,''), stripe_charge_id),
		       stripe_invoice_url = COALESCE(NULLIF($2,''), stripe_invoice_url)
		 WHERE id = $3
	`, c.ChargeID, c.InvoiceURL, purchaseID)
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
