package server_test

import (
	"context"
	"testing"

	"github.com/klahr/zonventure/internal/settings"
)

// TestPricing: only the platform admin reads and sets the prices, a bad
// price is refused, and a saved one is in force at once (the console's
// /whoami already shows it).
func TestPricing(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	t.Cleanup(func() { settings.LoadPricing(context.Background(), nil) })

	boss := newClient(t, ts)
	login(t, boss, "boss", "s3cret")
	if s := boss.do("GET", "/api/admin/pricing", nil, nil); s != 403 {
		t.Fatalf("org admin GET = %d, want 403", s)
	}

	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	var got struct {
		TeamCents  int64  `json:"team_cents"`
		TeamPhones int64  `json:"team_phones"`
		PhoneCents int64  `json:"phone_cents"`
		Currency   string `json:"currency"`
		TopUps     []struct {
			AmountCents int64 `json:"amount_cents"`
			BonusCents  int64 `json:"bonus_cents"`
		} `json:"top_ups"`
	}
	if s := root.do("GET", "/api/admin/pricing", nil, &got); s != 200 || got.TeamCents != 4900 || got.TeamPhones != 3 || got.PhoneCents != 1500 || got.Currency != "sek" || len(got.TopUps) != 4 {
		t.Fatalf("defaults = %d %+v", s, got)
	}
	valid := func() map[string]any {
		return map[string]any{"team_cents": 3900, "team_phones": 4, "phone_cents": 1000, "party_free_teams": 5, "party_games": 3,
			"overdraft_cents": 20000, "credit_new_cents": 50000, "credit_grown_cents": 500000, "credit_grown_after": 3,
			"top_ups": []map[string]any{{"amount_cents": 50000, "bonus_cents": 0}}}
	}
	for name, spoil := range map[string]func(map[string]any){
		"a negative price":   func(m map[string]any) { m["team_cents"] = -1 },
		"no phone in a team": func(m map[string]any) { m["team_phones"] = 0 },
		"no top-ups":         func(m map[string]any) { m["top_ups"] = []any{} },
		"a top-up twice": func(m map[string]any) {
			m["top_ups"] = []map[string]any{{"amount_cents": 50000}, {"amount_cents": 50000}}
		},
	} {
		m := valid()
		spoil(m)
		if s := root.do("PUT", "/api/admin/pricing", m, nil); s != 400 {
			t.Errorf("%s = %d, want 400", name, s)
		}
	}
	if s := root.do("PUT", "/api/admin/pricing", valid(), nil); s != 200 {
		t.Fatalf("save = %d", s)
	}

	var who struct {
		Pricing struct {
			TeamCents  int64  `json:"team_cents"`
			TeamPhones int64  `json:"team_phones"`
			Currency   string `json:"currency"`
		} `json:"pricing"`
	}
	if s := boss.do("GET", "/api/auth/whoami", nil, &who); s != 200 || who.Pricing.TeamCents != 3900 || who.Pricing.TeamPhones != 4 || who.Pricing.Currency != "SEK" {
		t.Fatalf("whoami after save = %d %+v", s, who.Pricing)
	}
}
