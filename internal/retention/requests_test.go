package retention

import (
	"context"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// A request for a quote goes a year after its last change; one changed
// lately stays, however old it is.
func TestPruneOrgRequests(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	request := func(createdAgo, updatedAgo string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`
			INSERT INTO org_requests(org_id, answers, name, email, created_at, updated_at)
			VALUES ($1, '{}', 'Kim', 'kim@bolaget.test', now() - $2::interval, now() - $3::interval) RETURNING id
		`, orgID, createdAgo, updatedAgo).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	stale := request("400 days", "370 days")
	touched := request("400 days", "10 days")
	fresh := request("2 days", "2 days")

	pruneOrgRequests(ctx, db)

	for id, want := range map[int64]bool{stale: false, touched: true, fresh: true} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM org_requests WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if (n == 1) != want {
			t.Errorf("request %d kept = %v, want %v", id, n == 1, want)
		}
	}
}
