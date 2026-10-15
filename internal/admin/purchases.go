package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

/* ---------------- Purchases ----------------
 *
 * Event passes arrangers bought before pay per team (internal/billing):
 * no new ones are sold, but the ones bought stay on record — listed,
 * billed and voided by the platform admin, refunded, with receipts.
 */

type purchaseDTO struct {
	ID                int64  `json:"id"`
	OrgID             int64  `json:"org_id"`
	OrgName           string `json:"org_name,omitempty"`
	AdventureSlug     string `json:"adventure_slug"`
	AdventureTitle    string `json:"adventure_title,omitempty"`
	MaxPlayers        int    `json:"max_players"`
	AmountCents       int    `json:"amount_cents"`
	Currency          string `json:"currency"`
	Provider          string `json:"provider"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	State             string `json:"state"`
	AssignmentID      *int64 `json:"assignment_id,omitempty"`
	// TopUpAssignmentID is set when the purchase tops up an existing
	// assignment's cap rather than creating a fresh one. Populated
	// from creation through the lifecycle so the listing UI can
	// distinguish a "new event slots" buy from an "added slots to
	// event #N" buy.
	TopUpAssignmentID *int64 `json:"top_up_assignment_id,omitempty"`
	InvoiceURL        string `json:"invoice_url,omitempty"`
	CreatedAt         string `json:"created_at"`
	PaidAt            string `json:"paid_at,omitempty"`
	// BilledAt is set by the operator (admin) after sending the real
	// off-platform invoice. Only meaningful for provider="invoice"
	// rows; on Stripe / free / simulated rows the column stays NULL
	// because there's nothing for an admin to bill afterwards.
	BilledAt string `json:"billed_at,omitempty"`
	// InvoiceNumber is the off-platform invoice's number, entered by
	// the operator with "Mark billed".
	InvoiceNumber string `json:"invoice_number,omitempty"`
	// VoidedAt is set by the operator to exclude this purchase from
	// invoice runs without unwinding the assignment grant. Only
	// meaningful for provider="invoice" rows.
	VoidedAt string `json:"voided_at,omitempty"`
	// PreMigration is set when this row was imported from another zv
	// instance via the org-bundle importer. The recorded charge id
	// is on the source instance's Stripe account; the refund button
	// in the UI swaps to a "refund through legacy dashboard" notice
	// when this is true, and the RefundPurchase handler 409s if
	// called anyway.
	PreMigration bool `json:"pre_migration,omitempty"`
	// EventCancelled says the event the pass was used for was cancelled,
	// for the operator to decide whether to void an invoice. Listing only.
	EventCancelled bool `json:"event_cancelled,omitempty"`
}

// SetPurchaseBilled flips the billed_at flag on a purchase row.
// Admin-only and only meaningful for provider="invoice" rows — the
// operator clicks "Mark billed" after sending the real off-platform
// invoice, and the purchase drops out of the "to bill" filter on the
// admin Purchases page.
//
// Body: {billed: true|false, invoice_number?}. true stamps now_text()
// and records the invoice number; false clears both. Idempotent on
// repeat clicks.
func (h *Handlers) SetPurchaseBilled(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	var req struct {
		Billed        bool   `json:"billed"`
		InvoiceNumber string `json:"invoice_number"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !req.Billed {
		noteAction(r, "unbilled")
	}
	var provider string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT provider FROM purchases WHERE id = $1`, purchaseID).Scan(&provider); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "purchase not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if provider != "invoice" {
		httpx.Error(w, http.StatusBadRequest,
			"only invoice purchases can be marked billed")
		return
	}
	number := strings.TrimSpace(req.InvoiceNumber)
	if len(number) > 100 {
		httpx.Error(w, http.StatusBadRequest, "invoice number is too long")
		return
	}
	if !req.Billed {
		number = ""
	}
	stmt := `UPDATE purchases SET billed_at = NULL, invoice_number = $2 WHERE id = $1`
	if req.Billed {
		stmt = `UPDATE purchases SET billed_at = now_text(), invoice_number = $2 WHERE id = $1`
	}
	if _, err := h.DB.ExecContext(r.Context(), stmt, purchaseID, number); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	var billedAt sql.NullString
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT billed_at FROM purchases WHERE id = $1`, purchaseID).Scan(&billedAt)
	httpx.JSON(w, http.StatusOK, map[string]any{"billed_at": billedAt.String, "invoice_number": number})
}

// VoidPurchase marks an invoice purchase as voided ("makulera") so it
// drops out of the operator's "to bill" filter and any future invoice
// run. The assignment grant is NOT unwound — the org keeps the slots
// they already received; the row just stops being a billable line.
//
// Admin-only. Idempotent: voiding an already-voided row is a no-op.
// Restricted to provider="invoice" rows that haven't been billed yet
// — once billed_at is set the operator has already issued an
// invoice, so voiding here would silently disagree with paperwork.
func (h *Handlers) VoidPurchase(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	var (
		provider string
		state    string
		billedAt sql.NullString
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT provider, state, billed_at FROM purchases WHERE id = $1`, purchaseID,
	).Scan(&provider, &state, &billedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "purchase not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if provider != "invoice" {
		httpx.Error(w, http.StatusBadRequest, "only invoice purchases can be voided")
		return
	}
	if state != "paid" {
		httpx.Error(w, http.StatusConflict, "purchase is "+state+"; cannot void")
		return
	}
	if billedAt.Valid && billedAt.String != "" {
		httpx.Error(w, http.StatusConflict, "purchase already billed; cannot void")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE purchases SET voided_at = COALESCE(voided_at, now_text()) WHERE id = $1`,
		purchaseID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	var voidedAt sql.NullString
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT voided_at FROM purchases WHERE id = $1`, purchaseID).Scan(&voidedAt)
	httpx.JSON(w, http.StatusOK, map[string]any{"voided_at": voidedAt.String})
}

// UnvoidPurchase undoes VoidPurchase: the row is billable again and
// shows under "to bill". Admin-only; a no-op on a row that isn't voided.
func (h *Handlers) UnvoidPurchase(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE purchases SET voided_at = NULL WHERE id = $1 AND provider = 'invoice'`, purchaseID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "purchase not found")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"voided_at": ""})
}

// ListPurchases returns every purchase visible to the caller — admins
// see all, org users see their own.
func (h *Handlers) ListPurchases(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT p.id, COALESCE(p.org_id, 0), COALESCE(o.name, ''), p.adventure_slug, COALESCE(a.title, ''),
		       p.max_players, p.amount_cents, p.currency, p.provider,
		       COALESCE(p.provider_session_id, ''), p.state, p.assignment_id,
		       p.top_up_assignment_id,
		       p.created_at, COALESCE(p.paid_at, ''),
		       COALESCE(p.stripe_invoice_url, ''),
		       COALESCE(p.billed_at, ''), p.invoice_number,
		       COALESCE(p.voided_at, ''),
		       p.pre_migration,
		       EXISTS (SELECT 1 FROM events e WHERE e.assignment_id = p.assignment_id AND e.state = 'cancelled')
		FROM purchases p
		LEFT JOIN adventures a ON a.slug = p.adventure_slug
		LEFT JOIN organizations o ON o.id = p.org_id
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
		query += ` WHERE p.org_id = $1`
		args = append(args, *me.OrgID)
	}
	query += ` ORDER BY p.created_at DESC`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []purchaseDTO{}
	for rows.Next() {
		var (
			d          purchaseDTO
			assignment sql.NullInt64
			topUp      sql.NullInt64
		)
		if err := rows.Scan(&d.ID, &d.OrgID, &d.OrgName, &d.AdventureSlug, &d.AdventureTitle,
			&d.MaxPlayers, &d.AmountCents, &d.Currency, &d.Provider,
			&d.ProviderSessionID, &d.State, &assignment, &topUp,
			&d.CreatedAt, &d.PaidAt, &d.InvoiceURL, &d.BilledAt, &d.InvoiceNumber, &d.VoidedAt, &d.PreMigration, &d.EventCancelled); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if assignment.Valid {
			v := assignment.Int64
			d.AssignmentID = &v
		}
		if topUp.Valid {
			v := topUp.Int64
			d.TopUpAssignmentID = &v
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"purchases": out})
}

// GetPurchase returns a single purchase with its checkout URL. Used by
// the simulated checkout page to show what's in the cart.
func (h *Handlers) GetPurchase(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	var (
		d          purchaseDTO
		assignment sql.NullInt64
		topUp      sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.id, COALESCE(p.org_id, 0), COALESCE(o.name, ''), p.adventure_slug, COALESCE(a.title, ''),
		       p.max_players, p.amount_cents, p.currency, p.provider,
		       COALESCE(p.provider_session_id, ''), p.state, p.assignment_id,
		       p.top_up_assignment_id,
		       p.created_at, COALESCE(p.paid_at, ''),
		       COALESCE(p.stripe_invoice_url, ''),
		       COALESCE(p.billed_at, ''), p.invoice_number,
		       COALESCE(p.voided_at, ''),
		       p.pre_migration
		FROM purchases p
		LEFT JOIN adventures a ON a.slug = p.adventure_slug
		LEFT JOIN organizations o ON o.id = p.org_id
		WHERE p.id = $1
	`, purchaseID).Scan(&d.ID, &d.OrgID, &d.OrgName, &d.AdventureSlug, &d.AdventureTitle,
		&d.MaxPlayers, &d.AmountCents, &d.Currency, &d.Provider,
		&d.ProviderSessionID, &d.State, &assignment, &topUp, &d.CreatedAt, &d.PaidAt, &d.InvoiceURL, &d.BilledAt, &d.InvoiceNumber, &d.VoidedAt, &d.PreMigration)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "purchase not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !readsAsAdmin(me) && (me.OrgID == nil || *me.OrgID != d.OrgID) {
		httpx.Error(w, http.StatusForbidden, "not your purchase")
		return
	}
	if assignment.Valid {
		v := assignment.Int64
		d.AssignmentID = &v
	}
	if topUp.Valid {
		v := topUp.Int64
		d.TopUpAssignmentID = &v
	}
	httpx.JSON(w, http.StatusOK, d)
}
