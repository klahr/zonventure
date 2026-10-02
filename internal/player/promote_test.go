package player

import (
	"context"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
)

// A player's poll must not flip a due event to running when its
// adventure has nothing published; the scheduler reports that instead.
func TestMaybePromoteScheduled_UnpublishedStaysScheduled(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, eventID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute).UTC().Format("2006-01-02 15:04:05")
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, 'scheduled', $2, 'lt-promote', 'X')
		RETURNING id
	`, orgID, at).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	promoted, err := maybePromoteScheduled(context.Background(), db, nil, eventID, at)
	if err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM events WHERE id = $1`, eventID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if promoted || state != "scheduled" {
		t.Fatalf("promoted = %v, state = %q; want false, scheduled", promoted, state)
	}
}
