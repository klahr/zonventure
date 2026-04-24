package server_test

import "testing"

func TestLibrary_CreateStubAndList(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}

	// Before: the demo adventure has 3 attached pages, zero in library.
	var before struct {
		Puzzles []struct {
			ID            int64  `json:"id"`
			AdventureSlug string `json:"adventure_slug"`
		} `json:"puzzles"`
	}
	if s := admin.do("GET", "/api/admin/puzzles", nil, &before); s != 200 {
		t.Fatalf("list = %d", s)
	}
	baseline := len(before.Puzzles)
	libBaseline := 0
	for _, p := range before.Puzzles {
		if p.AdventureSlug == "" {
			libBaseline++
		}
	}
	if libBaseline != 0 {
		t.Fatalf("expected no orphan puzzles from demo seed, got %d", libBaseline)
	}

	// Create a stub at some coordinates (no prompt, no accept — the
	// "just remember this spot" flow).
	var created struct {
		ID int64 `json:"id"`
	}
	if s := admin.do("POST", "/api/admin/puzzles",
		map[string]any{"lat": 59.3508, "lng": 18.0699, "notes": "corner bench at Odenplan"},
		&created); s != 201 {
		t.Fatalf("create stub = %d", s)
	}
	if created.ID <= 0 {
		t.Fatalf("no id returned")
	}

	// Listing should now have one extra, with adventure_slug empty and
	// the notes we supplied.
	var after struct {
		Puzzles []struct {
			ID            int64  `json:"id"`
			Title         string `json:"title"`
			Notes         string `json:"notes"`
			AdventureSlug string `json:"adventure_slug"`
		} `json:"puzzles"`
	}
	if s := admin.do("GET", "/api/admin/puzzles", nil, &after); s != 200 {
		t.Fatalf("list = %d", s)
	}
	if len(after.Puzzles) != baseline+1 {
		t.Fatalf("expected baseline+1 puzzles, got %d (was %d)", len(after.Puzzles), baseline)
	}
	var found bool
	for _, p := range after.Puzzles {
		if p.ID == created.ID {
			found = true
			if p.AdventureSlug != "" {
				t.Errorf("new stub shouldn't be attached, got %q", p.AdventureSlug)
			}
			if p.Notes != "corner bench at Odenplan" {
				t.Errorf("notes = %q, want corner bench…", p.Notes)
			}
		}
	}
	if !found {
		t.Fatalf("new stub not in listing")
	}
}

func TestLibrary_GetSinglePuzzle(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	var st struct {
		ID int64 `json:"id"`
	}
	if s := admin.do("POST", "/api/admin/puzzles",
		map[string]any{"lat": 59, "lng": 18, "title": "Found obj", "notes": "red"}, &st); s != 201 {
		t.Fatalf("create = %d", s)
	}
	var got struct {
		ID             int64    `json:"id"`
		Title          string   `json:"title"`
		Notes          string   `json:"notes"`
		Accept         []string `json:"accept"`
		AdventureSlug  string   `json:"adventure_slug"`
		Images         []any    `json:"images"`
	}
	if s := admin.do("GET", "/api/admin/puzzles/"+itoa(st.ID), nil, &got); s != 200 {
		t.Fatalf("get = %d", s)
	}
	if got.Title != "Found obj" || got.Notes != "red" {
		t.Fatalf("fields lost: %+v", got)
	}
	if got.AdventureSlug != "" {
		t.Fatalf("library puzzle should have no adventure, got %q", got.AdventureSlug)
	}
	if got.Accept == nil || len(got.Accept) != 0 {
		t.Errorf("accept should be []: %+v", got.Accept)
	}
}

func TestLibrary_CopyToAdventure(t *testing.T) {
	// Create an orphan stub, flesh it out, then copy it into the demo
	// adventure. The copy must gain an adventure_slug while the
	// original stays in the library.
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	var stub struct {
		ID int64 `json:"id"`
	}
	if s := admin.do("POST", "/api/admin/puzzles",
		map[string]any{"lat": 59.3, "lng": 18.1, "title": "bonus"}, &stub); s != 201 {
		t.Fatalf("create = %d", s)
	}
	// Flesh out: set a real prompt + accept list via the existing PATCH.
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(stub.ID), map[string]any{
		"title":  "bonus",
		"prompt": "What year?",
		"hint":   "",
		"accept": []string{"1999"},
		"lat":    59.3,
		"lng":    18.1,
	}, nil); s != 204 {
		t.Fatalf("patch = %d", s)
	}

	// Count demo pages before copy.
	var before struct {
		Pages []struct{ ID int64 } `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &before); s != 200 {
		t.Fatalf("edit = %d", s)
	}
	beforeN := len(before.Pages)

	// Copy into demo.
	var copied struct {
		ID    int64 `json:"id"`
		Index int   `json:"index"`
	}
	if s := admin.do("POST",
		"/api/admin/adventures/demo/pages/copy-from/"+itoa(stub.ID), map[string]any{},
		&copied); s != 201 {
		t.Fatalf("copy = %d", s)
	}
	if copied.ID == stub.ID {
		t.Fatalf("copy returned the source id, not a fresh row")
	}
	if copied.Index != beforeN {
		t.Fatalf("copy index = %d, want %d (next slot)", copied.Index, beforeN)
	}

	// The source should still exist as a library puzzle.
	var src struct {
		AdventureSlug string `json:"adventure_slug"`
	}
	if s := admin.do("GET", "/api/admin/puzzles/"+itoa(stub.ID), nil, &src); s != 200 {
		t.Fatalf("get src = %d", s)
	}
	if src.AdventureSlug != "" {
		t.Fatalf("source puzzle moved instead of copied — adventure_slug=%q", src.AdventureSlug)
	}

	// The copy should be attached to demo, carrying the fleshed content.
	var cp struct {
		AdventureSlug string   `json:"adventure_slug"`
		Title         string   `json:"title"`
		Accept        []string `json:"accept"`
	}
	if s := admin.do("GET", "/api/admin/puzzles/"+itoa(copied.ID), nil, &cp); s != 200 {
		t.Fatalf("get copy = %d", s)
	}
	if cp.AdventureSlug != "demo" {
		t.Fatalf("copy adventure_slug = %q, want demo", cp.AdventureSlug)
	}
	if cp.Title != "bonus" || len(cp.Accept) != 1 || cp.Accept[0] != "1999" {
		t.Fatalf("copy didn't carry content: %+v", cp)
	}
}

func TestLibrary_StubRejectsBadCoords(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	if s := admin.do("POST", "/api/admin/puzzles",
		map[string]any{"lat": 200.0, "lng": 0.0}, nil); s != 400 {
		t.Fatalf("bad lat status = %d, want 400", s)
	}
}

func TestLibrary_OrgUserCanAuthorOwnPuzzles(t *testing.T) {
	// Org users can now author their own puzzles and see the admin
	// global library. This test pins the visibility boundary — org
	// users can see admin-owned library stubs AND their own, but must
	// not be able to edit admin-owned stubs.
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	// Admin creates a global-library stub (org_id NULL).
	root := newClient(t, ts)
	if s := root.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	var adminStub struct {
		ID int64 `json:"id"`
	}
	if s := root.do("POST", "/api/admin/puzzles",
		map[string]any{"lat": 59, "lng": 18, "title": "admin global"}, &adminStub); s != 201 {
		t.Fatalf("admin create = %d", s)
	}

	// Org user creates their own stub, lists, and tries to edit the
	// admin's.
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	var bossStub struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/puzzles",
		map[string]any{"lat": 60, "lng": 19, "title": "boss local"}, &bossStub); s != 201 {
		t.Fatalf("org user should be able to create library stubs, got %d", s)
	}

	// Listing must include both.
	var list struct {
		Puzzles []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"puzzles"`
	}
	if s := boss.do("GET", "/api/admin/puzzles", nil, &list); s != 200 {
		t.Fatalf("list = %d", s)
	}
	var sawAdmin, sawBoss bool
	for _, p := range list.Puzzles {
		if p.ID == adminStub.ID {
			sawAdmin = true
		}
		if p.ID == bossStub.ID {
			sawBoss = true
		}
	}
	if !sawAdmin || !sawBoss {
		t.Fatalf("org user missing visibility: admin=%v boss=%v", sawAdmin, sawBoss)
	}

	// But the org user MUST NOT be able to edit the admin-owned stub.
	s := boss.do("PATCH", "/api/admin/pages/"+itoa(adminStub.ID), map[string]any{
		"title": "hacked", "lat": 0, "lng": 0,
	}, nil)
	if s != 403 {
		t.Fatalf("org user patched admin stub, status=%d (want 403)", s)
	}
}

