package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/payments/simulated"
)

// A pass paid through the simulated checkout mails its buyer a receipt
// and downloads a plain one; one Stripe has a receipt for downloads
// Stripe's; a free pass has none.
func TestPurchaseReceipt(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	purchase := func(provider, state string, cents int, invoiceURL string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`
			INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_players, amount_cents, currency, provider, state, stripe_invoice_url)
			VALUES ($1, $2, 'demo', 20, $3, 'SEK', $4, $5, NULLIF($6, '')) RETURNING id
		`, orgID, userID, cents, provider, state, invoiceURL).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: mail, PublicURL: "https://zv.example.com", Provider: simulated.New("")}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	r.Post("/api/admin/purchases/{purchaseID}/confirm", h.ConfirmPurchase)
	r.Get("/api/admin/purchases/{purchaseID}/receipt", h.PurchaseReceipt)
	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
		return w
	}

	id := purchase("simulated", "pending", 24500, "")
	if w := do("POST", "/api/admin/purchases/"+itoa64(id)+"/confirm"); w.Code != http.StatusOK {
		t.Fatalf("confirm = %d %s", w.Code, w.Body.String())
	}
	if len(mail.messages) != 1 {
		t.Fatalf("mails = %d, want the receipt", len(mail.messages))
	}
	m := mail.messages[0]
	if m.To[0] != "boss@acme.com" || !strings.Contains(m.Text, "Receipt no. "+itoa64(id)) || !strings.Contains(m.Text, "Amount paid: 245.00 SEK") ||
		!strings.Contains(m.Text, "Download receipt: https://zv.example.com/app/arrange/#/settings/purchases") {
		t.Errorf("receipt mail to %v:\n%s", m.To, m.Text)
	}
	w := do("GET", "/api/admin/purchases/"+itoa64(id)+"/receipt")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "receipt-"+itoa64(id)) || !strings.Contains(w.Body.String(), "245.00 SEK") {
		t.Errorf("download = %d %v", w.Code, w.Header())
	}

	stripe := purchase("stripe", "paid", 24500, "https://pay.stripe.com/receipts/x")
	if w := do("GET", "/api/admin/purchases/"+itoa64(stripe)+"/receipt"); w.Code != http.StatusFound || w.Header().Get("Location") != "https://pay.stripe.com/receipts/x" {
		t.Errorf("stripe receipt = %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do("GET", "/api/admin/purchases/"+itoa64(purchase("free", "paid", 0, ""))+"/receipt"); w.Code != http.StatusConflict {
		t.Errorf("free pass receipt = %d, want 409", w.Code)
	}
}
