package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/server"
)

// TestCreate_RequiresAuthor: creating content is an authoring act, so
// an org_member (who can run events but not author) is refused on
// every create path.
func TestCreate_RequiresAuthor(t *testing.T) {
	ts, _, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	body := map[string]string{"username": "member", "password": "longpw01", "role": "org_member"}
	if s := boss.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", body, nil); s != 201 {
		t.Fatalf("add member = %d", s)
	}
	member := newClient(t, ts)
	if s := member.do("POST", "/api/auth/login", map[string]string{"username": "member", "password": "longpw01"}, nil); s != 200 {
		t.Fatalf("member login = %d", s)
	}
	if s := member.do("POST", "/api/admin/adventures", map[string]any{"slug": "m-trip", "title": "M"}, nil); s != 403 {
		t.Errorf("member create adventure = %d, want 403", s)
	}
}

// TestLobby_CannotReachContent: a user with no organisation passes the
// /api/admin gate but has no org to scope content to — the list
// endpoints used to dereference the nil org and panic, and creates
// used to land in the global catalog.
func TestLobby_CannotReachContent(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()
	hash, _ := auth.HashPassword("longpw01")
	if _, err := db.Exec(`INSERT INTO users(username, password_hash, email_verified) VALUES ('drifter', $1, 1)`, hash); err != nil {
		t.Fatalf("seed lobby user: %v", err)
	}
	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "drifter", "password": "longpw01"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	for _, path := range []string{"/api/admin/adventures"} {
		if s := c.do("GET", path, nil, nil); s != 403 {
			t.Errorf("GET %s = %d, want 403", path, s)
		}
	}
	if s := c.do("POST", "/api/admin/adventures", map[string]any{"slug": "lobby-trip", "title": "L"}, nil); s != 403 {
		t.Errorf("create adventure = %d, want 403", s)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM adventures WHERE slug = 'lobby-trip'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("lobby create left a row (n=%d, err=%v)", n, err)
	}
}

func login(t *testing.T, c *client, user, pass string) {
	t.Helper()
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": user, "password": pass}, nil); s != 200 {
		t.Fatalf("%s login = %d", user, s)
	}
}

// orgIDByName looks an org up through the operator's list.
func orgIDByName(t *testing.T, root *client, name string) int64 {
	t.Helper()
	var list struct {
		Organizations []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			IsPublisher bool   `json:"is_publisher"`
		} `json:"organizations"`
	}
	if s := root.do("GET", "/api/admin/organizations", nil, &list); s != 200 {
		t.Fatalf("list orgs = %d", s)
	}
	for _, o := range list.Organizations {
		if o.Name == name {
			return o.ID
		}
	}
	t.Fatalf("org %q not found", name)
	return 0
}

// TestAdmin_CannotAuthorOrRunEvents: the platform admin manages orgs
// and monitors; every authoring and event-running route refuses them.
func TestAdmin_CannotAuthorOrRunEvents(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	boss := newClient(t, ts)
	login(t, boss, "boss", "s3cret")
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/admin/adventures"},
		{"POST", "/api/admin/adventures"},
		{"GET", "/api/admin/adventures/demo"},
		{"GET", "/api/admin/adventures/demo/edit"},
		{"PATCH", "/api/admin/adventures/demo"},
		{"GET", "/api/admin/adventures/demo/graph"},
		{"PUT", "/api/admin/adventures/demo/graph"},
		{"POST", "/api/admin/adventures/demo/pages"},
		{"GET", "/api/admin/events"},
		{"POST", "/api/admin/events"},
		{"GET", "/api/admin/events/" + itoa(ev.ID)},
		{"POST", "/api/admin/events/" + itoa(ev.ID) + "/start"},
		{"POST", "/api/admin/events/" + itoa(ev.ID) + "/teams"},
		{"GET", "/api/admin/assignments"},
		{"GET", "/api/admin/simulator"},
		{"GET", "/api/admin/webhooks"},
		{"GET", "/ws/arrange"},
	} {
		if s := root.do(c.method, c.path, map[string]any{}, nil); s != 403 {
			t.Errorf("admin %s %s = %d, want 403", c.method, c.path, s)
		}
	}
}

// TestOperator_OrgEvents: the operator lists an org's events without
// the live token, and cancels one only through its own org's path.
func TestOperator_OrgEvents(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	boss := newClient(t, ts)
	login(t, boss, "boss", "s3cret")
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	orgID := orgIDByName(t, root, "TestOrg")
	pubID := orgIDByName(t, root, "Catalog")

	var list struct {
		Events []struct {
			ID        int64  `json:"id"`
			LiveToken string `json:"live_token"`
		} `json:"events"`
	}
	if s := root.do("GET", "/api/admin/organizations/"+itoa(orgID)+"/events", nil, &list); s != 200 {
		t.Fatalf("org events = %d", s)
	}
	if len(list.Events) != 1 || list.Events[0].ID != ev.ID {
		t.Fatalf("org events = %+v, want the one event", list.Events)
	}
	if list.Events[0].LiveToken != "" {
		t.Errorf("operator list leaked the live token")
	}
	if s := boss.do("GET", "/api/admin/organizations/"+itoa(orgID)+"/events", nil, nil); s != 403 {
		t.Errorf("org user on operator list = %d, want 403", s)
	}

	cancel := func(org int64, pw string) int {
		return root.doWithConfirm("POST", "/api/admin/organizations/"+itoa(org)+"/events/"+itoa(ev.ID)+"/cancel", pw, nil, nil)
	}
	if s := cancel(pubID, "rootpw"); s != 404 {
		t.Errorf("cancel via another org = %d, want 404", s)
	}
	if s := cancel(orgID, "nope"); s != 401 {
		t.Errorf("cancel with wrong password = %d, want 401", s)
	}
	if s := cancel(orgID, "rootpw"); s != 204 {
		t.Fatalf("cancel = %d, want 204", s)
	}
	if s := cancel(orgID, "rootpw"); s != 409 {
		t.Errorf("re-cancel = %d, want 409", s)
	}
}

// TestCatalogCuration: the publisher authors, the operator lists /
// unlists and commissions; neither can do the other's part.
func TestCatalogCuration(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	pub := newClient(t, ts)
	login(t, pub, "pub", "pubpw")
	boss := newClient(t, ts)
	login(t, boss, "boss", "s3cret")
	orgID := orgIDByName(t, root, "TestOrg")
	pubID := orgIDByName(t, root, "Catalog")

	// The author can't list their own work in the catalog.
	if s := pub.do("PATCH", "/api/admin/adventures/demo", map[string]any{"published": true}, nil); s != 400 {
		t.Errorf("author sets published = %d, want 400", s)
	}
	// Boss's own adventure isn't the operator's to curate.
	if s := boss.do("POST", "/api/admin/adventures", map[string]any{"slug": "boss-trip", "title": "B"}, nil); s != 201 {
		t.Fatalf("boss create = %d", s)
	}
	if s := root.do("PUT", "/api/admin/catalog/boss-trip/published", map[string]any{"published": true}, nil); s != 404 {
		t.Errorf("publish org adventure = %d, want 404", s)
	}
	if s := root.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/assignments",
		map[string]any{"adventure_slug": "boss-trip", "max_players": 5}, nil); s != 400 {
		t.Errorf("assign org adventure = %d, want 400", s)
	}

	var cat struct {
		PublisherOrgID int64 `json:"publisher_org_id"`
		Adventures     []struct {
			Slug      string `json:"slug"`
			Published bool   `json:"published"`
			Shares    []struct {
				OrgID int64 `json:"org_id"`
			} `json:"shares"`
		} `json:"adventures"`
	}
	if s := root.do("GET", "/api/admin/catalog", nil, &cat); s != 200 {
		t.Fatalf("catalog = %d", s)
	}
	if cat.PublisherOrgID != pubID || len(cat.Adventures) != 1 || cat.Adventures[0].Slug != "demo" || cat.Adventures[0].Published {
		t.Fatalf("catalog = %+v", cat)
	}
	if len(cat.Adventures[0].Shares) != 1 || cat.Adventures[0].Shares[0].OrgID != orgID {
		t.Errorf("demo shares = %+v, want TestOrg", cat.Adventures[0].Shares)
	}

	// Listing makes a catalog adventure visible to every org.
	visible := func(c *client, slug string) bool {
		var list struct {
			Adventures []struct {
				Slug string `json:"slug"`
			} `json:"adventures"`
		}
		c.do("GET", "/api/admin/adventures", nil, &list)
		for _, a := range list.Adventures {
			if a.Slug == slug {
				return true
			}
		}
		return false
	}
	if s := pub.do("POST", "/api/admin/adventures", map[string]any{"slug": "new-tour", "title": "N"}, nil); s != 201 {
		t.Fatalf("publisher create = %d", s)
	}
	if visible(boss, "new-tour") {
		t.Errorf("unlisted catalog adventure visible to boss")
	}
	if s := root.do("PUT", "/api/admin/catalog/new-tour/published", map[string]any{"published": true}, nil); s != 204 {
		t.Fatalf("publish = %d", s)
	}
	if !visible(boss, "new-tour") {
		t.Errorf("listed catalog adventure not visible to boss")
	}
	if s := root.do("DELETE", "/api/admin/catalog/demo/shares/"+itoa(orgID), nil, nil); s != 204 {
		t.Fatalf("revoke = %d", s)
	}
	if s := root.do("POST", "/api/admin/catalog/demo/shares", map[string]any{"org_id": pubID}, nil); s != 400 {
		t.Errorf("share to the publisher itself = %d, want 400", s)
	}

	// One publisher at a time, and it can't be deleted.
	if s := root.do("PUT", "/api/admin/organizations/"+itoa(orgID)+"/publisher", map[string]any{"is_publisher": true}, nil); s != 409 {
		t.Errorf("second publisher = %d, want 409", s)
	}
	if s := root.doWithConfirm("DELETE", "/api/admin/organizations/"+itoa(pubID), "rootpw", nil, nil); s != 409 {
		t.Errorf("delete publisher = %d, want 409", s)
	}
	if s := boss.do("GET", "/api/admin/catalog", nil, nil); s != 403 {
		t.Errorf("org user on catalog = %d, want 403", s)
	}
}
