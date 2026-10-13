package player

import (
	"context"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
)

// The lobby's start time reaches the phone with its zone, so a phone
// in Stockholm doesn't read 11:00 UTC as 11:00 local.
func TestWireTime(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-16 11:00:00":  "2026-10-16T11:00:00Z",
		"2026-10-16T11:00:00Z": "2026-10-16T11:00:00Z",
		"soon":                 "soon",
	} {
		if got := wireTime(in); got != want {
			t.Errorf("wireTime(%q) = %q, want %q", in, got, want)
		}
	}
}

// A stray far-off fix let through after a quiet spell doesn't lock the
// phone out: its next honest fix, back where it was, counts. A real
// teleport from a steady position is still refused.
func TestPlausibleNextPing_AfterStrayFix(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	var orgID, eventID, teamID, slotID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name)
		VALUES ('demo', $1, 'running', 'lt-pings', 'X') RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'T', 'tok-pings') RETURNING id`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'P', 'slot-pings') RETURNING id`, teamID).Scan(&slotID); err != nil {
		t.Fatal(err)
	}
	ping := func(lat, lng float64, ago time.Duration) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts) VALUES ($1, $2, $3, $4, $5, $6)`,
			slotID, teamID, eventID, lat, lng, time.Now().Add(-ago).UTC().Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}
	const zoneLat, zoneLng = 59.3293, 18.0686
	const farLat, farLng = 59.4400, 18.0686 // ~12 km north

	// At the zone, then quiet for three minutes, then a stray far fix.
	ping(zoneLat, zoneLng, 4*time.Minute)
	ping(farLat, farLng, 20*time.Second)
	if !plausibleNextPing(ctx, db, slotID, zoneLat, zoneLng) {
		t.Error("an honest fix after a stray one is refused")
	}

	// Standing at the zone, pinging steadily: a jump far away is refused.
	ping(zoneLat, zoneLng, 15*time.Second)
	ping(zoneLat, zoneLng, 10*time.Second)
	if plausibleNextPing(ctx, db, slotID, farLat, farLng) {
		t.Error("a teleport from a steady position is let through")
	}
}
