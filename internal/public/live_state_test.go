package public_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/public"
)

type liveSnap struct {
	State         string `json:"state"`
	StartedAt     string `json:"started_at"`
	CompletedAt   string `json:"completed_at"`
	PausedSeconds int    `json:"paused_seconds"`
	PausedAt      string `json:"paused_at"`
	Round         int    `json:"round"`
	ResultsHeld   bool   `json:"results_held"`
	Teams         []struct {
		Name      string   `json:"name"`
		Rank      int      `json:"rank"`
		Score     *float64 `json:"score"`
		Withdrawn bool     `json:"withdrawn"`
	} `json:"teams"`
}

func getLive(t *testing.T, db *sql.DB, eventID int64) liveSnap {
	t.Helper()
	var token string
	if err := db.QueryRow(`SELECT live_token FROM events WHERE id = $1`, eventID).Scan(&token); err != nil {
		t.Fatal(err)
	}
	h := &public.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Get("/api/public/live/{token}", h.Live)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/public/live/"+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("live = %d: %s", rec.Code, rec.Body.String())
	}
	var s liveSnap
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// A withdrawn team drops below every team still playing on a lane
// board, as it already does on a graph board, and is flagged.
func TestLive_WithdrawnTeamsRankLast(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedEvent(t, db)
	// Alpha is created first, so it would win every tiebreak.
	if _, err := db.Exec(`
		INSERT INTO teams(event_id, name, token, created_at, withdrawn_at)
		VALUES ($1, 'Alpha', 'tok-a', '2026-09-28 09:00:00', '2026-09-28 10:00:00'),
		       ($1, 'Beta',  'tok-b', '2026-09-28 09:05:00', NULL)
	`, eventID); err != nil {
		t.Fatal(err)
	}

	s := getLive(t, db, eventID)
	if len(s.Teams) != 2 {
		t.Fatalf("teams = %d, want 2", len(s.Teams))
	}
	if s.Teams[0].Name != "Beta" || s.Teams[0].Withdrawn {
		t.Errorf("first = %+v, want Beta still playing", s.Teams[0])
	}
	if s.Teams[1].Name != "Alpha" || !s.Teams[1].Withdrawn || s.Teams[1].Rank != 2 {
		t.Errorf("second = %+v, want withdrawn Alpha at rank 2", s.Teams[1])
	}
}

// The board needs the pause totals to leave paused time out of the
// elapsed clock and to freeze it while paused.
func TestLive_ReportsPauses(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedEvent(t, db)
	if _, err := db.Exec(`
		UPDATE events SET started_at = '2026-09-28 09:00:00', paused_seconds = 90 WHERE id = $1
	`, eventID); err != nil {
		t.Fatal(err)
	}
	if s := getLive(t, db, eventID); s.PausedSeconds != 90 || s.PausedAt != "" {
		t.Errorf("running: paused_seconds=%d paused_at=%q, want 90 and none", s.PausedSeconds, s.PausedAt)
	}

	if _, err := db.Exec(`
		UPDATE events SET state = 'paused', paused_at = '2026-09-28 10:00:00' WHERE id = $1
	`, eventID); err != nil {
		t.Fatal(err)
	}
	s := getLive(t, db, eventID)
	if s.State != "paused" || s.PausedSeconds != 90 || s.PausedAt != "2026-09-28 10:00:00" {
		t.Errorf("paused: %+v, want paused_seconds 90 and paused_at set", s)
	}
}

// A party's board shows its latest game alone: that game's teams, timed
// from its start less its own pauses, and completed once the host
// finishes it — while the party itself runs on. A game that holds its
// results lists its teams unranked.
func TestLive_PartyShowsItsGame(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	eventID := seedEvent(t, db)
	if _, err := db.Exec(`
		UPDATE events SET party = TRUE, party_hold_results = FALSE, current_round = 2,
		       started_at = '2026-09-28 09:00:00', paused_seconds = 100 WHERE id = $1
	`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO event_rounds(event_id, round, started_at, finished_at, paused_from)
		VALUES ($1, 1, '2026-09-28 09:00:00Z', '2026-09-28 10:00:00Z', 0),
		       ($1, 2, '2026-09-28 11:00:00Z', NULL, 60)
	`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO teams(event_id, name, token, round)
		VALUES ($1, 'Old', 'tok-o', 1), ($1, 'New', 'tok-n', 2), ($1, 'Newer', 'tok-m', 2)
	`, eventID); err != nil {
		t.Fatal(err)
	}

	s := getLive(t, db, eventID)
	if s.Round != 2 || s.State != "running" || s.StartedAt != "2026-09-28 11:00:00" || s.PausedSeconds != 40 {
		t.Errorf("open game: %+v", s)
	}
	if len(s.Teams) != 2 || s.Teams[0].Rank == 0 {
		t.Errorf("open game's teams: %+v", s.Teams)
	}

	if _, err := db.Exec(`UPDATE events SET party_hold_results = TRUE WHERE id = $1`, eventID); err != nil {
		t.Fatal(err)
	}
	s = getLive(t, db, eventID)
	if !s.ResultsHeld || s.Teams[0].Name != "New" || s.Teams[0].Rank != 0 || s.Teams[0].Score != nil {
		t.Errorf("held game: %+v", s)
	}

	if _, err := db.Exec(`
		UPDATE event_rounds SET finished_at = '2026-09-28 11:30:00Z', paused_seconds = 30 WHERE event_id = $1 AND round = 2
	`, eventID); err != nil {
		t.Fatal(err)
	}
	s = getLive(t, db, eventID)
	if s.State != "completed" || s.CompletedAt != "2026-09-28 11:30:00" || s.PausedSeconds != 30 || s.ResultsHeld || s.Teams[0].Rank != 1 {
		t.Errorf("finished game: %+v", s)
	}
}

// Every viewer of a board asks for the tracks on every push; within the
// cache window they share one build instead of each re-aggregating the
// event's ping history.
func TestTracks_SharedWithinCacheWindow(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	eventID := seedEvent(t, db)
	token := seedPings(t, db, eventID, start, 25*time.Second, 5*time.Second)

	h := &public.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Get("/api/public/live/{token}/tracks", h.Tracks)
	points := func() int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/public/live/"+token+"/tracks", nil))
		var resp trackResp
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Tracks) != 1 {
			t.Fatalf("tracks: %v %s", err, rec.Body.String())
		}
		return len(resp.Tracks[0].Points)
	}

	first := points()
	if _, err := db.Exec(`
		INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts)
		SELECT slot_id, team_id, event_id, lat, lng, '2026-09-28 10:05:00'
		FROM location_pings WHERE event_id = $1 LIMIT 1
	`, eventID); err != nil {
		t.Fatal(err)
	}
	if again := points(); again != first {
		t.Errorf("points after a new ping = %d, want the cached %d", again, first)
	}
}
