package server_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// recapPath builds the public recap JSON URL for a token.
func recapPath(token string) string { return "/api/public/recap/" + token }

// readRecap fetches the recap JSON without going through any auth
// middleware — the recap is public-by-token by design.
func readRecap(t *testing.T, tsURL, token string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(tsURL + recapPath(token))
	if err != nil {
		t.Fatalf("get recap: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// seedRecapEvent inserts an event + team in the requested state with
// the given recap_token. Returns the team id so callers can attach
// pings / answers.
func seedRecapEvent(t *testing.T, db *sql.DB, orgID int64, state, recapTok string) int64 {
	t.Helper()
	var eventID int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, started_at)
		VALUES ('demo', $1, $2, 'live-' || $3, now_text())
		RETURNING id
	`, orgID, state, recapTok).Scan(&eventID); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if state == "completed" || state == "cancelled" {
		col := "completed_at"
		if state == "cancelled" {
			col = "cancelled_at"
		}
		if _, err := db.Exec(
			`UPDATE events SET `+col+` = now_text() WHERE id = $1`, eventID,
		); err != nil {
			t.Fatalf("stamp end: %v", err)
		}
	}
	var teamID int64
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, token, recap_token, completed_at)
		VALUES ($1, 'Solo', 'team-' || $2, $2,
		        CASE WHEN $3 = 'completed' THEN now_text() ELSE NULL END)
		RETURNING id
	`, eventID, recapTok, state).Scan(&teamID); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	return teamID
}

type recapPreview struct {
	TeamName        string  `json:"team_name"`
	AdventureTitle  string  `json:"adventure_title"`
	AnswersRight    int     `json:"answers_right"`
	AnswersWrong    int     `json:"answers_wrong"`
	DistanceM       float64 `json:"distance_m"`
	DurationSeconds int     `json:"duration_seconds"`
	State           string  `json:"state"`
	Track           []struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
		TS  string  `json:"ts"`
	} `json:"track"`
}

// recapServer is a small bootstrap over seedPurchaseStack, which gives
// a DB and an empty 'demo' adventure. A graph team's steps are covered
// by recap_graph_test.go.
func recapServer(t *testing.T) (*httptest.Server, *sql.DB, int64, func()) {
	t.Helper()
	return seedPurchaseStack(t, server.Config{})
}

// TestRecap covers the public recap's visibility gate on one server.
// Each subtest seeds its own event under its own token.
func TestRecap(t *testing.T) {
	ts, db, orgID, cleanup := recapServer(t)
	defer cleanup()

	// A recap URL is unreachable while the event is still running. The
	// shape is the load-bearing privacy property: a leaked token
	// mid-event must not expose live position data.
	t.Run("pre-completion 404", func(t *testing.T) {
		seedRecapEvent(t, db, orgID, "running", "rt-running")
		status, _ := readRecap(t, ts.URL, "rt-running")
		if status != http.StatusNotFound {
			t.Fatalf("running-event recap = %d, want 404", status)
		}
	})

	// A team that finished sees its recap while the event runs on: a
	// self-guided event may run for months, a hosted one for days.
	t.Run("finished team while the event runs", func(t *testing.T) {
		teamID := seedRecapEvent(t, db, orgID, "running", "rt-finished")
		var eventID int64
		_ = db.QueryRow(`SELECT event_id FROM teams WHERE id = $1`, teamID).Scan(&eventID)
		var graphID int64
		if err := db.QueryRow(`INSERT INTO adventure_graphs(adventure_id, version, graph, label)
			SELECT id, 900, '{"nodes":[],"edges":[]}', '' FROM adventures WHERE slug = 'demo' RETURNING id`).Scan(&graphID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO team_graph_state(team_id, event_id, graph_id) VALUES ($1, $2, $3)`, teamID, eventID, graphID); err != nil {
			t.Fatal(err)
		}
		if status, _ := readRecap(t, ts.URL, "rt-finished"); status != http.StatusNotFound {
			t.Fatalf("still playing = %d, want 404", status)
		}
		if _, err := db.Exec(`UPDATE team_graph_state SET completed_at = now() WHERE team_id = $1`, teamID); err != nil {
			t.Fatal(err)
		}
		if status, body := readRecap(t, ts.URL, "rt-finished"); status != http.StatusOK {
			t.Fatalf("finished team = %d %s, want 200", status, body)
		}
	})

	// A hosted event's team whose game was finished sees its recap,
	// finished or not.
	t.Run("hosted game finished", func(t *testing.T) {
		teamID := seedRecapEvent(t, db, orgID, "running", "rt-round")
		var eventID int64
		_ = db.QueryRow(`SELECT event_id FROM teams WHERE id = $1`, teamID).Scan(&eventID)
		if _, err := db.Exec(`UPDATE events SET party = TRUE, current_round = 1 WHERE id = $1`, eventID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE teams SET round = 1 WHERE id = $1`, teamID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO event_rounds(event_id, round) VALUES ($1, 1)`, eventID); err != nil {
			t.Fatal(err)
		}
		if status, _ := readRecap(t, ts.URL, "rt-round"); status != http.StatusNotFound {
			t.Fatalf("game on = %d, want 404", status)
		}
		if _, err := db.Exec(`UPDATE event_rounds SET finished_at = now() WHERE event_id = $1`, eventID); err != nil {
			t.Fatal(err)
		}
		if status, body := readRecap(t, ts.URL, "rt-round"); status != http.StatusOK {
			t.Fatalf("game finished = %d %s, want 200", status, body)
		}
	})

	// The same 404 shape for a token that doesn't exist at all — the
	// response should not let a client distinguish "doesn't exist" from
	// "exists but not yet visible".
	t.Run("unknown token 404", func(t *testing.T) {
		status, _ := readRecap(t, ts.URL, "definitely-not-a-real-token")
		if status != http.StatusNotFound {
			t.Fatalf("unknown token = %d, want 404", status)
		}
	})

	// The full lifecycle: seed a completed event + team, then fetch the
	// public recap. Asserts the response carries the shape the
	// front-end depends on (state, team name).
	t.Run("happy path", func(t *testing.T) {
		seedRecapEvent(t, db, orgID, "completed", "rt-done")

		status, body := readRecap(t, ts.URL, "rt-done")
		if status != http.StatusOK {
			t.Fatalf("happy GET = %d, body=%s", status, body)
		}
		var got recapPreview
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.TeamName != "Solo" {
			t.Errorf("team_name = %q, want Solo", got.TeamName)
		}
		if got.State != "completed" {
			t.Errorf("state = %q, want completed", got.State)
		}
	})

	// The visibility gate covers both terminal states. A cancelled event
	// ("snowed out") still has a worth-keeping memory of how far the
	// team got.
	t.Run("cancelled event also visible", func(t *testing.T) {
		seedRecapEvent(t, db, orgID, "cancelled", "rt-cancelled")
		status, body := readRecap(t, ts.URL, "rt-cancelled")
		if status != http.StatusOK {
			t.Fatalf("cancelled GET = %d, body=%s", status, body)
		}
		var got recapPreview
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.State != "cancelled" {
			t.Errorf("state = %q, want cancelled", got.State)
		}
	})
}
