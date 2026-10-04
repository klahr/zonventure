package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// A free ticket makes a team of the event's ticket size and emails the
// tester the buyer's email — team link and voucher code — and the reply
// carries both; another organisation's event is refused.
func TestGiveFreeTicket(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, otherOrg int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Other') RETURNING id`).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, public_for_sale, ticket_team_size)
		VALUES ('demo', $1, 'running', 'lt-free', 'Self-guided', TRUE, 3) RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	mail := &fakeMailer{}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Mailer: mail, PublicURL: "https://zv.example.com"}
	r := chi.NewRouter()
	r.Post("/api/admin/events/{eventID}/free-ticket", h.GiveFreeTicket)
	post := func(org int64, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/admin/events/"+itoa64(eventID)+"/free-ticket", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{ID: 1, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &org}))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := post(orgID, `{"email": "not an email"}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad email: status %d", w.Code)
	}
	if w := post(otherOrg, `{"email": "tester@example.com"}`); w.Code != http.StatusForbidden {
		t.Errorf("another organisation's event: status %d", w.Code)
	}
	w := post(orgID, `{"email": "Tester@Example.com"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TeamID      int64  `json:"team_id"`
		TeamURL     string `json:"team_url"`
		PlayURL     string `json:"play_url"`
		VoucherCode string `json:"voucher_code"`
		Emailed     bool   `json:"emailed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.TeamURL, "https://zv.example.com/team/") || len(resp.VoucherCode) != 6 ||
		resp.PlayURL != "https://zv.example.com/play?c="+resp.VoucherCode || !resp.Emailed {
		t.Errorf("response = %+v", resp)
	}
	var email, voucher string
	var slots int
	if err := db.QueryRow(`SELECT email, voucher_code, (SELECT COUNT(*) FROM player_slots WHERE team_id = teams.id) FROM teams WHERE id = $1`, resp.TeamID).
		Scan(&email, &voucher, &slots); err != nil {
		t.Fatal(err)
	}
	if email != "tester@example.com" || voucher != resp.VoucherCode || slots != 3 {
		t.Errorf("team: email %q voucher %q slots %d", email, voucher, slots)
	}
	if len(mail.messages) != 1 || mail.messages[0].To[0] != "tester@example.com" ||
		!strings.Contains(mail.messages[0].Text, resp.VoucherCode) || !strings.Contains(mail.messages[0].Text, resp.TeamURL) {
		t.Errorf("email = %+v", mail.messages)
	}
}
