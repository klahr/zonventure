package server_test

import (
	"net/http"
	"testing"
)

// TestSubmitAnswer_ReplaysCachedResponseOnRetry pins the idempotency
// contract on /api/player/answer. The offline replay queue retries
// queued submits with the same client_action_id; without server-side
// dedup, the second call would reach applyAnswer with the team
// already advanced to page 1, evaluate the page-0 answer against
// page-1's accept list, and return accepted=false — yanking the
// player back to "wrong" UI for an action that already succeeded.
//
// The expected behaviour: the second call replays the cached 200
// {accepted:true,next_index:1} body verbatim. We can observe that
// from the response alone — no DB access needed.
func TestSubmitAnswer_ReplaysCachedResponseOnRetry(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	type answerResp struct {
		Accepted  bool `json:"accepted"`
		Completed bool `json:"completed"`
		NextIndex int  `json:"next_index,omitempty"`
	}

	// First submit — correct answer for page 0. Team advances to page 1.
	const actionID = "test-action-page0-correct"
	var first answerResp
	if s := alice.do("POST", "/api/player/answer", map[string]any{
		"answer":           "hello",
		"client_action_id": actionID,
	}, &first); s != 200 {
		t.Fatalf("first submit = %d", s)
	}
	if !first.Accepted || first.NextIndex != 1 {
		t.Fatalf("first response = %+v, want accepted=true next_index=1", first)
	}

	// Second submit — same action id, same body. Dedup must replay
	// the cached 200 body. Without dedup, the server would evaluate
	// "hello" against page 1's accept list (["blue","azure"]) and
	// return accepted=false.
	var second answerResp
	if s := alice.do("POST", "/api/player/answer", map[string]any{
		"answer":           "hello",
		"client_action_id": actionID,
	}, &second); s != 200 {
		t.Fatalf("retry submit = %d", s)
	}
	if !second.Accepted || second.NextIndex != 1 {
		t.Fatalf("retry response = %+v, want cached replay {accepted:true next_index:1}", second)
	}

	// A FRESH action id must NOT replay — it should run against the
	// team's current state (page 1) and yield something different
	// from the page-0 cached response. Two acceptable outcomes:
	//   • 200 with accepted=false (the answer-mismatch path)
	//   • 403 "must reach the location first" (the geofence gate
	//     when page 1 has radius_m set in the seed)
	// Either is evidence the cached response wasn't blindly replayed;
	// the failure mode this test pins is "dedup applied to fresh
	// action and produced accepted=true" which would return the
	// page-0 cached response.
	var fresh answerResp
	s := alice.do("POST", "/api/player/answer", map[string]any{
		"answer":           "hello",
		"client_action_id": "different-id",
	}, &fresh)
	switch s {
	case 200:
		if fresh.Accepted {
			t.Fatalf("fresh action with old answer should be rejected, got %+v", fresh)
		}
	case 403:
		// Geofence-gated; the dedup path obviously didn't fire.
	default:
		t.Fatalf("fresh submit = %d, want 200 (rejected) or 403 (gated)", s)
	}
}

// TestRevealHint_ReplaysCachedResponseOnRetry covers the same
// idempotency contract for /api/player/hint. The (team, page, hint)
// upsert already makes the DB write idempotent — what dedup adds is
// a guarantee the cached response shape is replayed verbatim, so
// the offline queue's UI knows to flip the hint into "revealed"
// regardless of which retry actually landed the row.
func TestRevealHint_ReplaysCachedResponseOnRetry(t *testing.T) {
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

	type hintResp struct {
		HintsRevealed int `json:"hints_revealed"`
	}
	const actionID = "test-reveal-hint-0"

	// First reveal — fresh action id, count goes 0 → 1.
	var first hintResp
	if s := alice.do("POST", "/api/player/hint", map[string]any{
		"hint_index":       0,
		"client_action_id": actionID,
	}, &first); s != 200 {
		t.Fatalf("first reveal = %d", s)
	}
	if first.HintsRevealed != 1 {
		t.Errorf("first response = %+v, want hints_revealed=1", first)
	}

	// Same action id retried — must replay the cached body. The
	// underlying upsert is already idempotent on (team, page, hint),
	// so this also fail-safes the dedup itself: a regression that
	// shipped errors instead of cached 2xx would 4xx here.
	resp, err := postJSONRaw(alice, "/api/player/hint", map[string]any{
		"hint_index":       0,
		"client_action_id": actionID,
	})
	if err != nil {
		t.Fatalf("retry reveal: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retry reveal = %d, want 200 (cached replay)", resp.StatusCode)
	}
}
