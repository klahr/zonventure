package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/sitename"
)

// purchaseReceipt is an event pass's receipt and what decides where it
// comes from: Stripe's own (InvoiceURL), else the plain one served here.
type purchaseReceipt struct {
	emailtpl.Receipt
	OrgID      int64
	State      string
	Provider   string
	InvoiceURL string
	BuyerEmail string
	Lang       string
	TimeZone   string
}

// hasReceipt is whether a purchase was paid through checkout: free and
// invoiced passes have none (an invoice comes off-platform).
func (p purchaseReceipt) hasReceipt() bool {
	return (p.State == "paid" || p.State == "refunded") && p.AmountCents > 0 && p.Provider != "free" && p.Provider != "invoice"
}

func (h *Handlers) loadPurchaseReceipt(ctx context.Context, purchaseID int64) (purchaseReceipt, error) {
	var (
		p    purchaseReceipt
		paid string
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT p.id, COALESCE(p.org_id, 0), COALESCE(o.name, ''), COALESCE(a.title, p.adventure_slug), p.max_players,
		       p.top_up_assignment_id IS NOT NULL, COALESCE(p.paid_at, ''), p.amount_cents, p.currency, p.state, p.provider,
		       COALESCE(p.stripe_invoice_url, ''), COALESCE(u.email, ''), COALESCE(o.language, ''), COALESCE(o.time_zone, '')
		FROM purchases p
		LEFT JOIN organizations o ON o.id = p.org_id
		LEFT JOIN adventures a ON a.slug = p.adventure_slug
		LEFT JOIN users u ON u.id = p.buyer_user_id
		WHERE p.id = $1
	`, purchaseID).Scan(&p.Number, &p.OrgID, &p.Buyer, &p.Adventure, &p.Players, &p.TopUp, &paid, &p.AmountCents, &p.Currency,
		&p.State, &p.Provider, &p.InvoiceURL, &p.BuyerEmail, &p.Lang, &p.TimeZone)
	if err != nil {
		return p, err
	}
	p.Seller = sitename.Name()
	p.Paid, _ = parseRecapTimestamp(paid)
	p.Refunded = p.State == "refunded"
	return p, nil
}

// PurchaseReceipt downloads an event pass's receipt: Stripe's, where it
// has one, else a plain one (simulated checkout, or a Stripe payment
// whose receipt never arrived). The buying organisation and platform
// admins only.
//
// GET /api/admin/purchases/{purchaseID}/receipt
func (h *Handlers) PurchaseReceipt(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	p, err := h.loadPurchaseReceipt(r.Context(), purchaseID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "purchase not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !readsAsAdmin(me) && (me.OrgID == nil || *me.OrgID != p.OrgID) {
		httpx.Error(w, http.StatusForbidden, "not your purchase")
		return
	}
	if !p.hasReceipt() {
		httpx.Error(w, http.StatusConflict, "this purchase has no receipt")
		return
	}
	if p.InvoiceURL != "" {
		http.Redirect(w, r, p.InvoiceURL, http.StatusFound)
		return
	}
	lang := emailtpl.LangFromRequest(r)
	subject, c := emailtpl.PurchaseReceipt(lang, p.Receipt, p.TimeZone, "")
	page, _ := emailtpl.Render(emailtpl.Minimal, emailtpl.Platform(), subject, c)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="receipt-%d.html"`, purchaseID))
	_, _ = w.Write([]byte(page))
}

// sendPurchaseReceipt mails the buyer of a pass just paid through
// checkout its receipt, with the link to download it. Best effort: the
// purchase is already paid.
func (h *Handlers) sendPurchaseReceipt(ctx context.Context, purchaseID int64) {
	p, err := h.loadPurchaseReceipt(ctx, purchaseID)
	if err != nil {
		logx.Warn("admin.purchase.receipt", "purchase_id", purchaseID, "err", err.Error())
		return
	}
	if !p.hasReceipt() || strings.TrimSpace(p.BuyerEmail) == "" {
		return
	}
	link := p.InvoiceURL
	if base := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/"); link == "" && base != "" {
		link = base + "/app/arrange/#/settings/purchases"
	}
	lang := emailtpl.Lang(p.Lang)
	subject, c := emailtpl.PurchaseReceipt(lang, p.Receipt, p.TimeZone, link)
	msg := emailtpl.Message([]string{p.BuyerEmail}, subject, emailtpl.Classic, emailtpl.Platform(), c)
	if err := h.send(ctx, "purchase_receipt", msg); err != nil {
		logx.Warn("admin.purchase.receipt", "purchase_id", purchaseID, "err", err.Error())
	}
}
