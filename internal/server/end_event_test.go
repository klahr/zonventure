package server_test

import (
	"testing"
)

// TestEndEvent ends a paused event early: it completes with its open
// pause closed, the unfinished team's recap opens, players are signed
// out, and a second end — or ending a scheduled event — is refused.
func TestEndEvent(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	end := "/api/admin/events/" + itoa(run.ID) + "/end"
	if s := arr.doWithConfirm("POST", end, "s3cret", nil, nil); s != 409 {
		t.Fatalf("end of a scheduled event = %d, want 409", s)
	}
	var team struct {
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
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
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/pause", nil, nil); s != 204 {
		t.Fatalf("pause = %d", s)
	}

	if s := arr.do("POST", end, nil, nil); s != 401 {
		t.Fatalf("end without password = %d, want 401", s)
	}
	if s := arr.doWithConfirm("POST", end, "nope", nil, nil); s != 401 {
		t.Fatalf("end with wrong password = %d, want 401", s)
	}
	if s := arr.doWithConfirm("POST", end, "s3cret", nil, nil); s != 204 {
		t.Fatalf("end = %d, want 204", s)
	}

	var detail struct {
		Event struct {
			State       string `json:"state"`
			CompletedAt string `json:"completed_at"`
			PausedAt    string `json:"paused_at"`
		} `json:"event"`
		Teams []struct {
			CompletedAt string `json:"completed_at"`
			RecapToken  string `json:"recap_token"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(run.ID), nil, &detail); s != 200 {
		t.Fatalf("detail = %d", s)
	}
	if ev := detail.Event; ev.State != "completed" || ev.CompletedAt == "" || ev.PausedAt != "" {
		t.Fatalf("after end: state=%q completed_at=%q paused_at=%q", ev.State, ev.CompletedAt, ev.PausedAt)
	}
	if len(detail.Teams) != 1 || detail.Teams[0].CompletedAt != "" || detail.Teams[0].RecapToken == "" {
		t.Fatalf("the unfinished team stays unfinished, with a recap: %+v", detail.Teams)
	}
	if s := alice.do("GET", "/api/public/recap/"+detail.Teams[0].RecapToken, nil, nil); s != 200 {
		t.Fatalf("recap after end = %d, want 200", s)
	}
	if s := alice.do("GET", "/api/player/play", nil, nil); s != 401 {
		t.Fatalf("player after end = %d, want 401", s)
	}
	if s := arr.doWithConfirm("POST", end, "s3cret", nil, nil); s != 409 {
		t.Fatalf("second end = %d, want 409", s)
	}
}

// A self-guided adventure never ends: it is paused or cancelled.
func TestEndEventRefusesSelfGuided(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := pub.do("POST", "/api/admin/events", map[string]any{"adventure_slug": "demo", "public_for_sale": true, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create self-guided = %d", s)
	}
	if s := pub.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	if s := pub.doWithConfirm("POST", "/api/admin/events/"+itoa(ev.ID)+"/end", "pubpw", nil, nil); s != 409 {
		t.Errorf("end a self-guided adventure = %d, want 409", s)
	}
}
