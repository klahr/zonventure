package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
)

func TestParseSlotTimestamp(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{"wire format", "2026-04-25 12:34:56",
			time.Date(2026, 4, 25, 12, 34, 56, 0, time.UTC), true},
		{"rfc3339 with Z", "2026-04-25T12:34:56Z",
			time.Date(2026, 4, 25, 12, 34, 56, 0, time.UTC), true},
		{"rfc3339 with offset", "2026-04-25T14:34:56+02:00",
			time.Date(2026, 4, 25, 12, 34, 56, 0, time.UTC), true},
		{"junk", "not a timestamp", time.Time{}, false},
		{"empty", "", time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSlotTimestamp(c.in)
			if c.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("expected error for %q", c.in)
			}
			if c.ok && !got.Equal(c.want) {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

// Each request of a signed-in phone stamps it as seen, at most every
// seenEvery: a stamp seconds old stands, an old one is renewed.
func TestSeenSlot(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, eventID, teamID, slotID int64
	mustRow(t, db.QueryRow(`INSERT INTO organizations(name) VALUES ('Seen Org') RETURNING id`), &orgID)
	mustRow(t, db.QueryRow(`INSERT INTO adventures(slug, title, description, author, language, org_id) VALUES ('seen', 'seen', '', '', 'en', $1) RETURNING id`, orgID), new(int64))
	mustRow(t, db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name, show_map) VALUES ('seen', $1, 'running', 'tok-seen', 'seen', 0) RETURNING id`, orgID), &eventID)
	mustRow(t, db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'Crew', 'tok-crew') RETURNING id`, eventID), &teamID)
	mustRow(t, db.QueryRow(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'Ann', 'tok-ann') RETURNING id`, teamID), &slotID)

	h := SeenSlot(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := func() string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/player/play", nil)
		h.ServeHTTP(httptest.NewRecorder(), r.WithContext(WithSlot(r.Context(), Slot{ID: slotID})))
		var seen sql.NullString
		mustRow(t, db.QueryRow(`SELECT last_seen_at FROM player_slots WHERE id = $1`, slotID), &seen)
		return seen.String
	}
	if seen := request(); seen == "" {
		t.Fatal("the first request should stamp the phone as seen")
	}
	recent := time.Now().UTC().Add(-10 * time.Second).Format(time.DateTime)
	if _, err := db.Exec(`UPDATE player_slots SET last_seen_at = $2 WHERE id = $1`, slotID, recent); err != nil {
		t.Fatal(err)
	}
	if seen := request(); seen != recent {
		t.Fatalf("seen %q, want the stamp ten seconds old, %q, kept", seen, recent)
	}
	if _, err := db.Exec(`UPDATE player_slots SET last_seen_at = '2026-01-01 00:00:00' WHERE id = $1`, slotID); err != nil {
		t.Fatal(err)
	}
	if seen := request(); seen <= recent {
		t.Fatalf("seen %q, want renewed", seen)
	}
	// A stamp seenEvery old to the second — the socket's last tick — is
	// renewed by this one, not left for the tick after.
	due := time.Now().UTC().Add(-seenEvery).Format(time.DateTime)
	if _, err := db.Exec(`UPDATE player_slots SET last_seen_at = $2 WHERE id = $1`, slotID, due); err != nil {
		t.Fatal(err)
	}
	if seen := request(); seen <= due {
		t.Fatalf("seen %q, want the stamp seenEvery old, %q, renewed", seen, due)
	}
	// Forgotten — its stamp cleared — after the request found its
	// session: the request leaves it unstamped.
	if _, err := db.Exec(`UPDATE player_slots SET name = '(forgotten)', last_seen_at = NULL WHERE id = $1`, slotID); err != nil {
		t.Fatal(err)
	}
	if seen := request(); seen != "" {
		t.Fatalf("a forgotten slot stamped as seen at %q", seen)
	}
}

// An open socket is stamped only while its session lets its phone play:
// once the session is gone — here signed out — the socket's context is
// cancelled, closing it, and the stamping stops.
func TestSeenSlotSocketEnds(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	jitter := seenJitter
	seenTick = 20 * time.Millisecond
	seenJitter = func(time.Duration) time.Duration { return 0 }
	defer func() { seenTick, seenJitter = seenEvery, jitter }()
	var orgID, eventID, teamID, slotID int64
	mustRow(t, db.QueryRow(`INSERT INTO organizations(name) VALUES ('Sock Org') RETURNING id`), &orgID)
	mustRow(t, db.QueryRow(`INSERT INTO adventures(slug, title, description, author, language, org_id) VALUES ('sock', 'sock', '', '', 'en', $1) RETURNING id`, orgID), new(int64))
	mustRow(t, db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name, show_map) VALUES ('sock', $1, 'running', 'tok-sock', 'sock', 0) RETURNING id`, orgID), &eventID)
	mustRow(t, db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'Crew', 'tok-sock-crew') RETURNING id`, eventID), &teamID)
	mustRow(t, db.QueryRow(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'Ann', 'tok-sock-ann') RETURNING id`, teamID), &slotID)
	token, err := newSlotSession(context.Background(), db, slotID)
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	open := make(chan struct{})
	h := SlotMiddleware(db, nil)(RequireSlot(SeenSlot(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(open)
		<-r.Context().Done()
		close(closed)
	}))))
	r := httptest.NewRequest(http.MethodGet, "/ws/play", nil)
	r.Header.Set("Upgrade", "websocket")
	r.AddCookie(&http.Cookie{Name: SlotCookieName, Value: token})
	go h.ServeHTTP(httptest.NewRecorder(), r)
	<-open

	// While the session stands, the socket stays open.
	select {
	case <-closed:
		t.Fatal("the socket closed while its session stood")
	case <-time.After(100 * time.Millisecond):
	}
	if err := LogoutSlot(context.Background(), db, token); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the socket is still open after its session was signed out")
	}
}

func mustRow(t *testing.T, row *sql.Row, dst ...any) {
	t.Helper()
	if err := row.Scan(dst...); err != nil {
		t.Fatal(err)
	}
}
