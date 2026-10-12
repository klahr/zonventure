package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/payments"
)

// A store ticket for an adventure that posts a letter: the buyer picks
// the letter's language, checkout asks for an address in one of the
// organisation's countries, and the paid purchase is an order to post
// beside the box redemptions, in the letter's language throughout.
func TestPostedTicket(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID, userID, advID, eventID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.QueryRow(`INSERT INTO organizations(name, ship_countries) VALUES ('Acme', '{SE,NO}') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.test', 1) RETURNING id`).Scan(&userID))
	must(db.QueryRow(`INSERT INTO adventures(slug, title, org_id, language) VALUES ('walk', 'Walk', $1, 'en') RETURNING id`, orgID).Scan(&advID))
	var pdf int64
	must(db.QueryRow(`
		INSERT INTO adventure_media(adventure_id, kind, mime_type, size_bytes, sha256, content)
		VALUES ($1, 'pdf', 'application/pdf', 5, 'x', '%PDF-') RETURNING id`, advID).Scan(&pdf))
	var graphID int64
	must(db.QueryRow(`
		INSERT INTO adventure_graphs(adventure_id, version, graph)
		VALUES ($1, 1, jsonb_build_object('translations', '{"sv": {"title": "Promenad"}}'::jsonb,
		  'print_materials', jsonb_build_array(jsonb_build_object('id', 'letter', 'name', 'Letter', 'delivery', 'posted',
		    'files', jsonb_build_object('en', $2::bigint, 'sv', $2::bigint)))))
		RETURNING id`, advID, pdf).Scan(&graphID))
	must(db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, graph_id,
		                   public_for_sale, ticket_price_cents, ticket_currency, ticket_team_size, public_blurb)
		VALUES ('walk', $1, 'running', 'lt-posted', 'Walk', $2, TRUE, 19900, 'sek', 4, 'B')
		RETURNING id`, orgID, graphID).Scan(&eventID))
	_, err := db.Exec(`INSERT INTO event_listings(event_id, name, blurb, price_cents, currency, team_size)
		VALUES ($1, 'Walk', 'B', 19900, 'sek', 4)`, eventID)
	must(err)

	provider := &recordingProvider{}
	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Provider: provider, Mailer: mail, PublicURL: "https://zv.test"}
	boss := auth.User{ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID}
	r := chi.NewRouter()
	r.Post("/api/store/buy", h.BuyTicket)
	r.Group(func(r chi.Router) {
		r.Use(injectUser(boss))
		r.Get("/api/admin/redemptions", h.ListRedemptions)
		r.Patch("/api/admin/ticket-purchases/{purchaseID}", h.UpdatePurchaseOrder)
	})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	if w := call("POST", "/api/store/buy", `{"event_id": `+itoa64(eventID)+`, "buyer_email": "ada@buyer.test", "letter_lang": "de"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("German letter = %d %s, want 400", w.Code, w.Body)
	}
	if w := call("POST", "/api/store/buy", `{"event_id": `+itoa64(eventID)+`, "buyer_email": "ada@buyer.test", "letter_lang": "sv"}`); w.Code != http.StatusOK {
		t.Fatalf("buy = %d %s", w.Code, w.Body)
	}
	if got := provider.checkout.ShipCountries; !slices.Equal(got, []string{"SE", "NO"}) {
		t.Errorf("checkout ships to %v, want the organisation's SE, NO", got)
	}
	var purchaseID int64
	var lang string
	must(db.QueryRow(`SELECT id, letter_lang FROM ticket_purchases WHERE event_id = $1`, eventID).Scan(&purchaseID, &lang))
	if lang != "sv" {
		t.Errorf("purchase letter_lang = %q, want sv", lang)
	}

	ev := buildSessionCompletedEvent(t, purchaseID, "cs_rec")
	ev.Checkout.Shipping = &payments.Shipping{Name: "Ada Buyer", Line1: "Storgatan 1", PostalCode: "111 22", City: "Stockholm", Country: "SE"}
	must(admin.ExportedHandleTicketCheckoutCompleted(h, context.Background(), ev))
	var street, teamLang string
	must(db.QueryRow(`SELECT tp.street_address, t.lang FROM ticket_purchases tp JOIN teams t ON t.id = tp.team_id WHERE tp.id = $1`, purchaseID).Scan(&street, &teamLang))
	if street != "Storgatan 1" || teamLang != "sv" {
		t.Errorf("paid purchase: street %q, team language %q", street, teamLang)
	}
	if len(mail.messages) != 1 || !strings.Contains(mail.messages[0].Text, "Storgatan 1") || !strings.Contains(mail.messages[0].Text, "Kuvertet skickas till") {
		t.Errorf("ticket email isn't Swedish with the address: %+v", mail.messages)
	}

	type order struct {
		Kind  string `json:"kind"`
		ID    int64  `json:"id"`
		Print []struct {
			Lang string `json:"lang"`
		} `json:"print"`
	}
	var list struct {
		Redemptions []order        `json:"redemptions"`
		Counts      map[string]int `json:"counts"`
	}
	w := call("GET", "/api/admin/redemptions?state=to_post", "")
	must(json.Unmarshal(w.Body.Bytes(), &list))
	if len(list.Redemptions) != 1 || list.Redemptions[0].Kind != "purchase" || list.Counts["to_post"] != 1 || len(list.Redemptions[0].Print) != 1 {
		t.Fatalf("to post = %s", w.Body)
	}
	if w := call("PATCH", "/api/admin/ticket-purchases/"+itoa64(purchaseID), `{"posted": true}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"posted_at":"2`) {
		t.Fatalf("mark posted = %d %s", w.Code, w.Body)
	}
	w = call("GET", "/api/admin/redemptions?state=to_post", "")
	must(json.Unmarshal(w.Body.Bytes(), &list))
	if len(list.Redemptions) != 0 || list.Counts["to_post"] != 0 {
		t.Errorf("to post after posting = %s", w.Body)
	}

	// An event that posts nothing asks for neither language nor address.
	_, err = db.Exec(`UPDATE events SET print_delivery = '{"letter": "emailed"}' WHERE id = $1`, eventID)
	must(err)
	provider.checkout = payments.CheckoutParams{}
	if w := call("POST", "/api/store/buy", `{"event_id": `+itoa64(eventID)+`, "buyer_email": "bo@buyer.test"}`); w.Code != http.StatusOK {
		t.Fatalf("buy without a letter = %d %s", w.Code, w.Body)
	}
	if provider.checkout.ShipCountries != nil {
		t.Errorf("checkout without a posted letter asks for an address: %v", provider.checkout.ShipCountries)
	}
}
