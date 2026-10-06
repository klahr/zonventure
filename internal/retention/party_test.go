package retention

import (
	"context"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// A party goes whole once its days are up — with the graph it played and
// the teams that played it — while its purchase stays, email blanked.
// Recent parties and ordinary events are left alone.
func TestPrunePartyEvents(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()

	var orgID, advID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1) RETURNING id`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	event := func(name, state, extra string, party bool) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`
			INSERT INTO events(adventure_slug, org_id, state, live_token, name, party, valid_until)
			VALUES ('demo', $1, $2, 'lt-' || $3, $3, $4, now() + interval '30 days') RETURNING id
		`, orgID, state, name, party).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if extra != "" {
			if _, err := db.Exec(`UPDATE events SET `+extra+` WHERE id = $1`, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	old := event("old", "completed", "completed_at = to_char(now() - interval '20 days', 'YYYY-MM-DD HH24:MI:SS')", true)
	recent := event("recent", "completed", "completed_at = to_char(now() - interval '2 days', 'YYYY-MM-DD HH24:MI:SS')", true)
	lapsed := event("lapsed", "scheduled", "valid_until = now() - interval '1 day'", true)
	waiting := event("waiting", "scheduled", "", true)
	playing := event("playing", "running", "", true)
	stuck := event("stuck", "running", "valid_until = now() - interval '20 days'", true)
	ordinary := event("ordinary", "completed", "completed_at = to_char(now() - interval '20 days', 'YYYY-MM-DD HH24:MI:SS')", false)

	// The old party played its own graph, and a team played it.
	var graphID, teamID, purchaseID int64
	if err := db.QueryRow(`INSERT INTO adventure_graphs(adventure_id, version, graph, label) VALUES ($1, $2, '{"nodes":[],"edges":[]}', 'party') RETURNING id`,
		advID, -old).Scan(&graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET graph_id = $1 WHERE id = $2`, graphID, old); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'Owls', 'tok-owls') RETURNING id`, old).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_graph_state(team_id, event_id, graph_id) VALUES ($1, $2, $3)`, teamID, old, graphID); err != nil {
		t.Fatal(err)
	}
	// And a later game of it played a copy of its own.
	var roundGraphID int64
	if err := db.QueryRow(`INSERT INTO adventure_graphs(adventure_id, version, graph, label, event_id) VALUES ($1, -nextval('round_graph_version_seq'), '{"nodes":[],"edges":[]}', 'party', $2) RETURNING id`,
		advID, old).Scan(&roundGraphID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO ticket_purchases(kind, event_id, buyer_email, team_size, amount_cents, currency, state)
		VALUES ('party', $1, 'parent@example.com', 5, 34400, 'sek', 'paid') RETURNING id
	`, old).Scan(&purchaseID); err != nil {
		t.Fatal(err)
	}

	prunePartyEvents(ctx, db, 14)

	for id, want := range map[int64]bool{old: false, recent: true, lapsed: false, waiting: true, ordinary: true, playing: true, stuck: false} {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM events WHERE id = $1`, id).Scan(&n)
		if (n == 1) != want {
			t.Errorf("event %d kept = %v, want %v", id, n == 1, want)
		}
	}
	var graphs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM adventure_graphs WHERE id IN ($1, $2)`, graphID, roundGraphID).Scan(&graphs)
	if graphs != 0 {
		t.Error("the party's graph outlived it")
	}
	var email string
	var eventGone bool
	if err := db.QueryRow(`SELECT buyer_email, event_id IS NULL FROM ticket_purchases WHERE id = $1`, purchaseID).Scan(&email, &eventGone); err != nil {
		t.Fatalf("purchase gone with the party: %v", err)
	}
	if email != "" || !eventGone {
		t.Errorf("purchase after purge: email %q, detached %v", email, eventGone)
	}
}
