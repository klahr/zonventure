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
	"github.com/klahr/zonventure/internal/payments"
)

// The Purchases tab lists ticket sales of every kind: an org sees its
// own, the platform admin all. The admin refunds any — an org's sale
// with its fee clawed back, the platform's own as is — and an org admin
// only their own org's sales through its connected account.
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

	hash, err := auth.HashPassword("hunter2")
	must(err)
	var acme, other, adminID, memberID, orgAdminID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&acme))
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Other') RETURNING id`).Scan(&other))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin, password_hash) VALUES ('admin', 1, TRUE, $1) RETURNING id`, hash).Scan(&adminID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, password_hash) VALUES ('acme-member', 1, $1) RETURNING id`, hash).Scan(&memberID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, password_hash) VALUES ('acme-admin', 1, $1) RETURNING id`, hash).Scan(&orgAdminID))
	_, err = db.Exec(`INSERT INTO adventures(slug, title, author, org_id, published) VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1), ('elsewhere', 'Elsewhere', 'Other', $2, 1)`, acme, other)
	must(err)
	var acmeEvent, otherEvent int64
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('treasure', $1, 'running', 'lt-a', 'Harbour Night') RETURNING id`, acme).Scan(&acmeEvent))
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('elsewhere', $1, 'running', 'lt-o', 'Elsewhere Night') RETURNING id`, other).Scan(&otherEvent))

	// Acme's self-guided ticket, sold through its connected account;
	// a hosted pass Acme bought from the platform; Other's ticket; and
	// a pending one, which can't be refunded; and an Acme ticket that
	// settled on the platform account (no fee).
	var ticket, hosted, pending, abandoned, platformTicket, otherTicket int64
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, stripe_charge_id, application_fee_cents, paid_at)
		VALUES ($1, 'buyer@example.com', 4, 20000, 'sek', 'paid', 'ch_ticket', 2000, now_text()) RETURNING id`, acmeEvent).Scan(&ticket))
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(kind, buyer_org_id, adventure_slug, buyer_email, team_size, teams, amount_cents, currency, state, stripe_charge_id, paid_at)
		VALUES ('hosted', $1, 'treasure', 'host@example.com', 0, 1, 29900, 'sek', 'paid', 'ch_hosted', now_text()) RETURNING id`, acme).Scan(&hosted))
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, stripe_charge_id, application_fee_cents)
		VALUES ($1, 'someone@example.com', 2, 10000, 'sek', 'paid', 'ch_other', 1000) RETURNING id`, otherEvent).Scan(&otherTicket))
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, stripe_charge_id)
		VALUES ($1, 'early@example.com', 2, 10000, 'sek', 'paid', 'ch_platform') RETURNING id`, acmeEvent).Scan(&platformTicket))
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency)
		VALUES ($1, 'late@example.com', 2, 10000, 'sek') RETURNING id`, acmeEvent).Scan(&pending))
	must(db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, created_at)
		VALUES ($1, 'left@example.com', 2, 10000, 'sek', '2020-01-01 10:00:00') RETURNING id`, acmeEvent).Scan(&abandoned))

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
		Pending    bool   `json:"refund_pending"`
		Abandoned  bool   `json:"abandoned"`
	}
	listAt := func(r http.Handler, path string) map[int64]sale {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
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
	list := func(r http.Handler) map[int64]sale { return listAt(r, "/api/admin/ticket-purchases") }
	refundWith := func(r http.Handler, id int64, password string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/ticket-purchases/"+strconv.FormatInt(id, 10)+"/refund", nil)
		if password != "" {
			req.Header.Set("X-Confirm-Password", password)
		}
		r.ServeHTTP(w, req)
		return w.Code
	}
	refund := func(r http.Handler, id int64) int { return refundWith(r, id, "hunter2") }

	member := router(auth.User{ID: memberID, Username: "acme-member", Role: auth.RoleOrgMember, OrgID: &acme})
	// An event host doesn't see the org's sales at all.
	w := httptest.NewRecorder()
	member.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/ticket-purchases", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member list: %d, want 403", w.Code)
	}
	if code := refund(member, ticket); code != http.StatusForbidden || stub.refunded {
		t.Fatalf("member refund: %d, provider called %v", code, stub.refunded)
	}

	orgAdmin := router(auth.User{ID: orgAdminID, Username: "acme-admin", Role: auth.RoleOrgAdmin, OrgID: &acme})
	mine := list(orgAdmin)
	if len(mine) != 5 || mine[ticket].Kind != "ticket" || mine[ticket].EventName != "Harbour Night" ||
		mine[hosted].Kind != "hosted" || !mine[ticket].Refundable || mine[pending].Refundable ||
		mine[hosted].Refundable || mine[platformTicket].Refundable ||
		mine[pending].Abandoned || !mine[abandoned].Abandoned || mine[abandoned].Refundable {
		t.Fatalf("Acme's sales: %+v", mine)
	}
	// A ticket that settled on the platform account is the platform's
	// money: only the platform admin refunds it.
	for _, id := range []int64{hosted, otherTicket, platformTicket} {
		if code := refund(orgAdmin, id); code != http.StatusForbidden || stub.refunded {
			t.Fatalf("org admin refund of %d: %d, provider called %v", id, code, stub.refunded)
		}
	}
	for _, pw := range []string{"", "wrong"} {
		if code := refundWith(orgAdmin, ticket, pw); code != http.StatusUnauthorized || stub.refunded {
			t.Fatalf("refund with password %q: %d, provider called %v", pw, code, stub.refunded)
		}
	}
	if code := refund(orgAdmin, ticket); code != http.StatusAccepted ||
		stub.refundParams.ChargeID != "ch_ticket" || !stub.refundParams.RefundApplicationFee {
		t.Fatalf("org admin's own ticket refund: %d %+v", code, stub.refundParams)
	}
	// Until the webhook confirms it, the sale shows the refund pending
	// and nobody can start a second one.
	if s := list(orgAdmin)[ticket]; !s.Pending || s.Refundable {
		t.Fatalf("ticket after refund: %+v", s)
	}
	stub.refundParams = payments.RefundParams{}

	platform := router(auth.User{ID: adminID, Username: "admin", Role: auth.RoleAdmin})
	if one := listAt(platform, "/api/admin/ticket-purchases?org_id="+strconv.FormatInt(other, 10)); len(one) != 1 || !one[otherTicket].Refundable {
		t.Fatalf("Other's sales for the platform admin: %+v", one)
	}
	if all := list(platform); len(all) != 6 || all[hosted].OrgName != "Acme" || !all[hosted].Refundable {
		t.Fatalf("every sale: %+v", all)
	}
	if code := refund(platform, ticket); code != http.StatusConflict || stub.refundParams.ChargeID != "" {
		t.Fatalf("second refund of a ticket: %d %+v", code, stub.refundParams)
	}
	if code := refund(platform, otherTicket); code != http.StatusAccepted ||
		stub.refundParams.ChargeID != "ch_other" || !stub.refundParams.RefundApplicationFee {
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
