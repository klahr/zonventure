package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

/* ---------------- Adventure ownership ---------------- */

func TestOwnership_OrgUserCanCreateAndEditOwnAdventure(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}

	// Create an adventure as org_admin.
	var created struct {
		Slug string `json:"slug"`
	}
	if s := boss.do("POST", "/api/admin/adventures",
		map[string]any{"slug": "org-trip", "title": "Org Trip"}, &created); s != 201 {
		t.Fatalf("create = %d", s)
	}

	// The caller can see it in their list AND it's marked mine_to_edit.
	var list struct {
		Adventures []struct {
			Slug       string `json:"slug"`
			MineToEdit bool   `json:"mine_to_edit"`
			OrgID      *int64 `json:"org_id"`
		} `json:"adventures"`
	}
	if s := boss.do("GET", "/api/admin/adventures", nil, &list); s != 200 {
		t.Fatalf("list = %d", s)
	}
	var found bool
	for _, a := range list.Adventures {
		if a.Slug == "org-trip" {
			found = true
			if !a.MineToEdit {
				t.Errorf("own adventure not marked editable")
			}
			if a.OrgID == nil {
				t.Errorf("own adventure should have org_id set")
			}
		}
	}
	if !found {
		t.Fatalf("own adventure not in listing")
	}

	// And can edit it.
	if s := boss.do("PATCH", "/api/admin/adventures/org-trip",
		map[string]any{"title": "Org Trip (edited)"}, nil); s != 204 {
		t.Fatalf("patch = %d", s)
	}
}

func TestOwnership_OrgUserCannotEditCatalogAdventure(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	// Demo adventure is owned by the catalog publisher org; boss's org
	// only holds a share for it.
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	if s := boss.do("PATCH", "/api/admin/adventures/demo",
		map[string]any{"title": "hacked"}, nil); s != 403 {
		t.Fatalf("patch catalog adventure = %d, want 403", s)
	}
	if s := boss.doAny("PUT", "/api/admin/adventures/demo/graph",
		graph.Envelope{Format: graph.FormatV2, Graph: *demoGraph()}, nil); s != 403 {
		t.Fatalf("save graph on catalog adventure = %d, want 403", s)
	}
}

func TestOwnership_OrgUserSeesCatalogAdventure(t *testing.T) {
	// Demo adventure is publisher-owned and shared to boss's org. Boss
	// must see it in their listing but with mine_to_edit=false.
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	var list struct {
		Adventures []struct {
			Slug       string `json:"slug"`
			MineToEdit bool   `json:"mine_to_edit"`
			OrgID      *int64 `json:"org_id"`
		} `json:"adventures"`
	}
	if s := boss.do("GET", "/api/admin/adventures", nil, &list); s != 200 {
		t.Fatalf("list = %d", s)
	}
	var demoRow *struct {
		Slug       string `json:"slug"`
		MineToEdit bool   `json:"mine_to_edit"`
		OrgID      *int64 `json:"org_id"`
	}
	for i := range list.Adventures {
		if list.Adventures[i].Slug == "demo" {
			demoRow = &list.Adventures[i]
		}
	}
	if demoRow == nil {
		t.Fatalf("demo adventure missing from catalog view")
	}
	if demoRow.MineToEdit {
		t.Errorf("demo shouldn't be editable by org user")
	}
	if demoRow.OrgID == nil {
		t.Errorf("demo should carry its publisher org_id")
	}
}

/* ---------------- Purchase flow ---------------- */

func TestPurchase_CreateConfirmProducesAssignment(t *testing.T) {
	ts, _, cleanup := newTestServerWith(t, simulatedPaymentsConfig)
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}

	// Create a pending purchase for the demo adventure (admin-owned).
	var created struct {
		ID          int64  `json:"id"`
		AmountCents int    `json:"amount_cents"`
		State       string `json:"state"`
		CheckoutURL string `json:"checkout_url"`
	}
	// 20 players: 15 free + 5 chargeable @ 500c = 2500c, simulated path.
	if s := boss.do("POST", "/api/admin/purchases",
		map[string]any{"adventure_slug": "demo", "max_players": 20}, &created); s != 201 {
		t.Fatalf("create purchase = %d", s)
	}
	if created.AmountCents != 5*500 {
		t.Errorf("amount = %d cents, want %d", created.AmountCents, 5*500)
	}
	if created.State != "pending" {
		t.Errorf("state = %q, want pending", created.State)
	}
	if created.CheckoutURL == "" {
		t.Errorf("missing checkout_url")
	}

	// Confirm: simulated click-through. Produces an assignment_id.
	var confirmed struct {
		State        string `json:"state"`
		AssignmentID int64  `json:"assignment_id"`
	}
	if s := boss.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm",
		map[string]any{}, &confirmed); s != 200 {
		t.Fatalf("confirm = %d", s)
	}
	if confirmed.State != "paid" || confirmed.AssignmentID == 0 {
		t.Fatalf("after confirm: %+v", confirmed)
	}

	// The assignment is now visible via the existing assignments endpoint.
	var assigns struct {
		Assignments []struct {
			ID            int64  `json:"id"`
			AdventureSlug string `json:"adventure_slug"`
			MaxPlayers    int    `json:"max_players"`
			EventID       *int64 `json:"event_id,omitempty"`
		} `json:"assignments"`
	}
	if s := boss.do("GET", "/api/admin/assignments", nil, &assigns); s != 200 {
		t.Fatalf("assignments = %d", s)
	}
	var hit bool
	for _, a := range assigns.Assignments {
		if a.ID == confirmed.AssignmentID {
			hit = true
			if a.AdventureSlug != "demo" || a.MaxPlayers != 20 {
				t.Errorf("assignment fields wrong: %+v", a)
			}
		}
	}
	if !hit {
		t.Fatalf("fresh assignment not listed")
	}
}

func TestPurchase_ConfirmIsIdempotent(t *testing.T) {
	ts, _, cleanup := newTestServerWith(t, simulatedPaymentsConfig)
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	// 20 players forces the simulated /confirm path; ≤15 short-circuits to free.
	if s := boss.do("POST", "/api/admin/purchases",
		map[string]any{"adventure_slug": "demo", "max_players": 20}, &created); s != 201 {
		t.Fatalf("create = %d", s)
	}
	// First confirm.
	var first struct {
		AssignmentID int64 `json:"assignment_id"`
	}
	if s := boss.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm",
		map[string]any{}, &first); s != 200 {
		t.Fatalf("first confirm = %d", s)
	}
	// Second confirm: should succeed without creating a second assignment.
	var second struct {
		AssignmentID int64  `json:"assignment_id"`
		State        string `json:"state"`
	}
	if s := boss.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm",
		map[string]any{}, &second); s != 200 {
		t.Fatalf("second confirm = %d", s)
	}
	if second.AssignmentID != first.AssignmentID {
		t.Fatalf("idempotence broken: %d -> %d", first.AssignmentID, second.AssignmentID)
	}
	if second.State != "paid" {
		t.Fatalf("state = %q after replay, want paid", second.State)
	}
}

func TestPurchase_RejectsHugeOrZeroSlotCount(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	for _, n := range []int{0, -3, 501, 10000} {
		s := boss.do("POST", "/api/admin/purchases",
			map[string]any{"adventure_slug": "demo", "max_players": n}, nil)
		if s != 400 {
			t.Errorf("max_players=%d got %d, want 400", n, s)
		}
	}
}

func TestPurchase_AdminCannotBuy(t *testing.T) {
	// Admins aren't org-scoped so buying wouldn't have a target — the
	// route is org-users only.
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	root := newClient(t, ts)
	if s := root.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	if s := root.do("POST", "/api/admin/purchases",
		map[string]any{"adventure_slug": "demo", "max_players": 20}, nil); s != 403 {
		t.Fatalf("admin buy status = %d, want 403", s)
	}
}
