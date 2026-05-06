package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/dbtest"
	"git.sr.ht/~klahr/zv/internal/public"
	"github.com/go-chi/chi/v5"
	stripe "github.com/stripe/stripe-go/v82"
)

// TestStoreList_Filtering exercises the listing query: only events
// with public_for_sale=true AND a non-NULL price AND state in
// (running, paused) appear. Other rows must NOT leak.
func TestStoreList_Filtering(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, name, org_id) VALUES ('demo', 'Demo Adv', $1)
	`, orgID); err != nil {
		t.Fatal(err)
	}

	mintEvent := func(name, state string, public bool, price *int) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`
			INSERT INTO events(adventure_slug, org_id, state, live_token, name,
			                   public_for_sale, ticket_price_cents, ticket_currency, ticket_team_size, public_blurb)
			VALUES ('demo', $1, $2, 'lt-' || md5(random()::text), $3, $4, $5, 'eur', 5, 'A blurb')
			RETURNING id
		`, orgID, state, name, public, priceArg(price)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	listed := mintEvent("Listed running", "running", true, intp(2500))
	listedPaused := mintEvent("Listed paused", "paused", true, intp(2500))
	mintEvent("Public but no price", "running", true, nil)
	mintEvent("Priced but private", "running", false, intp(2500))
	mintEvent("Listed but completed", "completed", true, intp(2500))
	mintEvent("Listed but cancelled", "cancelled", true, intp(2500))

	publicH := &public.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Get("/api/store/items", publicH.StoreList)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/store/items", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []public.StoreItemDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items (running + paused), got %d: %+v", len(resp.Items), resp.Items)
	}
	got := map[int64]bool{}
	for _, it := range resp.Items {
		got[it.EventID] = true
	}
	if !got[listed] {
		t.Errorf("running listed event missing from /store: %d", listed)
	}
	if !got[listedPaused] {
		t.Errorf("paused listed event missing from /store: %d", listedPaused)
	}
}

// TestBuyTicket_RejectsBadInput exercises the input-validation paths
// of BuyTicket without spinning up Stripe — every path that would
// reach session.New is gated behind a stripeEnabled() check, so we
// can deterministically exercise the 4xx paths even on a server
// without keys.
func TestBuyTicket_RejectsBadInput(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	h := &admin.Handlers{
		DB:                  db,
		Lib:                 adventure.New(db),
		PublicURL:           "https://zv.example.com",
		StripeSecretKey:     "sk_test_dummy",
		StripeWebhookSecret: "whsec_dummy",
	}
	r := chi.NewRouter()
	r.Post("/api/store/buy", h.BuyTicket)

	cases := []struct {
		name, body string
		want       int
	}{
		{"empty body", `{}`, http.StatusBadRequest},
		{"missing email", `{"event_id": 1}`, http.StatusBadRequest},
		{"bad email", `{"event_id": 1, "buyer_email": "not-an-email"}`, http.StatusBadRequest},
		{"unknown event", `{"event_id": 9999, "buyer_email": "ok@example.com"}`, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/store/buy", strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, c.want, w.Body.String())
			}
		})
	}
}

// TestHandleTicketCheckoutCompleted_HappyPath drives the webhook
// handler directly with a synthetic Stripe event. Confirms a team
// + N slots are created, the purchase row flips to paid, and the
// buyer receives one email.
func TestHandleTicketCheckoutCompleted_HappyPath(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, name, org_id) VALUES ('demo', 'Demo Adv', $1)
	`, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name,
		                   public_for_sale, ticket_price_cents, ticket_currency, ticket_team_size, public_blurb)
		VALUES ('demo', $1, 'running', 'lt-' || md5(random()::text), 'Always-on demo',
		        TRUE, 2500, 'eur', 4, 'B')
		RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, stripe_session_id)
		VALUES ($1, 'buyer@example.com', 4, 2500, 'eur', 'cs_test_abc')
		RETURNING id
	`, eventID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	mailerStub := &fakeMailer{}
	h := &admin.Handlers{
		DB:        db,
		Lib:       adventure.New(db),
		Mailer:    mailerStub,
		PublicURL: "https://zv.example.com",
	}
	event := buildSessionCompletedEvent(t, purchaseID, "cs_test_abc")
	if err := admin.ExportedHandleTicketCheckoutCompleted(h, context.Background(), event); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Purchase row flipped + linked to a team.
	var (
		state  string
		teamID int64
	)
	if err := db.QueryRow(
		`SELECT state, COALESCE(team_id, 0) FROM ticket_purchases WHERE id = $1`,
		purchaseID).Scan(&state, &teamID); err != nil {
		t.Fatal(err)
	}
	if state != "paid" {
		t.Errorf("state = %q, want paid", state)
	}
	if teamID == 0 {
		t.Fatal("team_id was not set on the purchase row")
	}

	// Team has 4 slots (the configured ticket_team_size).
	var slotCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM player_slots WHERE team_id = $1`, teamID).
		Scan(&slotCount); err != nil {
		t.Fatal(err)
	}
	if slotCount != 4 {
		t.Errorf("slot count = %d, want 4", slotCount)
	}

	// Exactly one email went out, addressed to the buyer.
	if got := len(mailerStub.messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	msg := mailerStub.messages[0]
	if len(msg.To) == 0 || msg.To[0] != "buyer@example.com" {
		t.Errorf("To = %v, want [buyer@example.com]", msg.To)
	}
	if !strings.Contains(msg.Text, "/play?t=") {
		t.Errorf("body has no /play?t= link: %s", msg.Text)
	}

	// Idempotency: a second invocation must not create a second team
	// or a second email.
	if err := admin.ExportedHandleTicketCheckoutCompleted(h, context.Background(), event); err != nil {
		t.Fatalf("handle (replay): %v", err)
	}
	var teams int
	if err := db.QueryRow(`SELECT COUNT(*) FROM teams WHERE event_id = $1`, eventID).
		Scan(&teams); err != nil {
		t.Fatal(err)
	}
	if teams != 1 {
		t.Errorf("teams after replay = %d, want 1", teams)
	}
	if got := len(mailerStub.messages); got != 1 {
		t.Errorf("messages after replay = %d, want 1 (mail must NOT resend)", got)
	}
}

// TestHandleChargeRefunded_TicketWithdrawsTeam confirms the
// charge.refunded path also covers ticket purchases — the matching
// row flips to refunded, and the team it created is withdrawn so
// the buyer can no longer act on the magic link.
func TestHandleChargeRefunded_TicketWithdrawsTeam(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, name, org_id) VALUES ('demo', 'Demo Adv', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name)
		VALUES ('demo', $1, 'running', 'lt-' || md5(random()::text), 'X') RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	// Pre-create the team so the FK-from-ticket can point at it.
	var teamID int64
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, token) VALUES ($1, 'buyer', 'team-tok-' || md5(random()::text))
		RETURNING id
	`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency,
		                             state, paid_at, team_id, stripe_session_id, stripe_charge_id)
		VALUES ($1, 'b@x.com', 1, 1000, 'eur', 'paid', now_text(), $2, 'cs_x', 'ch_test_42')
		RETURNING id
	`, eventID, teamID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	h := &admin.Handlers{DB: db, Lib: adventure.New(db)}
	event := buildChargeRefundedEvent(t, "ch_test_42")
	if err := admin.ExportedHandleChargeRefunded(h, context.Background(), event); err != nil {
		t.Fatalf("handle: %v", err)
	}

	var state string
	if err := db.QueryRow(`SELECT state FROM ticket_purchases WHERE id = $1`, purchaseID).
		Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "refunded" {
		t.Errorf("state = %q, want refunded", state)
	}
	var withdrawn *string
	if err := db.QueryRow(`SELECT withdrawn_at FROM teams WHERE id = $1`, teamID).
		Scan(&withdrawn); err != nil {
		t.Fatal(err)
	}
	if withdrawn == nil || *withdrawn == "" {
		t.Error("team was not withdrawn after refund")
	}
}

// buildSessionCompletedEvent constructs a stripe.Event whose Data
// holds a CheckoutSession with the given metadata. The webhook
// handler decodes Data.Raw into a CheckoutSession via the same
// path Stripe takes in production.
func buildSessionCompletedEvent(t *testing.T, ticketPurchaseID int64, sessionID string) stripe.Event {
	t.Helper()
	body := map[string]any{
		"id":       sessionID,
		"object":   "checkout.session",
		"metadata": map[string]string{"zv_ticket_purchase_id": itoa64(ticketPurchaseID)},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return stripe.Event{
		Type: "checkout.session.completed",
		Data: &stripe.EventData{Raw: raw},
	}
}

func buildChargeRefundedEvent(t *testing.T, chargeID string) stripe.Event {
	t.Helper()
	body := map[string]any{
		"id":     chargeID,
		"object": "charge",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return stripe.Event{
		Type: "charge.refunded",
		Data: &stripe.EventData{Raw: raw},
	}
}

func intp(v int) *int { return &v }

func priceArg(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

