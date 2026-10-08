package admin

import (
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
	Blocks         int    `json:"blocks"`
	// Teams is how many teams a game holds for what a hosted pass bought.
	Teams       int    `json:"teams,omitempty"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	FeeCents    int64  `json:"application_fee_cents"`
	Refundable  bool   `json:"refundable"`
	InvoiceURL  string `json:"invoice_url,omitempty"`
	CreatedAt   string `json:"created_at"`
	PaidAt      string `json:"paid_at,omitempty"`
	RefundedAt  string `json:"refunded_at,omitempty"`
}

// ticketSaleOrg is the org a ticket_purchases row belongs to: the
// event's, the party offer's while its event is still to be made, or
// the buyer's for a hosted pass.
const ticketSaleOrg = `COALESCE(e.org_id, o.org_id, p.buyer_org_id)`

// ListTicketSales returns the ticket purchases visible to the caller:
// the platform admin sees every org's, an org user their own org's.
//
// GET /api/admin/ticket-purchases
func (h *Handlers) ListTicketSales(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT p.id, p.kind, p.state, COALESCE(p.event_id, 0),
		       COALESCE(NULLIF(p.party_name, ''), NULLIF(e.party_name, ''), e.name, ''),
		       COALESCE(NULLIF(a.title, ''), a.slug, ''), COALESCE(org.name, ''),
		       p.buyer_email, p.team_size, p.blocks, p.amount_cents, p.currency, p.application_fee_cents,
		       COALESCE(p.stripe_charge_id, '') <> '', COALESCE(p.stripe_invoice_url, ''),
		       p.created_at, COALESCE(p.paid_at, ''), COALESCE(p.refunded_at, '')
		FROM ticket_purchases p
		LEFT JOIN events e ON e.id = p.event_id
		LEFT JOIN party_offers o ON o.id = p.party_offer_id
		LEFT JOIN adventures a ON a.slug = COALESCE(e.adventure_slug, o.adventure_slug, NULLIF(p.adventure_slug, ''))
		LEFT JOIN organizations org ON org.id = ` + ticketSaleOrg + `
	`
	var args []any
	if !isAdmin(me) {
		if !isOrgScoped(me) {
			httpx.Error(w, http.StatusForbidden, "no org context")
			return
		}
		query += ` WHERE ` + ticketSaleOrg + ` = $1`
		args = append(args, *me.OrgID)
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
			&d.BuyerEmail, &d.TeamSize, &d.Blocks, &d.AmountCents, &d.Currency, &d.FeeCents,
			&hasCharge, &d.InvoiceURL, &d.CreatedAt, &d.PaidAt, &d.RefundedAt); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
			return
		}
		d.Refundable = d.State == "paid" && hasCharge && d.AmountCents > 0
		d.Teams = d.Blocks * TeamsPerBlock
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
// Admin-only: refunds touch real money.
//
// POST /api/admin/ticket-purchases/{purchaseID}/refund
func (h *Handlers) RefundTicketSale(w http.ResponseWriter, r *http.Request) {
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
		state    string
		chargeID sql.NullString
		fee      int64
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT state, stripe_charge_id, application_fee_cents FROM ticket_purchases WHERE id = $1`,
		purchaseID).Scan(&state, &chargeID, &fee); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "purchase not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
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
	rf, err := h.Provider.Refund(r.Context(), payments.RefundParams{
		ChargeID:             chargeID.String,
		RefundApplicationFee: fee > 0,
		IdempotencyKey:       fmt.Sprintf("refund-ticket-purchase-%d", purchaseID),
	})
	if err != nil {
		logx.Error("payments.ticket_refund.new_failed",
			"ticket_purchase_id", purchaseID, "charge_id", chargeID.String, "err", err.Error())
		httpx.Error(w, http.StatusBadGateway, "refund failed")
		return
	}
	logx.Info("payments.ticket_refund.created",
		"ticket_purchase_id", purchaseID, "charge_id", chargeID.String,
		"refund_id", rf.ID, "status", rf.Status, "reverse_transfer", fee > 0)
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"purchase_id": purchaseID,
		"refund_id":   rf.ID,
		"status":      rf.Status,
	})
}
