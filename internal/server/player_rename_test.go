package server_test

import (
	"testing"
)

// TestPlayerRenamesThemselfAndTeam covers the player's own renames: the
// self-rename wrote to a table that no longer exists and 500ed for
// everyone until this test.
func TestPlayerRenamesThemselfAndTeam(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	var run struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	var team struct {
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	for _, name := range []string{"Team Alpha", "Team Beta"} {
		if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams", map[string]any{"name": name, "member_names": []string{"Player 1"}}, &team); s != 201 {
			t.Fatalf("create team = %d", s)
		}
	}
	p := newClient(t, ts)
	if s := p.do("GET", "/play?t="+team.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("magic-link login = %d", s)
	}

	if s := p.do("POST", "/api/player/me/name", map[string]string{"name": "Klara"}, nil); s != 200 {
		t.Errorf("rename myself = %d, want 200", s)
	}
	if s := p.do("POST", "/api/player/team/name", map[string]string{"name": "Team Alpha"}, nil); s != 409 {
		t.Errorf("rename team to a taken name = %d, want 409", s)
	}
	if s := p.do("POST", "/api/player/team/name", map[string]string{"name": "The Otters"}, nil); s != 200 {
		t.Errorf("rename team = %d, want 200", s)
	}
}
