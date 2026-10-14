package server_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type voucherOfferView struct {
	Allowed    bool  `json:"allowed"`
	PriceCents int64 `json:"price_cents"`
	Batches    []struct {
		ID       int64 `json:"id"`
		Codes    int   `json:"codes"`
		Used     int   `json:"used"`
		TeamSize int   `json:"team_size"`
		Voided   bool  `json:"voided"`
	} `json:"batches"`
}

// A publisher prints pay-later codes for a self-guided event: refused
// without invoice billing; a code's first use starts a team and is
// charged once; the next use opens the same team; voiding the batch
// stops its unused codes; a platform admin marks the uses invoiced.
func TestVoucherBatchFlow(t *testing.T) {
	boss, db, hc, orgID, eventID, cleanup := redemptionStack(t)
	defer cleanup()
	base := fmt.Sprintf("/api/admin/events/%d/voucher-batches", eventID)

	var offer voucherOfferView
	if s := boss.do("GET", base, nil, &offer); s != 200 || offer.Allowed || offer.PriceCents <= 0 {
		t.Fatalf("offer: %d %+v", s, offer)
	}
	if s, e := doErr(boss, "POST", base, map[string]any{"count": 3}); s != 403 || e.Reason != "not_invoice" {
		t.Fatalf("without invoice billing: %d %+v", s, e)
	}
	if _, err := db.Exec(`UPDATE organizations SET trusted = 1 WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	if s, e := doErr(boss, "POST", base, map[string]any{"count": 5000}); s != 400 || e.Reason != "invalid_count" {
		t.Fatalf("too many: %d %+v", s, e)
	}
	if s := boss.do("POST", base, map[string]any{"count": 3, "team_size": 4, "label": "Bookstores"}, &offer); s != 200 || len(offer.Batches) != 1 || offer.Batches[0].Codes != 3 || offer.Batches[0].TeamSize != 4 {
		t.Fatalf("create: %d %+v", s, offer)
	}
	batchID := offer.Batches[0].ID

	// The codes, as the print shop gets them.
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/admin/voucher-batches/%d/codes.csv", boss.ts.URL, batchID), nil)
	resp, err := boss.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if resp.StatusCode != 200 || len(lines) != 4 || lines[0] != "code,used,used_at,team" {
		t.Fatalf("csv: %d\n%s", resp.StatusCode, body)
	}
	code1 := strings.Split(lines[1], ",")[0]
	code2 := strings.Split(lines[2], ",")[0]

	// On another organisation's store the code is unknown, and stays unused.
	player := newClient(t, boss.ts)
	if s, _ := doErr(player, "POST", "/api/play/voucher", map[string]string{"code": code1, "org": "someone-else"}); s != 404 {
		t.Fatalf("another org's store: %d, want 404", s)
	}
	var unused bool
	if err := db.QueryRow(`SELECT activated_at IS NULL FROM voucher_codes WHERE code = $1`, code1).Scan(&unused); err != nil || !unused {
		t.Fatalf("code used by another store: %v %v", unused, err)
	}

	// A buyer types the first code at its own store: a team of four starts.
	var hit struct {
		TeamURL string `json:"team_url"`
	}
	if s := player.do("POST", "/api/play/voucher", map[string]string{"code": strings.ToLower(code1), "org": "testorg"}, &hit); s != 200 || !strings.HasPrefix(hit.TeamURL, "/team/") {
		t.Fatalf("first use: %d %+v", s, hit)
	}
	first := hit.TeamURL
	var slots, price int
	if err := db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM player_slots s WHERE s.team_id = c.team_id), c.price_cents
		FROM voucher_codes c WHERE c.code = $1`, code1).Scan(&slots, &price); err != nil || slots != 4 || price <= 0 {
		t.Fatalf("activated: slots %d price %d %v", slots, price, err)
	}
	// A teammate types the same code: the same team, charged once.
	if s := player.do("POST", "/api/play/voucher", map[string]string{"code": code1}, &hit); s != 200 || hit.TeamURL != first {
		t.Fatalf("second use: %d %q, want %q", s, hit.TeamURL, first)
	}
	var use struct {
		Codes       int   `json:"codes"`
		AmountCents int64 `json:"amount_cents"`
	}
	if s := boss.do("GET", "/api/admin/orgs/me/voucher-use", nil, &use); s != 200 || use.Codes != 1 || use.AmountCents != int64(price) {
		t.Fatalf("owed: %d %+v", s, use)
	}

	// Voided: the unused codes stop; the started team plays on.
	if s := boss.do("POST", fmt.Sprintf("/api/admin/voucher-batches/%d/void", batchID), map[string]any{}, &offer); s != 200 || !offer.Batches[0].Voided || offer.Batches[0].Used != 1 {
		t.Fatalf("void: %d %+v", s, offer)
	}
	if s, _ := doErr(player, "POST", "/api/play/voucher", map[string]string{"code": code2}); s != 409 {
		t.Fatalf("voided code: %d, want 409", s)
	}
	if s := player.do("POST", "/api/play/voucher", map[string]string{"code": code1}, &hit); s != 200 || hit.TeamURL != first {
		t.Fatalf("used code after void: %d", s)
	}
	if s, _ := doErr(player, "POST", "/api/play/voucher", map[string]string{"code": "ZZZZZ9"}); s != 404 {
		t.Fatalf("unknown code: %d", s)
	}

	// The platform admin invoices it.
	if _, err := db.Exec(`UPDATE users SET is_admin = true WHERE username = 'boss'`); err != nil {
		t.Fatal(err)
	}
	var orgs struct {
		Orgs []struct {
			OrgID int64 `json:"org_id"`
			Codes int   `json:"codes"`
		} `json:"orgs"`
	}
	if s := boss.do("GET", "/api/admin/voucher-billing", nil, &orgs); s != 200 || len(orgs.Orgs) != 1 || orgs.Orgs[0].Codes != 1 {
		t.Fatalf("to invoice: %d %+v", s, orgs)
	}
	var billed struct {
		Billed int `json:"billed"`
	}
	if s := boss.do("POST", fmt.Sprintf("/api/admin/organizations/%d/voucher-billing", orgID), map[string]string{"invoice_number": "2026-117"}, &billed); s != 200 || billed.Billed != 1 {
		t.Fatalf("mark billed: %d %+v", s, billed)
	}
	if s := boss.do("GET", "/api/admin/voucher-billing", nil, &orgs); s != 200 || len(orgs.Orgs) != 0 {
		t.Fatalf("after invoicing: %d %+v", s, orgs)
	}
	_ = hc
}

// Five phones type the same new code at once: one team, charged once,
// and every phone lands on it.
func TestVoucherBatchConcurrentFirstUse(t *testing.T) {
	boss, db, _, orgID, eventID, cleanup := redemptionStack(t)
	defer cleanup()
	if _, err := db.Exec(`UPDATE organizations SET trusted = 1 WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	var offer voucherOfferView
	if s := boss.do("POST", fmt.Sprintf("/api/admin/events/%d/voucher-batches", eventID), map[string]any{"count": 1}, &offer); s != 200 {
		t.Fatalf("create: %d", s)
	}
	var code string
	if err := db.QueryRow(`SELECT code FROM voucher_codes WHERE batch_id = $1`, offer.Batches[0].ID).Scan(&code); err != nil {
		t.Fatal(err)
	}
	type res struct {
		status int
		url    string
	}
	out := make(chan res, 5)
	for range 5 {
		go func() {
			c := newClient(t, boss.ts)
			var hit struct {
				TeamURL string `json:"team_url"`
			}
			s := c.do("POST", "/api/play/voucher", map[string]string{"code": code}, &hit)
			out <- res{s, hit.TeamURL}
		}()
	}
	var url string
	for range 5 {
		r := <-out
		if r.status != 200 || r.url == "" {
			t.Fatalf("a phone got %d", r.status)
		}
		if url != "" && r.url != url {
			t.Fatalf("two teams: %q and %q", url, r.url)
		}
		url = r.url
	}
	var teams int
	if err := db.QueryRow(`SELECT COUNT(*) FROM teams WHERE voucher_code = $1`, code).Scan(&teams); err != nil || teams != 1 {
		t.Fatalf("teams with the code: %d %v", teams, err)
	}
}
