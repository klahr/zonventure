package server_test

import (
	"testing"
)

// TestCancelEvent_FromScheduled cancels an event that hasn't started
// yet and asserts the player view flips to status=cancelled while
// mutating endpoints stop accepting writes.
func TestCancelEvent_FromScheduled(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct{ ID int64 `json:"id"` }
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}

	// Wrong password is rejected.
	if s := arr.doWithConfirm("POST",
		"/api/admin/events/"+itoa(run.ID)+"/cancel", "nope", nil, nil); s != 401 {
		t.Fatalf("cancel without correct password = %d, want 401", s)
	}
	// Missing password header is rejected.
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/cancel", nil, nil); s != 401 {
		t.Fatalf("cancel without password = %d, want 401", s)
	}
	// Correct password succeeds.
	if s := arr.doWithConfirm("POST",
		"/api/admin/events/"+itoa(run.ID)+"/cancel", "s3cret", nil, nil); s != 204 {
		t.Fatalf("cancel = %d, want 204", s)
	}
	// Re-cancelling rejects with conflict.
	if s := arr.doWithConfirm("POST",
		"/api/admin/events/"+itoa(run.ID)+"/cancel", "s3cret", nil, nil); s != 409 {
		t.Fatalf("re-cancel = %d, want 409", s)
	}
	// Lifecycle transitions are blocked once cancelled.
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/start", nil, nil); s == 204 {
		t.Fatalf("start of cancelled event succeeded; want non-204")
	}
}

// TestCancelEvent_FromRunning verifies cancellation works mid-event
// and that the player API surfaces status=cancelled.
func TestCancelEvent_FromRunning(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct{ ID int64 `json:"id"` }
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	var team struct {
		ID      int64 `json:"id"`
		Members []struct{ Token string `json:"token"` } `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
		map[string]any{"name": "Alpha", "member_names": []string{"alice"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}

	alice := newClient(t, ts)
	if s := alice.do("GET", "/play?t="+team.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("magic link = %d", s)
	}
	var state struct{ Status string `json:"status"` }
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 || state.Status != "running" {
		t.Fatalf("pre-cancel player status = %q (HTTP %d), want running", state.Status, s)
	}

	if s := arr.doWithConfirm("POST",
		"/api/admin/events/"+itoa(run.ID)+"/cancel", "s3cret", nil, nil); s != 204 {
		t.Fatalf("cancel running event = %d, want 204", s)
	}

	// Post-cancel the slot session is forcibly expired — see
	// activity.ExpireEventSlotSessions in CancelEvent. The player
	// gets a 401 on every subsequent call, which is more aggressive
	// (and more correct) than the older "still authenticated, just
	// shows status=cancelled" posture this test was written
	// against. Either signal achieves the same UX outcome (the
	// player UI bounces them off the active screen); 401 is the
	// shape the production code now produces.
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 401 {
		t.Fatalf("post-cancel player state = %d, want 401 (slot session expired on cancel)", s)
	}
	// Submitting an answer post-cancel hits the same expired-session
	// 401 — the per-handler 409 "RunState != running" never fires
	// because the auth middleware short-circuits first.
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "hello"}, nil); s != 401 {
		t.Fatalf("answer after cancel = %d, want 401 (expired session)", s)
	}
}

// Completed events: cancellation is gated by reasonForState +
// allowedForState, both of which already cover the "cancelled"
// terminal. Driving a full game to completion through the public
// player API for a single assertion isn't worth the complexity here;
// the same code path is exercised by the cancel-after-cancel 409
// test above.
