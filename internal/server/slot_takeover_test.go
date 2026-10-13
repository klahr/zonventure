package server_test

import (
	"strings"
	"testing"
)

// TestSlotTakeover: opening a player's link on a second phone takes the
// seat over once that phone's app uses it. The first phone is then
// refused with session_replaced, the team page marks the seat taken, and
// merely fetching the link (as a mail scanner would) takes nothing over.
func TestSlotTakeover(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	arr, _, magic, eventID := setupRun(t, ts, assignmentID)
	var detail struct {
		Teams []struct {
			Token string `json:"token"`
		} `json:"teams"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(eventID), nil, &detail); s != 200 || len(detail.Teams) != 1 {
		t.Fatalf("detail = %d", s)
	}
	teamPage := "/team/" + detail.Teams[0].Token
	anon := newClient(t, ts)
	if _, body := getRaw(t, anon, teamPage); strings.Contains(body, "take over") {
		t.Fatalf("a seat no phone plays is marked taken")
	}

	first := loginPlayer(t, ts, magic)
	if s := first.do("GET", "/api/player/play", nil, nil); s != 200 {
		t.Fatalf("first phone = %d", s)
	}
	if _, body := getRaw(t, anon, teamPage); !strings.Contains(body, "take over") {
		t.Fatalf("team page doesn't mark the seat taken")
	}

	scanner := loginPlayer(t, ts, magic)
	if s := first.do("GET", "/api/player/play", nil, nil); s != 200 {
		t.Fatalf("first phone after a link fetch = %d, want 200", s)
	}

	second := loginPlayer(t, ts, magic)
	if s := second.do("GET", "/api/player/play", nil, nil); s != 200 {
		t.Fatalf("second phone = %d", s)
	}
	var refusal refusalDTO
	if s := first.doAny("GET", "/api/player/play", nil, &refusal); s != 401 || refusal.Code != "session_replaced" {
		t.Fatalf("first phone after takeover = %d %q, want 401 session_replaced", s, refusal.Code)
	}
	if s := scanner.doAny("GET", "/api/player/play", nil, &refusal); s != 401 || refusal.Code != "session_replaced" {
		t.Fatalf("scanner's session after takeover = %d %q, want 401 session_replaced", s, refusal.Code)
	}
	if s := second.do("GET", "/api/player/play", nil, nil); s != 200 {
		t.Fatalf("second phone plays on = %d", s)
	}
}
