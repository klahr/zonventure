package server_test

import (
	"database/sql"
	"testing"
)

// A running self-guided event keeps the adventure version it started
// on until its organiser moves it to the latest. Not for sale, it moves
// at once; for sale, the new version waits for the listing's approval,
// and new teams keep the reviewed one until then.
func TestEventVersionUpdate(t *testing.T) {
	boss, db, _, orgID, eventID, cleanup := redemptionStack(t)
	defer cleanup()

	publish := func(version int) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`
			INSERT INTO adventure_graphs(adventure_id, version, graph)
			VALUES ((SELECT id FROM adventures WHERE slug = 'demo'), $1, '{}') RETURNING id`, version).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE adventures SET published_graph_id = $1 WHERE slug = 'demo'`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	v1 := publish(1)
	if _, err := db.Exec(`UPDATE events SET graph_id = $1 WHERE id = $2`, v1, eventID); err != nil {
		t.Fatal(err)
	}
	type versionView struct {
		Version   int    `json:"version"`
		Next      int    `json:"next"`
		Latest    int    `json:"latest"`
		CanUpdate bool   `json:"can_update"`
		Reviewed  bool   `json:"reviewed"`
		Listing   string `json:"listing_review"`
	}
	path := "/api/admin/events/" + itoa(eventID) + "/version"
	var v versionView
	if s, e := doErr(boss, "POST", path+"/update", nil); s != 409 || e.Error == "" {
		t.Errorf("update on the latest = %d %+v, want 409", s, e)
	}

	// Not for sale: nothing reviews it, so it moves at once.
	v2 := publish(2)
	if s := boss.do("GET", path, nil, &v); s != 200 || !v.CanUpdate || v.Reviewed || v.Version != 1 || v.Latest != 2 {
		t.Fatalf("version = %d %+v", s, v)
	}
	if s := boss.do("POST", path+"/update", nil, &v); s != 200 || v.Version != 2 || v.Next != 0 {
		t.Fatalf("update = %d %+v, want on version 2", s, v)
	}

	// For sale, with its listing approved on version 2.
	if _, err := db.Exec(`
		UPDATE events SET ticket_price_cents = 19900, ticket_currency = 'sek', listing_review = 'approved' WHERE id = $1;
	`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO event_listings(event_id, name, blurb, price_cents, currency, team_size, graph_id)
		SELECT id, COALESCE(name, ''), public_blurb, ticket_price_cents, ticket_currency, ticket_team_size, $2 FROM events WHERE id = $1
	`, eventID, v2); err != nil {
		t.Fatal(err)
	}
	v3 := publish(3)
	if s := boss.do("POST", path+"/update", nil, &v); s != 200 || v.Version != 2 || v.Next != 3 || v.Listing != "draft" {
		t.Fatalf("update a listed event = %d %+v, want version 3 waiting in a draft", s, v)
	}
	var playing sql.NullInt64
	if err := db.QueryRow(`SELECT graph_id FROM events WHERE id = $1`, eventID).Scan(&playing); err != nil || playing.Int64 != v2 {
		t.Errorf("before approval new teams start on %v, want version 2 (%d)", playing, v2)
	}

	// Two teams placed on version 2: one hasn't made a move, one has.
	team := func(name string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`INSERT INTO teams(event_id, name, token, recap_token) VALUES ($1, $2, $3, $4) RETURNING id`,
			eventID, name, "tok-"+name, "recap-"+name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO team_graph_state(team_id, event_id, graph_id, current_node) VALUES ($1, $2, $3, 'first')`, id, eventID, v2); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO team_node_state(team_id, node) VALUES ($1, 'first')`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	idle, begun := team("idle"), team("begun")
	if _, err := db.Exec(`UPDATE team_node_state SET attempts = 1 WHERE team_id = $1`, begun); err != nil {
		t.Fatal(err)
	}

	// A verified creator's submission is approved at once, and moves it.
	if _, err := db.Exec(`UPDATE organizations SET verified_creator = TRUE WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	if s, e := doErr(boss, "POST", "/api/admin/events/"+itoa(eventID)+"/listing/submit", map[string]bool{"attest": true}); s != 200 {
		t.Fatalf("submit = %d %+v", s, e)
	}
	var listed sql.NullInt64
	if err := db.QueryRow(`SELECT e.graph_id, l.graph_id FROM events e JOIN event_listings l ON l.event_id = e.id WHERE e.id = $1`, eventID).Scan(&playing, &listed); err != nil {
		t.Fatal(err)
	}
	if playing.Int64 != v3 || listed.Int64 != v3 {
		t.Errorf("after approval the event plays %v and lists %v, want version 3 (%d)", playing, listed, v3)
	}
	// The idle team is placed again, on version 3, when it next opens
	// its link; the one that began keeps version 2.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM team_graph_state WHERE team_id = $1`, idle).Scan(&n); err != nil || n != 0 {
		t.Errorf("idle team still placed (%d, %v), want unplaced", n, err)
	}
	var kept int64
	if err := db.QueryRow(`SELECT graph_id FROM team_graph_state WHERE team_id = $1`, begun).Scan(&kept); err != nil || kept != v2 {
		t.Errorf("team that began plays %d (%v), want version 2 (%d)", kept, err, v2)
	}
	v = versionView{}
	if s := boss.do("GET", path, nil, &v); s != 200 || v.Version != 3 || v.Next != 0 || v.CanUpdate {
		t.Errorf("version after approval = %d %+v", s, v)
	}
}
