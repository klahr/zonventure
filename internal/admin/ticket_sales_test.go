package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// The Purchases tab lists ticket sales of every kind: an org sees its
// own, the platform admin all, and only the admin refunds them — an
// org's sale with its fee clawed back, the platform's own as is.
func TestTicketSales_ListAndRefund(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	var acme, other, adminID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&acme))
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Other') RETURNING id`).Scan(&other))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin) VALUES ('admin', 1, TRUE) RETURNING id`).Scan(&adminID))
	_, err := db.Exec(`INSERT INTO adventures(slug, title, author, org_id, published) VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1), ('elsewhere', 'Elsewhere', 'Other', $2, 1)`, acme, other)
	must(err)
	var acmeEvent, otherEvent int64
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('treasure', $1, 'running', 'lt-a', 'Harbour Night') RETURNING id`, acme).Scan(&acmeEvent))
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('elsewhere', $1, 'running', 'lt-o', 'Elsewhere Night') RETURNING id`, other).Scan(&otherEvent))

	// Acme's self-guided ticket, sold through its connected account;
	// a hosted pass Acme bought from the platform; Other's ticket; and
	// a pending one, which can't be refunded.
	var ticket, hosted, pending int64
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, stripe_charge_id, application_fee_cents, paid_at)
		VALUES ($1, 'buyer@example.com', 4, 20000, 'sek', 'paid', 'ch_ticket', 2000, now_text()) RETURNING id`, acmeEvent).Scan(&ticket))
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(kind, buyer_org_id, adventure_slug, buyer_email, team_size, blocks, amount_cents, currency, state, stripe_charge_id, paid_at)
		VALUES ('hosted', $1, 'treasure', 'host@example.com', 0, 1, 29900, 'sek', 'paid', 'ch_hosted', now_text()) RETURNING id`, acme).Scan(&hosted))
	_, err = db.Exec(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, stripe_charge_id)
		VALUES ($1, 'someone@example.com', 2, 10000, 'sek', 'paid', 'ch_other')`, otherEvent)
	must(err)
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency)
		VALUES ($1, 'late@example.com', 2, 10000, 'sek') RETURNING id`, acmeEvent).Scan(&pending))

	stub := &stubProvider{}
	h := &admin.Handlers{DB: db, Provider: stub, PublicURL: "http://localhost"}
	router := func(u auth.User) http.Handler {
		r := chi.NewRouter()
		r.Use(injectUser(u))
		r.Get("/api/admin/ticket-purchases", h.ListTicketSales)
		r.Post("/api/admin/ticket-purchases/{purchaseID}/refund", h.RefundTicketSale)
		return r
	}
	type sale struct {
		ID         int64  `json:"id"`
		Kind       string `json:"kind"`
		EventName  string `json:"event_name"`
		OrgName    string `json:"org_name"`
		Refundable bool   `json:"refundable"`
	}
	list := func(r http.Handler) map[int64]sale {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/ticket-purchases", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("list: %d %s", w.Code, w.Body)
		}
		var resp struct{ Sales []sale }
		must(json.Unmarshal(w.Body.Bytes(), &resp))
		out := map[int64]sale{}
		for _, s := range resp.Sales {
			out[s.ID] = s
		}
		return out
	}
	refund := func(r http.Handler, id int64) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/admin/ticket-purchases/"+strconv.FormatInt(id, 10)+"/refund", nil))
		return w.Code
	}

	orgAdmin := router(auth.User{ID: 99, Username: "acme-admin", Role: auth.RoleOrgAdmin, OrgID: &acme})
	mine := list(orgAdmin)
	if len(mine) != 3 || mine[ticket].Kind != "ticket" || mine[ticket].EventName != "Harbour Night" ||
		mine[hosted].Kind != "hosted" || !mine[ticket].Refundable || mine[pending].Refundable {
		t.Fatalf("Acme's sales: %+v", mine)
	}
	if code := refund(orgAdmin, ticket); code != http.StatusForbidden || stub.refunded {
		t.Fatalf("org admin refund: %d, provider called %v", code, stub.refunded)
	}

	platform := router(auth.User{ID: adminID, Username: "admin", Role: auth.RoleAdmin})
	if all := list(platform); len(all) != 4 || all[hosted].OrgName != "Acme" {
		t.Fatalf("every sale: %+v", all)
	}
	if code := refund(platform, ticket); code != http.StatusAccepted ||
		stub.refundParams.ChargeID != "ch_ticket" || !stub.refundParams.RefundApplicationFee {
		t.Fatalf("org's ticket refund: %d %+v", code, stub.refundParams)
	}
	if code := refund(platform, hosted); code != http.StatusAccepted ||
		stub.refundParams.ChargeID != "ch_hosted" || stub.refundParams.RefundApplicationFee {
		t.Fatalf("platform's pass refund: %d %+v", code, stub.refundParams)
	}
	if code := refund(platform, pending); code != http.StatusConflict {
		t.Fatalf("pending refund: %d", code)
	}
}
