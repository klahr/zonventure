package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
)

// cardProvider takes top-ups and saves and charges cards; decline makes
// every charge fail.
type cardProvider struct {
	fakeProvider
	mu        sync.Mutex
	checkouts []payments.CheckoutParams
	charges   []payments.CardChargeParams
	decline   bool
}

func (c *cardProvider) CreateCheckout(_ context.Context, p payments.CheckoutParams) (payments.CheckoutSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkouts = append(c.checkouts, p)
	return payments.CheckoutSession{URL: "https://fake/checkout", SessionID: "cs_top_up"}, nil
}
func (c *cardProvider) CreateCardSetup(_ context.Context, p payments.CardSetupParams) (payments.CheckoutSession, error) {
	return payments.CheckoutSession{URL: "https://fake/setup", SessionID: "cs_setup", CustomerID: "cus_acme"}, nil
}
func (c *cardProvider) ChargeCard(_ context.Context, p payments.CardChargeParams) (payments.CardCharge, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.charges = append(c.charges, p)
	if c.decline {
		return payments.CardCharge{Failure: "Your card was declined."}, nil
	}
	return payments.CardCharge{ID: "pi_" + strconv.Itoa(len(c.charges)), Succeeded: true}, nil
}

var billingPrices = settings.Pricing{TeamCents: 4900, TeamPhones: 3, PhoneCents: 1500, OverdraftCents: 25000,
	CreditNewCents: 50000, CreditGrownCents: 500000, CreditGrownAfter: 3,
	TopUps: []settings.TopUp{{AmountCents: 50000}, {AmountCents: 200000, BonusCents: 20000}}}

type billingFixture struct {
	db      *sql.DB
	h       *admin.Handlers
	pay     *cardProvider
	mail    *fakeMailer
	orgID   int64
	console http.Handler
}

func newBillingFixture(t *testing.T) (*billingFixture, func()) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	f := &billingFixture{db: db, pay: &cardProvider{}, mail: &fakeMailer{}}
	var userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Acme', 'acme') RETURNING id`).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(org_id, user_id, role) VALUES ($1, $2, 'org_admin')`, f.orgID, userID); err != nil {
		t.Fatal(err)
	}
	f.h = &admin.Handlers{DB: db, Mailer: f.mail, PublicURL: "https://zv.example.com", Provider: f.pay,
		Pricing: func() settings.Pricing { return billingPrices }}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &f.orgID, Email: "boss@acme.com", EmailVerified: true}))
	r.Get("/billing", f.h.GetMyBilling)
	r.Put("/billing", f.h.SetMyBilling)
	r.Post("/billing/top-up", f.h.TopUpMyBalance)
	r.Post("/billing/card", f.h.SaveMyCard)
	f.console = r
	return f, cleanup
}

func (f *billingFixture) balance(t *testing.T) int64 {
	t.Helper()
	var b int64
	if err := f.db.QueryRow(`SELECT COALESCE(SUM(amount_cents), 0) FROM billing_entries WHERE org_id = $1`, f.orgID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *billingFixture) use(t *testing.T, cents int64, ago string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO billing_entries(org_id, kind, amount_cents, currency, created_at) VALUES ($1, 'adjustment', $2, 'SEK', now() - $3::interval)`,
		f.orgID, -cents, ago); err != nil {
		t.Fatal(err)
	}
}

// TestBilling_TopUp: an org tops up one of the amounts on offer; paid,
// the amount and its bonus are on its balance, once.
func TestBilling_TopUp(t *testing.T) {
	t.Parallel()
	f, cleanup := newBillingFixture(t)
	defer cleanup()
	if w := call(t, f.console, "POST", "/billing/top-up", map[string]any{"amount_cents": 12345}); w.Code != http.StatusBadRequest {
		t.Errorf("an amount not on offer: %d", w.Code)
	}
	w := call(t, f.console, "POST", "/billing/top-up", map[string]any{"amount_cents": 200000})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("top up: %d %s", w.Code, w.Body)
	}
	c := f.pay.checkouts[0]
	if c.AmountCents != 200000 || c.ConnectedAccountID != "" || c.Metadata["zv_top_up_id"] == "" {
		t.Errorf("checkout: %+v", c)
	}
	ev := payments.Event{Type: payments.EventCheckoutCompleted, ID: "evt_top_up", Created: time.Now(), Metadata: c.Metadata,
		Checkout: &payments.CheckoutDetail{SessionID: "cs_top_up", AmountTotal: 200000, Currency: "sek", PaymentStatus: payments.PaymentStatusPaid, CustomerID: "cus_acme"}}
	for range 2 {
		if err := admin.ExportedHandleTopUpCompleted(f.h, context.Background(), ev); err != nil {
			t.Fatalf("fulfil: %v", err)
		}
	}
	if got := f.balance(t); got != 220000 {
		t.Errorf("balance = %d, want 2 000 kr and its 200 kr bonus, once", got)
	}
}

// TestBilling_Monthly: monthly needs a card; on the 1st the org is
// billed what last month's teams took, charged to its card.
func TestBilling_Monthly(t *testing.T) {
	t.Parallel()
	f, cleanup := newBillingFixture(t)
	defer cleanup()
	ctx := context.Background()
	if w := call(t, f.console, "PUT", "/billing", map[string]any{"mode": "monthly"}); w.Code != http.StatusConflict {
		t.Errorf("monthly without a card: %d", w.Code)
	}
	w := call(t, f.console, "POST", "/billing/card", map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("card setup: %d %s", w.Code, w.Body)
	}
	saved := payments.Event{Type: payments.EventCardSaved, ID: "evt_card", Created: time.Now(),
		Metadata: map[string]string{"zv_card_org": strconv.FormatInt(f.orgID, 10)},
		Card:     &payments.SavedCard{CustomerID: "cus_acme", PaymentMethodID: "pm_visa", Label: "Visa •••• 4242"}}
	if err := admin.ExportedHandleCardSaved(f.h, ctx, saved); err != nil {
		t.Fatalf("card saved: %v", err)
	}
	if w := call(t, f.console, "PUT", "/billing", map[string]any{"mode": "monthly"}); w.Code != http.StatusOK {
		t.Fatalf("monthly: %d %s", w.Code, w.Body)
	}
	var b struct {
		Account struct {
			Mode             string `json:"mode"`
			CreditLimitCents int64  `json:"credit_limit_cents"`
			Card             string `json:"card"`
		} `json:"account"`
	}
	_ = json.Unmarshal(call(t, f.console, "GET", "/billing", nil).Body.Bytes(), &b)
	if b.Account.Mode != "monthly" || b.Account.CreditLimitCents != 50000 || b.Account.Card != "Visa •••• 4242" {
		t.Errorf("account: %+v", b.Account)
	}

	// Last month's teams, and some of this month's: only last month's
	// are billed.
	now := time.Now()
	f.use(t, 3*4900, "40 days")
	f.use(t, 4900, "0 days")
	admin.ExportedBillMonth(f.h, ctx, now)
	admin.ExportedBillMonth(f.h, ctx, now)
	admin.ExportedChargeDueBills(f.h, ctx)
	var amount int64
	var state string
	if err := f.db.QueryRow(`SELECT amount_cents, state FROM billing_bills WHERE org_id = $1`, f.orgID).Scan(&amount, &state); err != nil {
		t.Fatal(err)
	}
	if amount != 3*4900 || state != "paid" {
		t.Errorf("bill: %d %s, want last month's 3 teams, paid", amount, state)
	}
	if len(f.pay.charges) != 1 || f.pay.charges[0].PaymentMethodID != "pm_visa" {
		t.Errorf("charges: %+v", f.pay.charges)
	}
	if got := f.balance(t); got != -4900 {
		t.Errorf("balance = %d, want only this month's team left", got)
	}
}

// TestBilling_Declined: a declined card is tried again daily, then the
// bill fails: no credit until it is paid. The admins hear of the first
// decline and the last.
func TestBilling_Declined(t *testing.T) {
	t.Parallel()
	f, cleanup := newBillingFixture(t)
	defer cleanup()
	ctx := context.Background()
	f.pay.decline = true
	if _, err := f.db.Exec(`UPDATE organizations SET billing_mode = 'monthly', billing_payment_method = 'pm_visa', billing_card_label = 'Visa' WHERE id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	f.use(t, 9800, "40 days")
	admin.ExportedBillMonth(f.h, ctx, time.Now())
	for range 7 {
		if _, err := f.db.Exec(`UPDATE billing_bills SET next_attempt_at = now() WHERE state = 'open'`); err != nil {
			t.Fatal(err)
		}
		admin.ExportedChargeDueBills(f.h, ctx)
	}
	var state string
	var attempts int
	if err := f.db.QueryRow(`SELECT state, attempts FROM billing_bills`).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || attempts != 7 {
		t.Errorf("bill %s after %d tries", state, attempts)
	}
	if n := len(f.mail.messages); n != 2 {
		t.Errorf("%d mails, want the first decline and the last", n)
	}
	var b struct {
		Account struct {
			FloorCents int64 `json:"floor_cents"`
			Overdue    bool  `json:"overdue"`
		} `json:"account"`
	}
	_ = json.Unmarshal(call(t, f.console, "GET", "/billing", nil).Body.Bytes(), &b)
	if !b.Account.Overdue || b.Account.FloorCents != 0 {
		t.Errorf("overdue account: %+v", b.Account)
	}
	// A new card pays it.
	f.pay.decline = false
	saved := payments.Event{Type: payments.EventCardSaved, ID: "evt_card2", Created: time.Now(),
		Metadata: map[string]string{"zv_card_org": strconv.FormatInt(f.orgID, 10)},
		Card:     &payments.SavedCard{CustomerID: "cus_acme", PaymentMethodID: "pm_new", Label: "Visa"}}
	if err := admin.ExportedHandleCardSaved(f.h, ctx, saved); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT state FROM billing_bills`).Scan(&state); err != nil || state != "paid" {
		t.Errorf("after a new card: %s %v", state, err)
	}
}

// TestBilling_Invoice: an org with invoice billing is billed by invoice,
// which a platform admin marks paid.
func TestBilling_Invoice(t *testing.T) {
	t.Parallel()
	f, cleanup := newBillingFixture(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := f.db.Exec(`UPDATE organizations SET billing_mode = 'monthly', trusted = 1 WHERE id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	f.use(t, 4900, "40 days")
	admin.ExportedBillMonth(f.h, ctx, time.Now())
	admin.ExportedChargeDueBills(f.h, ctx)
	var id int64
	var method, state string
	if err := f.db.QueryRow(`SELECT id, method, state FROM billing_bills`).Scan(&id, &method, &state); err != nil {
		t.Fatal(err)
	}
	if method != "invoice" || state != "open" || len(f.pay.charges) != 0 {
		t.Fatalf("invoice bill: %s %s, %d charges", method, state, len(f.pay.charges))
	}
	r := chi.NewRouter()
	r.Post("/bills/{billID}/paid", f.h.MarkBillPaid)
	if w := call(t, r, "POST", "/bills/"+strconv.FormatInt(id, 10)+"/paid", map[string]any{"invoice_number": "F-2026-17"}); w.Code != http.StatusOK {
		t.Fatalf("mark paid: %d %s", w.Code, w.Body)
	}
	if got := f.balance(t); got != 0 {
		t.Errorf("balance after the invoice = %d", got)
	}
}

// TestBilling_LowBalance: a prepaid org's admins hear once when its
// balance runs low, once when it is used up, and again after a top-up.
func TestBilling_LowBalance(t *testing.T) {
	t.Parallel()
	f, cleanup := newBillingFixture(t)
	defer cleanup()
	ctx := context.Background()
	f.use(t, -30000, "1 day") // 300 kr in
	f.use(t, 10000, "0 days") // 200 kr left: low
	admin.ExportedAlertLowBalances(f.h, ctx)
	admin.ExportedAlertLowBalances(f.h, ctx)
	f.use(t, 20000, "0 days") // used up
	admin.ExportedAlertLowBalances(f.h, ctx)
	if n := len(f.mail.messages); n != 2 || !strings.Contains(f.mail.messages[0].Subject, "running low") || !strings.Contains(f.mail.messages[1].Subject, "used up") {
		t.Errorf("%d mails: %+v", n, f.mail.messages)
	}
}
