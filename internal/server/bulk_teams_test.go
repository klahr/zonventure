package server_test

import (
	"testing"
)

// TestBulkCreateTeams posts rosters to one event; the subtests' team
// names don't overlap, so only the deliberate duplicate collides.
func TestBulkCreateTeams(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var run struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}

	// Posts a three-team roster against a fresh event and verifies all
	// three land with their member slots fully populated.
	t.Run("happy path", func(t *testing.T) {
		arr := boss.in(t)
		body := map[string]any{
			"teams": []map[string]any{
				{"name": "Alpha", "member_names": []string{"alice", "bob"}},
				{"name": "Beta", "member_names": []string{"carol"}},
				{"name": "Gamma", "member_names": []string{"dave", "eve", "frank"}},
			},
		}
		var resp struct {
			Created int `json:"created"`
			Failed  int `json:"failed"`
			Results []struct {
				Index int    `json:"index"`
				Name  string `json:"name"`
				Error string `json:"error,omitempty"`
				Team  *struct {
					ID      int64 `json:"id"`
					Members []struct {
						Token string `json:"token"`
					} `json:"members"`
				} `json:"team,omitempty"`
			} `json:"results"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams/bulk", body, &resp); s != 200 {
			t.Fatalf("bulk = %d, want 200", s)
		}
		if resp.Created != 3 || resp.Failed != 0 {
			t.Fatalf("created=%d failed=%d, want 3/0", resp.Created, resp.Failed)
		}
		if len(resp.Results) != 3 {
			t.Fatalf("results length = %d, want 3", len(resp.Results))
		}
		for i, r := range resp.Results {
			if r.Error != "" {
				t.Fatalf("results[%d] error = %q", i, r.Error)
			}
			if r.Team == nil || r.Team.ID == 0 {
				t.Fatalf("results[%d] missing team", i)
			}
			if len(r.Team.Members) == 0 {
				t.Fatalf("results[%d] no members", i)
			}
			for j, m := range r.Team.Members {
				if m.Token == "" {
					t.Fatalf("results[%d].members[%d] missing token", i, j)
				}
			}
		}
	})

	// Puts a duplicate-name row in the middle of an otherwise valid
	// roster and verifies the surrounding rows still land while the
	// offender comes back with an error.
	t.Run("partial failure", func(t *testing.T) {
		arr := boss.in(t)
		body := map[string]any{
			"teams": []map[string]any{
				{"name": "Delta", "member_names": []string{"alice"}},
				{"name": "Delta", "member_names": []string{"alice"}}, // dup of row 0
				{"name": "Epsilon", "member_names": []string{"bob"}},
			},
		}
		var resp struct {
			Created int `json:"created"`
			Failed  int `json:"failed"`
			Results []struct {
				Name  string `json:"name"`
				Error string `json:"error,omitempty"`
			} `json:"results"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams/bulk", body, &resp); s != 200 {
			t.Fatalf("bulk = %d", s)
		}
		if resp.Created != 2 || resp.Failed != 1 {
			t.Fatalf("created=%d failed=%d, want 2/1", resp.Created, resp.Failed)
		}
		if resp.Results[1].Error == "" {
			t.Fatalf("middle row should have errored")
		}
		if resp.Results[0].Error != "" || resp.Results[2].Error != "" {
			t.Fatalf("flanking rows should have succeeded; got errors %q / %q",
				resp.Results[0].Error, resp.Results[2].Error)
		}
	})

	// Enforces the 200-team cap.
	t.Run("rejects huge batch", func(t *testing.T) {
		arr := boss.in(t)
		teams := make([]map[string]any, 201)
		for i := range teams {
			teams[i] = map[string]any{
				"name":         "T" + itoa(int64(i)),
				"member_names": []string{"x"},
			}
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams/bulk",
			map[string]any{"teams": teams}, nil); s != 400 {
			t.Fatalf("oversized batch = %d, want 400", s)
		}
	})
}
