package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/settings"
)

// pay lands the checkout of the latest purchase of kind, as the webhook
// would.
func (f *partyFixture) pay(t *testing.T, kind string) int64 {
	t.Helper()
	var id int64
	if err := f.h.DB.QueryRow(`SELECT id FROM ticket_purchases WHERE kind = $1 ORDER BY id DESC LIMIT 1`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ev := buildSessionCompletedEvent(t, id, "cs_"+kind+"_"+strconv.FormatInt(id, 10))
	if err := admin.ExportedHandleTicketCheckoutCompleted(f.h, context.Background(), ev); err != nil {
		t.Fatalf("fulfil %s: %v", kind, err)
	}
	return id
}

func (f *partyFixture) teamsLimit(t *testing.T, token string) int {
	t.Helper()
	var v struct {
		TeamsLimit int `json:"teams_limit"`
	}
	w := call(t, f.public, "GET", "/api/party/"+token, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return v.TeamsLimit
}

// balance is the org's billing balance, in öre.
func (f *partyFixture) balance(t *testing.T) int64 {
	t.Helper()
	var b int64
	if err := f.h.DB.QueryRow(`SELECT COALESCE(SUM(amount_cents), 0) FROM billing_entries WHERE org_id = $1`, f.orgID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestHosted_HostIt: an arranger hosts their own adventure at once, with
// nothing to pay: each team that joins is charged to the org, and none
// can join past what its balance pays for.
func TestHosted_HostIt(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.h.Pricing = func() settings.Pricing { return settings.Pricing{TeamCents: 4900, TeamPhones: 3} }
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/host", f.h.HostAdventure)

	if w := call(t, f.console, "POST", "/api/admin/adventures/nope/host", map[string]any{}); w.Code != http.StatusNotFound {
		t.Errorf("someone else's adventure: %d", w.Code)
	}
	w := call(t, f.console, "POST", "/api/admin/adventures/treasure/host", map[string]any{"name": "Office quiz"})
	if w.Code != http.StatusCreated || strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("host it: %d %s", w.Code, w.Body)
	}
	var purchases int
	if err := f.h.DB.QueryRow(`SELECT COUNT(*) FROM ticket_purchases`).Scan(&purchases); err != nil {
		t.Fatal(err)
	}
	if purchases != 0 {
		t.Errorf("hosting made %d purchases", purchases)
	}
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil {
		t.Fatal("no host link mailed")
	}
	token := m[1]
	if got := f.teamsLimit(t, token); got != 0 {
		t.Errorf("teams limit = %d, want none", got)
	}
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"teams": 5}); w.Code != http.StatusConflict {
		t.Errorf("more teams for a game without a limit: %d", w.Code)
	}

	player := f.host(t, token).playerToken()
	// No balance and no overdraft: the first team can't join.
	if w := f.join(t, player, "Owls", 1); w.Code != http.StatusPaymentRequired {
		t.Fatalf("join with nothing to pay: %d %s", w.Code, w.Body)
	}
	if _, err := f.h.DB.Exec(`INSERT INTO billing_entries(org_id, kind, amount_cents, currency) VALUES ($1, 'top_up', 10000, 'SEK')`, f.orgID); err != nil {
		t.Fatal(err)
	}
	for _, team := range []string{"Owls", "Foxes"} {
		if w := f.join(t, player, team, 1); w.Code != http.StatusCreated {
			t.Fatalf("join %s: %d %s", team, w.Code, w.Body)
		}
	}
	if w := f.join(t, player, "Bears", 1); w.Code != http.StatusPaymentRequired {
		t.Errorf("a third team on 100 kr: %d", w.Code)
	}
	if got := f.balance(t); got != 10000-2*4900 {
		t.Errorf("balance = %d, want two teams charged", got)
	}
}

// TestHosted_AddTeams: a party's host buys more teams per game at the
// team price; a refund takes them back.
func TestHosted_AddTeams(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	_, token := f.book(t, 5)
	f.h.Pricing = func() settings.Pricing { return settings.Pricing{TeamCents: 4900, TeamPhones: 3} }

	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"teams": 200}); w.Code != http.StatusBadRequest {
		t.Errorf("past the most teams: %d", w.Code)
	}
	w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"teams": 3})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("add teams: %d %s", w.Code, w.Body)
	}
	var email string
	var amount int64
	_ = f.h.DB.QueryRow(`SELECT buyer_email, amount_cents FROM ticket_purchases WHERE kind = 'hosted_teams'`).Scan(&email, &amount)
	if email != "parent@example.com" || amount != 3*4900 {
		t.Errorf("purchase by %q of %d, want the party's booker paying 3 teams", email, amount)
	}
	if got := f.teamsLimit(t, token); got != 5 {
		t.Errorf("before paying: %d teams", got)
	}
	id := f.pay(t, "hosted_teams")
	if got := f.teamsLimit(t, token); got != 8 {
		t.Fatalf("after paying: %d teams, want 8", got)
	}

	if _, err := f.h.DB.Exec(`UPDATE ticket_purchases SET stripe_charge_id = 'ch_teams' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := admin.ExportedHandleChargeRefunded(f.h, context.Background(), buildChargeRefundedEvent(t, "ch_teams")); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if got := f.teamsLimit(t, token); got != 5 {
		t.Errorf("after the refund: %d teams, want 5", got)
	}
}

// TestHosted_Free: where teams cost nothing, Host it happens at once and
// a party's teams are added at once.
func TestHosted_Free(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/host", f.h.HostAdventure)
	w := call(t, f.console, "POST", "/api/admin/adventures/treasure/host", map[string]any{})
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), "event_id") {
		t.Fatalf("free host it: %d %s", w.Code, w.Body)
	}
	var name string
	if err := f.h.DB.QueryRow(`SELECT name FROM events WHERE org_id = $1 AND party ORDER BY id DESC LIMIT 1`, f.orgID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, strconv.Itoa(time.Now().Year())) || strings.TrimSpace(name) == strconv.Itoa(time.Now().Year()) {
		t.Errorf("default name = %q, want the adventure's title and the date", name)
	}
	_, token := f.book(t, 4)
	if w := call(t, f.public, "POST", "/api/party/"+token+"/teams", map[string]any{"teams": 2}); w.Code != http.StatusOK {
		t.Fatalf("free teams: %d %s", w.Code, w.Body)
	}
	if got := f.teamsLimit(t, token); got != 6 {
		t.Errorf("teams limit = %d, want 6", got)
	}
}
