package admin_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/results"
)

// An event's results fingerprint is mailed to its organisation's admins
// once the event is over, once; an event no team played in has nothing
// to keep, and one that ended before the window is left alone.
func TestAnchorEventResults(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	db := f.h.DB
	ctx := context.Background()

	var teamID int64
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'Gulls', 'tk-gulls') RETURNING id`, f.eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	var empty, old int64
	for _, id := range []*int64{&empty, &old} {
		if err := db.QueryRow(`
			INSERT INTO events(adventure_slug, org_id, state, live_token, name)
			VALUES ('treasure', $1, 'completed', 'lt-' || md5(random()::text), 'Other') RETURNING id`, f.orgID).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := results.Append(ctx, db, f.eventID, teamID, results.TeamFinished, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := results.Append(ctx, db, f.eventID, 0, results.EventCompleted, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := results.Append(ctx, db, empty, 0, results.EventCancelled, nil); err != nil {
		t.Fatal(err)
	}
	var oldTeam int64
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'Old', 'tk-old') RETURNING id`, old).Scan(&oldTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := results.Append(ctx, db, old, oldTeam, results.TeamFinished, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := results.Append(ctx, db, old, 0, results.EventCompleted, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		ALTER TABLE event_result_log DISABLE TRIGGER event_result_log_append_only;
		UPDATE event_result_log SET server_time = now() - interval '8 days' WHERE event_id = ` + itoa64(old) + `;
		ALTER TABLE event_result_log ENABLE TRIGGER event_result_log_append_only;`); err != nil {
		t.Fatal(err)
	}

	f.h.AnchorEventResults(ctx)
	f.h.AnchorEventResults(ctx)
	got := f.mailTo("boss@acme.com")
	if len(got) != 1 || !strings.Contains(got[0], "Treasure") || !strings.Contains(got[0], "fingerprint") {
		t.Fatalf("org admin mail = %v, want one fingerprint for Treasure", got)
	}
	var fp string
	if err := db.QueryRow(`SELECT fingerprint FROM event_result_anchors WHERE event_id = $1`, f.eventID).Scan(&fp); err != nil || fp != results.Sealed(ctx, db, f.eventID) {
		t.Fatalf("anchor = %q (%v)", fp, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM event_result_anchors WHERE event_id IN ($1, $2)`, empty, old).Scan(&n); err != nil || n != 0 {
		t.Fatalf("anchors for the empty and the old event = %d (%v)", n, err)
	}
}
