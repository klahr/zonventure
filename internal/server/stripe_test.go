package server_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.sr.ht/~klahr/zv/internal/server"
)

const (
	testStripeSecretKey    = "sk_test_zv_unit"
	testStripeWebhookKey   = "whsec_zv_unit_test"
)

// stripeSign constructs the Stripe-Signature header value for `body`
// using `secret` and the current time. Stripe's verification format is
// `t=<unix>,v1=<hex_hmac_sha256>`, where the HMAC's signed payload is
// `<unix>.<body>`. The default tolerance in stripe-go is 5 minutes, so
// time.Now() is fine.
func stripeSign(t *testing.T, body []byte, secret string) string {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// postWebhook sends a raw POST to /api/webhooks/stripe with a freshly
// signed body. Returns status code and response body.
func postWebhook(t *testing.T, ts *httptest.Server, body []byte, sigHeader string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/api/webhooks/stripe", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	if sigHeader != "" {
		req.Header.Set("Stripe-Signature", sigHeader)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do req: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

// stripeEventEnvelope wraps the inner object the way Stripe ships
// webhook deliveries. The library decodes Data.Raw → typed structs in
// the per-event handler, so we only need to make this shape match the
// envelope.
type stripeEventEnvelope struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Type    string          `json:"type"`
	Created int64           `json:"created"`
	Data    json.RawMessage `json:"data"`
}

func eventEnvelope(t *testing.T, id, eventType string, object any) []byte {
	t.Helper()
	objBytes, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal object: %v", err)
	}
	envBytes, err := json.Marshal(stripeEventEnvelope{
		ID:      id,
		Object:  "event",
		Type:    eventType,
		Created: time.Now().Unix(),
		Data:    json.RawMessage(`{"object":` + string(objBytes) + `}`),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return envBytes
}

func TestStripeWebhook_DisabledWithoutCreds(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	// No Stripe creds → handler should refuse with 503 before even
	// attempting signature verification.
	if status, _ := postWebhook(t, ts, []byte(`{}`), ""); status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
}

func TestStripeWebhook_RejectsBadSignature(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
	}
	ts, _, _, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()

	body := []byte(`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{}}}`)

	// Missing header.
	if status, _ := postWebhook(t, ts, body, ""); status != http.StatusBadRequest {
		t.Fatalf("missing sig = %d, want 400", status)
	}

	// Wrong secret.
	wrongSig := stripeSign(t, body, "whsec_wrong")
	if status, _ := postWebhook(t, ts, body, wrongSig); status != http.StatusBadRequest {
		t.Fatalf("wrong-secret sig = %d, want 400", status)
	}

	// Tampered body — signature is valid for a different payload.
	goodSig := stripeSign(t, body, testStripeWebhookKey)
	if status, _ := postWebhook(t, ts, []byte(`{"id":"tampered"}`), goodSig); status != http.StatusBadRequest {
		t.Fatalf("tampered body = %d, want 400", status)
	}
}

func TestStripeWebhook_CheckoutCompleted_HappyPath(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
	}
	ts, db, _, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()

	// Boss creates a pending stripe-flagged purchase row directly. We
	// can't go through CreatePurchase here because that would call out
	// to Stripe to mint a Checkout Session; the webhook test only
	// exercises the inbound side.
	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var orgID int64
	if err := db.QueryRow(`SELECT org_id FROM users WHERE username = 'boss'`).Scan(&orgID); err != nil {
		t.Fatalf("lookup org: %v", err)
	}
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_teams,
		                     amount_cents, currency, provider, state)
		VALUES ($1, (SELECT id FROM users WHERE username = 'boss'), 'demo', 4,
		        800, 'USD', 'stripe', 'pending')
		RETURNING id
	`, orgID).Scan(&purchaseID); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}

	body := eventEnvelope(t, "evt_test_completed_1", "checkout.session.completed",
		map[string]any{
			"id":           "cs_test_abc",
			"object":       "checkout.session",
			"amount_total": 800,
			"currency":     "usd",
			"metadata": map[string]string{
				"zv_purchase_id": fmt.Sprintf("%d", purchaseID),
			},
		})
	sig := stripeSign(t, body, testStripeWebhookKey)
	if status, msg := postWebhook(t, ts, body, sig); status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, msg)
	}

	// Purchase should be paid with an assignment id and the provider
	// session id stamped.
	var (
		state    string
		assID    *int64
		sessID   *string
	)
	if err := db.QueryRow(
		`SELECT state, assignment_id, provider_session_id FROM purchases WHERE id = $1`, purchaseID,
	).Scan(&state, &assID, &sessID); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if state != "paid" {
		t.Fatalf("state = %q, want paid", state)
	}
	if assID == nil || *assID == 0 {
		t.Fatalf("expected assignment_id")
	}
	if sessID == nil || *sessID != "cs_test_abc" {
		t.Fatalf("provider_session_id = %v, want cs_test_abc", sessID)
	}

	// Idempotency: redelivering the *same* event id is a no-op — no
	// new assignment row, state unchanged.
	if status, _ := postWebhook(t, ts, body, sig); status != http.StatusOK {
		t.Fatalf("redelivery = %d, want 200", status)
	}
	var assnCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM adventure_assignments WHERE org_id = $1 AND adventure_slug = 'demo'`, orgID,
	).Scan(&assnCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if assnCount != 1 {
		t.Fatalf("assignment count = %d after redelivery, want 1", assnCount)
	}
}

func TestStripeWebhook_CheckoutCompleted_UnknownPurchase(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
	}
	ts, _, _, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()

	// Metadata pointing at a nonexistent purchase row should still
	// 200 — the handler treats it as "not ours, skip" so Stripe doesn't
	// retry forever.
	body := eventEnvelope(t, "evt_test_orphan_1", "checkout.session.completed",
		map[string]any{
			"id":     "cs_orphan",
			"object": "checkout.session",
			"metadata": map[string]string{
				"zv_purchase_id": "999999",
			},
		})
	sig := stripeSign(t, body, testStripeWebhookKey)
	if status, msg := postWebhook(t, ts, body, sig); status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, msg)
	}
}

func TestStripeWebhook_ChargeRefunded(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
	}
	ts, db, _, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()

	// Seed a paid purchase with a known stripe_charge_id so the refund
	// handler has something to flip.
	var orgID int64
	if err := db.QueryRow(`SELECT org_id FROM users WHERE username = 'boss'`).Scan(&orgID); err != nil {
		t.Fatalf("lookup org: %v", err)
	}
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_teams,
		                     amount_cents, currency, provider, state, stripe_charge_id, paid_at)
		VALUES ($1, (SELECT id FROM users WHERE username = 'boss'), 'demo', 1,
		        200, 'USD', 'stripe', 'paid', 'ch_test_xyz', now_text())
		RETURNING id
	`, orgID).Scan(&purchaseID); err != nil {
		t.Fatalf("seed paid: %v", err)
	}

	body := eventEnvelope(t, "evt_test_refunded_1", "charge.refunded",
		map[string]any{
			"id":     "ch_test_xyz",
			"object": "charge",
			"amount": 200,
		})
	sig := stripeSign(t, body, testStripeWebhookKey)
	if status, msg := postWebhook(t, ts, body, sig); status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, msg)
	}

	var state string
	if err := db.QueryRow(`SELECT state FROM purchases WHERE id = $1`, purchaseID).Scan(&state); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if state != "refunded" {
		t.Fatalf("state = %q, want refunded", state)
	}
}

func TestStripeWebhook_UnhandledEventType(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
	}
	ts, _, _, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()

	// Event types we don't subscribe to should still 200 so Stripe
	// doesn't redrive forever.
	body := eventEnvelope(t, "evt_test_unhandled_1", "customer.created",
		map[string]any{"id": "cus_x", "object": "customer"})
	sig := stripeSign(t, body, testStripeWebhookKey)
	if status, _ := postWebhook(t, ts, body, sig); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
}
