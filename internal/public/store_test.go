package public_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/public"
)

// TestTicketTeamToken_Pending: a freshly-recorded purchase whose
// webhook hasn't fired yet returns state=pending and an empty token.
func TestTicketTeamToken_Pending(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	eventID := seedEvent(t, db)
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, stripe_session_id)
		VALUES ($1, 'b@x.com', 4, 2500, 'eur', 'cs_test_pending')
		RETURNING id
	`, eventID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	body := callTeamToken(t, db, purchaseID, "cs_test_pending", http.StatusOK)
	if body.State != "pending" {
		t.Errorf("state = %q, want pending", body.State)
	}
	if body.TeamToken != "" {
		t.Errorf("team_token = %q, want empty", body.TeamToken)
	}
}

// TestTicketTeamToken_PaidReturnsToken: the happy path. Purchase
// is paid, team is linked, session_id matches → state=paid plus the
// team_token the JS uses to redirect.
func TestTicketTeamToken_PaidReturnsToken(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	eventID := seedEvent(t, db)
	var teamID int64
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, token) VALUES ($1, 'buyer', 'team-tok-paid')
		RETURNING id
	`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency,
		                             state, paid_at, team_id, stripe_session_id)
		VALUES ($1, 'b@x.com', 1, 1000, 'eur', 'paid', now_text(), $2, 'cs_test_paid')
		RETURNING id
	`, eventID, teamID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	body := callTeamToken(t, db, purchaseID, "cs_test_paid", http.StatusOK)
	if body.State != "paid" {
		t.Errorf("state = %q, want paid", body.State)
	}
	if body.TeamToken != "team-tok-paid" {
		t.Errorf("team_token = %q, want team-tok-paid", body.TeamToken)
	}
}

// TestTicketTeamToken_WrongSessionIs404: the security-critical case.
// A scraper guesses a real purchase id but supplies a session_id that
// doesn't match. Endpoint MUST 404 (not 403, not 200 with empty
// token) — indistinguishable from "no such purchase".
func TestTicketTeamToken_WrongSessionIs404(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	eventID := seedEvent(t, db)
	var teamID int64
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, token) VALUES ($1, 'buyer', 'super-secret-token')
		RETURNING id
	`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency,
		                             state, paid_at, team_id, stripe_session_id)
		VALUES ($1, 'b@x.com', 1, 1000, 'eur', 'paid', now_text(), $2, 'cs_test_real')
		RETURNING id
	`, eventID, teamID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	rec := callTeamTokenRaw(t, db, purchaseID, "cs_test_DIFFERENT")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "super-secret-token") {
		t.Errorf("body leaked team token: %s", rec.Body.String())
	}
}

// TestTicketTeamToken_MissingSessionIs404: dropping the session_id
// query param entirely also 404s — same shape as the auth-fail case
// so a stripped URL doesn't reveal whether the id is real.
func TestTicketTeamToken_MissingSessionIs404(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	eventID := seedEvent(t, db)
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, stripe_session_id)
		VALUES ($1, 'b@x.com', 1, 1000, 'eur', 'cs_test_x')
		RETURNING id
	`, eventID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	publicH := &public.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Get("/api/store/ticket/{purchase_id}/team-token", publicH.TicketTeamToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/store/ticket/"+strconv.FormatInt(purchaseID, 10)+"/team-token", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (missing session_id)", rec.Code)
	}
}

// TestTicketTeamToken_BogusIdIs404: a non-existent purchase id 404s
// regardless of session id supplied.
func TestTicketTeamToken_BogusIdIs404(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	rec := callTeamTokenRaw(t, db, 999999, "cs_anything")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestTicketTeamToken_FailedNoToken: a purchase that flipped to
// failed surfaces state=failed (so the JS shows the "contact seller"
// message) and returns no token even if one happens to be linked.
func TestTicketTeamToken_FailedNoToken(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	eventID := seedEvent(t, db)
	var purchaseID int64
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency,
		                             state, stripe_session_id)
		VALUES ($1, 'b@x.com', 1, 1000, 'eur', 'failed', 'cs_test_failed')
		RETURNING id
	`, eventID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	body := callTeamToken(t, db, purchaseID, "cs_test_failed", http.StatusOK)
	if body.State != "failed" {
		t.Errorf("state = %q, want failed", body.State)
	}
	if body.TeamToken != "" {
		t.Errorf("team_token = %q, want empty on failed state", body.TeamToken)
	}
}

// seedEvent inserts an org, an adventure, and a public-for-sale
// running event, returning the event id. Each test wants its own
// schema so this is a per-call helper rather than a fixture struct.
func seedEvent(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo Adv', $1)
	`, orgID); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name,
		                   public_for_sale, ticket_price_cents, ticket_currency, ticket_team_size)
		VALUES ('demo', $1, 'running', 'lt-' || md5(random()::text), 'X',
		        TRUE, 2500, 'eur', 4)
		RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	return eventID
}

// callTeamToken fires GET /api/store/ticket/{id}/team-token?session_id=...
// expecting a JSON 200 with the standard headers, and decodes the body.
func callTeamToken(t *testing.T, db *sql.DB, purchaseID int64, sessionID string, wantStatus int) public.TicketTeamTokenDTO {
	t.Helper()
	rec := callTeamTokenRaw(t, db, purchaseID, sessionID)
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, wantStatus, rec.Body.String())
	}
	var dto public.TicketTeamTokenDTO
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
	}
	return dto
}

func callTeamTokenRaw(t *testing.T, db *sql.DB, purchaseID int64, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	publicH := &public.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Get("/api/store/ticket/{purchase_id}/team-token", publicH.TicketTeamToken)
	rec := httptest.NewRecorder()
	url := "/api/store/ticket/" + strconv.FormatInt(purchaseID, 10) + "/team-token?session_id=" + sessionID
	req := httptest.NewRequest("GET", url, nil)
	r.ServeHTTP(rec, req)
	return rec
}
