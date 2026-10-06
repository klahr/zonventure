package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/payments"
)

// stubProvider implements payments.Provider for the refund-gate
// tests. We need a non-nil provider so the handler doesn't 503
// at the paymentsEnabled() check before ever reaching the
// pre_migration gate; the test is asserting the gate's
// behaviour, not Stripe's.
type stubProvider struct {
	refunded     bool
	refundParams payments.RefundParams
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) CreateCheckout(ctx context.Context, p payments.CheckoutParams) (payments.CheckoutSession, error) {
	return payments.CheckoutSession{URL: "https://stub", SessionID: "stub_sess"}, nil
}
func (s *stubProvider) VerifyAndParseWebhook(body []byte, headers http.Header) (payments.Event, error) {
	return payments.Event{}, nil
}
func (s *stubProvider) Refund(ctx context.Context, p payments.RefundParams) (payments.Refund, error) {
	s.refunded = true
	s.refundParams = p
	return payments.Refund{ID: "re_stub_1", Status: "succeeded"}, nil
}
func (s *stubProvider) CustomerPortal(ctx context.Context, customerID, returnURL string) (string, error) {
	return "", payments.ErrNotSupported
}

// TestRefundPurchase_PreMigrationGate locks in the legacy-refund
// behaviour: a `purchases` row imported from another zv instance
// must NOT have its in-app Refund button call provider.Refund —
// the recorded charge ID is on the source platform's Stripe
// account, not ours, and routing the refund through this
// instance's credentials would either 404 (the charge isn't on
// this account) or silently match an unrelated charge.
//
// The gate is in stripe.go's RefundPurchase: if pre_migration is
// true, return 409 with a clear pointer back to the legacy
// dashboard before reaching the provider call. This test seeds two
// purchase rows (one legacy, one fresh), drives both through the
// handler, and asserts the provider sees only the fresh one.
func TestRefundPurchase_PreMigrationGate(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)

	// Seed an org + a global admin user (RefundPurchase is
	// admin-only at the role check inside the handler).
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var adminID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified, is_admin)
		VALUES ('platform-admin', 'admin@example.com', 1, TRUE) RETURNING id
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, title, author, org_id, published)
		VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1)
	`, orgID); err != nil {
		t.Fatal(err)
	}
	// Two paid purchases:
	//   • legacyID — pre_migration=TRUE; the gate must refuse it
	//   • freshID  — pre_migration=FALSE; the gate must let it through
	// Both carry a stripe_charge_id so the existing chargeID-required
	// guard doesn't fire.
	var legacyID, freshID int64
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency,
			provider, state, stripe_charge_id, pre_migration)
		VALUES ($1, 'treasure', 20, 5000, 'eur', 'stripe', 'paid', 'ch_legacy_42', TRUE)
		RETURNING id
	`, orgID).Scan(&legacyID); err != nil {
		t.Fatalf("seed legacy purchase: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency,
			provider, state, stripe_charge_id, pre_migration)
		VALUES ($1, 'treasure', 20, 5000, 'eur', 'stripe', 'paid', 'ch_fresh_42', FALSE)
		RETURNING id
	`, orgID).Scan(&freshID); err != nil {
		t.Fatalf("seed fresh purchase: %v", err)
	}

	stub := &stubProvider{}
	h := &admin.Handlers{DB: db, Provider: stub, PublicURL: "http://localhost"}

	adminUser := auth.User{
		ID: adminID, Username: "platform-admin", Role: auth.RoleAdmin,
	}
	r := chi.NewRouter()
	r.Use(injectUser(adminUser))
	r.Post("/api/admin/purchases/{purchaseID}/refund", h.RefundPurchase)

	// 1. Legacy row: must 409 with a body that mentions
	// "pre-migration" / "legacy" so the operator knows what to do.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/admin/purchases/"+strconv.FormatInt(legacyID, 10)+"/refund", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("legacy refund: status=%d body=%s, want 409", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(strings.ToLower(body), "pre-migration") &&
		!strings.Contains(strings.ToLower(body), "legacy") {
		t.Errorf("legacy refund: body should reference pre-migration / legacy dashboard, got %s", body)
	}
	if stub.refunded {
		t.Errorf("legacy refund reached provider.Refund — gate didn't fire")
	}

	// State must NOT have changed. The legacy row is still 'paid'.
	var stateAfter string
	if err := db.QueryRow(
		`SELECT state FROM purchases WHERE id = $1`, legacyID).Scan(&stateAfter); err != nil {
		t.Fatal(err)
	}
	if stateAfter != "paid" {
		t.Errorf("legacy purchase state after blocked refund: got %q, want paid", stateAfter)
	}

	// 2. Fresh row: gate must let it through, provider sees the
	// expected charge ID. The webhook flips the row to 'refunded'
	// in real life; the handler itself returns 202.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST",
		"/api/admin/purchases/"+strconv.FormatInt(freshID, 10)+"/refund", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("fresh refund: status=%d body=%s, want 202", w.Code, w.Body.String())
	}
	if !stub.refunded {
		t.Errorf("fresh refund: provider.Refund not called — handler diverged unexpectedly")
	}
	if stub.refundParams.ChargeID != "ch_fresh_42" {
		t.Errorf("fresh refund: charge id passed to provider: got %q, want ch_fresh_42",
			stub.refundParams.ChargeID)
	}
}

// TestListPurchases_SurfacesPreMigration confirms the listing
// endpoint exposes the flag so the frontend can render the legacy
// notice in place of the Refund button.
func TestListPurchases_SurfacesPreMigration(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var adminID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email_verified, is_admin)
		VALUES ('admin', 1, TRUE) RETURNING id
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, title, author, org_id, published)
		VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1)
	`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency,
			provider, state, stripe_charge_id, pre_migration)
		VALUES ($1, 'treasure', 20, 5000, 'eur', 'stripe', 'paid', 'ch_legacy', TRUE),
		       ($1, 'treasure', 20, 5000, 'eur', 'stripe', 'paid', 'ch_fresh',  FALSE)
	`, orgID); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db}
	adminUser := auth.User{ID: adminID, Username: "admin", Role: auth.RoleAdmin}
	r := chi.NewRouter()
	r.Use(injectUser(adminUser))
	r.Get("/api/admin/purchases", h.ListPurchases)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/admin/purchases", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", w.Code, w.Body.String())
	}
	// Decode the response so the assertion targets the parsed shape
	// rather than substrings — `stripe_charge_id` isn't in the
	// public DTO, so we check structural facts: two purchases, one
	// with pre_migration=true, one without.
	var resp struct {
		Purchases []struct {
			ID           int64 `json:"id"`
			PreMigration bool  `json:"pre_migration,omitempty"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Purchases) != 2 {
		t.Fatalf("got %d purchases, want 2: %+v", len(resp.Purchases), resp.Purchases)
	}
	var legacyCount, freshCount int
	for _, p := range resp.Purchases {
		if p.PreMigration {
			legacyCount++
		} else {
			freshCount++
		}
	}
	if legacyCount != 1 || freshCount != 1 {
		t.Errorf("counts: legacy=%d, fresh=%d, want 1 + 1", legacyCount, freshCount)
	}
	_ = sql.ErrNoRows // keep the database/sql import used if a future check needs it
}
