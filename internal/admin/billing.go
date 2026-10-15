package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/billing"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
)

// Paying for teams (PLATFORM_PRICING.md). Every team that plays an
// organisation's event, and every phone past those a team includes, is
// taken from the organisation's balance (internal/billing). It pays in
// one of two ways, chosen on its Billing page:
//
//   - prepaid: it tops up the balance by card, a bonus on bigger
//     top-ups, and may go a little below zero (the overdraft);
//   - monthly: on the 1st it is billed what brings the balance back to
//     zero — charged to a saved card, retried for a week, or invoiced by
//     hand for organisations a platform admin allows that — and may run
//     up to its credit limit meanwhile.
//
// Store sales pay their teams in the payment (store_fee.go), never here.

// billingRetries is how many times a monthly card bill is tried, a day
// apart, before it is failed and the organisation can't start teams.
const billingRetries = 7

// lowBalanceTeams is how many teams' worth of balance left warns a
// prepaid organisation's admins.
const lowBalanceTeams = 5

type billingPricesDTO struct {
	TeamCents   int64  `json:"team_cents"`
	TeamPhones  int64  `json:"team_phones"`
	PhoneCents  int64  `json:"phone_cents"`
	Currency    string `json:"currency"`
	VATExcluded bool   `json:"vat_excluded"`
}

type billingUseDTO struct {
	EventID int64  `json:"event_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Teams   int    `json:"teams"`
	Phones  int    `json:"phones"`
	Cents   int64  `json:"cents"`
}

type billingBillDTO struct {
	ID            int64      `json:"id"`
	OrgID         int64      `json:"org_id,omitempty"`
	OrgName       string     `json:"org_name,omitempty"`
	Period        string     `json:"period"`
	Method        string     `json:"method"`
	AmountCents   int64      `json:"amount_cents"`
	Currency      string     `json:"currency"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	LastError     string     `json:"last_error,omitempty"`
	InvoiceNumber string     `json:"invoice_number,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	PaidAt        *time.Time `json:"paid_at,omitempty"`
}

type billingTopUpDTO struct {
	AmountCents int64     `json:"amount_cents"`
	BonusCents  int64     `json:"bonus_cents"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
}

// billingDTO is the org's Billing page.
type billingDTO struct {
	Account billing.Account   `json:"account"`
	Prices  billingPricesDTO  `json:"prices"`
	TopUps  []settings.TopUp  `json:"top_ups"`
	Month   billingUseDTO     `json:"month"`
	Events  []billingUseDTO   `json:"events"`
	Bills   []billingBillDTO  `json:"bills"`
	Bought  []billingTopUpDTO `json:"bought"`
	// Payments: a provider takes top-ups; Cards: it can save a card and
	// charge it monthly.
	Payments bool `json:"payments"`
	Cards    bool `json:"cards"`
}

// billingOrg is the caller's organisation, for its admins.
func billingOrg(w http.ResponseWriter, r *http.Request) (auth.User, int64, bool) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins see its billing")
		return me, 0, false
	}
	return me, *me.OrgID, true
}

// GetMyBilling is the org's balance, how it pays, what it used this
// month and its bills.
//
// GET /api/admin/orgs/me/billing
func (h *Handlers) GetMyBilling(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := billingOrg(w, r)
	if !ok {
		return
	}
	h.writeBilling(w, r, orgID)
}

func (h *Handlers) writeBilling(w http.ResponseWriter, r *http.Request, orgID int64) {
	ctx := r.Context()
	p := h.pricing()
	out := billingDTO{
		Prices:   billingPricesDTO{TeamCents: p.TeamCents, TeamPhones: p.TeamPhones, PhoneCents: p.PhoneCents, Currency: strings.ToUpper(settings.Currency), VATExcluded: p.VATExcluded},
		TopUps:   p.TopUps,
		Events:   []billingUseDTO{},
		Bills:    []billingBillDTO{},
		Bought:   []billingTopUpDTO{},
		Payments: h.paymentsEnabled(),
	}
	_, out.Cards = h.Provider.(payments.CardBiller)
	var err error
	if out.Account, err = billing.Load(ctx, h.DB, orgID, p); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
		return
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT b.event_id, COALESCE(NULLIF(e.name, ''), e.party_name, ''),
		       COUNT(*) FILTER (WHERE b.kind = 'team'), COUNT(*) FILTER (WHERE b.kind = 'phone'), -SUM(b.amount_cents)
		FROM billing_entries b LEFT JOIN events e ON e.id = b.event_id
		WHERE b.org_id = $1 AND b.kind IN ('team', 'phone') AND b.created_at >= date_trunc('month', now())
		GROUP BY b.event_id, e.name, e.party_name ORDER BY MAX(b.created_at) DESC
	`, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			u  billingUseDTO
			id sql.NullInt64
		)
		if err := rows.Scan(&id, &u.Name, &u.Teams, &u.Phones, &u.Cents); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
			return
		}
		u.EventID = id.Int64
		out.Month.Teams += u.Teams
		out.Month.Phones += u.Phones
		out.Month.Cents += u.Cents
		out.Events = append(out.Events, u)
	}
	if out.Bills, err = h.bills(ctx, `b.org_id = $1`, orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
		return
	}
	trows, err := h.DB.QueryContext(ctx, `
		SELECT amount_cents, bonus_cents, state, created_at FROM billing_top_ups
		WHERE org_id = $1 AND state <> 'pending' ORDER BY created_at DESC LIMIT 12
	`, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
		return
	}
	defer trows.Close()
	for trows.Next() {
		var t billingTopUpDTO
		if err := trows.Scan(&t.AmountCents, &t.BonusCents, &t.State, &t.CreatedAt); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
			return
		}
		out.Bought = append(out.Bought, t)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

// bills lists bills matching cond (b is the bill, o its organisation),
// newest first.
func (h *Handlers) bills(ctx context.Context, cond string, args ...any) ([]billingBillDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT b.id, b.org_id, o.name, b.period, b.method, b.amount_cents, b.currency, b.state, b.attempts,
		       b.last_error, b.invoice_number, b.created_at, b.paid_at
		FROM billing_bills b JOIN organizations o ON o.id = b.org_id
		WHERE `+cond+` ORDER BY b.created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []billingBillDTO{}
	for rows.Next() {
		var (
			b    billingBillDTO
			paid sql.NullTime
		)
		if err := rows.Scan(&b.ID, &b.OrgID, &b.OrgName, &b.Period, &b.Method, &b.AmountCents, &b.Currency, &b.State,
			&b.Attempts, &b.LastError, &b.InvoiceNumber, &b.CreatedAt, &paid); err != nil {
			return nil, err
		}
		if paid.Valid {
			b.PaidAt = &paid.Time
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetMyBilling chooses how the org pays: {"mode": "prepaid"} or
// {"mode": "monthly"}. Monthly needs a saved card, or invoice billing.
//
// PUT /api/admin/orgs/me/billing
func (h *Handlers) SetMyBilling(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := billingOrg(w, r)
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Mode != "prepaid" && req.Mode != "monthly" {
		httpx.Error(w, http.StatusBadRequest, "mode is prepaid or monthly")
		return
	}
	if req.Mode == "monthly" {
		a, err := billing.Load(r.Context(), h.DB, orgID, h.pricing())
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
			return
		}
		if !a.Billable() {
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": "save a card first", "code": "card_required"})
			return
		}
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE organizations SET billing_mode = $2 WHERE id = $1`, orgID, req.Mode); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.writeBilling(w, r, orgID)
}

// billingURL is the org's Billing page in the console.
func (h *Handlers) billingURL(query string) string {
	return strings.TrimRight(strings.TrimSpace(h.PublicURL), "/") + "/app/arrange/" + query + "#/settings/billing"
}

// TopUpMyBalance opens the checkout for a top-up: {"amount_cents":
// 200000}, one of the top-ups on offer, its bonus added once paid.
//
// POST /api/admin/orgs/me/billing/top-up
func (h *Handlers) TopUpMyBalance(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := billingOrg(w, r)
	if !ok {
		return
	}
	var req struct {
		AmountCents int64 `json:"amount_cents"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	i := slices.IndexFunc(h.pricing().TopUps, func(t settings.TopUp) bool { return t.AmountCents == req.AmountCents })
	if i < 0 {
		httpx.Error(w, http.StatusBadRequest, "choose one of the top-ups on offer")
		return
	}
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "payments are not set up on this server")
		return
	}
	top := h.pricing().TopUps[i]
	ctx := r.Context()
	currency := strings.ToUpper(settings.Currency)
	var (
		id       int64
		customer sql.NullString
	)
	if err := h.DB.QueryRowContext(ctx, `
		INSERT INTO billing_top_ups(org_id, buyer_user_id, amount_cents, bonus_cents, currency, provider)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, (SELECT stripe_customer_id FROM organizations WHERE id = $1)
	`, orgID, me.ID, top.AmountCents, top.BonusCents, currency, h.Provider.Name()).Scan(&id, &customer); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "top-up failed", err)
		return
	}
	lang := emailtpl.LangFromRequest(r)
	product := emailtpl.T(lang, "Balance top-up, %s", emailtpl.Money(lang, top.AmountCents, currency))
	if top.BonusCents > 0 {
		product = emailtpl.T(lang, "Balance top-up, %[1]s, with %[2]s bonus", emailtpl.Money(lang, top.AmountCents, currency), emailtpl.Money(lang, top.BonusCents, currency))
	}
	params := payments.CheckoutParams{
		ProductName: product, AmountCents: top.AmountCents, Currency: currency,
		SuccessURL: h.billingURL("?topped_up=1"), CancelURL: h.billingURL(""),
		CustomerEmail: me.Email, CustomerID: customer.String,
		Metadata:       map[string]string{"zv_top_up_id": strconv.FormatInt(id, 10)},
		IdempotencyKey: fmt.Sprintf("checkout-top-up-%d", id),
		CollectTaxID:   true, InvoiceCreation: true,
	}
	if customer.String != "" {
		params.CustomerEmail = ""
	}
	sess, err := h.Provider.CreateCheckout(ctx, params)
	if err != nil {
		_, _ = h.DB.ExecContext(ctx, `UPDATE billing_top_ups SET state = 'failed' WHERE id = $1`, id)
		httpx.ServerErr(w, http.StatusBadGateway, "checkout create failed", err)
		return
	}
	if _, err := h.DB.ExecContext(ctx, `UPDATE billing_top_ups SET provider_session_id = $2 WHERE id = $1`, id, sess.SessionID); err != nil {
		logx.Warn("admin.billing.top_up.session", "top_up_id", id, "err", err.Error())
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"checkout_url": sess.URL})
}

// handleTopUpCompleted adds a paid top-up, and its bonus, to the org's
// balance: once, however often the webhook comes.
func (h *Handlers) handleTopUpCompleted(ctx context.Context, event payments.Event) error {
	idStr := event.Metadata["zv_top_up_id"]
	if event.Checkout == nil || idStr == "" || !event.Checkout.PaymentStatus.FulfilmentReady() {
		return nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("metadata parse: %w", err)
	}
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		orgID           int64
		amount, bonus   int64
		currency, state string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT org_id, amount_cents, bonus_cents, currency, state FROM billing_top_ups WHERE id = $1 FOR UPDATE
	`, id).Scan(&orgID, &amount, &bonus, &currency, &state)
	if errors.Is(err, sql.ErrNoRows) {
		logx.Warn("payments.top_up.unknown", "top_up_id", id)
		return nil
	}
	if err != nil || state == "paid" {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE billing_top_ups SET state = 'paid', paid_at = now(), provider_session_id = $2 WHERE id = $1
	`, id, event.Checkout.SessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO billing_entries(org_id, kind, amount_cents, currency, top_up_id) VALUES ($1, 'top_up', $2, $3, $4)
	`, orgID, amount, currency, id); err != nil {
		return err
	}
	if bonus > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO billing_entries(org_id, kind, amount_cents, currency, top_up_id) VALUES ($1, 'bonus', $2, $3, $4)
		`, orgID, bonus, currency, id); err != nil {
			return err
		}
	}
	if event.Checkout.CustomerID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE organizations SET stripe_customer_id = COALESCE(stripe_customer_id, $2) WHERE id = $1
		`, orgID, event.Checkout.CustomerID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	h.clearBalanceAlert(ctx, orgID)
	logx.Info("admin.billing.top_up.paid", "top_up_id", id, "org_id", orgID, "amount_cents", amount, "bonus_cents", bonus)
	return nil
}

// SaveMyCard opens the hosted page that saves the org's card for
// monthly billing.
//
// POST /api/admin/orgs/me/billing/card
func (h *Handlers) SaveMyCard(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := billingOrg(w, r)
	if !ok {
		return
	}
	cb, ok := h.Provider.(payments.CardBiller)
	if !ok {
		httpx.Error(w, http.StatusServiceUnavailable, "monthly billing by card is not set up on this server")
		return
	}
	ctx := r.Context()
	var customer sql.NullString
	if err := h.DB.QueryRowContext(ctx, `SELECT stripe_customer_id FROM organizations WHERE id = $1`, orgID).Scan(&customer); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "billing lookup failed", err)
		return
	}
	sess, err := cb.CreateCardSetup(ctx, payments.CardSetupParams{
		CustomerID: customer.String, Email: me.Email, Currency: settings.Currency,
		SuccessURL: h.billingURL("?card=saved"), CancelURL: h.billingURL(""),
		Metadata: map[string]string{"zv_card_org": strconv.FormatInt(orgID, 10)},
	})
	if err != nil {
		httpx.ServerErr(w, http.StatusBadGateway, "card setup failed", err)
		return
	}
	if sess.CustomerID != "" && !customer.Valid {
		if _, err := h.DB.ExecContext(ctx, `UPDATE organizations SET stripe_customer_id = $2 WHERE id = $1 AND stripe_customer_id IS NULL`, orgID, sess.CustomerID); err != nil {
			logx.Warn("admin.billing.customer", "org_id", orgID, "err", err.Error())
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"checkout_url": sess.URL})
}

// handleCardSaved records the card an org saved, then charges its bills
// still waiting.
func (h *Handlers) handleCardSaved(ctx context.Context, event payments.Event) error {
	orgID, err := strconv.ParseInt(event.Metadata["zv_card_org"], 10, 64)
	if err != nil || event.Card == nil {
		return nil
	}
	if _, err := h.DB.ExecContext(ctx, `
		UPDATE organizations SET billing_payment_method = $2, billing_card_label = $3,
		       stripe_customer_id = COALESCE(NULLIF($4, ''), stripe_customer_id)
		WHERE id = $1
	`, orgID, event.Card.PaymentMethodID, event.Card.Label, event.Card.CustomerID); err != nil {
		return err
	}
	if _, err := h.DB.ExecContext(ctx, `
		UPDATE billing_bills SET method = 'card', next_attempt_at = now(), attempts = 0,
		       state = CASE WHEN state = 'failed' THEN 'open' ELSE state END
		WHERE org_id = $1 AND state IN ('open', 'failed') AND method = 'card'
	`, orgID); err != nil {
		return err
	}
	h.chargeDueBills(ctx, orgID)
	return nil
}

// RemoveMyCard forgets the org's saved card. A monthly org without one
// (or invoice billing) is back to the overdraft until it saves another.
//
// DELETE /api/admin/orgs/me/billing/card
func (h *Handlers) RemoveMyCard(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := billingOrg(w, r)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE organizations SET billing_payment_method = '', billing_card_label = '' WHERE id = $1
	`, orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.writeBilling(w, r, orgID)
}

// PayMyBills charges the org's card for its unpaid card bills now.
//
// POST /api/admin/orgs/me/billing/pay
func (h *Handlers) PayMyBills(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := billingOrg(w, r)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE billing_bills SET next_attempt_at = now(), state = 'open'
		WHERE org_id = $1 AND method = 'card' AND state IN ('open', 'failed')
	`, orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "pay failed", err)
		return
	}
	h.chargeDueBills(r.Context(), orgID)
	h.writeBilling(w, r, orgID)
}

/* ---------------- monthly bills ---------------- */

// SweepBilling bills monthly orgs on the 1st, charges card bills due,
// and warns prepaid orgs running low. Run every hour or so.
func (h *Handlers) SweepBilling(ctx context.Context) {
	h.billLastMonth(ctx, time.Now())
	h.chargeDueBills(ctx, 0)
	h.alertLowBalances(ctx)
}

// billLastMonth bills every monthly org what its balance was below zero
// at the start of now's month, less its bills still unpaid: one bill a
// month, by card when it has one, else by invoice.
func (h *Handlers) billLastMonth(ctx context.Context, now time.Time) {
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	period := cutoff.AddDate(0, -1, 0).Format("2006-01")
	res, err := h.DB.ExecContext(ctx, `
		INSERT INTO billing_bills(org_id, period, method, amount_cents, currency, next_attempt_at)
		SELECT o.id, $2, CASE WHEN o.billing_payment_method <> '' THEN 'card' ELSE 'invoice' END, d.due, $3,
		       CASE WHEN o.billing_payment_method <> '' THEN now() END
		FROM organizations o
		CROSS JOIN LATERAL (
		  SELECT -COALESCE((SELECT SUM(amount_cents) FROM billing_entries WHERE org_id = o.id AND created_at < $1), 0)
		         - COALESCE((SELECT SUM(amount_cents) FROM billing_bills WHERE org_id = o.id AND state IN ('open', 'failed')), 0) AS due
		) d
		WHERE o.billing_mode = 'monthly' AND (o.billing_payment_method <> '' OR o.trusted <> 0) AND d.due > 0
		ON CONFLICT (org_id, period) DO NOTHING
	`, cutoff, period, strings.ToUpper(settings.Currency))
	if err != nil {
		logx.Error("admin.billing.bill", "period", period, "err", err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logx.Info("admin.billing.billed", "period", period, "bills", n)
	}
}

// chargeDueBills charges the card bills due now: of orgID, or of every
// org when it is 0. A paid bill is added to the balance; a declined one
// is tried again a day later, and failed after billingRetries tries.
func (h *Handlers) chargeDueBills(ctx context.Context, orgID int64) {
	cb, ok := h.Provider.(payments.CardBiller)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT b.id, b.org_id, b.period, b.amount_cents, b.currency, b.attempts,
		       COALESCE(o.stripe_customer_id, ''), o.billing_payment_method
		FROM billing_bills b JOIN organizations o ON o.id = b.org_id
		WHERE b.state = 'open' AND b.method = 'card' AND b.next_attempt_at <= now() AND ($1 = 0 OR b.org_id = $1)
		ORDER BY b.id LIMIT 100
	`, orgID)
	if err != nil {
		logx.Error("admin.billing.due", "err", err.Error())
		return
	}
	type due struct {
		id, orgID, amount int64
		period, currency  string
		attempts          int
		customer, card    string
	}
	var bills []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.orgID, &d.period, &d.amount, &d.currency, &d.attempts, &d.customer, &d.card); err != nil {
			rows.Close()
			logx.Error("admin.billing.due", "err", err.Error())
			return
		}
		bills = append(bills, d)
	}
	rows.Close()
	for _, d := range bills {
		if d.card == "" {
			continue
		}
		res, err := cb.ChargeCard(ctx, payments.CardChargeParams{
			CustomerID: d.customer, PaymentMethodID: d.card, AmountCents: d.amount, Currency: d.currency,
			Description:    "Zonventure teams, " + d.period,
			IdempotencyKey: fmt.Sprintf("bill-%d-try-%d", d.id, d.attempts+1),
			Metadata:       map[string]string{"zv_bill_id": strconv.FormatInt(d.id, 10)},
		})
		if err != nil {
			res.Failure = err.Error()
		}
		if res.Succeeded {
			h.billPaid(ctx, d.id, res.ID, "")
			continue
		}
		final := d.attempts+1 >= billingRetries
		state := "open"
		if final {
			state = "failed"
		}
		if _, err := h.DB.ExecContext(ctx, `
			UPDATE billing_bills SET attempts = attempts + 1, last_error = $2, state = $3,
			       next_attempt_at = CASE WHEN $3 = 'open' THEN now() + interval '1 day' END
			WHERE id = $1
		`, d.id, res.Failure, state); err != nil {
			logx.Error("admin.billing.charge_failed", "bill_id", d.id, "err", err.Error())
		}
		logx.Warn("admin.billing.declined", "bill_id", d.id, "org_id", d.orgID, "attempt", d.attempts+1, "reason", res.Failure)
		// The first decline and the last are mailed; the days between
		// aren't.
		if d.attempts == 0 || final {
			h.mailOrgAdmins(ctx, d.orgID, "billing_bill_failed", func(lang string) (string, emailtpl.Content) {
				return emailtpl.BillFailed(lang, d.period, emailtpl.Money(lang, d.amount, d.currency), res.Failure, final, h.billingURL(""))
			})
		}
	}
}

// billPaid marks a bill paid, with the charge or the invoice that paid
// it, and puts its amount back on the org's balance: once.
func (h *Handlers) billPaid(ctx context.Context, billID int64, paymentIntent, invoice string) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		logx.Error("admin.billing.paid", "bill_id", billID, "err", err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		orgID, amount int64
		currency      string
	)
	err = tx.QueryRowContext(ctx, `
		UPDATE billing_bills SET state = 'paid', paid_at = now(), next_attempt_at = NULL, last_error = '',
		       stripe_payment_intent = $2, invoice_number = COALESCE(NULLIF($3, ''), invoice_number)
		WHERE id = $1 AND state <> 'paid' RETURNING org_id, amount_cents, currency
	`, billID, paymentIntent, invoice).Scan(&orgID, &amount, &currency)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO billing_entries(org_id, kind, amount_cents, currency, bill_id) VALUES ($1, 'payment', $2, $3, $4)
		`, orgID, amount, currency, billID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		logx.Error("admin.billing.paid", "bill_id", billID, "err", err.Error())
		return
	}
	logx.Info("admin.billing.bill_paid", "bill_id", billID, "org_id", orgID, "amount_cents", amount)
}

// alertLowBalances mails a prepaid org's admins once when its balance
// has room for few more teams, and once when it is used up; a top-up
// past them clears the warning.
func (h *Handlers) alertLowBalances(ctx context.Context) {
	p := h.pricing()
	low := lowBalanceTeams * p.TeamCents
	if low <= 0 {
		return
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT o.id, o.billing_alert, COALESCE(SUM(b.amount_cents), 0)
		FROM organizations o JOIN billing_entries b ON b.org_id = o.id
		WHERE o.billing_mode = 'prepaid'
		GROUP BY o.id, o.billing_alert
	`)
	if err != nil {
		logx.Error("admin.billing.alerts", "err", err.Error())
		return
	}
	type org struct {
		id      int64
		alert   string
		balance int64
	}
	var orgs []org
	for rows.Next() {
		var o org
		if err := rows.Scan(&o.id, &o.alert, &o.balance); err == nil {
			orgs = append(orgs, o)
		}
	}
	rows.Close()
	currency := strings.ToUpper(settings.Currency)
	for _, o := range orgs {
		want := ""
		switch {
		case o.balance <= 0:
			want = "empty"
		case o.balance <= low:
			want = "low"
		}
		if want == o.alert || (want == "low" && o.alert == "empty") {
			continue
		}
		if _, err := h.DB.ExecContext(ctx, `UPDATE organizations SET billing_alert = $2 WHERE id = $1`, o.id, want); err != nil {
			logx.Error("admin.billing.alert", "org_id", o.id, "err", err.Error())
			continue
		}
		balance := o.balance
		switch want {
		case "low":
			h.mailOrgAdmins(ctx, o.id, "billing_balance_low", func(lang string) (string, emailtpl.Content) {
				return emailtpl.BalanceLow(lang, emailtpl.Money(lang, balance, currency), balance/p.TeamCents, h.billingURL(""))
			})
		case "empty":
			h.mailOrgAdmins(ctx, o.id, "billing_balance_empty", func(lang string) (string, emailtpl.Content) {
				return emailtpl.BalanceEmpty(lang, emailtpl.Money(lang, balance, currency), h.billingURL(""))
			})
		}
	}
}

// clearBalanceAlert forgets the last balance warning once the org has
// topped up past it.
func (h *Handlers) clearBalanceAlert(ctx context.Context, orgID int64) {
	low := lowBalanceTeams * h.pricing().TeamCents
	if _, err := h.DB.ExecContext(ctx, `
		UPDATE organizations SET billing_alert = '' WHERE id = $1
		  AND (SELECT COALESCE(SUM(amount_cents), 0) FROM billing_entries WHERE org_id = $1) > $2
	`, orgID, low); err != nil {
		logx.Warn("admin.billing.alert_clear", "org_id", orgID, "err", err.Error())
	}
}

// mailOrgAdmins mails an org's admins, in the org's look and language.
func (h *Handlers) mailOrgAdmins(ctx context.Context, orgID int64, kind string, build func(lang string) (string, emailtpl.Content)) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT u.email FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role = 'org_admin' AND COALESCE(u.email, '') <> ''
	`, orgID)
	if err != nil {
		logx.Warn("admin.billing.mail", "kind", kind, "org_id", orgID, "err", err.Error())
		return
	}
	var to []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			to = append(to, e)
		}
	}
	rows.Close()
	if len(to) == 0 {
		return
	}
	l := h.orgLook(ctx, orgID)
	subject, c := build(l.Lang)
	if err := h.send(ctx, kind, l.message(to, subject, c)); err != nil {
		logx.Warn("admin.billing.mail", "kind", kind, "org_id", orgID, "err", err.Error())
	}
}

/* ---------------- platform admin ---------------- */

// ListBills is every org's bills, for the platform admin: the invoices
// to send and mark paid, the cards that failed.
//
// GET /api/admin/billing/bills?state=open
func (h *Handlers) ListBills(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state != "" && !slices.Contains([]string{"open", "paid", "failed", "void"}, state) {
		state = ""
	}
	out, err := h.bills(r.Context(), `($1 = '' OR b.state = $1)`, state)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "bills lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"bills": out})
}

// MarkBillPaid marks an invoice bill paid: {"invoice_number": "…"}.
//
// POST /api/admin/billing/bills/{billID}/paid
func (h *Handlers) MarkBillPaid(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "billID"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such bill")
		return
	}
	var req struct {
		InvoiceNumber string `json:"invoice_number"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	h.billPaid(r.Context(), id, "", strings.TrimSpace(req.InvoiceNumber))
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GetOrgBilling is an org's billing as its admins see it, for the
// platform admin.
//
// GET /api/admin/organizations/{orgID}/billing
func (h *Handlers) GetOrgBilling(w http.ResponseWriter, r *http.Request) {
	orgID, err := strconv.ParseInt(chi.URLParam(r, "orgID"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such organisation")
		return
	}
	h.writeBilling(w, r, orgID)
}

// SetOrgBilling is a platform admin's say over an org's billing: its
// credit limit ({"credit_limit_cents": 100000}, null for the platform's)
// and an adjustment to its balance ({"adjust_cents": 50000, "note":
// "…"}), a credit or a charge.
//
// PUT /api/admin/organizations/{orgID}/billing
func (h *Handlers) SetOrgBilling(w http.ResponseWriter, r *http.Request) {
	orgID, err := strconv.ParseInt(chi.URLParam(r, "orgID"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such organisation")
		return
	}
	var req struct {
		CreditLimitCents *int64 `json:"credit_limit_cents"`
		ClearCreditLimit bool   `json:"clear_credit_limit"`
		AdjustCents      int64  `json:"adjust_cents"`
		Note             string `json:"note"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	me, _ := auth.FromContext(r.Context())
	ctx := r.Context()
	if req.CreditLimitCents != nil || req.ClearCreditLimit {
		if req.CreditLimitCents != nil && *req.CreditLimitCents < 0 {
			httpx.Error(w, http.StatusBadRequest, "a credit limit is zero or more")
			return
		}
		if _, err := h.DB.ExecContext(ctx, `UPDATE organizations SET credit_limit_cents = $2 WHERE id = $1`, orgID, req.CreditLimitCents); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
			return
		}
	}
	if req.AdjustCents != 0 {
		if strings.TrimSpace(req.Note) == "" {
			httpx.Error(w, http.StatusBadRequest, "say why in a note")
			return
		}
		if _, err := h.DB.ExecContext(ctx, `
			INSERT INTO billing_entries(org_id, kind, amount_cents, currency, note, created_by) VALUES ($1, 'adjustment', $2, $3, $4, $5)
		`, orgID, req.AdjustCents, strings.ToUpper(settings.Currency), strings.TrimSpace(req.Note), me.ID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
			return
		}
		h.clearBalanceAlert(ctx, orgID)
	}
	logx.Info("admin.billing.org", "org_id", orgID, "by", me.Username, "credit_limit", req.CreditLimitCents, "adjust_cents", req.AdjustCents)
	h.writeBilling(w, r, orgID)
}
