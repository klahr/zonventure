package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		// org_admin "boss" can add and remove members in their own org;
		// a global admin may still create one with a password.
		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		root := newClient(t, ts)
		login(t, root, "root", "rootpw")

		body := map[string]string{"username": "newbie", "password": "longpw01", "role": "org_member"} // gitleaks:allow (test fixture)
		if s := root.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", body, nil); s != 202 {
			t.Fatalf("add member = %d, want 202", s)
		}
		// An org admin invites by email; without one there is no one to invite.
		if s := boss.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", map[string]string{"username": "noaddress", "role": "org_member"}, nil); s != 400 {
			t.Fatalf("org admin adding without email = %d, want 400", s)
		}
		var added struct{ ID int64 }
		if err := db.QueryRow(`SELECT u.id FROM users u JOIN org_memberships m ON m.user_id = u.id WHERE u.username = 'newbie' AND m.org_id = $1 AND m.role = 'org_member'`, orgID).Scan(&added.ID); err != nil {
			t.Fatalf("newbie membership: %v", err)
		}

		// An event host reads the org but not its members.
		newbie := newClient(t, ts)
		if s := newbie.do("POST", "/api/auth/login", map[string]string{"username": "newbie", "password": "longpw01"}, nil); s != 200 { // gitleaks:allow (test fixture)
			t.Fatalf("newbie login = %d", s)
		}
		var seen struct {
			Members []struct {
				Username string `json:"username"`
			} `json:"members"`
		}
		if s := newbie.do("GET", "/api/admin/organizations/"+itoa(orgID), nil, &seen); s != 200 || len(seen.Members) != 0 {
			t.Fatalf("event host's org = %d %+v, want 200 and no members", s, seen.Members)
		}
		if s := boss.do("GET", "/api/admin/organizations/"+itoa(orgID), nil, &seen); s != 200 || len(seen.Members) < 2 {
			t.Fatalf("org admin's org = %d %+v, want the members", s, seen.Members)
		}

		// A common password is refused, like a self-service change.
		weak := map[string]string{"username": "weakling", "password": "password1", "role": "org_member"}
		if s := root.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", weak, nil); s != 400 {
			t.Fatalf("common password = %d, want 400", s)
		}

		// An org admin can't look users up at all: it would tell them
		// which usernames and addresses have accounts.
		if s := boss.do("GET", "/api/admin/users/search?q=NEWBIE", nil, nil); s != 403 {
			t.Fatalf("org admin search = %d, want 403", s)
		}

		// Bad role → 400.
		bad := map[string]string{"username": "x", "email": "x@example.test", "role": "wizard"}
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

	t.Run("existing account is invited, not added", func(t *testing.T) {
		var otherOrgID int64
		if err := db.QueryRow(`SELECT id FROM organizations WHERE name = 'OtherOrg'`).Scan(&otherOrgID); err != nil {
			t.Fatalf("lookup other: %v", err)
		}
		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		root := newClient(t, ts)
		if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
			t.Fatalf("root login = %d", s)
		}
		body := map[string]string{"username": "joiner", "password": "longpw01", "role": "org_member", "email": "joiner@example.test"} // gitleaks:allow (test fixture)
		var created map[string]any
		if s := root.do("POST", "/api/admin/organizations/"+itoa(orgID)+"/members", body, &created); s != 202 {
			t.Fatalf("add member = %d, want 202", s)
		}
		var byAdmin, verified, tokens int
		if err := db.QueryRow(`SELECT password_set_by_admin, email_verified, (SELECT COUNT(*) FROM email_verifications v WHERE v.user_id = users.id AND v.purpose = 'verify') FROM users WHERE username = 'joiner'`).Scan(&byAdmin, &verified, &tokens); err != nil || byAdmin != 1 {
			t.Fatalf("password_set_by_admin = %d (%v), want 1", byAdmin, err)
		}
		// The address is stored unverified and the verification mail goes out at once.
		if verified != 0 || tokens != 1 {
			t.Fatalf("email_verified = %d, verify tokens = %d; want 0 and 1", verified, tokens)
		}

		// OtherOrg's admin adds by joiner's address: an invitation,
		// answered exactly like creating a new account. A username
		// alone never finds an existing account for an org admin.
		other := newClient(t, ts)
		var otherBossID int64
		if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) SELECT 'otherboss', password_hash, 1 FROM users WHERE username = 'boss' RETURNING id`).Scan(&otherBossID); err != nil {
			t.Fatalf("seed otherboss: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, otherBossID, otherOrgID); err != nil {
			t.Fatalf("seed otherboss membership: %v", err)
		}
		if s := other.do("POST", "/api/auth/login", map[string]string{"username": "otherboss", "password": "s3cret"}, nil); s != 200 { // gitleaks:allow (test fixture)
			t.Fatalf("otherboss login = %d", s)
		}
		byName := map[string]string{"username": "joiner", "role": "org_member"}
		if s := other.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", byName, nil); s != 400 {
			t.Fatalf("org admin adding by username alone = %d, want 400 (email required)", s)
		}
		byEmail := map[string]string{"username": "whatever", "password": "longpw01", "role": "org_member", "email": "JOINER@example.test"} // gitleaks:allow (test fixture)
		var invitedAnswer map[string]any
		if s := other.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", byEmail, &invitedAnswer); s != 202 || fmt.Sprint(invitedAnswer) != fmt.Sprint(created) {
			t.Fatalf("invite by email = %d %v, want 202 %v", s, invitedAnswer, created)
		}
		var org struct {
			Invitations []struct {
				UserID int64  `json:"user_id"`
				Email  string `json:"email"`
			} `json:"invitations"`
		}
		if s := other.do("GET", "/api/admin/organizations/"+itoa(otherOrgID), nil, &org); s != 200 || len(org.Invitations) != 1 || org.Invitations[0].Email != "JOINER@example.test" {
			t.Fatalf("pending invitations = %d %+v, want joiner's, by the address typed", s, org.Invitations)
		}
		// Withdrawn, it no longer works.
		if s := boss.do("DELETE", "/api/admin/organizations/"+itoa(otherOrgID)+"/invitations/"+itoa(org.Invitations[0].UserID), nil, nil); s != 403 {
			t.Fatalf("another org's admin withdrawing = %d, want 403", s)
		}
		if s := other.do("DELETE", "/api/admin/organizations/"+itoa(otherOrgID)+"/invitations/"+itoa(org.Invitations[0].UserID), nil, nil); s != 204 {
			t.Fatalf("withdraw = %d, want 204", s)
		}
		joinerEarly := newClient(t, ts)
		if s := joinerEarly.do("POST", "/api/auth/login", map[string]string{"username": "joiner", "password": "longpw01"}, nil); s != 200 { // gitleaks:allow (test fixture)
			t.Fatalf("joiner login = %d", s)
		}
		if s := joinerEarly.do("POST", "/api/admin/me/org-invitations/"+itoa(otherOrgID)+"/accept", nil, nil); s != 404 {
			t.Fatalf("accepting a withdrawn invitation = %d, want 404", s)
		}
		// Declined, it stays on the org's list marked so, and leaves the
		// person's; inviting again asks anew.
		if s := other.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", byEmail, nil); s != 202 {
			t.Fatalf("invite again = %d, want 202", s)
		}
		if s := joinerEarly.do("POST", "/api/admin/me/org-invitations/"+itoa(otherOrgID)+"/decline", nil, nil); s != 204 {
			t.Fatalf("decline = %d, want 204", s)
		}
		var declinedOrg struct {
			Invitations []struct {
				Declined bool `json:"declined"`
			} `json:"invitations"`
		}
		if s := other.do("GET", "/api/admin/organizations/"+itoa(otherOrgID), nil, &declinedOrg); s != 200 || len(declinedOrg.Invitations) != 1 || !declinedOrg.Invitations[0].Declined {
			t.Fatalf("after decline = %d %+v, want one declined", s, declinedOrg.Invitations)
		}
		var declinedWho struct {
			Invitations []struct{} `json:"org_invitations"`
		}
		if s := joinerEarly.do("GET", "/api/auth/whoami", nil, &declinedWho); s != 200 || len(declinedWho.Invitations) != 0 {
			t.Fatalf("whoami after decline = %d %+v, want no invitation", s, declinedWho)
		}
		if s := joinerEarly.do("POST", "/api/admin/me/org-invitations/"+itoa(otherOrgID)+"/accept", nil, nil); s != 404 {
			t.Fatalf("accepting a declined invitation = %d, want 404", s)
		}
		if s := other.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", byEmail, nil); s != 202 {
			t.Fatalf("re-invite after decline = %d, want 202", s)
		}
		var reinvited struct {
			Invitations []struct {
				Declined bool `json:"declined"`
			} `json:"invitations"`
		}
		if s := other.do("GET", "/api/admin/organizations/"+itoa(otherOrgID), nil, &reinvited); s != 200 || len(reinvited.Invitations) != 1 || reinvited.Invitations[0].Declined {
			t.Fatalf("after re-invite = %d %+v, want one pending", s, reinvited.Invitations)
		}
		if s := other.do("DELETE", "/api/admin/organizations/"+itoa(otherOrgID)+"/invitations/"+itoa(org.Invitations[0].UserID), nil, nil); s != 204 {
			t.Fatalf("withdraw again = %d, want 204", s)
		}

		// A global admin may still name an existing account by username.
		invite := map[string]string{"username": "joiner", "role": "org_admin"}
		if s := root.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", invite, nil); s != 202 {
			t.Fatalf("invite = %d, want 202", s)
		}
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM org_memberships m JOIN users u ON u.id = m.user_id WHERE u.username = 'joiner' AND m.org_id = $1`, otherOrgID).Scan(&n)
		if n != 0 {
			t.Fatalf("member before accepting")
		}

		joiner := newClient(t, ts)
		if s := joiner.do("POST", "/api/auth/login", map[string]string{"username": "joiner", "password": "longpw01"}, nil); s != 200 {
			t.Fatalf("joiner login = %d", s)
		}
		var who struct {
			Invitations []struct {
				OrgID int64  `json:"org_id"`
				Role  string `json:"role"`
			} `json:"org_invitations"`
			ByAdmin bool `json:"password_set_by_admin"`
		}
		if s := joiner.do("GET", "/api/auth/whoami", nil, &who); s != 200 || len(who.Invitations) != 1 || who.Invitations[0].OrgID != otherOrgID || !who.ByAdmin {
			t.Fatalf("whoami = %d %+v", s, who)
		}
		if s := joiner.do("POST", "/api/admin/me/org-invitations/"+itoa(otherOrgID)+"/accept", nil, nil); s != 200 {
			t.Fatalf("accept = %d, want 200", s)
		}
		var role string
		if err := db.QueryRow(`SELECT m.role FROM org_memberships m JOIN users u ON u.id = m.user_id WHERE u.username = 'joiner' AND m.org_id = $1`, otherOrgID).Scan(&role); err != nil || role != "org_admin" {
			t.Fatalf("membership after accept = %q (%v)", role, err)
		}
		if s := joiner.do("POST", "/api/admin/me/org-invitations/"+itoa(otherOrgID)+"/decline", nil, nil); s != 404 {
			t.Fatalf("decline after accept = %d, want 404", s)
		}
		if s := root.do("POST", "/api/admin/organizations/"+itoa(otherOrgID)+"/members", invite, nil); s != 409 {
			t.Fatalf("re-invite member = %d, want 409", s)
		}
	})

	t.Run("whoami carries the chosen language", func(t *testing.T) {
		boss := newClient(t, ts)
		if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 { // gitleaks:allow (test fixture)
			t.Fatalf("login = %d", s)
		}
		if s := boss.do("PUT", "/api/admin/me/language", map[string]string{"language": "sv"}, nil); s != 204 {
			t.Fatalf("set language = %d", s)
		}
		var who struct {
			Language string `json:"language"`
		}
		if s := boss.do("GET", "/api/auth/whoami", nil, &who); s != 200 || who.Language != "sv" {
			t.Fatalf("whoami = %d language %q, want sv", s, who.Language)
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
		// A common password is refused for an admin too.
		if s := root.doWithConfirm("POST", "/api/admin/admins", "rootpw", map[string]string{"username": "weak", "password": "password1"}, nil); s != 400 {
			t.Fatalf("common-password admin = %d, want 400", s)
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
