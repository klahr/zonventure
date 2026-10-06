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

type trackResp struct {
	Tracks []struct {
		TeamID int64 `json:"team_id"`
		Points []struct {
			Lat      float64 `json:"lat"`
			ServerTS string  `json:"server_ts"`
		} `json:"points"`
	} `json:"tracks"`
}

// seedPings gives one team of eventID a ping every step from start for
// the given span, and returns the event's live token.
func seedPings(t *testing.T, db *sql.DB, eventID int64, start time.Time, span, step time.Duration) string {
	t.Helper()
	var token string
	if err := db.QueryRow(`UPDATE events SET show_map = 1 WHERE id = $1 RETURNING live_token`, eventID).Scan(&token); err != nil {
		t.Fatal(err)
	}
	var teamID, slotID int64
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'T', 'tok-tracks') RETURNING id`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'p', 'slot-tracks') RETURNING id`, teamID).Scan(&slotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts)
		SELECT $1, $2, $3, 59.0, 18.0, to_char(ts, 'YYYY-MM-DD HH24:MI:SS')
		FROM generate_series($4::timestamp, $4::timestamp + $5::interval, $6::interval) AS ts
	`, slotID, teamID, eventID, start.Format("2006-01-02 15:04:05"),
		span.String(), step.String()); err != nil {
		t.Fatal(err)
	}
	return token
}

func getTracks(t *testing.T, db *sql.DB, token string) trackResp {
	t.Helper()
	h := &public.Handlers{DB: db, Lib: adventure.New(db)}
	r := chi.NewRouter()
	r.Get("/api/public/live/{token}/tracks", h.Tracks)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/public/live/"+token+"/tracks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("tracks = %d: %s", rec.Code, rec.Body.String())
	}
	var resp trackResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(resp.Tracks))
	}
	return resp
}

// A short event keeps the 10 s buckets, each stamped with its start.
func TestTracks_ShortEventKeeps10sBuckets(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	token := seedPings(t, db, seedEvent(t, db), start, 25*time.Second, 5*time.Second)

	pts := getTracks(t, db, token).Tracks[0].Points
	want := []string{"2026-09-28 10:00:00", "2026-09-28 10:00:10", "2026-09-28 10:00:20"}
	if len(pts) != len(want) {
		t.Fatalf("points = %d, want %d", len(pts), len(want))
	}
	for i, p := range pts {
		if p.ServerTS != want[i] {
			t.Errorf("point %d at %q, want %q", i, p.ServerTS, want[i])
		}
	}
}

// The track is resent on every push, so a long event must not grow it
// without bound: a 3 h event at one ping per 5 s is 2,160 pings.
func TestTracks_LongEventIsBounded(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	token := seedPings(t, db, seedEvent(t, db), start, 3*time.Hour, 5*time.Second)

	pts := getTracks(t, db, token).Tracks[0].Points
	if len(pts) > 121 || len(pts) < 100 {
		t.Fatalf("points = %d, want about 120", len(pts))
	}
	if pts[0].ServerTS != "2026-09-28 10:00:00" {
		t.Errorf("first point at %q, want the start", pts[0].ServerTS)
	}
	if last := pts[len(pts)-1].ServerTS; last < "2026-09-28 12:58:00" {
		t.Errorf("last point at %q, want near the end", last)
	}
}
