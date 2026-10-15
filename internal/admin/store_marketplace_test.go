package admin_test

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
)

// marketProvider is a payment provider that connects organisations'
// own accounts, recording what it is asked to do.
type marketProvider struct {
	fakeProvider
	mu         sync.Mutex
	checkouts  []payments.CheckoutParams
	refunds    []payments.RefundParams
	feeRefunds []string
}

func (m *marketProvider) CreateCheckout(_ context.Context, p payments.CheckoutParams) (payments.CheckoutSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkouts = append(m.checkouts, p)
	return payments.CheckoutSession{URL: "https://fake/checkout", SessionID: "cs_market"}, nil
}
func (m *marketProvider) Refund(_ context.Context, p payments.RefundParams) (payments.Refund, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refunds = append(m.refunds, p)
	return payments.Refund{ID: "re_market", Status: "succeeded"}, nil
}
func (m *marketProvider) CreateConnectedAccount(context.Context, payments.ConnectedAccountParams) (string, error) {
	return "acct_new", nil
}
func (m *marketProvider) CreateAccountLink(context.Context, payments.AccountLinkParams) (string, error) {
	return "https://fake/onboarding", nil
}
func (m *marketProvider) GetConnectedAccount(_ context.Context, id string) (payments.ConnectedAccount, error) {
	return payments.ConnectedAccount{ID: id, ChargesEnabled: true, PayoutsEnabled: true}, nil
}
func (m *marketProvider) RefundApplicationFee(_ context.Context, account, chargeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feeRefunds = append(m.feeRefunds, account+"/"+chargeID)
	return nil
}

type marketFixture struct {
	db      *sql.DB
	h       *admin.Handlers
	pay     *marketProvider
	orgID   int64
	eventID int64
	public  http.Handler
}

func newMarketFixture(t *testing.T, price, teamSize int) (*marketFixture, func()) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	f := &marketFixture{db: db, pay: &marketProvider{}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.QueryRow(`
		INSERT INTO organizations(name, slug, stripe_connect_account_id, stripe_connect_charges_enabled)
		VALUES ('Acme', 'acme', 'acct_acme', TRUE) RETURNING id`).Scan(&f.orgID))
	_, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, f.orgID)
	must(err)
	must(db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name,
		                   public_for_sale, ticket_price_cents, ticket_currency, ticket_team_size, public_blurb)
		VALUES ('demo', $1, 'running', 'lt-market', 'Walk', TRUE, $2, 'sek', $3, 'A walk')
		RETURNING id`, f.orgID, price, teamSize).Scan(&f.eventID))
	dbtest.ApproveListings(t, db)
	f.h = &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: &fakeMailer{}, PublicURL: "https://zv.example.com",
		Provider: f.pay,
		Pricing: func() settings.Pricing {
			return settings.Pricing{TeamCents: 4900, TeamPhones: 3, PhoneCents: 1500}
		}}
	r := chi.NewRouter()
	r.Post("/api/o/{slug}/store/buy", f.h.BuyTicketByOrg)
	f.public = r
	return f, cleanup
}

// TestStoreSale_DirectChargeWithTeamFee: a store sale is charged on the
// org's own account, the platform's price for its team (and the phones
// past those a team includes) its application fee.
func TestStoreSale_DirectChargeWithTeamFee(t *testing.T) {
	t.Parallel()
	f, cleanup := newMarketFixture(t, 49900, 5)
	defer cleanup()
	w := call(t, f.public, "POST", "/api/o/acme/store/buy", map[string]any{"event_id": f.eventID, "buyer_email": "kim@example.com"})
	if w.Code != http.StatusOK {
		t.Fatalf("buy: %d %s", w.Code, w.Body)
	}
	if len(f.pay.checkouts) != 1 {
		t.Fatalf("%d checkouts", len(f.pay.checkouts))
	}
	c := f.pay.checkouts[0]
	if c.ConnectedAccountID != "acct_acme" || c.ApplicationFeeCents != 4900+2*1500 {
		t.Errorf("checkout on %q with fee %d, want the org's account and 79 kr", c.ConnectedAccountID, c.ApplicationFeeCents)
	}
}

// TestStoreSale_BelowTheFee: a ticket that costs no more than its team
// can't be sold.
func TestStoreSale_BelowTheFee(t *testing.T) {
	t.Parallel()
	f, cleanup := newMarketFixture(t, 4900, 3)
	defer cleanup()
	w := call(t, f.public, "POST", "/api/o/acme/store/buy", map[string]any{"event_id": f.eventID, "buyer_email": "kim@example.com"})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "price_below_fee") {
		t.Errorf("49 kr ticket: %d %s", w.Code, w.Body)
	}
}

// TestStoreTeam_PaidPhones: a store-bought team costs the org nothing
// for the phones its ticket paid for; one past them is charged.
func TestStoreTeam_PaidPhones(t *testing.T) {
	t.Parallel()
	f, cleanup := newMarketFixture(t, 49900, 4)
	defer cleanup()
	var purchaseID int64
	if err := f.db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, stripe_session_id, application_fee_cents)
		VALUES ($1, 'kim@example.com', 4, 49900, 'sek', 'cs_paid', 6400) RETURNING id`, f.eventID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}
	if err := admin.ExportedHandleTicketCheckoutCompleted(f.h, context.Background(), buildSessionCompletedEvent(t, purchaseID, "cs_paid")); err != nil {
		t.Fatalf("fulfil: %v", err)
	}
	var paid int
	if err := f.db.QueryRow(`SELECT t.paid_phones FROM teams t JOIN ticket_purchases p ON p.team_id = t.id WHERE p.id = $1`, purchaseID).Scan(&paid); err != nil {
		t.Fatal(err)
	}
	if paid != 4 {
		t.Errorf("paid phones = %d, want the ticket's 4", paid)
	}
}

// TestStoreRefund_FeeBackOnlyBeforePlay: a refund made here is made on
// the org's account; the platform's fee goes back only when the team
// never played. One the org made in its own dashboard does the same.
func TestStoreRefund_FeeBackOnlyBeforePlay(t *testing.T) {
	t.Parallel()
	f, cleanup := newMarketFixture(t, 49900, 3)
	defer cleanup()
	ctx := context.Background()
	sale := func(session, charge string) (int64, int64) {
		t.Helper()
		var id, team int64
		if err := f.db.QueryRow(`
			INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, stripe_session_id, application_fee_cents)
			VALUES ($1, 'kim@example.com', 3, 49900, 'sek', $2, 4900) RETURNING id`, f.eventID, session).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err := admin.ExportedHandleTicketCheckoutCompleted(f.h, ctx, buildSessionCompletedEvent(t, id, session)); err != nil {
			t.Fatalf("fulfil: %v", err)
		}
		if err := f.db.QueryRow(`UPDATE ticket_purchases SET stripe_charge_id = $2 WHERE id = $1 RETURNING team_id`, id, charge).Scan(&team); err != nil {
			t.Fatal(err)
		}
		return id, team
	}
	_, played := sale("cs_played", "ch_played")
	if _, err := f.db.Exec(`UPDATE player_slots SET claimed_at = now_text() WHERE id = (SELECT MIN(id) FROM player_slots WHERE team_id = $1)`, played); err != nil {
		t.Fatal(err)
	}
	_, _ = sale("cs_unplayed", "ch_unplayed")

	// Refunded by the org in its own Stripe dashboard.
	for _, ch := range []string{"ch_played", "ch_unplayed"} {
		ev := buildChargeRefundedEvent(t, ch)
		ev.Account = "acct_acme"
		if err := admin.ExportedHandleChargeRefunded(f.h, ctx, ev); err != nil {
			t.Fatalf("refund %s: %v", ch, err)
		}
	}
	if got := strings.Join(f.pay.feeRefunds, ","); got != "acct_acme/ch_unplayed" {
		t.Errorf("fees refunded: %q, want only the unplayed sale's", got)
	}
}
