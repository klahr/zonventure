package server_test

import (
	"testing"
)

// findPage scans the adventure's /edit payload for a page whose title
// matches the given prefix. Kept as a helper so individual tests don't
// need to grow their own page-lookup boilerplate.
func findPage(t *testing.T, admin *client, slug, titlePrefix string) (id int64, idx int) {
	t.Helper()
	var body struct {
		Pages []struct {
			ID    int64  `json:"id"`
			Index int    `json:"index"`
			Title string `json:"title"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/"+slug+"/edit", nil, &body); s != 200 {
		t.Fatalf("GET /edit = %d", s)
	}
	for _, p := range body.Pages {
		if len(p.Title) >= len(titlePrefix) && p.Title[:len(titlePrefix)] == titlePrefix {
			return p.ID, p.Index
		}
	}
	t.Fatalf("page %q not found", titlePrefix)
	return 0, 0
}

/* ---------------- disabled ---------------- */

func TestPages_DisabledRoundTripsThroughEdit(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID, _ := findPage(t, admin, "demo", "Landmark")

	// PATCH disabled=true
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title":    "Landmark",
		"prompt":   "Find the fountain in the central park. What colour are its tiles?",
		"accept":   []string{"blue", "azure"},
		"lat":      59.3326,
		"lng":      18.0649,
		"radius_m": 50.0,
		"disabled": true,
	}, nil); s != 204 {
		t.Fatalf("PATCH = %d", s)
	}
	var edit struct {
		Pages []struct {
			ID       int64 `json:"id"`
			Disabled bool  `json:"disabled"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &edit); s != 200 {
		t.Fatalf("GET /edit = %d", s)
	}
	for _, p := range edit.Pages {
		if p.ID == pageID && !p.Disabled {
			t.Fatalf("page %d didn't round-trip as disabled", pageID)
		}
	}
}

func TestPages_DisabledIsSkippedByPlayer(t *testing.T) {
	// Wire up: three pages in the demo, disable the middle one. A player
	// who solves page 0 must land directly on page 2 (skipping the
	// disabled one) and see the right page_count.
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID, _ := findPage(t, admin, "demo", "Landmark")
	// Disable the middle puzzle and clear its gate so the player state
	// machine doesn't try to gate on it (it shouldn't, because we skip
	// it entirely, but clearing the gate makes the intent explicit).
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title":    "Landmark",
		"prompt":   "(skipped)",
		"accept":   []string{"x"},
		"lat":      59.3326,
		"lng":      18.0649,
		"disabled": true,
	}, nil); s != 204 {
		t.Fatalf("PATCH = %d", s)
	}

	// Now the adventure has 2 enabled pages (indices 0 and 2).
	_, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start run = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	var st struct {
		Status    string `json:"status"`
		PageIndex int    `json:"page_index"`
		PageCount int    `json:"page_count"`
		Prompt    string `json:"prompt"`
	}
	if s := alice.do("GET", "/api/player/state", nil, &st); s != 200 {
		t.Fatalf("state = %d", s)
	}
	if st.PageCount != 2 {
		t.Fatalf("page_count = %d, want 2 (demo has 3 pages, one disabled)", st.PageCount)
	}
	if st.PageIndex != 0 || st.Status != "running" {
		t.Fatalf("expected fresh event on page 0, got %+v", st)
	}

	// Solve page 0 — should skip the disabled page and land on the final
	// page (which is the third demo page, no gate).
	var ans struct {
		Accepted  bool `json:"accepted"`
		Completed bool `json:"completed"`
	}
	if s := alice.do("POST", "/api/player/answer",
		map[string]string{"answer": "hello"}, &ans); s != 200 || !ans.Accepted {
		t.Fatalf("page 0 answer = %d accepted=%v", s, ans.Accepted)
	}
	if ans.Completed {
		t.Fatalf("should not be completed yet — one enabled page remains")
	}
	// Re-read state: should be on the last puzzle, enabled-index 1 of 2.
	st = struct {
		Status    string `json:"status"`
		PageIndex int    `json:"page_index"`
		PageCount int    `json:"page_count"`
		Prompt    string `json:"prompt"`
	}{}
	if s := alice.do("GET", "/api/player/state", nil, &st); s != 200 {
		t.Fatalf("state = %d", s)
	}
	if st.PageIndex != 1 || st.PageCount != 2 {
		t.Fatalf("expected enabled-index 1/2, got %d/%d", st.PageIndex, st.PageCount)
	}
	// Finish.
	if s := alice.do("POST", "/api/player/answer",
		map[string]string{"answer": "äventyr"}, &ans); s != 200 || !ans.Accepted || !ans.Completed {
		t.Fatalf("final answer = %d accepted=%v completed=%v", s, ans.Accepted, ans.Completed)
	}
}

/* ---------------- reorder ---------------- */

func TestPages_ReorderViaAPI(t *testing.T) {
	// The PATCH /pages/reorder endpoint rewrites the idx column for an
	// adventure. Verify the new order is reflected in GET /edit (which
	// sorts by idx).
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}

	var before struct {
		Pages []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &before); s != 200 {
		t.Fatalf("GET = %d", s)
	}
	if len(before.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(before.Pages))
	}

	// Reverse the order.
	reversed := []int64{before.Pages[2].ID, before.Pages[1].ID, before.Pages[0].ID}
	if s := admin.do("PATCH", "/api/admin/adventures/demo/pages/reorder",
		map[string]any{"page_ids": reversed}, nil); s != 204 {
		t.Fatalf("reorder = %d", s)
	}

	var after struct {
		Pages []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Index int    `json:"index"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &after); s != 200 {
		t.Fatalf("GET after = %d", s)
	}
	for i, p := range after.Pages {
		if p.ID != reversed[i] || p.Index != i {
			t.Fatalf("position %d: id=%d idx=%d, want id=%d idx=%d",
				i, p.ID, p.Index, reversed[i], i)
		}
	}
}
