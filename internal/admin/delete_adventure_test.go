package admin_test

import (
	"context"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/dbtest"
)

// An author's dev event is a test run: it goes with the adventure. A
// real event still blocks the delete.
func TestDeleteAdventureTakesDevEvents(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"tested", "booked"} {
		if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ($1, 'Walk', $2)`, slug, orgID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, dev)
		VALUES ('tested', $1, 'running', 'lt-dev', 'Dev · author', TRUE), ('booked', $1, 'scheduled', 'lt-real', 'Saturday', FALSE)
	`, orgID); err != nil {
		t.Fatal(err)
	}

	if err := admin.DeleteAdventureBySlug(ctx, db, "tested"); err != nil {
		t.Fatalf("an adventure with only a dev event: %v", err)
	}
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE adventure_slug = 'tested'`).Scan(&left); err != nil || left != 0 {
		t.Errorf("dev events left: %d (%v)", left, err)
	}
	if err := admin.DeleteAdventureBySlug(ctx, db, "booked"); err == nil {
		t.Error("an adventure with a real event: expected a refusal")
	}
}
