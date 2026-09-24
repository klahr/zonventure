package server_test

import (
	"testing"
)

// TestLanes_CRUD exercises the lane CRUD surface end-to-end against a
// real Postgres (skips without ZV_TEST_DSN). Verifies create, list
// via the editor DTO, rename, mode flip, reorder, and the
// non-empty-lane delete refusal.
func TestLanes_CRUD(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	// The seeded adventure ("demo") starts with one default lane from
	// the migration backfill. Editor DTO should expose it.
	type laneJSON struct {
		ID   int64  `json:"id"`
		Idx  int    `json:"idx"`
		Mode string `json:"mode"`
		Name string `json:"name"`
	}
	type editDTO struct {
		ID    int64      `json:"id"`
		Lanes []laneJSON `json:"lanes"`
		Pages []struct {
			ID     int64 `json:"id"`
			LaneID int64 `json:"lane_id"`
		} `json:"pages"`
	}
	var dto editDTO
	if s := arr.do("GET", "/api/admin/adventures/demo/edit", nil, &dto); s != 200 {
		t.Fatalf("load adventure for edit = %d", s)
	}
	if len(dto.Lanes) != 1 {
		t.Fatalf("expected one default lane, got %+v", dto.Lanes)
	}
	defaultLane := dto.Lanes[0]
	if defaultLane.Mode != "linear" {
		t.Fatalf("default lane mode = %q, want linear", defaultLane.Mode)
	}
	for _, p := range dto.Pages {
		if p.LaneID != defaultLane.ID {
			t.Fatalf("page %d in lane %d, want default %d", p.ID, p.LaneID, defaultLane.ID)
		}
	}

	// Create a second lane with explicit mode + name.
	var newLane laneJSON
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes",
		map[string]any{"mode": "shuffled", "name": "Park puzzles"}, &newLane); s != 201 {
		t.Fatalf("create lane = %d", s)
	}
	if newLane.Mode != "shuffled" || newLane.Name != "Park puzzles" || newLane.Idx != 1 {
		t.Fatalf("create lane returned %+v", newLane)
	}

	// Rename + flip mode in one PATCH.
	if s := arr.do("PATCH", "/api/admin/adventures/demo/lanes/"+itoa(newLane.ID),
		map[string]any{"mode": "linear", "name": "Renamed"}, nil); s != 204 {
		t.Fatalf("update lane = %d", s)
	}
	if s := arr.do("GET", "/api/admin/adventures/demo/edit", nil, &dto); s != 200 {
		t.Fatalf("reload edit = %d", s)
	}
	if len(dto.Lanes) != 2 {
		t.Fatalf("expected two lanes, got %+v", dto.Lanes)
	}
	for _, l := range dto.Lanes {
		if l.ID == newLane.ID && (l.Mode != "linear" || l.Name != "Renamed") {
			t.Fatalf("after PATCH, lane = %+v", l)
		}
	}

	// Delete the empty new lane — should succeed (no pages assigned).
	if s := arr.do("DELETE", "/api/admin/adventures/demo/lanes/"+itoa(newLane.ID), nil, nil); s != 204 {
		t.Fatalf("delete empty lane = %d", s)
	}

	// Try deleting the default lane while it still has pages — must 409.
	if s := arr.do("DELETE", "/api/admin/adventures/demo/lanes/"+itoa(defaultLane.ID), nil, nil); s != 409 {
		t.Fatalf("delete non-empty lane = %d, want 409", s)
	}

	// Add three more lanes and reorder them.
	var l1, l2, l3 laneJSON
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes", map[string]any{"mode": "linear", "name": "A"}, &l1); s != 201 {
		t.Fatalf("create A = %d", s)
	}
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes", map[string]any{"mode": "linear", "name": "B"}, &l2); s != 201 {
		t.Fatalf("create B = %d", s)
	}
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes", map[string]any{"mode": "linear", "name": "C"}, &l3); s != 201 {
		t.Fatalf("create C = %d", s)
	}
	// Reorder so the default lane lands at the end: [A, B, C, default].
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes/reorder",
		map[string]any{"lane_ids": []int64{l1.ID, l2.ID, l3.ID, defaultLane.ID}}, nil); s != 204 {
		t.Fatalf("reorder = %d", s)
	}
	if s := arr.do("GET", "/api/admin/adventures/demo/edit", nil, &dto); s != 200 {
		t.Fatalf("reload after reorder = %d", s)
	}
	wantOrder := []int64{l1.ID, l2.ID, l3.ID, defaultLane.ID}
	if len(dto.Lanes) != 4 {
		t.Fatalf("expected 4 lanes after reorder, got %+v", dto.Lanes)
	}
	for i, want := range wantOrder {
		if dto.Lanes[i].ID != want || dto.Lanes[i].Idx != i {
			t.Fatalf("position %d: got id=%d idx=%d, want id=%d", i, dto.Lanes[i].ID, dto.Lanes[i].Idx, want)
		}
	}
}

// TestLanes_ReorderMovesPageAcrossLanes verifies the lane-aware
// shape of /pages/reorder: a single PATCH that both repositions
// pages and switches their lane assignment commits atomically.
func TestLanes_ReorderMovesPageAcrossLanes(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	// Snapshot the seeded adventure: 3 pages all in the default lane.
	type laneJSON struct {
		ID  int64 `json:"id"`
		Idx int   `json:"idx"`
	}
	type pageJSON struct {
		ID     int64 `json:"id"`
		Index  int   `json:"index"`
		LaneID int64 `json:"lane_id"`
	}
	type editDTO struct {
		Lanes []laneJSON `json:"lanes"`
		Pages []pageJSON `json:"pages"`
	}
	var dto editDTO
	if s := arr.do("GET", "/api/admin/adventures/demo/edit", nil, &dto); s != 200 {
		t.Fatalf("load = %d", s)
	}
	if len(dto.Lanes) != 1 || len(dto.Pages) != 3 {
		t.Fatalf("seed shape unexpected: %d lanes, %d pages", len(dto.Lanes), len(dto.Pages))
	}
	defaultLane := dto.Lanes[0]

	// Add a second lane and move the middle page into it.
	var newLane laneJSON
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes",
		map[string]any{"mode": "shuffled", "name": "Park"}, &newLane); s != 201 {
		t.Fatalf("create lane = %d", s)
	}

	// Build a reorder body: page 0 stays in default, page 1 moves to
	// the new lane (becoming its first puzzle), page 2 stays in default.
	body := map[string]any{
		"pages": []map[string]any{
			{"id": dto.Pages[0].ID, "lane_id": defaultLane.ID},
			{"id": dto.Pages[1].ID, "lane_id": newLane.ID},
			{"id": dto.Pages[2].ID, "lane_id": defaultLane.ID},
		},
	}
	if s := arr.do("PATCH", "/api/admin/adventures/demo/pages/reorder", body, nil); s != 204 {
		t.Fatalf("reorder = %d", s)
	}

	// Reload and verify the lane assignments stuck.
	if s := arr.do("GET", "/api/admin/adventures/demo/edit", nil, &dto); s != 200 {
		t.Fatalf("reload = %d", s)
	}
	for _, p := range dto.Pages {
		switch p.ID {
		case dto.Pages[0].ID, dto.Pages[2].ID:
			// These are the same as before; lane should still match defaults.
		}
	}
	// Easier check: count pages per lane.
	perLane := map[int64]int{}
	for _, p := range dto.Pages {
		perLane[p.LaneID]++
	}
	if perLane[newLane.ID] != 1 {
		t.Fatalf("expected new lane to own 1 page, got %d", perLane[newLane.ID])
	}
	if perLane[defaultLane.ID] != 2 {
		t.Fatalf("expected default lane to own 2 pages, got %d", perLane[defaultLane.ID])
	}
}

// TestLanes_ReorderRejectsForeignLane confirms a page can't be
// drag-dropped into a lane that belongs to a different adventure
// even if the request body says so.
func TestLanes_ReorderRejectsForeignLane(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	var dto struct {
		Pages []struct {
			ID int64 `json:"id"`
		} `json:"pages"`
	}
	if s := arr.do("GET", "/api/admin/adventures/demo/edit", nil, &dto); s != 200 {
		t.Fatalf("load = %d", s)
	}
	body := map[string]any{
		"pages": []map[string]any{
			{"id": dto.Pages[0].ID, "lane_id": int64(987654321)},
		},
	}
	if s := arr.do("PATCH", "/api/admin/adventures/demo/pages/reorder", body, nil); s != 400 {
		t.Fatalf("foreign lane = %d, want 400", s)
	}
}

// TestLanes_BadInput covers the validation paths so a bad client
// can't trip over a 500.
func TestLanes_BadInput(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	// Unknown mode rejected.
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes",
		map[string]any{"mode": "spiral"}, nil); s != 400 {
		t.Fatalf("bad mode = %d, want 400", s)
	}

	// Empty patch rejected.
	var lane struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/adventures/demo/lanes",
		map[string]any{"mode": "linear"}, &lane); s != 201 {
		t.Fatalf("create = %d", s)
	}
	if s := arr.do("PATCH", "/api/admin/adventures/demo/lanes/"+itoa(lane.ID),
		map[string]any{}, nil); s != 400 {
		t.Fatalf("empty patch = %d, want 400", s)
	}

	// Unknown lane id 404s.
	if s := arr.do("PATCH", "/api/admin/adventures/demo/lanes/999999",
		map[string]any{"mode": "linear"}, nil); s != 404 {
		t.Fatalf("unknown lane = %d, want 404", s)
	}
}
