package player

import (
	"context"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/ws"
)

// A phone wakes for its own team's moves and for anything event-wide;
// another team's move wakes it only once its own team has finished.
func TestForPhoneSkipsOtherTeamsProgress(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	var orgID, eventID, teamID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('o') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, language, org_id) VALUES ('a', 'a', '', '', 'en', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, started_at, live_token, name, show_map)
		VALUES ('a', $1, 'running', now_text(), 'tok', 'e', 0) RETURNING id`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 't', 'tt') RETURNING id`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}

	hub := ws.NewHub()
	h := &Handlers{DB: db, Hub: hub}
	events, unsub := hub.Subscribe(ws.EventTopic(eventID))
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := h.forPhone(ctx, auth.Slot{TeamID: teamID, EventID: eventID}, events)

	woke := func() bool {
		select {
		case <-out:
			return true
		case <-time.After(200 * time.Millisecond):
			return false
		}
	}
	topic := ws.EventTopic(eventID)
	cases := []struct {
		name string
		kind string
		data any
		want bool
	}{
		{"own move", ws.KindProgress, ws.Progress{Team: teamID}, true},
		{"other team's move", ws.KindProgress, ws.Progress{Team: teamID + 1}, false},
		{"other team scored, still playing", ws.KindProgress, ws.Progress{Team: teamID + 1, Scored: true}, false},
		{"event-wide change", "changed", nil, true},
	}
	for _, c := range cases {
		hub.Publish(topic, c.kind, c.data)
		if got := woke(); got != c.want {
			t.Errorf("%s: woke = %v, want %v", c.name, got, c.want)
		}
	}

	if _, err := db.Exec(`UPDATE teams SET completed_at = now_text() WHERE id = $1`, teamID); err != nil {
		t.Fatal(err)
	}
	hub.Publish(topic, ws.KindProgress, ws.Progress{Team: teamID + 1, Scored: true})
	if !woke() {
		t.Error("a finished team should wake when another team scores")
	}
}
