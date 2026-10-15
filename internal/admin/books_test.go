package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/payments"
)

// TestShop_Book: a book from the shop is charged on the home
// organisation's own account at the price in products.json, nothing
// taken by the platform, with a postal address asked for; once paid it
// is in the organisation's orders to post.
func TestShop_Book(t *testing.T) {
	t.Parallel()
	f, cleanup := newMarketFixture(t, 49900, 3)
	defer cleanup()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "products.json"),
		[]byte(`{"products": [{"slug": "blackis", "name": "På äventyr med Bläckis", "price_cents": 19900}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f.h.HomeOrg, f.h.ShopDir = "acme", dir
	r := chi.NewRouter()
	r.Post("/api/shop/checkout", f.h.BuyBook)
	r.Get("/api/admin/redemptions", f.h.ListRedemptions)

	buy := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return call(t, r, "POST", "/api/shop/checkout", json.RawMessage(body))
	}
	if w := buy(`{"product": "nope", "buyer_email": "kim@example.com"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown book: %d", w.Code)
	}
	if w := buy(`{"product": "blackis", "buyer_email": "kim@example.com", "lang": "sv"}`); w.Code != http.StatusOK {
		t.Fatalf("buy: %d %s", w.Code, w.Body)
	}
	c := f.pay.checkouts[len(f.pay.checkouts)-1]
	if c.AmountCents != 19900 || c.ConnectedAccountID != "acct_acme" || c.ApplicationFeeCents != 0 || len(c.ShipCountries) == 0 {
		t.Errorf("checkout: %+v, want 199 kr on acct_acme, no fee, an address asked for", c)
	}

	var id int64
	if err := f.db.QueryRow(`SELECT id FROM ticket_purchases WHERE kind = 'book'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ev := buildSessionCompletedEvent(t, id, "cs_market")
	ev.Checkout.Shipping = &payments.Shipping{Name: "Kim", Line1: "Storgatan 1", PostalCode: "111 22", City: "Stockholm", Country: "SE"}
	if err := admin.ExportedHandleTicketCheckoutCompleted(f.h, context.Background(), ev); err != nil {
		t.Fatalf("fulfil: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/admin/redemptions?state=to_post", nil)
	req = req.WithContext(auth.WithUser(req.Context(), auth.User{ID: 1, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &f.orgID}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Bläckis") || !strings.Contains(w.Body.String(), "Storgatan 1") {
		t.Errorf("orders to post: %d %s", w.Code, w.Body)
	}
}
