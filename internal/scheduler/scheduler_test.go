package scheduler

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
)

func seedScheduled(t *testing.T, db *sql.DB, publish bool, at time.Time) int64 {
	t.Helper()
	var orgID, advID, eventID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1) RETURNING id`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	if publish {
		g := &graph.Graph{
			Nodes: []graph.Node{
				{Slug: "start", Type: graph.Start},
				{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
					Blocks:  graph.LegacyBlocks("?", nil),
					Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}},
				}},
				{Slug: "end", Type: graph.End},
			},
			Edges: []graph.Edge{{From: "start", FromPort: graph.PortOut, To: "q"}, {From: "q", FromPort: "solved", To: "end"}},
		}
		if _, _, err := graphstore.Publish(context.Background(), db, advID, 0, g, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, scheduled_at, live_token, name)
		VALUES ('demo', $1, 'scheduled', $2, 'lt-sched', 'X')
		RETURNING id
	`, orgID, at.UTC().Format("2006-01-02 15:04:05")).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	return eventID
}

func eventState(t *testing.T, db *sql.DB, eventID int64) string {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM events WHERE id = $1`, eventID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTick_StartsDuePublishedEvent(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedScheduled(t, db, true, time.Now().Add(-time.Minute))

	tick(context.Background(), db, nil)
	if s := eventState(t, db, eventID); s != "running" {
		t.Fatalf("state = %q, want running", s)
	}
}

// An unpublished adventure leaves the event scheduled and tells the
// arranger once, however many sweeps see it.
func TestTick_UnpublishedStaysScheduledAndRecordsOnce(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedScheduled(t, db, false, time.Now().Add(-time.Minute))

	for i := 0; i < 3; i++ {
		tick(context.Background(), db, nil)
	}
	if s := eventState(t, db, eventID); s != "scheduled" {
		t.Fatalf("state = %q, want scheduled", s)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_activity WHERE event_id = $1 AND kind = 'graph.start.failed'`, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("graph.start.failed rows = %d, want 1", n)
	}
}

// scheduled_at is UTC; a session TimeZone ahead of UTC must not make a
// future event due early.
func TestTick_ScheduledAtIsUTC(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER DATABASE ` + name + ` SET TimeZone = 'Asia/Karachi'`); err != nil {
		t.Fatal(err)
	}
	db.SetMaxIdleConns(0)
	db.SetMaxIdleConns(5)
	var tz string
	if err := db.QueryRow(`SHOW TimeZone`).Scan(&tz); err != nil || tz != "Asia/Karachi" {
		t.Fatalf("TimeZone = %q (%v), want Asia/Karachi", tz, err)
	}
	eventID := seedScheduled(t, db, true, time.Now().Add(30*time.Minute))

	tick(context.Background(), db, nil)
	if s := eventState(t, db, eventID); s != "scheduled" {
		t.Fatalf("state = %q, want scheduled until scheduled_at (UTC) arrives", s)
	}
}
