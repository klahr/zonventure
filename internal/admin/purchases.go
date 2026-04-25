package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Purchases ----------------
 *
 * Arrangers buy team-slot assignments here. The flow is deliberately
 * Stripe-shaped — it's three endpoints:
 *
 *   POST /api/admin/purchases               → create pending (returns checkout_url)
 *   GET  /purchase/{id}                     → simulated checkout HTML
 *   POST /api/admin/purchases/{id}/confirm  → mark paid, create assignment
 *
 * The simulated provider confirms via an in-app click-through; a real
 * Stripe integration points Checkout at /purchase/{id}, which would in
 * turn redirect to a Checkout Session, with the webhook eventually
 * calling the same /confirm endpoint.
 *
 * The persisted row holds provider + provider_session_id so mixed
 * provider histories can coexist once live payments ship.
 */

// PricePerSlotCents is the hardcoded price. Living as a constant makes
// the eventual Stripe migration a single find-and-replace: swap for a
// Stripe Price ID + a server-side catalog row.
const PricePerSlotCents = 200

type purchaseDTO struct {
	ID                int64  `json:"id"`
	OrgID             int64  `json:"org_id"`
	AdventureSlug     string `json:"adventure_slug"`
	AdventureTitle    string `json:"adventure_title,omitempty"`
	MaxTeams          int    `json:"max_teams"`
	AmountCents       int    `json:"amount_cents"`
	Currency          string `json:"currency"`
	Provider          string `json:"provider"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	State             string `json:"state"`
	AssignmentID      *int64 `json:"assignment_id,omitempty"`
	CheckoutURL       string `json:"checkout_url,omitempty"`
	InvoiceURL        string `json:"invoice_url,omitempty"`
	CreatedAt         string `json:"created_at"`
	PaidAt            string `json:"paid_at,omitempty"`
}

type createPurchaseRequest struct {
	AdventureSlug string `json:"adventure_slug"`
	MaxTeams      int    `json:"max_teams"`
	// Provider lets callers opt into a specific flow. Empty defaults
	// to "simulated"; "stripe" is reserved for the live integration.
	Provider string `json:"provider,omitempty"`
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
	if req.MaxTeams <= 0 || req.MaxTeams > 100 {
		httpx.Error(w, http.StatusBadRequest, "max_teams must be 1–100")
		return
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
	provider := req.Provider
	if provider == "" {
		// Default to stripe when configured so production deployments
		// don't accidentally serve the simulated flow; fall back to
		// simulated otherwise so a fresh dev clone still works.
		if h.stripeEnabled() {
			provider = "stripe"
		} else {
			provider = "simulated"
		}
	}
	if provider != "simulated" && provider != "stripe" {
		httpx.Error(w, http.StatusBadRequest, "unsupported provider")
		return
	}
	if provider == "stripe" && !h.stripeEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "stripe is not configured on this server")
		return
	}
	amount := req.MaxTeams * PricePerSlotCents

	// Look up the adventure title up-front so we can pass a friendly
	// product name to Stripe Checkout instead of the bare slug.
	var advTitle string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT title FROM adventures WHERE slug = $1`, req.AdventureSlug).Scan(&advTitle)

	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_teams,
		                      amount_cents, currency, provider, state)
		VALUES ($1, $2, $3, $4, $5, 'USD', $6, 'pending')
		RETURNING id
	`, *me.OrgID, me.ID, req.AdventureSlug, req.MaxTeams, amount, provider).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}

	// Build the URL the browser navigates to. Simulated mode keeps
	// pointing at our in-app /purchase/{id} fake checkout; Stripe
	// mode creates a Checkout Session and uses its hosted URL.
	checkoutURL := fmt.Sprintf("/purchase/%d", id)
	if provider == "stripe" {
		url, sessID, serr := h.createStripeCheckoutSession(r.Context(),
			id, req.AdventureSlug, advTitle, req.MaxTeams, amount, me.Email)
		if serr != nil {
			// Roll back the pending row so we don't leave dangling
			// pendings every time Stripe is down.
			_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM purchases WHERE id = $1`, id)
			httpx.Error(w, http.StatusBadGateway, "stripe checkout failed: "+serr.Error())
			return
		}
		// Stash the session id so the webhook can correlate.
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE purchases SET provider_session_id = $1 WHERE id = $2`, sessID, id)
		checkoutURL = url
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id":           id,
		"amount_cents": amount,
		"currency":     "USD",
		"state":        "pending",
		"provider":     provider,
		"checkout_url": checkoutURL,
	})
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
		maxTeams      int
		state         string
		existingAssn  sql.NullInt64
	)
	// FOR UPDATE serialises overlapping confirms (double-clicked Pay
	// button, webhook arriving alongside a manual confirm) so two
	// transactions can't both observe state='pending' and each insert
	// their own assignment row.
	err = tx.QueryRowContext(r.Context(), `
		SELECT org_id, adventure_slug, max_teams, state, assignment_id
		FROM purchases WHERE id = $1 FOR UPDATE
	`, purchaseID).Scan(&orgID, &advSlug, &maxTeams, &state, &existingAssn)
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
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgID, advSlug, maxTeams).Scan(&assID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assignment insert failed: "+err.Error())
		return
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
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id": purchaseID, "state": "paid", "assignment_id": assID,
	})
}

// ListPurchases returns every purchase visible to the caller — admins
// see all, org users see their own.
func (h *Handlers) ListPurchases(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT p.id, p.org_id, p.adventure_slug, COALESCE(a.title, ''),
		       p.max_teams, p.amount_cents, p.currency, p.provider,
		       COALESCE(p.provider_session_id, ''), p.state, p.assignment_id,
		       p.created_at, COALESCE(p.paid_at, ''),
		       COALESCE(p.stripe_invoice_url, '')
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
		)
		if err := rows.Scan(&d.ID, &d.OrgID, &d.AdventureSlug, &d.AdventureTitle,
			&d.MaxTeams, &d.AmountCents, &d.Currency, &d.Provider,
			&d.ProviderSessionID, &d.State, &assignment,
			&d.CreatedAt, &d.PaidAt, &d.InvoiceURL); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if assignment.Valid {
			v := assignment.Int64
			d.AssignmentID = &v
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
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.id, p.org_id, p.adventure_slug, COALESCE(a.title, ''),
		       p.max_teams, p.amount_cents, p.currency, p.provider,
		       COALESCE(p.provider_session_id, ''), p.state, p.assignment_id,
		       p.created_at, COALESCE(p.paid_at, ''),
		       COALESCE(p.stripe_invoice_url, '')
		FROM purchases p
		LEFT JOIN adventures a ON a.slug = p.adventure_slug
		WHERE p.id = $1
	`, purchaseID).Scan(&d.ID, &d.OrgID, &d.AdventureSlug, &d.AdventureTitle,
		&d.MaxTeams, &d.AmountCents, &d.Currency, &d.Provider,
		&d.ProviderSessionID, &d.State, &assignment, &d.CreatedAt, &d.PaidAt, &d.InvoiceURL)
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
  return new Intl.NumberFormat(undefined, { style:'currency', currency: currency || 'USD' }).format(cents / 100);
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
    '<dt>Team slots</dt><dd>' + p.max_teams + '</dd>' +
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
