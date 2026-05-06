package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	mailerpkg "git.sr.ht/~klahr/zv/internal/mailer"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
)

// BuyTicketRequest is the body of POST /api/store/buy. The buyer
// types their email into the store form; everything else (price,
// team size, currency) is read from the event row server-side so a
// client can't tamper with the amount.
type BuyTicketRequest struct {
	EventID    int64  `json:"event_id"`
	BuyerEmail string `json:"buyer_email"`
}

// BuyTicketResponse carries the hosted Stripe Checkout URL the
// buyer's browser is redirected to. Once the buyer pays, the
// webhook creates the team and emails the magic link.
type BuyTicketResponse struct {
	CheckoutURL string `json:"checkout_url"`
}

// BuyTicket initiates a Stripe Checkout session for one ticket on a
// public-for-sale event. Public POST: no auth. Rate-limited at the
// route level to keep someone from spamming pending purchase rows.
//
// Flow:
//  1. Look up the event; refuse if it isn't public_for_sale or
//     isn't currently 'running' (paused events temporarily refuse
//     fresh tickets — the listing should already hide them but we
//     re-check here to close the race).
//  2. Insert a ticket_purchases row in 'pending'.
//  3. Open a Stripe Checkout session, stash zv_ticket_purchase_id
//     in metadata so the webhook can find the row, and persist the
//     session id on the row for redelivery dedupe.
//  4. Return the hosted URL; the browser redirects.
func (h *Handlers) BuyTicket(w http.ResponseWriter, r *http.Request) {
	if !h.stripeEnabled() {
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

	var purchaseID int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, req.EventID, email, teamSize, priceCents.Int64, currency).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "purchase create failed", err)
		return
	}

	productName := storeProductName(eventName, advTitle, advSlug, teamSize)
	url, sessID, err := h.createTicketCheckoutSession(r.Context(), purchaseID, productName, currency, int(priceCents.Int64), email)
	if err != nil {
		// Mark the orphan as failed so the listing pages don't see a
		// stale 'pending' that's already dead Stripe-side.
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE ticket_purchases SET state = 'failed' WHERE id = $1 AND state = 'pending'`, purchaseID)
		httpx.ServerErr(w, http.StatusBadGateway, "stripe checkout create failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ticket_purchases SET stripe_session_id = $1 WHERE id = $2`, sessID, purchaseID); err != nil {
		// Non-fatal — the checkout URL still works; we lose the dedupe
		// cushion but the metadata-keyed lookup in the webhook still
		// finds the row.
		logx.Warn("admin.store.buy.session_id_persist", "purchase_id", purchaseID, "err", err.Error())
	}
	httpx.JSON(w, http.StatusOK, BuyTicketResponse{CheckoutURL: url})
}

// storeProductName builds the line-item name shown on the Stripe
// Checkout page and on the resulting invoice / receipt. Order of
// preference: adventure title → event name → adventure slug.
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
// createStripeCheckoutSession. Differences from the org-purchase
// flow it parallels:
//   - Customer email is always passed inline (the buyer is anonymous;
//     no org → no stripe_customer_id to reuse).
//   - Tax + invoice + billing-address collection are off: a $30
//     consumer ticket purchase doesn't need a B2B invoice with VAT
//     id, and forcing them through that flow tanks conversion.
//     Operators who do need that for a particular event can ship a
//     follow-up flag later.
//   - Success URL goes back to /store with a paid token; the buyer
//     waits for the email to arrive rather than landing on /play
//     directly (the magic link doesn't exist until the webhook runs).
func (h *Handlers) createTicketCheckoutSession(
	ctx context.Context,
	purchaseID int64,
	productName, currency string,
	amountCents int,
	buyerEmail string,
) (string, string, error) {
	stripe.Key = h.StripeSecretKey

	publicURL := strings.TrimRight(h.PublicURL, "/")
	if publicURL == "" {
		return "", "", errors.New("ticket checkout requires PublicURL to be configured")
	}

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:   stripe.String(currency),
				UnitAmount: stripe.Int64(int64(amountCents)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(productName),
				},
			},
		}},
		SuccessURL: stripe.String(fmt.Sprintf("%s/store?paid=%d", publicURL, purchaseID)),
		CancelURL:  stripe.String(fmt.Sprintf("%s/store?cancelled=%d", publicURL, purchaseID)),
	}
	params.AddMetadata("zv_ticket_purchase_id", fmt.Sprintf("%d", purchaseID))
	if buyerEmail != "" {
		params.CustomerEmail = stripe.String(buyerEmail)
	}

	s, err := session.New(params)
	if err != nil {
		return "", "", err
	}
	return s.URL, s.ID, nil
}

// handleTicketCheckoutCompleted runs when a Stripe checkout.session.
// completed event references a ticket_purchases row. Creates the
// team + slots, mails the magic links to the buyer, and flips the
// purchase to 'paid'. Idempotent: a redelivery sees state='paid'
// and short-circuits, so the buyer never gets a duplicate email or
// a duplicate team.
func (h *Handlers) handleTicketCheckoutCompleted(ctx context.Context, event stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := decodeEvent(event, &sess); err != nil {
		return fmt.Errorf("decode session: %w", err)
	}
	idStr := sess.Metadata["zv_ticket_purchase_id"]
	if idStr == "" {
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
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT event_id, buyer_email, team_size, state
		FROM ticket_purchases WHERE id = $1 FOR UPDATE
	`, purchaseID).Scan(&eventID, &buyerEmail, &teamSize, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if state == "paid" {
		// Already fulfilled. Refresh the charge id in case the field
		// only landed on a later delivery.
		return updateTicketStripeMetadata(ctx, tx, purchaseID, sess)
	}
	if state != "pending" {
		return nil
	}

	teamID, slots, err := createTicketTeam(ctx, tx, eventID, buyerEmail, teamSize)
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
	`, teamID, sess.ID, purchaseID); err != nil {
		return fmt.Errorf("purchase update: %w", err)
	}
	if err := updateTicketStripeMetadata(ctx, tx, purchaseID, sess); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Email the magic links. Best-effort: a send failure leaves the
	// purchase in a paid-but-not-emailed state, which the operator
	// can re-send via the existing per-team SendTeamInvitation route.
	if err := h.sendTicketMagicLinks(ctx, buyerEmail, slots); err != nil {
		logx.Warn("admin.store.ticket.mail",
			"purchase_id", purchaseID, "team_id", teamID, "err", err.Error())
	}
	logx.Info("admin.store.ticket.paid",
		"purchase_id", purchaseID, "team_id", teamID, "slots", len(slots))
	return nil
}

// ticketSlot is the (player-name, magic-token) pair used to render
// the buyer's email body. Mirrors invitationSlot but local to the
// ticket-purchase flow so changes to either don't entangle.
type ticketSlot struct {
	Player string
	Token  string
}

// createTicketTeam inserts the team + N slots inside the webhook's
// transaction and returns the slot tokens so the caller can email
// them. Slots are pre-named "Player 1".."Player N" — the buyer can
// rename them inside /play once they open their link.
func createTicketTeam(ctx context.Context, tx *sql.Tx, eventID int64, buyerEmail string, teamSize int) (int64, []ticketSlot, error) {
	if teamSize < 1 {
		teamSize = 1
	}
	teamTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, nil, fmt.Errorf("team token: %w", err)
	}
	recapTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, nil, fmt.Errorf("recap token: %w", err)
	}
	teamName, err := uniqueTicketTeamName(ctx, tx, eventID, buyerEmail)
	if err != nil {
		return 0, nil, fmt.Errorf("team name: %w", err)
	}

	var teamID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO teams(event_id, name, token, recap_token, email)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, eventID, teamName, teamTok, recapTok, buyerEmail).Scan(&teamID); err != nil {
		return 0, nil, fmt.Errorf("teams insert: %w", err)
	}

	expiresAt := auth.DefaultSlotExpiresAt(ctx, tx, eventID)
	slots := make([]ticketSlot, 0, teamSize)
	for i := 1; i <= teamSize; i++ {
		tok, err := auth.NewSlotToken()
		if err != nil {
			return 0, nil, fmt.Errorf("slot token: %w", err)
		}
		name := fmt.Sprintf("Player %d", i)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO player_slots(team_id, name, token, expires_at) VALUES ($1, $2, $3, $4)`,
			teamID, name, tok, expiresAt); err != nil {
			return 0, nil, fmt.Errorf("player_slots insert: %w", err)
		}
		slots = append(slots, ticketSlot{Player: name, Token: tok})
	}
	return teamID, slots, nil
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

// sendTicketMagicLinks emails the buyer one message containing
// every slot's magic-link URL. The buyer is the team's "captain";
// they forward the per-player links (or show the team-share QR
// from /play) to friends.
func (h *Handlers) sendTicketMagicLinks(ctx context.Context, buyerEmail string, slots []ticketSlot) error {
	if h.Mailer == nil {
		// No mailer is a self-host LogMailer fallback. The order is
		// "create team, then mail" so we still log via the LogMailer
		// branch — the operator can read the link out of the logs.
		return errors.New("no mailer configured")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		return errors.New("ticket email requires PublicURL")
	}
	var b strings.Builder
	b.WriteString("Thanks for your purchase! Below are the magic links for your team. Forward each link to one teammate, or open the first link yourself and share the team QR code from inside the app.\r\n\r\n")
	for _, s := range slots {
		fmt.Fprintf(&b, "%s: %s/play?t=%s\r\n", s.Player, publicURL, s.Token)
	}
	b.WriteString("\r\nLinks stay valid for the duration of the event. Lose one? Use the \"I lost my link\" recovery on the play page with this same email address.\r\n")
	return h.Mailer.Send(ctx, mailerpkg.Message{
		To:      []string{buyerEmail},
		Subject: "Your Zonventure ticket — magic links inside",
		Text:    b.String(),
	})
}

// updateTicketStripeMetadata captures Stripe-side identifiers on
// the purchase row. Mirrors the equivalent helper in the org
// purchases flow but writes to ticket_purchases. The charge id is
// what RefundPurchase / charge.refunded keys on, so getting it
// here matters.
func updateTicketStripeMetadata(ctx context.Context, tx *sql.Tx, purchaseID int64, sess stripe.CheckoutSession) error {
	var (
		chargeID   sql.NullString
		invoiceURL sql.NullString
	)
	if sess.PaymentIntent != nil && sess.PaymentIntent.LatestCharge != nil {
		chargeID.String, chargeID.Valid = sess.PaymentIntent.LatestCharge.ID, true
	}
	if sess.Invoice != nil && sess.Invoice.HostedInvoiceURL != "" {
		invoiceURL.String, invoiceURL.Valid = sess.Invoice.HostedInvoiceURL, true
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE ticket_purchases
		   SET stripe_charge_id    = COALESCE(NULLIF($1, ''), stripe_charge_id),
		       stripe_invoice_url  = COALESCE(NULLIF($2, ''), stripe_invoice_url)
		 WHERE id = $3
	`, chargeID.String, invoiceURL.String, purchaseID)
	return err
}
