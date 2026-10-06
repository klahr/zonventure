package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// doWithConfirm performs an authenticated request that needs the
// X-Confirm-Password header (delete-org, delete-member, delete-admin,
// delete-self). Returns status code and decoded JSON body if `into` is
// non-nil.
func (c *client) doWithConfirm(method, path, password string, body any, into any) int {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := c.csrfCookie(); tok != "" {
		req.Header.Set("X-CSRF-Token", tok)
	}
	req.Header.Set("X-Confirm-Password", password)
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if into != nil && resp.StatusCode < 400 && resp.StatusCode != http.StatusNoContent {
		_ = json.NewDecoder(resp.Body).Decode(into)
	}
	return resp.StatusCode
}

// TestOrganizations covers organisation, member and admin management on
// one server. Subtests make their own orgs and users under distinct
// names; only "admins CRUD and guards" counts admins, and no other
// subtest adds one.
func TestOrganizations(t *testing.T) {
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	t.Run("create requires admin", func(t *testing.T) {
		// org_admin (non-global) should be 403 on the admin-only route.
		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		if s := boss.do("POST", "/api/admin/organizations", map[string]string{"name": "Sneaky"}, nil); s != 403 {
			t.Fatalf("org_admin create = %d, want 403", s)
		}

		// Anon: 401.
		anon := newClient(t, ts)
		if s := anon.do("POST", "/api/admin/organizations", map[string]string{"name": "Sneaky"}, nil); s != 401 {
			t.Fatalf("anon create = %d, want 401", s)
		}

		// Global admin: 201.
		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("root login = %d", s)
		}
		var created struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if s := root.do("POST", "/api/admin/organizations", map[string]string{"name": "Acme Inc"}, &created); s != 201 {
			t.Fatalf("admin create = %d, want 201", s)
		}
		if created.ID == 0 || created.Name != "Acme Inc" {
			t.Fatalf("unexpected: %+v", created)
		}
	})

	t.Run("create rejects duplicate and empty", func(t *testing.T) {
		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}

		// Empty name → 400.
		if s := root.do("POST", "/api/admin/organizations", map[string]string{"name": "   "}, nil); s != 400 {
			t.Fatalf("empty = %d, want 400", s)
		}
		// "TestOrg" was seeded; collision → 409.
		if s := root.do("POST", "/api/admin/organizations", map[string]string{"name": "TestOrg"}, nil); s != 409 {
			t.Fatalf("dup = %d, want 409", s)
		}
	})

	t.Run("get cross-org forbidden", func(t *testing.T) {
		var otherOrgID int64
		if err := db.QueryRow(`SELECT id FROM organizations WHERE name = 'OtherOrg'`).Scan(&otherOrgID); err != nil {
			t.Fatalf("lookup other org: %v", err)
		}

		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		// Own org: 200.
		if s := boss.do("GET", "/api/admin/organizations/"+itoa(orgID), nil, nil); s != 200 {
			t.Fatalf("own = %d, want 200", s)
		}
		// Cross-org: 403.
		if s := boss.do("GET", "/api/admin/organizations/"+itoa(otherOrgID), nil, nil); s != 403 {
			t.Fatalf("cross-org = %d, want 403", s)
		}

		// Unknown id (admin-tier): 404.
		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("root login = %d", s)
		}
		if s := root.do("GET", "/api/admin/organizations/999999", nil, nil); s != 404 {
			t.Fatalf("unknown = %d, want 404", s)
		}
	})

	t.Run("delete requires password confirm", func(t *testing.T) {
		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}

		// Seed a disposable org so we don't tear down a fixture.
		var doomedID int64
		if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Doomed') RETURNING id`).Scan(&doomedID); err != nil {
			t.Fatalf("seed doomed: %v", err)
		}

		// Without header: 401.
		if s := root.do("DELETE", "/api/admin/organizations/"+itoa(doomedID), nil, nil); s != 401 {
			t.Fatalf("no-header delete = %d, want 401", s)
		}
		// Wrong password: 401.
		if s := root.doWithConfirm("DELETE", "/api/admin/organizations/"+itoa(doomedID), "nope", nil, nil); s != 401 {
			t.Fatalf("wrong-pw delete = %d, want 401", s)
		}
		// Correct password: 204.
		if s := root.doWithConfirm("DELETE", "/api/admin/organizations/"+itoa(doomedID), "rootpw", nil, nil); s != 204 {
			t.Fatalf("good delete = %d, want 204", s)
		}

		// Verify the row is gone.
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM organizations WHERE id = $1`, doomedID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("org survived delete (count=%d)", n)
		}
	})

	t.Run("delete cascades members and assignments", func(t *testing.T) {
		// Seed a disposable org with one member + one assignment so we can
		// verify the cascade fires on delete.
		var orgID int64
		if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Cascadia') RETURNING id`).Scan(&orgID); err != nil {
			t.Fatalf("seed org: %v", err)
		}
		var cascID int64
		if err := db.QueryRow(
			`INSERT INTO users(username, password_hash, email_verified) VALUES ('casc', 'x', 1) RETURNING id`,
		).Scan(&cascID); err != nil {
			t.Fatalf("seed member: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
			cascID, orgID,
		); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO adventure_assignments(org_id, adventure_slug, max_players) VALUES ($1, 'demo', 5)`,
			orgID,
		); err != nil {
			t.Fatalf("seed assignment: %v", err)
		}

		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		if s := root.doWithConfirm("DELETE", "/api/admin/organizations/"+itoa(orgID), "rootpw", nil, nil); s != 204 {
			t.Fatalf("delete = %d, want 204", s)
		}

		// Post-migration 0020: users no longer carries org_id; org
		// membership is on org_memberships, which CASCADE-deletes when
		// the org row goes away. The deleted-org count surfaces as
		// "no rows in org_memberships for this org_id".
		var memberships, assns int
		if err := db.QueryRow(`SELECT COUNT(*) FROM org_memberships WHERE org_id = $1`, orgID).Scan(&memberships); err != nil {
			t.Fatalf("count memberships: %v", err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM adventure_assignments WHERE org_id = $1`, orgID).Scan(&assns); err != nil {
			t.Fatalf("count assns: %v", err)
		}
		if memberships != 0 || assns != 0 {
			t.Fatalf("cascade incomplete: memberships=%d assignments=%d (want 0/0)", memberships, assns)
		}
	})

	t.Run("members create and remove", func(t *testing.T) {
		// org_admin "boss" can add and remove members in their own org.
		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}

		var added struct {
			ID   int64  `json:"id"`
			Role string `json:"role"`
		}
		body := map[string]string{"username": "newbie", "password": "longpw01", "role": "org_member"}
		if s := boss.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", body, &added); s != 201 {
			t.Fatalf("add member = %d, want 201", s)
		}
		if added.Role != "org_member" || added.ID == 0 {
			t.Fatalf("unexpected: %+v", added)
		}

		// Bad role → 400.
		bad := map[string]string{"username": "x", "password": "longpw01", "role": "wizard"}
		if s := boss.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", bad, nil); s != 400 {
			t.Fatalf("bad role = %d, want 400", s)
		}

		// Self-delete is refused with 409. Look up boss's real id to hit
		// the guard cleanly (a zero/invalid id would 400 earlier).
		var bossID int64
		if err := db.QueryRow(`SELECT id FROM users WHERE username = 'boss'`).Scan(&bossID); err != nil {
			t.Fatalf("lookup boss: %v", err)
		}
		if s := boss.doWithConfirm("DELETE", "/api/admin/organizations/"+itoa(orgID)+"/members/"+itoa(bossID),
			"s3cret", nil, nil); s != 409 {
			t.Fatalf("self-delete = %d, want 409", s)
		}

		// Removing the just-added member needs password confirm; 204 on success.
		if s := boss.doWithConfirm("DELETE", "/api/admin/organizations/"+itoa(orgID)+"/members/"+itoa(added.ID),
			"s3cret", nil, nil); s != 204 {
			t.Fatalf("remove member = %d, want 204", s)
		}
	})

	t.Run("members cross-org forbidden", func(t *testing.T) {
		var otherOrgID int64
		if err := db.QueryRow(`SELECT id FROM organizations WHERE name = 'OtherOrg'`).Scan(&otherOrgID); err != nil {
			t.Fatalf("lookup other: %v", err)
		}

		// boss is org_admin of TestOrg, not OtherOrg → can't add to OtherOrg.
		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		body := map[string]string{"username": "intruder", "password": "longpw01", "role": "org_member"}
		if s := boss.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", body, nil); s != 403 {
			t.Fatalf("cross-org create = %d, want 403", s)
		}
	})

	t.Run("admins CRUD and guards", func(t *testing.T) {
		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}

		// Find the root user id, then create a second admin so we have two,
		// then delete the second — that should succeed. Try to delete the
		// first (the only remaining admin) → 409.
		var list struct {
			Admins []struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"admins"`
		}
		if s := root.do("GET", "/api/admin/admins", nil, &list); s != 200 {
			t.Fatalf("list = %d", s)
		}
		if len(list.Admins) != 1 {
			t.Fatalf("expected one seeded admin, got %d", len(list.Admins))
		}
		rootID := list.Admins[0].ID

		var second struct {
			ID int64 `json:"id"`
		}
		body := map[string]string{"username": "second", "password": "secondpw"}
		// Creating an admin requires password re-confirmation, like deleting one.
		if s := root.do("POST", "/api/admin/admins", body, nil); s != 401 {
			t.Fatalf("no-confirm create = %d, want 401", s)
		}
		if s := root.doWithConfirm("POST", "/api/admin/admins", "rootpw", body, &second); s != 201 {
			t.Fatalf("create second admin = %d, want 201", s)
		}

		// Delete-without-password-confirm refused.
		if s := root.do("DELETE", "/api/admin/admins/"+itoa(second.ID), nil, nil); s != 401 {
			t.Fatalf("no-confirm delete = %d, want 401", s)
		}
		// Wrong password refused.
		if s := root.doWithConfirm("DELETE", "/api/admin/admins/"+itoa(second.ID), "nope", nil, nil); s != 401 {
			t.Fatalf("wrong-pw delete = %d, want 401", s)
		}

		// Delete the second one — should succeed (still leaves root).
		if s := root.doWithConfirm("DELETE", "/api/admin/admins/"+itoa(second.ID), "rootpw", nil, nil); s != 204 {
			t.Fatalf("delete second = %d, want 204", s)
		}

		// Self-delete is refused with 409 (root is the caller and target).
		if s := root.doWithConfirm("DELETE", "/api/admin/admins/"+itoa(rootID), "rootpw", nil, nil); s != 409 {
			t.Fatalf("self-delete = %d, want 409", s)
		}

		// Deleting a missing user → 404.
		if s := root.doWithConfirm("DELETE", "/api/admin/admins/999999", "rootpw", nil, nil); s != 404 {
			t.Fatalf("missing = %d, want 404", s)
		}
	})
}

// Note on the last-admin guard: DeleteAdmin's `remaining <= 1` check is
// gated by the admin-only route group plus the self-delete check, so
// in normal flow it's unreachable. The self-delete coverage above is
// the load-bearing protection.
