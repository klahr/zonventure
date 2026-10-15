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
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/server"

	stripego "github.com/stripe/stripe-go/v87"
)

const (
	testStripeSecretKey  = "sk_test_zv_unit"
	testStripeWebhookKey = "whsec_zv_unit_test"
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
	// APIVersion needs to match what stripe-go expects, otherwise
	// ConstructEvent fails with "Received event with API version ,
	// but stripe-go N.M.K expects ...". The library's expected
	// version moves with stripe-go upgrades; testStripeAPIVersion
	// holds the current one so tests don't drift silently when the
	// SDK is bumped.
	APIVersion string `json:"api_version"`
}

func eventEnvelope(t *testing.T, id, eventType string, object any) []byte {
	t.Helper()
	objBytes, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal object: %v", err)
	}
	envBytes, err := json.Marshal(stripeEventEnvelope{
		ID:         id,
		Object:     "event",
		Type:       eventType,
		Created:    time.Now().Unix(),
		Data:       json.RawMessage(`{"object":` + string(objBytes) + `}`),
		APIVersion: testStripeAPIVersion,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return envBytes
}

// testStripeAPIVersion is the API version stripe-go expects to see
// in incoming webhook envelopes. Bump this in lockstep with the
// stripe-go module upgrade. Mismatch surfaces as a 400 "bad
// signature" with a wrapped log line about API version mismatch.
const testStripeAPIVersion = "2026-09-30.endive"

// TestStripeWebhook drives the inbound webhook on one server with
// Stripe configured. Every subtest seeds its own purchase rows and uses
// its own event ids, so they don't see each other's deliveries.
func TestStripeWebhook(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
		Env:                 "dev",
	}
	ts, db, orgID, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()

	t.Run("rejects bad signature", func(t *testing.T) {
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
	})

	t.Run("checkout completed", func(t *testing.T) {
		// Boss creates a pending stripe-flagged purchase row directly. We
		// can't go through CreatePurchase here because that would call out
		// to Stripe to mint a Checkout Session; the webhook test only
		// exercises the inbound side.
		c := newClient(t, ts)
		if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		var purchaseID int64
		if err := db.QueryRow(`
			INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_players,
			                     amount_cents, currency, provider, state)
			VALUES ($1, (SELECT id FROM users WHERE username = 'boss'), 'demo', 20,
			        2500, 'EUR', 'stripe', 'pending')
			RETURNING id
		`, orgID).Scan(&purchaseID); err != nil {
			t.Fatalf("seed purchase: %v", err)
		}

		body := eventEnvelope(t, "evt_test_completed_1", "checkout.session.completed",
			map[string]any{
				"id":              "cs_test_abc",
				"object":          "checkout.session",
				"amount_total":    2500,
				"amount_subtotal": 2500,
				"currency":        "eur",
				"payment_status":  "paid",
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
			state  string
			assID  *int64
			sessID *string
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
		// new assignment row, state unchanged. Other subtests grant to
		// the same org, so count against this subtest's own baseline.
		countAssignments := func() int {
			t.Helper()
			var n int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM adventure_assignments WHERE org_id = $1 AND adventure_slug = 'demo'`, orgID,
			).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			return n
		}
		before := countAssignments()
		if status, _ := postWebhook(t, ts, body, sig); status != http.StatusOK {
			t.Fatalf("redelivery = %d, want 200", status)
		}
		if n := countAssignments(); n != before {
			t.Fatalf("assignment count = %d after redelivery, want %d", n, before)
		}
		var afterAssID *int64
		if err := db.QueryRow(
			`SELECT state, assignment_id FROM purchases WHERE id = $1`, purchaseID,
		).Scan(&state, &afterAssID); err != nil {
			t.Fatalf("readback after redelivery: %v", err)
		}
		if state != "paid" || afterAssID == nil || *afterAssID != *assID {
			t.Fatalf("after redelivery: state=%q assignment=%v, want paid / %d", state, afterAssID, *assID)
		}
	})

	t.Run("checkout completed for unknown purchase", func(t *testing.T) {
		// Metadata pointing at a nonexistent purchase row should still
		// 200 — the handler treats it as "not ours, skip" so Stripe doesn't
		// retry forever.
		body := eventEnvelope(t, "evt_test_orphan_1", "checkout.session.completed",
			map[string]any{
				"id":             "cs_orphan",
				"object":         "checkout.session",
				"payment_status": "paid",
				"metadata": map[string]string{
					"zv_purchase_id": "999999",
				},
			})
		sig := stripeSign(t, body, testStripeWebhookKey)
		if status, msg := postWebhook(t, ts, body, sig); status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", status, msg)
		}
	})

	t.Run("charge refunded", func(t *testing.T) {
		// Seed a paid purchase with a known stripe_charge_id so the refund
		// handler has something to flip.
		var purchaseID int64
		if err := db.QueryRow(`
			INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_players,
			                     amount_cents, currency, provider, state, stripe_charge_id, paid_at)
			VALUES ($1, (SELECT id FROM users WHERE username = 'boss'), 'demo', 16,
			        500, 'EUR', 'stripe', 'paid', 'ch_test_xyz', now_text())
			RETURNING id
		`, orgID).Scan(&purchaseID); err != nil {
			t.Fatalf("seed paid: %v", err)
		}

		body := eventEnvelope(t, "evt_test_refunded_1", "charge.refunded",
			map[string]any{
				"id":              "ch_test_xyz",
				"object":          "charge",
				"amount":          200,
				"amount_refunded": 200,
				"refunded":        true,
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
	})

	t.Run("unhandled event type", func(t *testing.T) {
		// Event types we don't subscribe to should still 200 so Stripe
		// doesn't redrive forever.
		body := eventEnvelope(t, "evt_test_unhandled_1", "customer.created",
			map[string]any{"id": "cus_x", "object": "customer"})
		sig := stripeSign(t, body, testStripeWebhookKey)
		if status, _ := postWebhook(t, ts, body, sig); status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
	})

	// An async payment method completes the session "unpaid": nothing is
	// granted until async_payment_succeeded, and async_payment_failed
	// marks the purchase failed.
	t.Run("async payment", func(t *testing.T) {
		seed := func() int64 {
			var id int64
			if err := db.QueryRow(`
				INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state)
				VALUES ($1, 'demo', 20, 2500, 'EUR', 'stripe', 'pending') RETURNING id
			`, orgID).Scan(&id); err != nil {
				t.Fatalf("seed purchase: %v", err)
			}
			return id
		}
		deliver := func(evtID, typ, sessID, status string, purchaseID int64) {
			body := eventEnvelope(t, evtID, typ, map[string]any{
				"id": sessID, "object": "checkout.session",
				"amount_total": 2500, "amount_subtotal": 2500, "currency": "eur", "payment_status": status,
				"metadata": map[string]string{"zv_purchase_id": fmt.Sprintf("%d", purchaseID)},
			})
			if code, msg := postWebhook(t, ts, body, stripeSign(t, body, testStripeWebhookKey)); code != http.StatusOK {
				t.Fatalf("%s = %d (%s), want 200", typ, code, msg)
			}
		}
		stateOf := func(id int64) string {
			var st string
			if err := db.QueryRow(`SELECT state FROM purchases WHERE id = $1`, id).Scan(&st); err != nil {
				t.Fatalf("readback: %v", err)
			}
			return st
		}

		ok := seed()
		deliver("evt_async_1", "checkout.session.completed", "cs_async_ok", "unpaid", ok)
		if st := stateOf(ok); st != "pending" {
			t.Fatalf("after unpaid completion state = %q, want pending", st)
		}
		deliver("evt_async_2", "checkout.session.async_payment_succeeded", "cs_async_ok", "paid", ok)
		if st := stateOf(ok); st != "paid" {
			t.Fatalf("after async success state = %q, want paid", st)
		}

		bad := seed()
		deliver("evt_async_3", "checkout.session.completed", "cs_async_bad", "unpaid", bad)
		deliver("evt_async_4", "checkout.session.async_payment_failed", "cs_async_bad", "unpaid", bad)
		if st := stateOf(bad); st != "failed" {
			t.Fatalf("after async failure state = %q, want failed", st)
		}
		// A late failure for an already-paid purchase must not undo it.
		deliver("evt_async_5", "checkout.session.async_payment_failed", "cs_async_ok", "unpaid", ok)
		if st := stateOf(ok); st != "paid" {
			t.Fatalf("late failure flipped paid purchase to %q", st)
		}
	})
}

// stubStripeAPI points stripe-go at a local server for the test: a
// session read returns its payment intent and invoice expanded, a
// session list by payment_intent names the session in sessionsByPI,
// and a session create succeeds. Returns the last create's form.
func stubStripeAPI(t *testing.T, sessionsByPI map[string]string) func() url.Values {
	t.Helper()
	var (
		mu      sync.Mutex
		created url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/checkout/sessions":
			_ = r.ParseForm()
			mu.Lock()
			created = r.PostForm
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cs_created", "object": "checkout.session", "url": "https://checkout.example/cs_created",
			})
		case r.Method == "GET" && r.URL.Path == "/v1/checkout/sessions":
			data := []map[string]any{}
			if id := sessionsByPI[r.URL.Query().Get("payment_intent")]; id != "" {
				data = append(data, map[string]any{"id": id, "object": "checkout.session"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list", "url": "/v1/checkout/sessions", "has_more": false, "data": data,
			})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/checkout/sessions/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/checkout/sessions/")
			if id == "cs_receipt" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": id, "object": "checkout.session",
					"payment_intent": map[string]any{"id": "pi_r", "object": "payment_intent",
						"latest_charge": map[string]any{"id": "ch_r", "object": "charge", "receipt_url": "https://receipt.example/ch_r"}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": id, "object": "checkout.session",
				"payment_intent": map[string]any{"id": "pi_123", "object": "payment_intent", "latest_charge": "ch_123"},
				"invoice":        map[string]any{"id": "in_123", "object": "invoice", "hosted_invoice_url": "https://invoice.example/in_123"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stripego.SetBackend(stripego.APIBackend, stripego.GetBackendWithConfig(stripego.APIBackend,
		&stripego.BackendConfig{URL: stripego.String(srv.URL)}))
	t.Cleanup(func() { stripego.SetBackend(stripego.APIBackend, nil) })
	return func() url.Values {
		mu.Lock()
		defer mu.Unlock()
		return created
	}
}

// A session with no invoice records the charge's Stripe receipt as the
// purchase's receipt link.
func TestStripeWebhook_ReceiptWithoutInvoice(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
		Env:                 "dev",
	}
	ts, db, orgID, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()
	stubStripeAPI(t, nil)
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state, provider_session_id)
		VALUES ($1, 'demo', 20, 2500, 'EUR', 'stripe', 'pending', 'cs_receipt') RETURNING id
	`, orgID).Scan(&purchaseID); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}
	body := eventEnvelope(t, "evt_receipt_1", "checkout.session.completed", map[string]any{
		"id": "cs_receipt", "object": "checkout.session",
		"amount_total": 2500, "amount_subtotal": 2500, "currency": "eur", "payment_status": "paid",
		"payment_intent": "pi_r",
		"metadata":       map[string]string{"zv_purchase_id": fmt.Sprintf("%d", purchaseID)},
	})
	if code, msg := postWebhook(t, ts, body, stripeSign(t, body, testStripeWebhookKey)); code != http.StatusOK {
		t.Fatalf("webhook = %d (%s), want 200", code, msg)
	}
	var chargeID, receipt string
	if err := db.QueryRow(`
		SELECT COALESCE(stripe_charge_id, ''), COALESCE(stripe_invoice_url, '') FROM purchases WHERE id = $1
	`, purchaseID).Scan(&chargeID, &receipt); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if chargeID != "ch_r" || receipt != "https://receipt.example/ch_r" {
		t.Fatalf("charge=%q receipt=%q", chargeID, receipt)
	}
}

// A real checkout.session.completed names its payment intent and
// invoice by bare id. The charge id must still be recorded so a later
// charge.refunded finds the purchase; rows fulfilled before that was
// recorded match on their session instead.
func TestStripeWebhook_UnexpandedSessionRecordsChargeForRefund(t *testing.T) {
	cfg := server.Config{
		StripeSecretKey:     testStripeSecretKey,
		StripeWebhookSecret: testStripeWebhookKey,
		Env:                 "dev",
	}
	ts, db, orgID, cleanup := seedPurchaseStack(t, cfg)
	defer cleanup()
	stubStripeAPI(t, map[string]string{"pi_123": "cs_unexp", "pi_legacy": "cs_legacy"})

	deliver := func(evtID, typ string, object map[string]any) {
		t.Helper()
		body := eventEnvelope(t, evtID, typ, object)
		if code, msg := postWebhook(t, ts, body, stripeSign(t, body, testStripeWebhookKey)); code != http.StatusOK {
			t.Fatalf("%s = %d (%s), want 200", typ, code, msg)
		}
	}
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state, provider_session_id)
		VALUES ($1, 'demo', 20, 2500, 'EUR', 'stripe', 'pending', 'cs_unexp') RETURNING id
	`, orgID).Scan(&purchaseID); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}
	deliver("evt_unexp_1", "checkout.session.completed", map[string]any{
		"id": "cs_unexp", "object": "checkout.session",
		// Tax added on top (an exclusive-tax session) must still match
		// the stored price: the check is against the subtotal.
		"amount_total": 3125, "amount_subtotal": 2500, "currency": "eur", "payment_status": "paid",
		"payment_intent": "pi_123", "invoice": "in_123",
		"metadata": map[string]string{"zv_purchase_id": fmt.Sprintf("%d", purchaseID)},
	})
	var state, chargeID, invoiceURL string
	if err := db.QueryRow(`
		SELECT state, COALESCE(stripe_charge_id, ''), COALESCE(stripe_invoice_url, '') FROM purchases WHERE id = $1
	`, purchaseID).Scan(&state, &chargeID, &invoiceURL); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if state != "paid" || chargeID != "ch_123" || invoiceURL != "https://invoice.example/in_123" {
		t.Fatalf("after completion: state=%q charge=%q invoice=%q", state, chargeID, invoiceURL)
	}

	deliver("evt_unexp_2", "charge.refunded", map[string]any{
		"id": "ch_123", "object": "charge", "payment_intent": "pi_123",
		"amount": 2500, "amount_refunded": 2500, "refunded": true,
	})
	if err := db.QueryRow(`SELECT state FROM purchases WHERE id = $1`, purchaseID).Scan(&state); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if state != "refunded" {
		t.Fatalf("after refund state = %q, want refunded", state)
	}

	var legacyID int64
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state, provider_session_id, paid_at)
		VALUES ($1, 'demo', 20, 2500, 'EUR', 'stripe', 'paid', 'cs_legacy', now_text()) RETURNING id
	`, orgID).Scan(&legacyID); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	deliver("evt_legacy_1", "charge.refunded", map[string]any{
		"id": "ch_legacy", "object": "charge", "payment_intent": "pi_legacy",
		"amount": 2500, "amount_refunded": 2500, "refunded": true,
	})
	if err := db.QueryRow(`
		SELECT state, COALESCE(stripe_charge_id, '') FROM purchases WHERE id = $1
	`, legacyID).Scan(&state, &chargeID); err != nil {
		t.Fatalf("readback legacy: %v", err)
	}
	if state != "refunded" || chargeID != "ch_legacy" {
		t.Fatalf("legacy row: state=%q charge=%q, want refunded/ch_legacy", state, chargeID)
	}
}

func TestStripe_RefusesTestKeyOutsideDev(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	build := func(key, env string) (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		server.New(server.Config{
			DB: db, Library: adventure.New(db),
			StripeSecretKey: key, StripeWebhookSecret: testStripeWebhookKey, Env: env,
		})
		return ""
	}
	for _, key := range []string{"sk_test_x", "rk_test_x"} {
		if msg := build(key, "production"); !strings.Contains(msg, "test-mode key") {
			t.Fatalf("%s in production: panic = %q, want test-mode refusal", key, msg)
		}
	}
	if msg := build("sk_test_x", "development"); msg != "" {
		t.Fatalf("test key in development panicked: %s", msg)
	}
	if msg := build("sk_live_x", "production"); msg != "" {
		t.Fatalf("live key in production panicked: %s", msg)
	}
}
