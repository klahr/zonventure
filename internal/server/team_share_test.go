package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// getRaw is a sibling of client.do that leaves the response body
// untouched — used when we want to inspect an HTML body, not JSON.
func getRaw(t *testing.T, c *client, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", c.ts.URL+path, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestTeamShare_CreateTeamMintsToken(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr, _, _, eventID := setupRun(t, ts, assignmentID)

	// Read the event detail — the team row should carry a non-empty token.
	var detail struct {
		Teams []struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(eventID), nil, &detail); s != 200 {
		t.Fatalf("run detail = %d", s)
	}
	if len(detail.Teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(detail.Teams))
	}
	if detail.Teams[0].Token == "" {
		t.Fatalf("team.token is empty — CreateTeam didn't mint one")
	}
}

func TestTeamShare_PageRendersSlots(t *testing.T) {
	// End-to-end: the captain opens /team/{token} with no session and
	// sees a page that names the team, the adventure, and links to each
	// slot's magic-link URL.
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr, _, _, eventID := setupRun(t, ts, assignmentID)
	var detail struct {
		Teams []struct {
			Token   string `json:"token"`
			Members []struct {
				Name  string `json:"name"`
				Token string `json:"token"`
			} `json:"members"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(eventID), nil, &detail); s != 200 {
		t.Fatalf("run detail = %d", s)
	}
	team := detail.Teams[0]

	// Unauthenticated fetch of /team/{token} — the captain gets the URL
	// from the arranger and has no session of their own.
	anon := newClient(t, ts)
	status, body := getRaw(t, anon, "/team/"+team.Token)
	if status != 200 {
		t.Fatalf("team page status = %d", status)
	}
	// Adventure title ("Demo: First Steps") must be on the page.
	if !strings.Contains(body, "Demo") {
		t.Errorf("team page missing adventure title")
	}
	// Team name ("Team Alpha") too.
	if !strings.Contains(body, "Team Alpha") {
		t.Errorf("team page missing team name")
	}
	// Each slot's magic-link URL is embedded as an <a href>.
	for _, m := range team.Members {
		want := `/play?t=` + m.Token
		if !strings.Contains(body, want) {
			t.Errorf("team page missing link for %s", m.Name)
		}
	}
}

func TestTeamShare_UnknownToken404(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	anon := newClient(t, ts)
	status, _ := getRaw(t, anon, "/team/doesnotexist")
	if status != 404 {
		t.Errorf("unknown team token = %d, want 404", status)
	}
}
