package server_test

import (
	"encoding/json"
	"testing"
	"time"
)

// demoPageID returns the id of the first page of the demo adventure,
// which TestFullFlow and friends use as a convenient real puzzle to
// patch against. Avoids each test having to scan /edit itself.
func demoPageID(t *testing.T, admin *client) int64 {
	t.Helper()
	var body struct {
		Pages []struct{ ID int64 } `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &body); s != 200 {
		t.Fatalf("edit GET = %d", s)
	}
	if len(body.Pages) == 0 {
		t.Fatalf("demo has no pages")
	}
	return body.Pages[0].ID
}

func TestHints_RoundTripThroughPatch(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)

	hints := []map[string]any{
		{"text": "First clue", "delay_seconds": 0},
		{"text": "Second clue — **bold** and *italic*", "delay_seconds": 120},
		{"text": "Third clue", "delay_seconds": 300},
	}
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title":  "Welcome",
		"prompt": "What word do you say when you meet someone for the first time?",
		"accept": acceptLits("hello", "hi"),
		"lat":    59.3308,
		"lng":    18.0731,
		"hints":  hints,
	}, nil); s != 204 {
		t.Fatalf("PATCH = %d", s)
	}

	var got struct {
		Pages []struct {
			ID    int64 `json:"id"`
			Hints []struct {
				Text         string `json:"text"`
				DelaySeconds int    `json:"delay_seconds"`
			} `json:"hints"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &got); s != 200 {
		t.Fatalf("GET = %d", s)
	}
	var actual []struct {
		Text         string `json:"text"`
		DelaySeconds int    `json:"delay_seconds"`
	}
	for _, p := range got.Pages {
		if p.ID == pageID {
			actual = p.Hints
		}
	}
	if len(actual) != 3 {
		t.Fatalf("expected 3 hints, got %d: %+v", len(actual), actual)
	}
	if actual[0].Text != "First clue" || actual[0].DelaySeconds != 0 {
		t.Errorf("hint 0 = %+v", actual[0])
	}
	if actual[1].Text != "Second clue — **bold** and *italic*" || actual[1].DelaySeconds != 120 {
		t.Errorf("hint 1 = %+v", actual[1])
	}
	if actual[2].DelaySeconds != 300 {
		t.Errorf("hint 2 delay = %d", actual[2].DelaySeconds)
	}
}

func TestHints_PlayerStateComputesUnlockCountdown(t *testing.T) {
	// Attach hints with increasing delays, start the event, and verify
	// the player state carries unlocks_in_seconds that reflect how
	// long ago the team "arrived" at the current puzzle (here = run
	// start).
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title":  "Welcome",
		"prompt": "What word do you say when you meet someone for the first time?",
		"accept": acceptLits("hello", "hi"),
		"lat":    59.3308,
		"lng":    18.0731,
		"hints": []map[string]any{
			{"text": "Ready now", "delay_seconds": 0},
			{"text": "Ready later", "delay_seconds": 300},
		},
	}, nil); s != 204 {
		t.Fatalf("PATCH = %d", s)
	}

	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	// The state call happens close enough to run-start that the later
	// hint's countdown should still be near its full window.
	t0 := time.Now()
	var st struct {
		Hints []struct {
			TextHTML         string `json:"text_html"`
			UnlocksInSeconds int    `json:"unlocks_in_seconds"`
		} `json:"hints"`
	}
	if s := alice.do("GET", "/api/player/state", nil, &st); s != 200 {
		t.Fatalf("state = %d", s)
	}
	if len(st.Hints) != 2 {
		j, _ := json.Marshal(st)
		t.Fatalf("expected 2 hints, got %d (state: %s)", len(st.Hints), j)
	}
	if st.Hints[0].UnlocksInSeconds != 0 {
		t.Fatalf("hint 0 should unlock immediately, got %d", st.Hints[0].UnlocksInSeconds)
	}
	// The second hint should count down toward 300s, less whatever
	// seconds have elapsed between run start and the state call.
	// Give the test a generous ±15s envelope for CI jitter.
	elapsed := int(time.Since(t0).Seconds()) + 5
	if st.Hints[1].UnlocksInSeconds < 300-elapsed-15 || st.Hints[1].UnlocksInSeconds > 300 {
		t.Fatalf("hint 1 countdown = %d; expected ~300 (±15)", st.Hints[1].UnlocksInSeconds)
	}
	// Note on TextHTML: a later refactor gated `text_html` behind
	// the player having explicitly revealed each hint (so devtools-
	// peeking can't surface every body before the countdown
	// completes). Pre-reveal the field is intentionally empty;
	// the structural + countdown checks above are what this test
	// pins. Verifying TextHTML round-trips lives in the dedicated
	// reveal-hint test instead.
}
