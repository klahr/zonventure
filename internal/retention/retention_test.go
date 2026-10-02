package retention

import (
	"context"
	"database/sql"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// Collapsing a user's last org must keep its payment records (with the
// org / event detached) and blank the ticket buyer's email.
func TestHardDeleteUser_RetainsPurchases(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var userID, orgID, eventID, purchaseID, ticketID int64
	if err := db.QueryRow(`INSERT INTO users(username, password_hash) VALUES ('gone', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name)
		VALUES ('demo', $1, 'completed', 'lt-ret', 'X') RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO purchases(org_id, buyer_user_id, adventure_slug, max_players, amount_cents, currency, provider, state)
		VALUES ($1, $2, 'demo', 10, 2500, 'EUR', 'stripe', 'paid') RETURNING id
	`, orgID, userID).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, stripe_charge_id)
		VALUES ($1, 'buyer@example.com', 2, 1500, 'EUR', 'paid', 'ch_1') RETURNING id
	`, eventID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}

	hardDeleteUser(context.Background(), db, userID, "gone")

	var orgLeft int
	if err := db.QueryRow(`SELECT COUNT(*) FROM organizations WHERE id = $1`, orgID).Scan(&orgLeft); err != nil {
		t.Fatal(err)
	}
	if orgLeft != 0 {
		t.Fatalf("org not collapsed")
	}
	var pOrg sql.NullInt64
	var amount int
	if err := db.QueryRow(`SELECT org_id, amount_cents FROM purchases WHERE id = $1`, purchaseID).Scan(&pOrg, &amount); err != nil {
		t.Fatalf("purchase row gone: %v", err)
	}
	if pOrg.Valid || amount != 2500 {
		t.Fatalf("purchase org_id = %v amount = %d, want NULL / 2500", pOrg, amount)
	}
	var tEvent sql.NullInt64
	var email, charge string
	if err := db.QueryRow(`SELECT event_id, buyer_email, stripe_charge_id FROM ticket_purchases WHERE id = $1`, ticketID).Scan(&tEvent, &email, &charge); err != nil {
		t.Fatalf("ticket row gone: %v", err)
	}
	if tEvent.Valid || email != "" || charge != "ch_1" {
		t.Fatalf("ticket event_id = %v email = %q charge = %q, want NULL / \"\" / ch_1", tEvent, email, charge)
	}
}
