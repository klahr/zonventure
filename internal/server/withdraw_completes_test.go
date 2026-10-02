package server_test

import (
	"testing"
)

// Withdrawing the last unfinished team completes the event; the
// event.completed webhook must go out as it does when a team finishes.
func TestWithdrawLastTeam_SendsEventCompleted(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := arr.do("POST", "/api/admin/webhooks", map[string]any{
		"name": "hook", "url": "https://example.com/hook", "event_kinds": []string{"event.completed"},
	}, nil); s != 201 && s != 200 {
		t.Fatalf("create webhook = %d", s)
	}
	var run struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	var team struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
		map[string]any{"name": "Alpha", "member_names": []string{"alice"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	if s := arr.do("POST", "/api/admin/teams/"+itoa(team.ID)+"/withdraw", nil, nil); s != 200 {
		t.Fatalf("withdraw = %d", s)
	}

	var out struct {
		Deliveries []struct {
			EventKind string `json:"event_kind"`
		} `json:"deliveries"`
	}
	if s := arr.do("GET", "/api/admin/webhooks/deliveries", nil, &out); s != 200 {
		t.Fatalf("deliveries = %d", s)
	}
	for _, d := range out.Deliveries {
		if d.EventKind == "event.completed" {
			return
		}
	}
	t.Fatalf("no event.completed delivery queued: %+v", out.Deliveries)
}
