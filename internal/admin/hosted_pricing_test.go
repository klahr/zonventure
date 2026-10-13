package admin_test

import (
	"context"
	"encoding/json"
	"github.com/klahr/zonventure/internal/settings"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/admin"
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

// TestHosted_HostIt: an arranger buys a hosted event of their own
// adventure at the pass price per block; once paid it is their org's,
// with the teams they bought, and the host link is in their mail.
func TestHosted_HostIt(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.h.Pricing = func() settings.Pricing { return settings.Pricing{HostedBlockCents: 14900} }
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/host", f.h.HostAdventure)

	if w := call(t, f.console, "POST", "/api/admin/adventures/nope/host", map[string]any{"blocks": 1}); w.Code != http.StatusNotFound {
		t.Errorf("someone else's adventure: %d", w.Code)
	}
	if w := call(t, f.console, "POST", "/api/admin/adventures/treasure/host", map[string]any{"blocks": -1}); w.Code != http.StatusBadRequest {
		t.Errorf("negative blocks: %d", w.Code)
	}
	w := call(t, f.console, "POST", "/api/admin/adventures/treasure/host", map[string]any{"name": "Office quiz", "blocks": 2})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("host it: %d %s", w.Code, w.Body)
	}
	var amount, org int64
	if err := f.h.DB.QueryRow(`SELECT amount_cents, buyer_org_id FROM ticket_purchases WHERE kind = 'hosted'`).Scan(&amount, &org); err != nil {
		t.Fatal(err)
	}
	if amount != 29800 || org != f.orgID {
		t.Errorf("purchase: %d cents for org %d", amount, org)
	}

	f.pay(t, "hosted")
	var name string
	var blocks int
	if err := f.h.DB.QueryRow(`SELECT party_name, hosted_blocks FROM events WHERE party AND org_id = $1`, f.orgID).Scan(&name, &blocks); err != nil {
		t.Fatal(err)
	}
	if name != "Office quiz" || blocks != 2 {
		t.Errorf("event: %q with %d blocks", name, blocks)
	}
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil {
		t.Fatal("no host link mailed")
	}
	if got := f.teamsLimit(t, m[1]); got != 30 {
		t.Errorf("teams limit = %d, want 30", got)
	}

	// No blocks: free for FreeTeams teams a game, made at once, unpaid.
	w = call(t, f.console, "POST", "/api/admin/adventures/treasure/host", map[string]any{"name": "Small party", "blocks": 0})
	if w.Code != http.StatusCreated || strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("free: %d %s", w.Code, w.Body)
	}
	var purchases int
	if err := f.h.DB.QueryRow(`SELECT COUNT(*) FROM ticket_purchases WHERE kind = 'hosted'`).Scan(&purchases); err != nil {
		t.Fatal(err)
	}
	if purchases != 1 {
		t.Errorf("free hosting made a purchase: %d", purchases)
	}
	m = regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil {
		t.Fatal("no host link mailed for the free event")
	}
	if got := f.teamsLimit(t, m[1]); got != admin.FreeTeams {
		t.Errorf("free teams limit = %d, want %d", got, admin.FreeTeams)
	}
}

// TestHosted_AddBlocks: a host buys 15 more teams per game; a refund
// takes them back.
func TestHosted_AddBlocks(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.h.Pricing = func() settings.Pricing { return settings.Pricing{HostedBlockCents: 14900} }
	_, token := f.book(t, 5)

	if w := call(t, f.public, "POST", "/api/party/"+token+"/blocks", map[string]any{"blocks": 10}); w.Code != http.StatusBadRequest {
		t.Errorf("past the most teams: %d", w.Code)
	}
	w := call(t, f.public, "POST", "/api/party/"+token+"/blocks", map[string]any{"blocks": 1})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("add teams: %d %s", w.Code, w.Body)
	}
	var email string
	_ = f.h.DB.QueryRow(`SELECT buyer_email FROM ticket_purchases WHERE kind = 'hosted_blocks'`).Scan(&email)
	if email != "parent@example.com" {
		t.Errorf("buyer = %q, want the party's booker", email)
	}
	if got := f.teamsLimit(t, token); got != 15 {
		t.Errorf("before paying: %d teams", got)
	}
	id := f.pay(t, "hosted_blocks")
	if got := f.teamsLimit(t, token); got != 30 {
		t.Fatalf("after paying: %d teams, want 30", got)
	}

	if _, err := f.h.DB.Exec(`UPDATE ticket_purchases SET stripe_charge_id = 'ch_blocks' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := admin.ExportedHandleChargeRefunded(f.h, context.Background(), buildChargeRefundedEvent(t, "ch_blocks")); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if got := f.teamsLimit(t, token); got != 15 {
		t.Errorf("after the refund: %d teams, want 15", got)
	}
}

// TestHosted_Free: where hosting costs nothing, Host it and more teams
// happen at once.
func TestHosted_Free(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.consoleRoute("POST", "/api/admin/adventures/{slug}/host", f.h.HostAdventure)
	w := call(t, f.console, "POST", "/api/admin/adventures/treasure/host", map[string]any{"blocks": 1})
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
	m := regexp.MustCompile(`/party/([A-Za-z0-9_-]+)`).FindStringSubmatch(f.mail.messages[len(f.mail.messages)-1].Text)
	if m == nil {
		t.Fatal("no host link mailed")
	}
	if w := call(t, f.public, "POST", "/api/party/"+m[1]+"/blocks", map[string]any{"blocks": 2}); w.Code != http.StatusOK {
		t.Fatalf("free teams: %d %s", w.Code, w.Body)
	}
	if got := f.teamsLimit(t, m[1]); got != 45 {
		t.Errorf("teams limit = %d, want 45", got)
	}
}

// TestHosted_PartyFee: where the platform takes a fee, its cut of a
// party is the pass price, and a party must cost more than that, in the
// pass's currency.
func TestHosted_PartyFee(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	f.h.Pricing = func() settings.Pricing { return settings.Pricing{HostedBlockCents: 14900, FeePercentBPS: 500} }
	if _, err := f.h.DB.Exec(`UPDATE organizations SET stripe_connect_account_id = 'acct_acme', stripe_connect_charges_enabled = TRUE WHERE id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]map[string]any{
		"below the fee": {"enabled": true, "base_cents": 10000, "per_kid_cents": 2000, "currency": "sek", "max_kids": 10},
		"other money":   {"enabled": true, "base_cents": 50000, "currency": "eur", "max_kids": 10},
	} {
		if w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	f.book(t, 5)
	var fee int64
	if err := f.h.DB.QueryRow(`SELECT application_fee_cents FROM ticket_purchases WHERE kind = 'party'`).Scan(&fee); err != nil {
		t.Fatal(err)
	}
	if fee != 14900 {
		t.Errorf("application fee = %d, want the pass price", fee)
	}
}
