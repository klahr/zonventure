package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestTeamEmail_CreateAndRoundTrip mints a team with an email,
// then reads it back via the event-detail endpoint to confirm the
// value round-trips through the create + read paths and that the
// server-side normalisation (trim + lowercase) is applied.
func TestTeamEmail_CreateAndRoundTrip(t *testing.T) {
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
		t.Fatalf("event = %d", s)
	}
	// Whitespace + uppercase to verify normaliseTeamEmail strips both.
	body := map[string]any{
		"name":         "Alpha",
		"member_names": []string{"alice"},
		"email":        "  Captain@Example.ORG  ",
	}
	var created struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams", body, &created); s != 201 {
		t.Fatalf("create = %d", s)
	}
	if created.Email != "captain@example.org" {
		t.Errorf("Email after create = %q, want trimmed+lowercase form", created.Email)
	}

	var detail struct {
		Teams []struct {
			ID    int64  `json:"id"`
			Email string `json:"email"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(run.ID), nil, &detail); s != 200 {
		t.Fatalf("event detail = %d", s)
	}
	var found *struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	for i := range detail.Teams {
		if detail.Teams[i].ID == created.ID {
			found = &detail.Teams[i]
		}
	}
	if found == nil {
		t.Fatalf("team %d not in event detail", created.ID)
	}
	if found.Email != "captain@example.org" {
		t.Errorf("event-detail Email = %q, want round-tripped value", found.Email)
	}
}

// TestTeamEmail_PatchUpdatesIndependently confirms PATCH can edit
// just the email without changing the name, and just the name
// without changing the email — the partial-update contract.
func TestTeamEmail_PatchUpdatesIndependently(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct{ ID int64 }
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("event = %d", s)
	}
	var team struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
		map[string]any{"name": "Beta", "member_names": []string{"bob"},
			"email": "first@example.org"}, &team); s != 201 {
		t.Fatalf("create = %d", s)
	}

	// Email-only update — name must stay "Beta".
	if s := arr.do("PATCH", "/api/admin/teams/"+itoa(team.ID),
		map[string]any{"email": "second@example.org"}, nil); s != 204 {
		t.Fatalf("email-only patch = %d", s)
	}
	// Name-only update — email must stay "second@example.org".
	if s := arr.do("PATCH", "/api/admin/teams/"+itoa(team.ID),
		map[string]any{"name": "Beta Renamed"}, nil); s != 204 {
		t.Fatalf("name-only patch = %d", s)
	}
	var detail struct {
		Teams []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(run.ID), nil, &detail); s != 200 {
		t.Fatalf("event detail = %d", s)
	}
	for _, tm := range detail.Teams {
		if tm.ID != team.ID {
			continue
		}
		if tm.Name != "Beta Renamed" {
			t.Errorf("Name = %q, want Beta Renamed", tm.Name)
		}
		if tm.Email != "second@example.org" {
			t.Errorf("Email = %q, want second@example.org", tm.Email)
		}
		return
	}
	t.Fatalf("team %d not found in detail", team.ID)
}

// TestTeamEmail_RejectsBadShape pins the validation contract: a
// malformed string (no @) and an over-long string both 400. The
// regex/parse details live in net/mail; we just confirm we wired
// it up correctly.
func TestTeamEmail_RejectsBadShape(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct{ ID int64 }
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("event = %d", s)
	}
	for _, bad := range []string{"not-an-email", strings.Repeat("a", 320) + "@x.io"} {
		s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
			map[string]any{"name": "C-" + bad[:1], "member_names": []string{"c"},
				"email": bad}, nil)
		if s != http.StatusBadRequest {
			t.Errorf("create with email=%q got %d, want 400", bad, s)
		}
	}
}

// TestTeamEmail_EmptyAccepted confirms an absent / empty email
// sails through — the field is optional by design.
func TestTeamEmail_EmptyAccepted(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct{ ID int64 }
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("event = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
		map[string]any{"name": "Delta", "member_names": []string{"d"}}, nil); s != 201 {
		t.Errorf("create without email = %d, want 201", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
		map[string]any{"name": "Echo", "member_names": []string{"e"}, "email": ""}, nil); s != 201 {
		t.Errorf("create with empty email = %d, want 201", s)
	}
}
