package activity_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/dbtest"
)

func seedPausedEvent(t *testing.T, db *sql.DB, pausedFor string) int64 {
	t.Helper()
	var orgID, eventID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, paused_seconds, paused_at)
		VALUES ('demo', $1, 'paused', 'lt-pause', 'X', 30,
		        to_char((now() AT TIME ZONE 'UTC') - $2::interval, 'YYYY-MM-DD HH24:MI:SS'))
		RETURNING id
	`, orgID, pausedFor).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	return eventID
}

// Ending a pause adds it to the total and closes it.
func TestClosePause_FoldsOpenPause(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedPausedEvent(t, db, "90 seconds")

	if _, err := db.Exec(`UPDATE events SET state='running', `+activity.ClosePause+` WHERE id=$1`, eventID); err != nil {
		t.Fatal(err)
	}
	var secs int
	var pausedAt sql.NullString
	if err := db.QueryRow(`SELECT paused_seconds, paused_at FROM events WHERE id=$1`, eventID).Scan(&secs, &pausedAt); err != nil {
		t.Fatal(err)
	}
	if secs < 119 || secs > 122 {
		t.Errorf("paused_seconds = %d, want 30 + ~90", secs)
	}
	if pausedAt.Valid {
		t.Errorf("paused_at = %q, want NULL once the pause is closed", pausedAt.String)
	}

	// With no open pause it changes nothing.
	if _, err := db.Exec(`UPDATE events SET `+activity.ClosePause+` WHERE id=$1`, eventID); err != nil {
		t.Fatal(err)
	}
	var again int
	if err := db.QueryRow(`SELECT paused_seconds FROM events WHERE id=$1`, eventID).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != secs {
		t.Errorf("paused_seconds = %d after a second close, want %d", again, secs)
	}
}

// A paused event that completes (its last team withdrawn, say) closes
// the pause too.
func TestMaybeCompleteEvent_ClosesPause(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedPausedEvent(t, db, "60 seconds")
	if _, err := db.Exec(`INSERT INTO teams(event_id, name, token, completed_at) VALUES ($1, 'T', 'tok-p', now_text())`, eventID); err != nil {
		t.Fatal(err)
	}

	done, err := activity.MaybeCompleteEvent(context.Background(), db, eventID)
	if err != nil || !done {
		t.Fatalf("MaybeCompleteEvent = %v, %v; want true", done, err)
	}
	var secs int
	var pausedAt sql.NullString
	if err := db.QueryRow(`SELECT paused_seconds, paused_at FROM events WHERE id=$1`, eventID).Scan(&secs, &pausedAt); err != nil {
		t.Fatal(err)
	}
	if secs < 89 || secs > 92 || pausedAt.Valid {
		t.Errorf("paused_seconds = %d, paused_at valid = %v; want ~90 and NULL", secs, pausedAt.Valid)
	}
}
