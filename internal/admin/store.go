package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/klahr/zonventure/internal/emailtpl"
	"net/http"
	"net/mail"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
)

// BuyTicketRequest is the body of POST /api/store/buy. The buyer
// types their email into the store form; everything else (price,
// team size, currency) is read from the event row server-side so a
// client can't tamper with the amount.
type BuyTicketRequest struct {
	EventID    int64  `json:"event_id"`
	BuyerEmail string `json:"buyer_email"`
}

// BuyTicketResponse carries the hosted Checkout URL the buyer's
// browser is redirected to. Once the buyer pays, the webhook
// creates the team and emails the magic link.
type BuyTicketResponse struct {
	CheckoutURL string `json:"checkout_url"`
}

// BuyTicket initiates a Checkout session for one ticket on a
// public-for-sale event. Public POST: no auth. Rate-limited at the
// route level to keep someone from spamming pending purchase rows.
//
// Flow:
//  1. Look up the event; refuse if it isn't public_for_sale or
//     isn't currently 'running' (paused events temporarily refuse
//     fresh tickets — the listing should already hide them but we
//     re-check here to close the race).
//  2. Insert a ticket_purchases row in 'pending'.
//  3. Open a provider Checkout session, stash zv_ticket_purchase_id
//     in metadata so the webhook can find the row, and persist the
//     session id on the row for redelivery dedupe.
//  4. Return the hosted URL; the browser redirects.
func (h *Handlers) BuyTicket(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "ticket sales not configured on this server")
		return
	}
	var req BuyTicketRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.EventID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "missing event_id")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.BuyerEmail))
	if email == "" {
		httpx.Error(w, http.StatusBadRequest, "missing buyer_email")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		httpx.Error(w, http.StatusBadRequest, "buyer_email is not a valid email address")
		return
	}

	var (
		advSlug    string
		eventName  sql.NullString
		advTitle   sql.NullString
		priceCents sql.NullInt64
		currency   string
		teamSize   int
		state      string
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT e.adventure_slug,
		       e.name,
		       a.title AS adv_title,
		       e.ticket_price_cents,
		       e.ticket_currency,
		       e.ticket_team_size,
		       e.state
		FROM events e
		LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
		WHERE e.id = $1
		  AND e.public_for_sale
		  AND e.ticket_price_cents IS NOT NULL
	`, req.EventID).Scan(&advSlug, &eventName, &advTitle, &priceCents, &currency, &teamSize, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "event not for sale")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "event lookup failed", err)
		return
	}
	if state != "running" {
		// Same posture as the listing filter — paused/scheduled/etc.
		// surfaces as "not for sale right now" rather than a more
		// specific error, since the buyer can't act on the distinction.
		httpx.Error(w, http.StatusConflict, "event not for sale")
		return
	}
	if !priceCents.Valid || priceCents.Int64 <= 0 {
		httpx.Error(w, http.StatusConflict, "event has no price set")
		return
	}

	// Marketplace routing: when the platform fee is configured
	// (= the hosted instance), the charge must destination-charge
	// into the org's connected Stripe account with the platform
	// fee captured off the top. Self-host (no fee) skips this
	// entirely and the charge lands on the platform account.
	var (
		connectedAccountID string
		applicationFee     int64
	)
	if h.platformFee().Enabled() {
		acct, err := h.stripeConnectAccountForEvent(r.Context(), req.EventID)
		if err != nil {
			if errors.Is(err, ErrConnectNotReady) {
				// The /store listing filter should have hidden this
				// quest already, but a stale tab or someone hitting
				// the API directly can race past it. Return a clear
				// 409 the buyer's UI can translate into "this quest
				// isn't selling tickets right now."
				httpx.Error(w, http.StatusConflict, "this adventure is not selling tickets right now")
				return
			}
			httpx.ServerErr(w, http.StatusInternalServerError, "connect account lookup failed", err)
			return
		}
		connectedAccountID = acct
		applicationFee = h.platformFee().Compute(priceCents.Int64)
	}

	var purchaseID int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, application_fee_cents)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, req.EventID, email, teamSize, priceCents.Int64, currency, applicationFee).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "purchase create failed", err)
		return
	}

	productName := storeProductName(eventName, advTitle, advSlug, teamSize)
	url, sessID, err := h.createTicketCheckoutSession(r.Context(), purchaseID, productName, currency, priceCents.Int64, email, connectedAccountID, applicationFee, "/store")
	if err != nil {
		// Mark the orphan as failed so the listing pages don't see a
		// stale 'pending' that's already dead provider-side.
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE ticket_purchases SET state = 'failed' WHERE id = $1 AND state = 'pending'`, purchaseID)
		httpx.ServerErr(w, http.StatusBadGateway, "checkout create failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ticket_purchases SET stripe_session_id = $1 WHERE id = $2`, sessID, purchaseID); err != nil {
		// Non-fatal — the checkout URL still works; we lose the dedupe
		// cushion but the metadata-keyed lookup in the webhook still
		// finds the row.
		logx.Warn("admin.store.buy.session_id_persist", "purchase_id", purchaseID, "err", err.Error())
	}
	// no-store on the buyer-specific checkout URL so a shared proxy
	// can't hand the same Stripe Checkout session to a second buyer.
	// Matches the Cache-Control posture on /api/store/items[/{id}].
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, BuyTicketResponse{CheckoutURL: url})
}

// BuyTicketByOrg is the org-scoped variant of BuyTicket. Same buy
// flow, two extra invariants:
//  1. The slug must resolve to an org. 404 otherwise — we don't want
//     to mint a Checkout session against a typo-domain.
//  2. The event_id must belong to that org. Cross-org event ids
//     return 404 so a deep-linked or hand-crafted POST can't tunnel
//     a buy through one company's store URL into another's quest.
//
// Success / cancel URLs route the buyer back to /o/{slug}/store
// rather than the global /store, so the post-payment confirmation
// stays on the company's branded URL.
func (h *Handlers) BuyTicketByOrg(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "ticket sales not configured on this server")
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		httpx.Error(w, http.StatusBadRequest, "missing org slug")
		return
	}
	var orgID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM organizations WHERE slug = $1`, slug).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "organization not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "org lookup failed", err)
		return
	}
	var req BuyTicketRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.EventID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "missing event_id")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.BuyerEmail))
	if email == "" {
		httpx.Error(w, http.StatusBadRequest, "missing buyer_email")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		httpx.Error(w, http.StatusBadRequest, "buyer_email is not a valid email address")
		return
	}

	var (
		advSlug    string
		eventName  sql.NullString
		advTitle   sql.NullString
		priceCents sql.NullInt64
		currency   string
		teamSize   int
		state      string
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT e.adventure_slug,
		       e.name,
		       a.title AS adv_title,
		       e.ticket_price_cents,
		       e.ticket_currency,
		       e.ticket_team_size,
		       e.state
		FROM events e
		LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
		WHERE e.id = $1
		  AND e.org_id = $2
		  AND e.public_for_sale
		  AND e.ticket_price_cents IS NOT NULL
	`, req.EventID, orgID).Scan(&advSlug, &eventName, &advTitle, &priceCents, &currency, &teamSize, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "event not for sale")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "event lookup failed", err)
		return
	}
	if state != "running" {
		httpx.Error(w, http.StatusConflict, "event not for sale")
		return
	}
	if !priceCents.Valid || priceCents.Int64 <= 0 {
		httpx.Error(w, http.StatusConflict, "event has no price set")
		return
	}

	var (
		connectedAccountID string
		applicationFee     int64
	)
	if h.platformFee().Enabled() {
		acct, err := h.stripeConnectAccountForEvent(r.Context(), req.EventID)
		if err != nil {
			if errors.Is(err, ErrConnectNotReady) {
				httpx.Error(w, http.StatusConflict, "this adventure is not selling tickets right now")
				return
			}
			httpx.ServerErr(w, http.StatusInternalServerError, "connect account lookup failed", err)
			return
		}
		connectedAccountID = acct
		applicationFee = h.platformFee().Compute(priceCents.Int64)
	}

	var purchaseID int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, application_fee_cents)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, req.EventID, email, teamSize, priceCents.Int64, currency, applicationFee).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "purchase create failed", err)
		return
	}

	productName := storeProductName(eventName, advTitle, advSlug, teamSize)
	storeBase := "/o/" + slug
	url, sessID, err := h.createTicketCheckoutSession(r.Context(), purchaseID, productName, currency, priceCents.Int64, email, connectedAccountID, applicationFee, storeBase)
	if err != nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE ticket_purchases SET state = 'failed' WHERE id = $1 AND state = 'pending'`, purchaseID)
		httpx.ServerErr(w, http.StatusBadGateway, "checkout create failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ticket_purchases SET stripe_session_id = $1 WHERE id = $2`, sessID, purchaseID); err != nil {
		logx.Warn("admin.store.buy.session_id_persist", "purchase_id", purchaseID, "err", err.Error())
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, BuyTicketResponse{CheckoutURL: url})
}

// storeProductName builds the line-item name shown on the Checkout
// page and on the resulting invoice / receipt. Order of preference:
// adventure title → event name → adventure slug.
func storeProductName(eventName, advTitle sql.NullString, advSlug string, teamSize int) string {
	title := ""
	switch {
	case advTitle.Valid && strings.TrimSpace(advTitle.String) != "":
		title = strings.TrimSpace(advTitle.String)
	case eventName.Valid && strings.TrimSpace(eventName.String) != "":
		title = strings.TrimSpace(eventName.String)
	default:
		title = advSlug
	}
	if teamSize <= 1 {
		return title + " — solo ticket"
	}
	return fmt.Sprintf("%s — team of up to %d", title, teamSize)
}

// createTicketCheckoutSession is the ticket-flow analogue of
// createPurchaseCheckoutSession. Differences:
//   - Customer email is always passed inline (the buyer is anonymous;
//     no org → no stripe_customer_id to reuse).
//   - Tax + invoice + billing-address collection are off: a $30
//     consumer ticket purchase doesn't need a B2B invoice with VAT
//     id, and forcing them through that flow tanks conversion.
//   - Success URL goes back to /store with a paid token; the buyer
//     waits for the email to arrive rather than landing on /play
//     directly (the magic link doesn't exist until the webhook runs).
func (h *Handlers) createTicketCheckoutSession(
	ctx context.Context,
	purchaseID int64,
	productName, currency string,
	amountCents int64,
	buyerEmail string,
	connectedAccountID string,
	applicationFeeCents int64,
	storeBasePath string,
) (string, string, error) {
	publicURL := strings.TrimRight(h.PublicURL, "/")
	if publicURL == "" {
		return "", "", errors.New("ticket checkout requires PublicURL to be configured")
	}
	// storeBasePath lets the org-scoped buy flow send the buyer back
	// to /o/{slug}/store while the global buy flow lands on /store.
	// Default keeps the legacy posture for any caller that doesn't
	// set it explicitly.
	if storeBasePath == "" {
		storeBasePath = "/store"
	}
	sess, err := h.Provider.CreateCheckout(ctx, payments.CheckoutParams{
		AmountCents: amountCents,
		Currency:    currency,
		ProductName: productName,
		// `{CHECKOUT_SESSION_ID}` is Stripe's redirect-time
		// substitution: at the moment Stripe sends the buyer back
		// to us, it replaces the literal placeholder with the
		// actual session id. The /store post-purchase poll then
		// uses the session_id as a capability token, verifying
		// against the purchase row's stripe_session_id before
		// returning the team token. The simulated provider does
		// the same substitution in devConfirm so both backends
		// behave identically end-to-end.
		SuccessURL:    fmt.Sprintf("%s%s?paid=%d&session_id={CHECKOUT_SESSION_ID}", publicURL, storeBasePath, purchaseID),
		CancelURL:     fmt.Sprintf("%s%s?cancelled=%d", publicURL, storeBasePath, purchaseID),
		CustomerEmail: buyerEmail,
		Metadata: map[string]string{
			"zv_ticket_purchase_id": fmt.Sprintf("%d", purchaseID),
		},
		// Marketplace routing — both empty/zero leaves the charge on
		// the platform account (self-host posture). When set, the
		// Stripe impl wires up PaymentIntentData.{TransferData,
		// ApplicationFeeAmount} so the money lands in the connected
		// account and the platform takes its cut off the top.
		ConnectedAccountID:  connectedAccountID,
		ApplicationFeeCents: applicationFeeCents,
		// Stable per-ticket-purchase: a retried BuyTicket handler
		// (transport timeout) receives the same Stripe session back
		// instead of an orphan duplicate.
		IdempotencyKey: fmt.Sprintf("checkout-ticket-%d", purchaseID),
	})
	if err != nil {
		return "", "", err
	}
	return sess.URL, sess.SessionID, nil
}

// handleTicketCheckoutCompleted runs when a checkout.completed
// event references a ticket_purchases row (zv_ticket_purchase_id
// metadata). Creates the team + slots, mails the magic links to
// the buyer, and flips the purchase to 'paid'. Idempotent: a
// redelivery sees state='paid' and short-circuits, so the buyer
// never gets a duplicate email or a duplicate team.
func (h *Handlers) handleTicketCheckoutCompleted(ctx context.Context, event payments.Event) error {
	if event.Checkout == nil {
		return nil
	}
	idStr := event.Metadata["zv_ticket_purchase_id"]
	if idStr == "" {
		return nil
	}
	// Same fail-closed posture as handleCheckoutCompleted: an "unpaid"
	// async session must not mail the magic link or create the team.
	if !event.Checkout.PaymentStatus.FulfilmentReady() {
		logx.Warn("payments.ticket_checkout.unpaid_skipped",
			"ticket_purchase_id", idStr, "status", string(event.Checkout.PaymentStatus))
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
		eventID    int64
		buyerEmail string
		teamSize   int
		state      string
		kind       string
		offerID    sql.NullInt64
		partyName  string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(event_id, 0), buyer_email, team_size, state, kind, party_offer_id, party_name
		FROM ticket_purchases WHERE id = $1 FOR UPDATE
	`, purchaseID).Scan(&eventID, &buyerEmail, &teamSize, &state, &kind, &offerID, &partyName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if state == "paid" {
		// Already fulfilled. Refresh the charge id in case the field
		// only landed on a later delivery.
		return updateTicketStripeMetadata(ctx, tx, purchaseID, *event.Checkout)
	}
	if state != "pending" {
		return nil
	}
	switch kind {
	case "party":
		return h.fulfilPartyCheckout(ctx, tx, event, purchaseID, offerID, partyName, buyerEmail, teamSize)
	case "hosted", "hosted_blocks":
		return h.fulfilHostedCheckout(ctx, tx, event, purchaseID, kind)
	}
	if eventID == 0 {
		// Event deleted while the payment was in flight; the row is
		// kept for bookkeeping but there is no event to join.
		logx.Warn("payments.ticket_checkout.event_gone", "ticket_purchase_id", purchaseID)
		return nil
	}

	teamID, teamToken, voucher, err := createTicketTeam(ctx, tx, eventID, buyerEmail, teamSize)
	if err != nil {
		return fmt.Errorf("ticket team create: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_purchases
		   SET state = 'paid',
		       paid_at = now_text(),
		       team_id = $1,
		       stripe_session_id = COALESCE(stripe_session_id, $2)
		 WHERE id = $3
	`, teamID, event.Checkout.SessionID, purchaseID); err != nil {
		return fmt.Errorf("purchase update: %w", err)
	}
	if err := updateTicketStripeMetadata(ctx, tx, purchaseID, *event.Checkout); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Email the team link. Best-effort: a send failure leaves the
	// purchase in a paid-but-not-emailed state, which the operator
	// can re-send via the existing per-team SendTeamInvitation route.
	if err := h.sendTicketTeamLink(ctx, eventID, buyerEmail, teamToken, voucher); err != nil {
		logx.Warn("admin.store.ticket.mail",
			"purchase_id", purchaseID, "team_id", teamID, "err", err.Error())
	}
	logx.Info("admin.store.ticket.paid",
		"purchase_id", purchaseID, "team_id", teamID, "team_size", teamSize)
	return nil
}

// createTicketTeam inserts the team + N slots inside the webhook's
// transaction and returns the team's share token. Slots are pre-named
// "Player 1".."Player N" — the buyer (team captain) opens the team
// link, claims their own slot, and shares the remaining slots from
// inside the app via the team-share QR.
func createTicketTeam(ctx context.Context, tx *sql.Tx, eventID int64, buyerEmail string, teamSize int) (teamID int64, teamToken, voucherCode string, err error) {
	if teamSize < 1 {
		teamSize = 1
	}
	teamTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, "", "", fmt.Errorf("team token: %w", err)
	}
	recapTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, "", "", fmt.Errorf("recap token: %w", err)
	}
	teamName, err := uniqueTicketTeamName(ctx, tx, eventID, buyerEmail)
	if err != nil {
		return 0, "", "", fmt.Errorf("team name: %w", err)
	}
	voucher, err := uniqueVoucherCode(ctx, tx)
	if err != nil {
		return 0, "", "", fmt.Errorf("voucher code: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO teams(event_id, name, token, recap_token, email, voucher_code)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, eventID, teamName, teamTok, recapTok, buyerEmail, voucher).Scan(&teamID); err != nil {
		return 0, "", "", fmt.Errorf("teams insert: %w", err)
	}

	expiresAt := auth.DefaultSlotExpiresAt(ctx, tx, eventID)
	for i := 1; i <= teamSize; i++ {
		tok, err := auth.NewSlotToken()
		if err != nil {
			return 0, "", "", fmt.Errorf("slot token: %w", err)
		}
		name := fmt.Sprintf("Player %d", i)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO player_slots(team_id, name, token, expires_at) VALUES ($1, $2, $3, $4)`,
			teamID, name, tok, expiresAt); err != nil {
			return 0, "", "", fmt.Errorf("player_slots insert: %w", err)
		}
	}
	return teamID, teamTok, voucher, nil
}

// uniqueVoucherCode allocates a fresh team voucher code not currently in
// use. The 50-bit space makes a collision astronomically unlikely, so a
// handful of tries is plenty; the UNIQUE index on teams.voucher_code is
// the ultimate backstop against a concurrent insert racing in.
func uniqueVoucherCode(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (string, error) {
	for range 5 {
		code, err := auth.NewVoucherCode()
		if err != nil {
			return "", err
		}
		var exists bool
		if err := q.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM teams WHERE voucher_code = $1)`, code).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("could not allocate a unique voucher code")
}

// uniqueTicketTeamName picks a team name unique within the event.
// Default base is the local-part of the buyer email; on collision
// we suffix " (2)", " (3)", … so a buyer who buys a second ticket
// for the same event lands on a fresh row instead of erroring.
func uniqueTicketTeamName(ctx context.Context, tx *sql.Tx, eventID int64, buyerEmail string) (string, error) {
	base := buyerEmail
	if at := strings.IndexByte(base, '@'); at > 0 {
		base = base[:at]
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = "ticket"
	}
	candidate := base
	for n := 2; n < 100; n++ {
		var taken bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM teams WHERE event_id = $1 AND name = $2)`,
			eventID, candidate).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s (%d)", base, n)
	}
	// Fall back to a token-suffixed name; almost certainly unique
	// and the only way this branch triggers is a buyer running 100
	// purchases against the same event.
	tok, err := auth.NewSlotToken()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", base, tok[:6]), nil
}

// sendTicketTeamLink emails the buyer a single team-share URL. Quests
// are bought-and-played-immediately: the buyer opens the link, claims
// a player slot, and shares the remaining slots with their teammates
// from inside the team-share page (QR + per-player URLs). Sending one
// link instead of N keeps the email short, avoids the "which link is
// mine" friction, and lets a buyer playing solo skip the share step
// entirely.
//
// The send goes through MailOutbox so a transient SMTP failure
// doesn't strand a paid buyer. The outbox row commits inside the
// purchase-fulfilment tx (callers must invoke this BEFORE the tx
// commits to share atomicity); the worker then drains with retry +
// backoff. Falls back to a direct Mailer.Send when the outbox is
// not configured (early dev setups) so a fresh checkout still
// behaves the same way.
func (h *Handlers) sendTicketTeamLink(ctx context.Context, eventID int64, buyerEmail, teamToken, voucher string) error {
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		return errors.New("ticket email requires PublicURL")
	}
	l := h.eventLook(ctx, eventID)
	subject, c := emailtpl.TeamLink(l.Lang, publicURL+"/team/"+teamToken, publicURL+"/play", auth.FormatVoucherCode(voucher), l.Brand.Name)
	c.Picture = l.Cover
	if h.SellerIdentity != nil {
		if seller := h.SellerIdentity(l.Lang); seller != "" {
			c.Footer = strings.TrimSpace(c.Footer + " " + seller)
		}
	}
	msg := l.message([]string{buyerEmail}, subject, c)
	if h.MailOutbox != nil {
		return h.MailOutbox.Enqueue(ctx, "ticket_team_link", msg)
	}
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	return h.Mailer.Send(ctx, msg)
}

// updateTicketStripeMetadata captures provider-side identifiers on
// the purchase row. Mirrors the org-purchase helper but writes to
// ticket_purchases. The charge id is what RefundPurchase /
// charge.refunded keys on, so getting it here matters.
func updateTicketStripeMetadata(ctx context.Context, tx *sql.Tx, purchaseID int64, c payments.CheckoutDetail) error {
	if c.ChargeID == "" && c.InvoiceURL == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE ticket_purchases
		   SET stripe_charge_id    = COALESCE(NULLIF($1, ''), stripe_charge_id),
		       stripe_invoice_url  = COALESCE(NULLIF($2, ''), stripe_invoice_url)
		 WHERE id = $3
	`, c.ChargeID, c.InvoiceURL, purchaseID)
	return err
}
