package simulated

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/payments"
	"github.com/go-chi/chi/v5"
)

// TestRoundTrip exercises the full flow: a buyer hits CreateCheckout,
// browses to the dev confirm route, and the synthetic webhook lands
// in a stub handler with the right metadata + checkout details.
//
// Validates the abstraction's "the rest of the app stays
// provider-agnostic" claim: nothing in this test imports
// stripe-go, but the events delivered to the (stand-in) webhook
// handler match the same `payments.Event` shape the Stripe impl
// produces.
func TestRoundTrip(t *testing.T) {
	// Stand up a webhook stub that captures whatever the provider
	// posts to it. In production this would be adminH.PaymentsWebhook;
	// for the unit test we just want to assert the body shape.
	var (
		got     payments.Event
		gotBody []byte
	)
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.Unmarshal(gotBody, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer hookSrv.Close()

	prov := New(hookSrv.URL)
	prov.HTTPClient = hookSrv.Client()

	sess, err := prov.CreateCheckout(context.Background(), payments.CheckoutParams{
		AmountCents:   2500,
		Currency:      "eur",
		ProductName:   "Test ticket",
		SuccessURL:    "https://buyer.example.com/store?paid=42",
		CancelURL:     "https://buyer.example.com/store?cancelled=42",
		CustomerEmail: "buyer@example.com",
		Metadata:      map[string]string{"zv_ticket_purchase_id": "42"},
	})
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if !strings.HasPrefix(sess.URL, "/dev/payments/checkout/") {
		t.Errorf("URL = %q, want /dev/payments/checkout/* prefix", sess.URL)
	}
	if sess.SessionID == "" {
		t.Error("SessionID empty")
	}

	// Mount dev routes on a chi router and walk the confirm path.
	r := chi.NewRouter()
	prov.MountDevRoutes(r)

	// GET the checkout page first — must render with a 200.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", sess.URL, nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkout page status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Test ticket") {
		t.Errorf("checkout page missing product name")
	}

	// POST to confirm — should deliver the webhook + redirect.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", sess.URL+"/confirm", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("confirm status = %d, want 303; body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://buyer.example.com/store?paid=42" {
		t.Errorf("redirect = %q, want SuccessURL", loc)
	}

	// The webhook stub captured the body — verify the shape
	// matches the payments.Event contract.
	if got.Type != payments.EventCheckoutCompleted {
		t.Errorf("Type = %q, want %q", got.Type, payments.EventCheckoutCompleted)
	}
	if got.Provider != "simulated" {
		t.Errorf("Provider = %q, want simulated", got.Provider)
	}
	if got.Metadata["zv_ticket_purchase_id"] != "42" {
		t.Errorf("metadata round-trip lost: %+v", got.Metadata)
	}
	if got.Checkout == nil {
		t.Fatal("Checkout detail missing")
	}
	if got.Checkout.SessionID != sess.SessionID {
		t.Errorf("SessionID round-trip: got %q, want %q", got.Checkout.SessionID, sess.SessionID)
	}
	if got.Checkout.AmountTotal != 2500 {
		t.Errorf("AmountTotal = %d, want 2500", got.Checkout.AmountTotal)
	}
	if got.Checkout.ChargeID == "" {
		t.Error("ChargeID empty — refund path won't find the row")
	}

	// Replay must 404 — session is consumed.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", sess.URL+"/confirm", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("replay status = %d, want 404", rec.Code)
	}
}

// TestRefund_Stub asserts that Refund returns success without
// hitting any external service. The actual state-flip side effect
// happens via the charge.refunded webhook path; this just locks
// in the contract for the trigger.
func TestRefund_Stub(t *testing.T) {
	prov := New("")
	rf, err := prov.Refund(context.Background(), payments.RefundParams{ChargeID: "sim_ch_x"})
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if rf.Status != "succeeded" {
		t.Errorf("Status = %q, want succeeded", rf.Status)
	}
	if rf.ID == "" {
		t.Error("Refund.ID empty")
	}
}

// TestCustomerPortal_Unsupported checks that the stub surfaces
// ErrNotSupported rather than silently returning empty — admin
// callers translate ErrNotSupported into a clear 501 instead of a
// generic 5xx.
func TestCustomerPortal_Unsupported(t *testing.T) {
	prov := New("")
	url, err := prov.CustomerPortal(context.Background(), "cust_x", "https://return.example.com")
	if err != payments.ErrNotSupported {
		t.Errorf("err = %v, want ErrNotSupported", err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

// TestVerifyAndParseWebhook_RoundTrip confirms a body produced by
// json.Marshal of a payments.Event survives the parser unchanged.
// (The simulated provider doesn't sign anything; the parse is
// strictly JSON unmarshal.)
func TestVerifyAndParseWebhook_RoundTrip(t *testing.T) {
	prov := New("")
	in := payments.Event{
		Type:     payments.EventChargeRefunded,
		ID:       "sim_evt_round",
		Created:  time.Now().Truncate(time.Second),
		Charge:   &payments.ChargeDetail{ID: "sim_ch_a", AmountCents: 1000, Currency: "eur"},
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	hdr := http.Header{}
	hdr.Set("X-Simulated-Secret", prov.Secret())
	out, err := prov.VerifyAndParseWebhook(body, hdr)
	if err != nil {
		t.Fatalf("VerifyAndParseWebhook: %v", err)
	}
	if out.Type != in.Type {
		t.Errorf("Type drift: got %q, want %q", out.Type, in.Type)
	}
	if out.Provider != "simulated" {
		t.Errorf("Provider = %q, want simulated (impl always sets its own name)", out.Provider)
	}
	if out.Charge == nil || out.Charge.ID != "sim_ch_a" {
		t.Errorf("Charge round-trip: %+v", out.Charge)
	}
}
