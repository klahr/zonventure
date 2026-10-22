package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/billing"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/settings"
	"github.com/klahr/zonventure/internal/ws"
)

// The site's own organisation makes a self-guided event free: its price
// is 0, the store gives a ticket for an email address with no payment
// provider at all, once per address, and the team that plays costs the
// organisation nothing. Only its admins set billing, and no other
// organisation's; a free event takes no price.
func TestFreeEvent(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Zonventure', 'zonventure') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, public_for_sale, ticket_price_cents, ticket_currency, ticket_team_size)
		VALUES ('demo', $1, 'running', 'lt-trial', 'Trial', TRUE, 19900, 'sek', 2) RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Hub: ws.NewHub(), Mailer: mail, PublicURL: "https://zv.example.com", HomeOrg: "elsewhere"}
	r := chi.NewRouter()
	r.Patch("/api/admin/events/{eventID}", h.UpdateEvent)
	r.Post("/api/store/buy", h.BuyTicket)
	do := func(method, path string, user *auth.User, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if user != nil {
			req = req.WithContext(auth.WithUser(req.Context(), *user))
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	owner := &auth.User{ID: 1, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID}
	member := &auth.User{ID: 2, Username: "helper", Role: auth.RoleOrgMember, OrgID: &orgID}
	eventPath := "/api/admin/events/" + itoa64(eventID)

	if w := do("PATCH", eventPath, owner, `{"billing": "free"}`); w.Code != http.StatusForbidden {
		t.Errorf("not the home organisation: status %d", w.Code)
	}
	h.HomeOrg = "zonventure"
	if w := do("PATCH", eventPath, member, `{"billing": "free"}`); w.Code != http.StatusForbidden {
		t.Errorf("member sets billing: status %d", w.Code)
	}
	if w := do("PATCH", eventPath, owner, `{"billing": "free", "ticket_price_cents": 100}`); w.Code != http.StatusConflict {
		t.Errorf("billing and price together: status %d", w.Code)
	}
	if w := do("PATCH", eventPath, owner, `{"billing": "free"}`); w.Code != http.StatusNoContent {
		t.Fatalf("make free: status %d: %s", w.Code, w.Body.String())
	}
	if w := do("PATCH", eventPath, owner, `{"ticket_price_cents": 100}`); w.Code != http.StatusConflict {
		t.Errorf("price on a free event: status %d", w.Code)
	}
	var price int64
	if err := db.QueryRow(`SELECT ticket_price_cents FROM events WHERE id = $1`, eventID).Scan(&price); err != nil || price != 0 {
		t.Fatalf("price = %d, %v; want 0", price, err)
	}
	dbtest.ApproveListings(t, db)

	buy := `{"event_id": ` + itoa64(eventID) + `, "buyer_email": "Curious@Example.com"}`
	w := do("POST", "/api/store/buy", nil, buy)
	if w.Code != http.StatusOK {
		t.Fatalf("free ticket: status %d: %s", w.Code, w.Body.String())
	}
	var resp admin.BuyTicketResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var teamID int64
	var token string
	var slots int
	if err := db.QueryRow(`
		SELECT t.id, t.token, (SELECT COUNT(*) FROM player_slots WHERE team_id = t.id)
		FROM teams t WHERE t.event_id = $1 AND t.email = 'curious@example.com' AND t.paid_phones IS NULL
	`, eventID).Scan(&teamID, &token, &slots); err != nil {
		t.Fatal(err)
	}
	if resp.CheckoutURL != "/team/"+token || slots != 2 {
		t.Errorf("checkout_url = %q, slots = %d", resp.CheckoutURL, slots)
	}
	if len(mail.messages) != 1 {
		t.Errorf("mails = %d, want 1", len(mail.messages))
	}
	if w := do("POST", "/api/store/buy", nil, buy); w.Code != http.StatusConflict {
		t.Errorf("second ticket for the address: status %d", w.Code)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := billing.ChargeTeam(context.Background(), tx, teamID, settings.Pricing{TeamCents: 4900, TeamPhones: 4}); err != nil {
		t.Fatal(err)
	}
	var charged int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM billing_entries WHERE team_id = $1`, teamID).Scan(&charged); err != nil || charged != 0 {
		t.Errorf("billing entries = %d, %v; want none", charged, err)
	}
}
