package server_test

import (
	"testing"
)

// The public board must not give away puzzle locations while the event
// can still be played: anyone holding the /live link (every player at
// the venue, in practice) would otherwise see the whole route. It sends
// the play area's box instead, and the pins once the event is over.
func TestPublicLive_PuzzleLocationsHiddenUntilOver(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, liveToken, _, eventID := setupRun(t, ts, assignmentID)

	type snapshot struct {
		State   string `json:"state"`
		Puzzles []struct {
			Title string   `json:"title"`
			Lat   *float64 `json:"lat"`
			Lng   *float64 `json:"lng"`
		} `json:"puzzles"`
		Bounds *[4]float64 `json:"bounds"`
	}
	get := func() snapshot {
		t.Helper()
		var s snapshot
		if st := arr.do("GET", "/api/public/live/"+liveToken, nil, &s); st != 200 {
			t.Fatalf("live = %d", st)
		}
		if len(s.Puzzles) != 3 {
			t.Fatalf("puzzles = %d, want 3", len(s.Puzzles))
		}
		return s
	}

	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}
	s := get()
	for _, p := range s.Puzzles {
		if p.Lat != nil || p.Lng != nil {
			t.Errorf("puzzle %q exposes its location while %s", p.Title, s.State)
		}
	}
	// The demo puzzles span 59.3252–59.3326 N, 18.0649–18.0731 E.
	want := [4]float64{59.3252, 18.0649, 59.3326, 18.0731}
	if s.Bounds == nil || *s.Bounds != want {
		t.Errorf("bounds = %v, want %v", s.Bounds, want)
	}

	if st := arr.doWithConfirm("POST",
		"/api/admin/events/"+itoa(eventID)+"/cancel", "s3cret", nil, nil); st != 204 {
		t.Fatalf("cancel = %d", st)
	}
	s = get()
	for _, p := range s.Puzzles {
		if p.Lat == nil || p.Lng == nil {
			t.Errorf("puzzle %q has no location once %s", p.Title, s.State)
		}
	}
}
