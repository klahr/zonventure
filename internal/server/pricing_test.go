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
		PlayerCents int64  `json:"player_cents"`
		FreePlayers int64  `json:"free_players"`
		Currency    string `json:"currency"`
	}
	if s := root.do("GET", "/api/admin/pricing", nil, &got); s != 200 || got.PlayerCents != 4900 || got.FreePlayers != 15 || got.Currency != "sek" {
		t.Fatalf("defaults = %d %+v", s, got)
	}
	if s := root.do("PUT", "/api/admin/pricing", map[string]any{"player_cents": -1}, nil); s != 400 {
		t.Fatalf("negative price = %d, want 400", s)
	}
	if s := root.do("PUT", "/api/admin/pricing", map[string]any{"fee_percent_bps": 10001}, nil); s != 400 {
		t.Fatalf("fee over 100 %% = %d, want 400", s)
	}
	set := map[string]any{"player_cents": 3900, "free_players": 10, "hosted_block_cents": 9900, "fee_percent_bps": 500}
	if s := root.do("PUT", "/api/admin/pricing", set, nil); s != 200 {
		t.Fatalf("save = %d", s)
	}

	var who struct {
		Pricing struct {
			PlayerCents int    `json:"player_cents"`
			Free        int    `json:"free_players_per_event"`
			Currency    string `json:"currency"`
		} `json:"pricing"`
	}
	if s := boss.do("GET", "/api/auth/whoami", nil, &who); s != 200 || who.Pricing.PlayerCents != 3900 || who.Pricing.Free != 10 || who.Pricing.Currency != "SEK" {
		t.Fatalf("whoami after save = %d %+v", s, who.Pricing)
	}
}
