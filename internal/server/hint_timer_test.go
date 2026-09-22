package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
)

// postJSONRaw is the test-side bypass that lets a test inspect raw
// response headers (e.g. Retry-After) — client.do swallows those.
// Mirrors the cookie/CSRF setup the real client uses.
func postJSONRaw(c *client, path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", c.ts.URL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := c.csrfCookie(); tok != "" {
		req.Header.Set(auth.CSRFHeaderName, tok)
	}
	return c.c.Do(req)
}

// TestRevealHint_RejectsBeforeUnlockTime is the regression pin for the
// server-side timer gate added in 0.1.6. Before that fix the client
// alone decided when a hint was unlockable (UnlocksInSeconds in the
// state DTO); a misclocked or malicious client could POST
// /api/player/hint immediately after reaching a puzzle and the server
// would happily insert the reveal row regardless of delay_seconds.
//
// The fix: RevealHint computes elapsed = now - team.current_page_started_at
// and 403s with Retry-After when elapsed < hint.delay_seconds. This
// test wires a hint with a 5-minute delay and asserts the immediate
// reveal attempt fails with the expected status + header.
func TestRevealHint_RejectsBeforeUnlockTime(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
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
			{"text": "Wait for it", "delay_seconds": 300},
		},
	}, nil); s != 204 {
		t.Fatalf("PATCH hints = %d", s)
	}

	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	// Reveal attempt fires immediately — well inside the 300s delay.
	// We hit the raw HTTP path so we can inspect Retry-After (the
	// header is load-bearing for the offline queue's backoff).
	resp, err := postJSONRaw(alice, "/api/player/hint",
		map[string]any{"hint_index": 0})
	if err != nil {
		t.Fatalf("raw POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("early reveal = %d, want 403", resp.StatusCode)
	}
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		t.Fatalf("403 missing Retry-After header — offline replay queue won't know when to retry")
	}
	secs, err := strconv.Atoi(ra)
	if err != nil || secs <= 0 || secs > 300 {
		t.Fatalf("Retry-After = %q, want positive integer ≤ 300", ra)
	}
}

// TestRevealHint_AllowsAtZeroDelay is the inverse: a hint with
// delay_seconds=0 must reveal immediately on the first POST. Pinning
// the boundary so a future tweak to the timer comparison (e.g.
// elapsed > delay vs >=) can't silently break the common
// "instant-help" hint shape every adventure starts with.
func TestRevealHint_AllowsAtZeroDelay(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title":  "Welcome",
		"prompt": "What word do you say when you meet someone for the first time?",
		"accept": acceptLits("hello", "hi"),
		"lat":    59.3308,
		"lng":    18.0731,
		"hints":  []map[string]any{{"text": "Now", "delay_seconds": 0}},
	}, nil); s != 204 {
		t.Fatalf("PATCH = %d", s)
	}

	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	var resp struct {
		HintsRevealed int `json:"hints_revealed"`
	}
	if s := alice.do("POST", "/api/player/hint",
		map[string]any{"hint_index": 0}, &resp); s != 200 {
		t.Fatalf("reveal = %d, want 200", s)
	}
	if resp.HintsRevealed != 1 {
		t.Errorf("hints_revealed = %d, want 1", resp.HintsRevealed)
	}
}
