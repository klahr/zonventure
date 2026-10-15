package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
)

// ticketSaleDTO is one ticket_purchases row as the Purchases tab lists
// it: a self-guided ticket, a party booking, a hosted pass or more
// teams for one.
type ticketSaleDTO struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	EventID        int64  `json:"event_id,omitempty"`
	EventName      string `json:"event_name,omitempty"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	OrgName        string `json:"org_name,omitempty"`
	BuyerEmail     string `json:"buyer_email"`
	TeamSize       int    `json:"team_size"`
	// Teams is how many teams a game holds for what a party bought.
	Teams       int    `json:"teams,omitempty"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	FeeCents    int64  `json:"application_fee_cents"`
	Refundable  bool   `json:"refundable"`
	InvoiceURL  string `json:"invoice_url,omitempty"`
	CreatedAt   string `json:"created_at"`
	PaidAt      string `json:"paid_at,omitempty"`
	RefundedAt  string `json:"refunded_at,omitempty"`
	// RefundPending is a refund asked of the provider that its webhook
	// has not confirmed yet.
	RefundPending bool `json:"refund_pending,omitempty"`
	// Abandoned is a checkout still pending a day after it began: the
	// buyer left it, and the list keeps it out of sight unless asked.
	Abandoned bool `json:"abandoned,omitempty"`
}

// ticketSaleOrg is the org a ticket_purchases row belongs to: the
// event's, the party offer's while its event is still to be made, or
// the buyer's for a hosted pass.
const ticketSaleOrg = `COALESCE(e.org_id, o.org_id, p.seller_org_id, p.buyer_org_id)`

// ListTicketSales returns the ticket purchases visible to the caller:
// the platform admin sees every org's, or one org's with ?org_id=, an
// org user their own org's.
//
// GET /api/admin/ticket-purchases
func (h *Handlers) ListTicketSales(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT p.id, p.kind, p.state, COALESCE(p.event_id, 0),
		       COALESCE(NULLIF(p.party_name, ''), NULLIF(e.party_name, ''), e.name, p.product_name),
		       COALESCE(NULLIF(a.title, ''), a.slug, ''), COALESCE(org.name, ''),
		       p.buyer_email, p.team_size, p.teams, p.amount_cents, p.currency, p.application_fee_cents,
		       COALESCE(p.stripe_charge_id, '') <> '', COALESCE(p.stripe_invoice_url, ''),
		       p.created_at, COALESCE(p.paid_at, ''), COALESCE(p.refunded_at, ''),
		       p.state = 'paid' AND p.refund_requested_at IS NOT NULL,
		       p.state = 'pending' AND p.created_at < to_char((now() - interval '24 hours') AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
		FROM ticket_purchases p
		LEFT JOIN events e ON e.id = p.event_id
		LEFT JOIN party_offers o ON o.id = p.party_offer_id
		LEFT JOIN adventures a ON a.slug = COALESCE(e.adventure_slug, o.adventure_slug, NULLIF(p.adventure_slug, ''))
		LEFT JOIN organizations org ON org.id = ` + ticketSaleOrg + `
	`
	var args []any
	if !readsAsAdmin(me) {
		if !isOrgScoped(me) {
			httpx.Error(w, http.StatusForbidden, "no org context")
			return
		}
		// Billing is the org admin's: an event host runs events but
		// doesn't see what the organisation paid or sold.
		if me.Role != auth.RoleOrgAdmin {
			httpx.Error(w, http.StatusForbidden, "only an organization admin can see purchases")
			return
		}
		query += ` WHERE ` + ticketSaleOrg + ` = $1`
		args = append(args, *me.OrgID)
	} else if v := r.URL.Query().Get("org_id"); v != "" {
		orgID, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad org_id")
			return
		}
		query += ` WHERE ` + ticketSaleOrg + ` = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY p.created_at DESC, p.id DESC`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
		return
	}
	defer rows.Close()
	out := []ticketSaleDTO{}
	for rows.Next() {
		var (
			d         ticketSaleDTO
			hasCharge bool
		)
		if err := rows.Scan(&d.ID, &d.Kind, &d.State, &d.EventID, &d.EventName, &d.AdventureTitle, &d.OrgName,
			&d.BuyerEmail, &d.TeamSize, &d.Teams, &d.AmountCents, &d.Currency, &d.FeeCents,
			&hasCharge, &d.InvoiceURL, &d.CreatedAt, &d.PaidAt, &d.RefundedAt, &d.RefundPending, &d.Abandoned); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
			return
		}
		d.Refundable = d.State == "paid" && !d.RefundPending && hasCharge && d.AmountCents > 0 &&
			(isAdmin(me) || me.Role == auth.RoleOrgAdmin && sellerRefundable(d.Kind, d.FeeCents))
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"sales": out})
}

// RefundTicketSale refunds a ticket purchase in full. As RefundPurchase,
// it only asks Stripe: the charge.refunded webhook marks the row
// refunded and withdraws its team, or calls off its party.
//
// A sale that went to an org's connected account (it carries a platform
// fee) takes the org's share back from that account and returns the
// fee, so the buyer gets everything back and nobody keeps any of it.
//
// Refunds touch real money: the platform admin refunds any sale, an
// org admin only their own org's tickets and parties (see
// sellerRefundable). Reauth: X-Confirm-Password.
//
// POST /api/admin/ticket-purchases/{purchaseID}/refund
func (h *Handlers) RefundTicketSale(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "payments not configured")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !isAdmin(me) && me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "admin only")
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
		fee      int64
		kind     string
		orgID    sql.NullInt64
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT p.state, p.stripe_charge_id, p.application_fee_cents, p.kind, `+ticketSaleOrg+`
		FROM ticket_purchases p
		LEFT JOIN events e ON e.id = p.event_id
		LEFT JOIN party_offers o ON o.id = p.party_offer_id
		WHERE p.id = $1`,
		purchaseID).Scan(&state, &chargeID, &fee, &kind, &orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "purchase not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !isAdmin(me) && (!isOrgScoped(me) || !orgID.Valid || orgID.Int64 != *me.OrgID || !sellerRefundable(kind, fee)) {
		httpx.Error(w, http.StatusForbidden, "only the platform operator can refund this sale: ask them")
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
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	// One refund per sale: a second click, or a second admin, while the
	// first waits for the provider's webhook must not cut another.
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE ticket_purchases SET refund_requested_at = now_text()
		 WHERE id = $1 AND state = 'paid' AND refund_requested_at IS NULL`, purchaseID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "refund failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "a refund of this purchase is already under way", "code": "refund_pending"})
		return
	}
	// A sale with a platform fee was charged on the org's own account,
	// and is refunded there. The fee comes back to the org only when
	// what it paid for never played.
	params := payments.RefundParams{ChargeID: chargeID.String, IdempotencyKey: fmt.Sprintf("refund-ticket-purchase-%d", purchaseID)}
	if fee > 0 {
		acct, played, err := h.saleAccountAndPlay(r.Context(), purchaseID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "refund failed", err)
			return
		}
		params.ConnectedAccountID, params.RefundApplicationFee = acct, !played
	}
	rf, err := h.Provider.Refund(r.Context(), params)
	if err != nil {
		logx.Error("payments.ticket_refund.new_failed",
			"ticket_purchase_id", purchaseID, "charge_id", chargeID.String, "err", err.Error())
		if _, cerr := h.DB.ExecContext(r.Context(), `
			UPDATE ticket_purchases SET refund_requested_at = NULL WHERE id = $1 AND state = 'paid'`, purchaseID); cerr != nil {
			logx.Warn("payments.ticket_refund.unclaim", "ticket_purchase_id", purchaseID, "err", cerr.Error())
		}
		httpx.Error(w, http.StatusBadGateway, "refund failed")
		return
	}
	logx.Info("payments.ticket_refund.created",
		"ticket_purchase_id", purchaseID, "charge_id", chargeID.String, "by_user", me.ID,
		"refund_id", rf.ID, "status", rf.Status, "fee_refunded", params.RefundApplicationFee)
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"purchase_id": purchaseID,
		"refund_id":   rf.ID,
		"status":      rf.Status,
	})
}

// saleAccountAndPlay is the Stripe account a store sale was charged on
// (its organisation's own) and whether what it bought has played: a
// ticket's team has opened a seat, a party has started a game.
func (h *Handlers) saleAccountAndPlay(ctx context.Context, purchaseID int64) (string, bool, error) {
	var (
		acct   sql.NullString
		played bool
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT g.stripe_connect_account_id,
		       EXISTS (SELECT 1 FROM player_slots s WHERE s.team_id = p.team_id AND s.claimed_at IS NOT NULL)
		       OR EXISTS (SELECT 1 FROM events e WHERE e.id = p.event_id AND p.kind = 'party' AND e.first_game_at IS NOT NULL)
		FROM ticket_purchases p
		LEFT JOIN events e ON e.id = p.event_id
		LEFT JOIN party_offers o ON o.id = p.party_offer_id
		LEFT JOIN organizations g ON g.id = COALESCE(e.org_id, o.org_id, p.seller_org_id)
		WHERE p.id = $1
	`, purchaseID).Scan(&acct, &played)
	return acct.String, played, err
}

// sellerRefundable reports whether an org admin may refund a sale
// themselves: a ticket or party they sold through their own Stripe
// account (a platform fee marks it), since their buyers are theirs to
// serve and the money is theirs.
// A sale that settled on the platform account is the platform's money,
// and hosted passes the org bought are the platform's sale: both stay
// with the platform admin. A book from the shop is the home org's own
// sale.
func sellerRefundable(kind string, feeCents int64) bool {
	return (kind == "ticket" || kind == "party") && feeCents > 0 || kind == "book"
}
