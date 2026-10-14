package retention

import (
	"context"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// A posted letter's address goes once its days are up, as does a turned
// down box's; one still to post, or posted lately, stays. The order and
// its email stay either way.
func TestClearPostedAddresses(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()

	var orgID, partnerID, eventID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO partners(org_id, name) VALUES ($1, 'TrueStory') RETURNING id`, orgID).Scan(&partnerID))
	_, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID)
	must(err)
	must(db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name) VALUES ('demo', $1, 'running', 'lt-addr', 'Demo') RETURNING id`, orgID).Scan(&eventID))

	box := func(code, state, postedAgo, decidedAgo string) int64 {
		t.Helper()
		var id int64
		must(db.QueryRow(`
			INSERT INTO partner_redemptions(partner_id, product_name, security_code, name, email, street_address, postal_code, city, state, posted_at, decided_at)
			VALUES ($1, 'Walk', $2, 'Ada', 'ada@buyer.test', 'Storgatan 1', '111 22', 'Stockholm', $3,
			        CASE WHEN $4 = '' THEN NULL ELSE to_char(now() - $4::interval, 'YYYY-MM-DD HH24:MI:SS') END,
			        CASE WHEN $5 = '' THEN NULL ELSE to_char(now() - $5::interval, 'YYYY-MM-DD HH24:MI:SS') END)
			RETURNING id`, partnerID, code, state, postedAgo, decidedAgo).Scan(&id))
		return id
	}
	oldPosted := box("CODE00001", "approved", "100 days", "101 days")
	newPosted := box("CODE00002", "approved", "10 days", "11 days")
	toPost := box("CODE00003", "approved", "", "200 days")
	oldRejected := box("CODE00004", "rejected", "", "100 days")

	var oldPurchase, newPurchase int64
	purchase := func(postedAgo string) int64 {
		t.Helper()
		var id int64
		must(db.QueryRow(`
			INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, ship_name, street_address, postal_code, city, country, posted_at)
			VALUES ($1, 'bo@buyer.test', 4, 19900, 'sek', 'paid', 'Bo', 'Gatan 2', '222 33', 'Göteborg', 'SE',
			        to_char(now() - $2::interval, 'YYYY-MM-DD HH24:MI:SS'))
			RETURNING id`, eventID, postedAgo).Scan(&id))
		return id
	}
	oldPurchase, newPurchase = purchase("95 days"), purchase("5 days")

	clearPostedAddresses(ctx, db, 90)

	street := func(table string, id int64) (string, string) {
		t.Helper()
		var s, e string
		email := "email"
		if table == "ticket_purchases" {
			email = "buyer_email"
		}
		must(db.QueryRow(`SELECT street_address, `+email+` FROM `+table+` WHERE id = $1`, id).Scan(&s, &e))
		return s, e
	}
	for _, c := range []struct {
		table   string
		id      int64
		cleared bool
	}{
		{"partner_redemptions", oldPosted, true},
		{"partner_redemptions", newPosted, false},
		{"partner_redemptions", toPost, false},
		{"partner_redemptions", oldRejected, true},
		{"ticket_purchases", oldPurchase, true},
		{"ticket_purchases", newPurchase, false},
	} {
		s, e := street(c.table, c.id)
		if (s == "") != c.cleared {
			t.Errorf("%s %d: street %q, want cleared=%v", c.table, c.id, s, c.cleared)
		}
		if e == "" {
			t.Errorf("%s %d lost its email", c.table, c.id)
		}
	}
}
