package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/metrics"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Purchases ----------------
 *
 * Arrangers buy player-slot assignments here. Each assignment lets
 * one event run with up to N player slots across however many teams
 * the arranger wants to form. The flow is deliberately Stripe-shaped:
 *
 *   POST /api/admin/purchases               → create pending (returns checkout_url)
 *   GET  /purchase/{id}                     → simulated checkout HTML
 *   POST /api/admin/purchases/{id}/confirm  → mark paid, create assignment
 *
 * Free tier: when the buyer asks for ≤ FreePlayersPerEvent slots the
 * computed amount is zero. Stripe rejects $0 Checkout Sessions, so we
 * short-circuit: the purchase + assignment are created and marked
 * paid in one transaction with provider="free", and the response
 * carries the assignment_id directly so the UI skips the redirect.
 *
 * The persisted row holds provider + provider_session_id so mixed
 * provider histories (free / simulated / stripe) coexist cleanly.
 */

// PricePerPlayerCents is what each chargeable player slot costs. The
// first FreePlayersPerEvent slots in any event are free, regardless of
// how often the buyer comes back.
const (
	PricePerPlayerCents = 500
	FreePlayersPerEvent = 15
)

// purchaseAmountCents computes the chargeable total for a NEW
// assignment buy of N player slots: the first FreePlayersPerEvent are
// free, the rest cost PricePerPlayerCents each. Returns 0 for any
// N ≤ FreePlayersPerEvent.
//
// Top-ups (adding slots to an existing assignment) bypass this helper
// and are always linear — the free band is consumed by the original
// purchase, so subsequent additions charge per slot regardless of
// where the resulting total lands. Otherwise a buyer could mint a
// 1-slot free assignment and grow it 14 more free slots later.
func purchaseAmountCents(addedPlayers int) int {
	chargeable := addedPlayers - FreePlayersPerEvent
	if chargeable <= 0 {
		return 0
	}
	return chargeable * PricePerPlayerCents
}

// topUpAmountCents charges PricePerPlayerCents per added slot. No
// free-band deduction — see purchaseAmountCents for the rationale.
func topUpAmountCents(addedPlayers int) int {
	return addedPlayers * PricePerPlayerCents
}

type purchaseDTO struct {
	ID                int64  `json:"id"`
	OrgID             int64  `json:"org_id"`
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
	CheckoutURL       string `json:"checkout_url,omitempty"`
	InvoiceURL        string `json:"invoice_url,omitempty"`
	CreatedAt         string `json:"created_at"`
	PaidAt            string `json:"paid_at,omitempty"`
	// BilledAt is set by the operator (admin) after sending the real
	// off-platform invoice. Only meaningful for provider="invoice"
	// rows; on Stripe / free / simulated rows the column stays NULL
	// because there's nothing for an admin to bill afterwards.
	BilledAt          string `json:"billed_at,omitempty"`
	// VoidedAt is set by the operator to exclude this purchase from
	// invoice runs without unwinding the assignment grant. Only
	// meaningful for provider="invoice" rows.
	VoidedAt          string `json:"voided_at,omitempty"`
}

type createPurchaseRequest struct {
	AdventureSlug string `json:"adventure_slug"`
	MaxPlayers    int    `json:"max_players"`
	// Provider lets callers opt into a specific flow. Empty defaults
	// to "stripe" if configured, "simulated" otherwise. A computed
	// amount of zero forces "free" regardless of caller preference.
	Provider string `json:"provider,omitempty"`
	// TopUpAssignmentID, when set, makes this purchase a top-up of an
	// existing assignment's cap rather than a fresh assignment. The
	// adventure_slug is ignored if it disagrees with the assignment's
	// own slug (the server uses the assignment's slug authoritatively
	// so the UI can stay terse and just send the assignment id).
	TopUpAssignmentID *int64 `json:"top_up_assignment_id,omitempty"`
}

// CreatePurchase registers a pending purchase and returns its checkout
// URL. The actual "payment" happens when the client (or a Stripe
// webhook) calls ConfirmPurchase.
func (h *Handlers) CreatePurchase(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusBadRequest, "admins can't purchase — create a free assignment instead")
		return
	}
	if !me.EmailVerified {
		httpx.JSON(w, http.StatusForbidden, map[string]string{
			"error": "verify your email before buying assignments",
			"code":  "verification_required",
		})
		return
	}
	var req createPurchaseRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.MaxPlayers <= 0 || req.MaxPlayers > 500 {
		httpx.Error(w, http.StatusBadRequest, "max_players must be 1–500")
		return
	}
	// New-assignment buys carry the FreePlayersPerEvent floor: anything
	// smaller would still cost €0 and produce a smaller event for no
	// upside. Top-ups skip this — they're billed linearly per slot, so
	// a +1 add is a meaningful (chargeable) operation.
	if req.TopUpAssignmentID == nil && req.MaxPlayers < FreePlayersPerEvent {
		httpx.Error(w, http.StatusBadRequest,
			fmt.Sprintf("minimum %d players per event", FreePlayersPerEvent))
		return
	}
	// Top-up branch: derive the adventure slug from the target
	// assignment, refusing if the assignment is missing, not owned by
	// the caller's org, or attached to a terminal-state event. The
	// eventual UPDATE happens at confirm/webhook time; here we just
	// validate and pin down the slug for the purchases row.
	var topUpID *int64
	if req.TopUpAssignmentID != nil && *req.TopUpAssignmentID > 0 {
		var (
			assnOrg    int64
			assnSlug   string
			eventID    sql.NullInt64
			eventState sql.NullString
		)
		err := h.DB.QueryRowContext(r.Context(), `
			SELECT aa.org_id, aa.adventure_slug, ar.id, ar.state
			FROM adventure_assignments aa
			LEFT JOIN events ar ON ar.assignment_id = aa.id
			WHERE aa.id = $1
		`, *req.TopUpAssignmentID).Scan(&assnOrg, &assnSlug, &eventID, &eventState)
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "assignment not found")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "assignment lookup failed")
			return
		}
		if assnOrg != *me.OrgID {
			httpx.Error(w, http.StatusForbidden, "assignment is not yours")
			return
		}
		if eventState.Valid && (eventState.String == "completed" || eventState.String == "cancelled") {
			httpx.Error(w, http.StatusConflict, "cannot top up a completed or cancelled event")
			return
		}
		req.AdventureSlug = assnSlug // canonical — ignore any client value
		topUpID = req.TopUpAssignmentID
	}
	// Source adventure must exist + be visible to the buyer (either
	// their own or the global catalog).
	advOrg, err := h.adventureOwner(r.Context(), req.AdventureSlug)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure lookup failed")
		return
	}
	if !canSeeOwned(me, advOrg) {
		httpx.Error(w, http.StatusForbidden, "that adventure is not available to your org")
		return
	}
	amount := purchaseAmountCents(req.MaxPlayers)
	if topUpID != nil {
		amount = topUpAmountCents(req.MaxPlayers)
	}

	provider := req.Provider
	switch provider {
	case "":
		// Free buys skip Stripe regardless of how the operator is
		// configured — Stripe rejects $0 Checkout Sessions. Otherwise
		// default to stripe when wired, simulated as the dev fallback.
		if amount == 0 {
			provider = "free"
		} else if h.stripeEnabled() {
			provider = "stripe"
		} else {
			provider = "simulated"
		}
	case "free", "simulated", "stripe", "invoice":
		// Caller-specified. Validated below.
	default:
		httpx.Error(w, http.StatusBadRequest, "unsupported provider")
		return
	}
	if provider == "stripe" && !h.stripeEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "stripe is not configured on this server")
		return
	}
	if provider == "free" && amount > 0 {
		httpx.Error(w, http.StatusBadRequest, "free provider requires amount=0")
		return
	}
	if provider == "invoice" {
		// Off-platform invoice billing requires the org to be marked
		// trusted by a global admin. Anyone else trying to ride this
		// path (eg. a hand-crafted POST) gets the same 403 they'd
		// expect for any other admin-gated affordance.
		var trusted bool
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT trusted <> 0 FROM organizations WHERE id = $1`,
			*me.OrgID).Scan(&trusted); err != nil || !trusted {
			httpx.Error(w, http.StatusForbidden, "invoice billing is not enabled for this organization")
			return
		}
	}
	if provider != "free" && amount == 0 {
		// A non-free provider for a $0 buy would just bounce off
		// Stripe / sit pending forever. Coerce to free.
		provider = "free"
	}

	// Look up the adventure title up-front so we can pass a friendly
	// product name to Stripe Checkout instead of the bare slug. Best-
	// effort: a miss falls through to using the slug itself.
	var advTitle string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT title FROM adventures WHERE slug = $1`, req.AdventureSlug).Scan(&advTitle)

	// Free + invoice both skip Stripe entirely: insert the purchase +
	// assignment + flip to paid in one transaction so the response
	// can carry the assignment id directly and the UI doesn't
	// redirect. Invoice preserves the actual amount on the row so the
	// operator can pull "WHERE provider='invoice' AND state='paid'"
	// to find what to bill the org for off-platform.
	if provider == "free" || provider == "invoice" {
		assID, purchaseID, ferr := h.createOffPlatformPurchase(
			r.Context(), *me.OrgID, me.ID, req.AdventureSlug, req.MaxPlayers, amount, provider, topUpID)
		if ferr != nil {
			httpx.Error(w, http.StatusInternalServerError, provider+" purchase failed: "+ferr.Error())
			return
		}
		metrics.PurchaseEvents.WithLabelValues("created_" + provider).Inc()
		metrics.PurchaseEvents.WithLabelValues("confirmed_" + provider).Inc()
		httpx.JSON(w, http.StatusCreated, map[string]any{
			"id":            purchaseID,
			"amount_cents":  amount,
			"currency":      "EUR",
			"state":         "paid",
			"provider":      provider,
			"assignment_id": assID,
		})
		return
	}

	var id int64
	var topUpArg any
	if topUpID != nil {
		topUpArg = *topUpID
	}
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_players,
		                      amount_cents, currency, provider, state, top_up_assignment_id)
		VALUES ($1, $2, $3, $4, $5, 'EUR', $6, 'pending', $7)
		RETURNING id
	`, *me.OrgID, me.ID, req.AdventureSlug, req.MaxPlayers, amount, provider, topUpArg).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}

	// Build the URL the browser navigates to. Simulated mode keeps
	// pointing at our in-app /purchase/{id} fake checkout; Stripe
	// mode creates a Checkout Session and uses its hosted URL.
	checkoutURL := fmt.Sprintf("/purchase/%d", id)
	if provider == "stripe" {
		// Look up the org's Stripe customer ID (set by an earlier
		// completed checkout) so this session attaches to the same
		// customer record. Empty for first-ever purchases — the
		// webhook will capture it on completion.
		var existingCustomerID sql.NullString
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT stripe_customer_id FROM organizations WHERE id = $1`,
			*me.OrgID).Scan(&existingCustomerID)
		url, sessID, serr := h.createStripeCheckoutSession(r.Context(),
			id, req.AdventureSlug, advTitle, req.MaxPlayers, amount, me.Email,
			existingCustomerID.String)
		if serr != nil {
			// Roll back the pending row so we don't leave dangling
			// pendings every time Stripe is down. Best-effort: if
			// the DELETE itself fails, the user already sees a 502
			// and the orphan row is cleaned by the next admin sweep.
			_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM purchases WHERE id = $1`, id)
			httpx.Error(w, http.StatusBadGateway, "stripe checkout failed: "+serr.Error())
			return
		}
		// Stash the session id so the webhook can correlate. Best-
		// effort: a missing UPDATE means the webhook still finds the
		// purchase via the metadata.zv_purchase_id round-trip.
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE purchases SET provider_session_id = $1 WHERE id = $2`, sessID, id)
		checkoutURL = url
	}

	metrics.PurchaseEvents.WithLabelValues("created").Inc()
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id":           id,
		"amount_cents": amount,
		"currency":     "EUR",
		"state":        "pending",
		"provider":     provider,
		"checkout_url": checkoutURL,
	})
}

// createOffPlatformPurchase inserts a purchase + assignment in one tx
// for the free-tier and invoice short-circuits. State is set to paid
// immediately; the assignment id is returned so the response carries
// it directly. amountCents is preserved on the row (zero for free,
// the chargeable amount for invoice) so the operator's "what do we
// invoice next?" query is a simple WHERE provider='invoice'.
//
// When topUpID is non-nil, the assignment isn't created — instead its
// max_players is bumped by `addedPlayers` and the topped-up id is
// returned. The purchases row records both top_up_assignment_id (the
// historical "this was a top-up" marker) and assignment_id (the
// fulfilled assignment, here equal to the top-up target so the
// listing UI can link to it the same way as a fresh-assignment buy).
func (h *Handlers) createOffPlatformPurchase(ctx context.Context, orgID, buyerUserID int64, advSlug string, addedPlayers, amountCents int, provider string, topUpID *int64) (assignmentID, purchaseID int64, err error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	var topUpArg any
	if topUpID != nil {
		topUpArg = *topUpID
		// FOR UPDATE pins the row across the bump so a concurrent
		// top-up can't interleave (each would read the same pre-bump
		// max_players and double-count the addition).
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM adventure_assignments WHERE id = $1 AND org_id = $2 FOR UPDATE`,
			*topUpID, orgID).Scan(&assignmentID); err != nil {
			return 0, 0, fmt.Errorf("top-up lookup: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE adventure_assignments SET max_players = max_players + $1 WHERE id = $2`,
			addedPlayers, assignmentID); err != nil {
			return 0, 0, fmt.Errorf("top-up bump: %w", err)
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
			VALUES ($1, $2, $3) RETURNING id
		`, orgID, advSlug, addedPlayers).Scan(&assignmentID); err != nil {
			return 0, 0, fmt.Errorf("assignment insert: %w", err)
		}
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_players,
		                      amount_cents, currency, provider, state, paid_at, assignment_id, top_up_assignment_id)
		VALUES ($1, $2, $3, $4, $5, 'EUR', $6, 'paid', now_text(), $7, $8)
		RETURNING id
	`, orgID, buyerUserID, advSlug, addedPlayers, amountCents, provider, assignmentID, topUpArg).Scan(&purchaseID); err != nil {
		return 0, 0, fmt.Errorf("purchase insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return assignmentID, purchaseID, nil
}

// ConfirmPurchase marks a purchase paid and creates the assignment. In
// the simulated flow the user clicks "Pay" on the checkout page, which
// POSTs here with the session cookie. For Stripe, this same endpoint
// becomes the webhook target — the caller would be Stripe's server
// with a signature header we'd verify there instead of the session.
func (h *Handlers) ConfirmPurchase(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	me, _ := auth.FromContext(r.Context())

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()

	var (
		orgID         int64
		advSlug       string
		maxPlayers    int
		state         string
		existingAssn  sql.NullInt64
		topUpID       sql.NullInt64
	)
	// FOR UPDATE serialises overlapping confirms (double-clicked Pay
	// button, webhook arriving alongside a manual confirm) so two
	// transactions can't both observe state='pending' and each insert
	// their own assignment row.
	err = tx.QueryRowContext(r.Context(), `
		SELECT org_id, adventure_slug, max_players, state, assignment_id, top_up_assignment_id
		FROM purchases WHERE id = $1 FOR UPDATE
	`, purchaseID).Scan(&orgID, &advSlug, &maxPlayers, &state, &existingAssn, &topUpID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "purchase not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	// Only the buying org can confirm (simulated flow). Admins can
	// confirm on behalf of anyone — useful for reconciling pending
	// purchases after a Stripe webhook fails to deliver.
	if !isAdmin(me) {
		if me.OrgID == nil || *me.OrgID != orgID {
			httpx.Error(w, http.StatusForbidden, "not your purchase")
			return
		}
	}
	if state == "paid" {
		// Idempotent — Stripe webhooks redeliver. Return the existing
		// assignment id so the caller can pick up state.
		httpx.JSON(w, http.StatusOK, map[string]any{
			"id": purchaseID, "state": "paid", "assignment_id": existingAssn.Int64,
		})
		return
	}
	if state != "pending" {
		httpx.Error(w, http.StatusConflict, "purchase is "+state+"; cannot confirm")
		return
	}

	var assID int64
	if topUpID.Valid {
		// Top-up: pin the existing assignment row, bump its cap, and
		// return its id as the fulfilled assignment.
		if err := tx.QueryRowContext(r.Context(),
			`SELECT id FROM adventure_assignments WHERE id = $1 AND org_id = $2 FOR UPDATE`,
			topUpID.Int64, orgID).Scan(&assID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "top-up lookup failed: "+err.Error())
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE adventure_assignments SET max_players = max_players + $1 WHERE id = $2`,
			maxPlayers, assID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "top-up bump failed: "+err.Error())
			return
		}
	} else {
		if err := tx.QueryRowContext(r.Context(), `
			INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
			VALUES ($1, $2, $3)
			RETURNING id
		`, orgID, advSlug, maxPlayers).Scan(&assID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "assignment insert failed: "+err.Error())
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE purchases
		SET state = 'paid', paid_at = now_text(), assignment_id = $1
		WHERE id = $2
	`, assID, purchaseID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "purchase update failed")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	metrics.PurchaseEvents.WithLabelValues("confirmed_simulated").Inc()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id": purchaseID, "state": "paid", "assignment_id": assID,
	})
}

// SetPurchaseBilled flips the billed_at flag on a purchase row.
// Admin-only and only meaningful for provider="invoice" rows — the
// operator clicks "Mark billed" after sending the real off-platform
// invoice, and the purchase drops out of the "to bill" filter on the
// admin Purchases page.
//
// Body: {billed: true|false}. true stamps now_text(); false clears
// the column. Idempotent on repeat clicks.
func (h *Handlers) SetPurchaseBilled(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	var req struct {
		Billed bool `json:"billed"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
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
	var stmt string
	if req.Billed {
		stmt = `UPDATE purchases SET billed_at = now_text() WHERE id = $1`
	} else {
		stmt = `UPDATE purchases SET billed_at = NULL WHERE id = $1`
	}
	if _, err := h.DB.ExecContext(r.Context(), stmt, purchaseID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	var billedAt sql.NullString
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT billed_at FROM purchases WHERE id = $1`, purchaseID).Scan(&billedAt)
	httpx.JSON(w, http.StatusOK, map[string]any{"billed_at": billedAt.String})
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

// GrantSlotsToEvent is the admin-only "add player slots to an org's
// event on the org's behalf" affordance. The slots are granted by
// bumping the underlying assignment's max_players cap; the cost of
// the bump is recorded as a purchases row routed via either the
// "free" or "invoice" provider:
//
//   - org NOT trusted (invoice billing off): amount is forced to 0
//     and the row is provider="free". Admin can't surprise-bill an
//     org that hasn't agreed to invoice billing.
//   - org trusted: admin sets amount_cents. Zero → provider="free"
//     (a goodwill grant). Positive → provider="invoice" so the row
//     shows up in the "to bill" filter on the operator's Purchases
//     page, ready to land on the next off-platform invoice.
//
// Either way the assignment is bumped and the org keeps the slots —
// the price is bookkeeping. Voiding the resulting invoice purchase
// later (POST /purchases/{id}/void) doesn't unwind the bump.
type grantSlotsRequest struct {
	AddedPlayers int `json:"added_players"`
	AmountCents  int `json:"amount_cents"`
}

func (h *Handlers) GrantSlotsToEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := strconv.ParseInt(chi.URLParam(r, "eventID"), 10, 64)
	if err != nil || eventID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	var req grantSlotsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AddedPlayers <= 0 || req.AddedPlayers > 500 {
		httpx.Error(w, http.StatusBadRequest, "added_players must be 1–500")
		return
	}
	if req.AmountCents < 0 {
		httpx.Error(w, http.StatusBadRequest, "amount_cents must be ≥ 0")
		return
	}
	me, _ := auth.FromContext(r.Context())
	// Pull the event + its assignment + the owning org's trusted flag
	// in one shot. The event must be in a non-terminal state so the
	// grant lands on a live event the org can still use.
	var (
		orgID         int64
		eventState    string
		assignmentID  sql.NullInt64
		advSlug       string
		trusted       bool
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT ar.org_id, ar.state, ar.assignment_id, ar.adventure_slug, (o.trusted <> 0)
		FROM events ar
		JOIN organizations o ON o.id = ar.org_id
		WHERE ar.id = $1
	`, eventID).Scan(&orgID, &eventState, &assignmentID, &advSlug, &trusted)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !assignmentID.Valid || assignmentID.Int64 == 0 {
		httpx.Error(w, http.StatusConflict, "event has no assignment to grow")
		return
	}
	if eventState == "completed" || eventState == "cancelled" {
		httpx.Error(w, http.StatusConflict, "cannot grant slots to a completed or cancelled event")
		return
	}
	// Untrusted orgs can't be silently invoiced. Force free.
	amount := req.AmountCents
	if !trusted {
		amount = 0
	}
	provider := "free"
	if amount > 0 {
		provider = "invoice"
	}
	topUp := assignmentID.Int64
	// Reuse createOffPlatformPurchase: same assignment-bump + paid-row
	// shape used by the org-side free / invoice buys. buyer_user_id is
	// the admin who granted (audit trail) — not an org member.
	assID, purchaseID, ferr := h.createOffPlatformPurchase(
		r.Context(), orgID, me.ID, advSlug, req.AddedPlayers, amount, provider, &topUp)
	if ferr != nil {
		httpx.Error(w, http.StatusInternalServerError, "grant failed: "+ferr.Error())
		return
	}
	metrics.PurchaseEvents.WithLabelValues("granted_" + provider).Inc()
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"purchase_id":   purchaseID,
		"assignment_id": assID,
		"amount_cents":  amount,
		"provider":      provider,
	})
}

// ListPurchases returns every purchase visible to the caller — admins
// see all, org users see their own.
func (h *Handlers) ListPurchases(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT p.id, p.org_id, p.adventure_slug, COALESCE(a.title, ''),
		       p.max_players, p.amount_cents, p.currency, p.provider,
		       COALESCE(p.provider_session_id, ''), p.state, p.assignment_id,
		       p.top_up_assignment_id,
		       p.created_at, COALESCE(p.paid_at, ''),
		       COALESCE(p.stripe_invoice_url, ''),
		       COALESCE(p.billed_at, ''),
		       COALESCE(p.voided_at, '')
		FROM purchases p
		LEFT JOIN adventures a ON a.slug = p.adventure_slug
	`
	var args []any
	if !isAdmin(me) {
		if !isOrgScoped(me) {
			httpx.Error(w, http.StatusForbidden, "no org context")
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
		if err := rows.Scan(&d.ID, &d.OrgID, &d.AdventureSlug, &d.AdventureTitle,
			&d.MaxPlayers, &d.AmountCents, &d.Currency, &d.Provider,
			&d.ProviderSessionID, &d.State, &assignment, &topUp,
			&d.CreatedAt, &d.PaidAt, &d.InvoiceURL, &d.BilledAt, &d.VoidedAt); err != nil {
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
		SELECT p.id, p.org_id, p.adventure_slug, COALESCE(a.title, ''),
		       p.max_players, p.amount_cents, p.currency, p.provider,
		       COALESCE(p.provider_session_id, ''), p.state, p.assignment_id,
		       p.top_up_assignment_id,
		       p.created_at, COALESCE(p.paid_at, ''),
		       COALESCE(p.stripe_invoice_url, ''),
		       COALESCE(p.billed_at, ''),
		       COALESCE(p.voided_at, '')
		FROM purchases p
		LEFT JOIN adventures a ON a.slug = p.adventure_slug
		WHERE p.id = $1
	`, purchaseID).Scan(&d.ID, &d.OrgID, &d.AdventureSlug, &d.AdventureTitle,
		&d.MaxPlayers, &d.AmountCents, &d.Currency, &d.Provider,
		&d.ProviderSessionID, &d.State, &assignment, &topUp, &d.CreatedAt, &d.PaidAt, &d.InvoiceURL, &d.BilledAt, &d.VoidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "purchase not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !isAdmin(me) && (me.OrgID == nil || *me.OrgID != d.OrgID) {
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
	d.CheckoutURL = fmt.Sprintf("/purchase/%d", d.ID)
	httpx.JSON(w, http.StatusOK, d)
}

// SimulatedCheckoutPage serves a tiny HTML page standing in for a
// Stripe Checkout redirect. It fetches its own data via the JSON API
// (so the same session cookie authorises) and posts the confirm when
// the user clicks Pay. Real Stripe deployments would redirect away
// from this page to Stripe's hosted checkout.
func (h *Handlers) SimulatedCheckoutPage(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || purchaseID <= 0 {
		http.Error(w, "bad purchase id", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// The page deliberately has zero external deps — it's what an
	// arranger briefly sees while "paying". Reuses the landing page's
	// colour language so it feels part of the app.
	fmt.Fprintf(w, simulatedCheckoutHTML, purchaseID)
}

const simulatedCheckoutHTML = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Simulated checkout · Zonventure</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Aclonica&family=Open+Sans:wght@400;600;700&display=swap" rel="stylesheet">
<style>
:root { color-scheme: light dark; --accent:#ad1357; --ink:#1f3349; --bg:#fbfbfd; --muted:#6b7f96; --surface:#ffffff; --border:#c6d2e166; }
@media (prefers-color-scheme: dark) { :root { --bg:#14202e; --ink:#e7ecf4; --muted:#9db1cc; --accent:#e24080; --surface:#1f3349; --border:#3a517266; } }
body { font-family:'Open Sans',system-ui,sans-serif; color:var(--ink); background:var(--bg); margin:0; min-height:100vh; display:grid; place-items:center; padding:1rem; }
.card { background:var(--surface); border:1px solid var(--border); border-radius:16px; padding:1.75rem; max-width:440px; width:100%%; box-shadow: 0 4px 24px rgba(0,0,0,.12); }
h1 { font-family:'Aclonica',Georgia,serif; color:var(--accent); font-size:1.8rem; margin:0 0 .25rem; }
.sub { color:var(--muted); margin:0 0 1.25rem; font-size:.9rem; text-transform:uppercase; letter-spacing:.1em; font-weight:700; }
dl { margin:0 0 1.25rem; display:grid; grid-template-columns:auto 1fr; gap:.35rem 1rem; }
dt { color:var(--muted); }
dd { margin:0; font-weight:600; }
button { font: inherit; font-weight:700; width:100%%; padding:.85rem 1.2rem; border:none; border-radius:32px; cursor:pointer; background:var(--accent); color:#fff; }
button.secondary { background:transparent; color:var(--ink); border:1px solid var(--border); margin-top:.6rem; }
button:disabled { opacity:.6; cursor:wait; }
.note { margin-top:1rem; padding:.75rem; background:rgba(173,19,87,.07); border-left:3px solid var(--accent); border-radius:6px; font-size:.85rem; color:var(--muted); }
#result { margin-top:1rem; font-weight:600; }
#result.ok { color:#429a3f; }
#result.err { color:#ff5252; }
</style></head>
<body>
<div class="card">
  <h1>Checkout</h1>
  <p class="sub">Simulated — no card required</p>
  <dl id="cart"><dt>loading…</dt><dd></dd></dl>
  <button id="pay">Pay</button>
  <button class="secondary" id="cancel">Cancel</button>
  <p class="note">In production this screen would be a Stripe-hosted Checkout Session. The button below calls the same /confirm endpoint the Stripe webhook will hit.</p>
  <p id="result"></p>
</div>
<script>
const PURCHASE_ID = %d;
const cart = document.getElementById('cart');
const payBtn = document.getElementById('pay');
const cancelBtn = document.getElementById('cancel');
const result = document.getElementById('result');

function dollars(cents, currency) {
  return new Intl.NumberFormat(undefined, { style:'currency', currency: currency || 'EUR' }).format(cents / 100);
}

// csrf reads the zv_csrf cookie set by EnsureCSRF and is echoed on
// POSTs back to /api/admin so the request matches the cookie.
function csrf() {
  const m = document.cookie.match(/(?:^|;\s*)zv_csrf=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : '';
}

async function load() {
  const r = await fetch('/api/admin/purchases/' + PURCHASE_ID, { credentials: 'same-origin' });
  if (r.status === 401) { location.href = '/arrange'; return; }
  if (!r.ok) { result.className = 'err'; result.textContent = 'Could not load purchase.'; return; }
  const p = await r.json();
  cart.innerHTML =
    '<dt>Adventure</dt><dd>' + (p.adventure_title || p.adventure_slug) + '</dd>' +
    '<dt>Player slots</dt><dd>' + p.max_players + '</dd>' +
    '<dt>Total</dt><dd>' + dollars(p.amount_cents, p.currency) + '</dd>' +
    '<dt>Status</dt><dd>' + p.state + '</dd>';
  if (p.state === 'paid') { payBtn.disabled = true; payBtn.textContent = 'Already paid'; }
}

payBtn.addEventListener('click', async () => {
  payBtn.disabled = true;
  result.className = '';
  result.textContent = 'Processing…';
  const r = await fetch('/api/admin/purchases/' + PURCHASE_ID + '/confirm', {
    method: 'POST', credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf() },
  });
  if (r.ok) {
    result.className = 'ok';
    result.textContent = 'Paid. Taking you back…';
    // Tell the arranger UI to auto-open the schedule dialog on the
    // newly-minted assignment. Survives the redirect via sessionStorage;
    // arrange.html clears it on consume so it fires once.
    try {
      const body = await r.json();
      if (body.assignment_id) {
        sessionStorage.setItem('zv-just-purchased', String(body.assignment_id));
      }
    } catch { /* no-op */ }
    setTimeout(() => { location.href = '/arrange#adventures'; }, 800);
  } else {
    const t = await r.text();
    result.className = 'err';
    result.textContent = 'Payment failed: ' + t;
    payBtn.disabled = false;
  }
});
cancelBtn.addEventListener('click', () => { location.href = '/arrange#purchases'; });
load();
</script>
</body></html>`

// Ensure json is referenced in case the provider field grows more
// structure in the future.
var _ = json.RawMessage{}
