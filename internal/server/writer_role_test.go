package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// TestWriterRole runs a new writer from invitation to role change on
// one server: invited by email, they choose their own password and
// accept; then they author but reach no events, members, billing or
// store; an org admin changes their role in place.
func TestWriterRole(t *testing.T) {
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()
	org := "/api/admin/organizations/" + itoa(orgID)
	boss := newClient(t, ts)
	login(t, boss, "boss", "s3cret")

	invite := map[string]string{"username": "wendy", "email": "wendy@example.test", "role": "org_writer"}
	if s := boss.do("POST", org+"/members", invite, nil); s != 202 {
		t.Fatalf("invite writer = %d, want 202", s)
	}
	var (
		wendyID  int64
		hasPass  bool
		role     string
		inviteTk string
	)
	if err := db.QueryRow(`
		SELECT u.id, u.password_hash IS NOT NULL, i.role, v.token
		FROM users u JOIN org_invitations i ON i.user_id = u.id
		JOIN email_verifications v ON v.user_id = u.id AND v.purpose = 'invite'
		WHERE u.username = 'wendy' AND i.org_id = $1`, orgID).Scan(&wendyID, &hasPass, &role, &inviteTk); err != nil {
		t.Fatalf("invitation: %v", err)
	}
	if hasPass || role != "org_writer" {
		t.Fatalf("new account has password %v, invited as %q; want none, org_writer", hasPass, role)
	}

	// The invitation's link sets the password and confirms the address;
	// accepting makes them a writer.
	wendy := newClient(t, ts)
	if s := wendy.do("POST", "/api/auth/reset", map[string]string{"token": inviteTk, "new_password": "wendy-pw-77"}, nil); s != 200 { // gitleaks:allow (test fixture)
		t.Fatalf("choose password = %d, want 200", s)
	}
	var verified int
	if err := db.QueryRow(`SELECT email_verified FROM users WHERE id = $1`, wendyID).Scan(&verified); err != nil || verified != 1 {
		t.Fatalf("email_verified = %d (%v), want 1", verified, err)
	}
	if s := wendy.do("POST", "/api/admin/me/org-invitations/"+itoa(orgID)+"/accept", nil, nil); s != 200 {
		t.Fatalf("accept = %d, want 200", s)
	}
	wendy = newClient(t, ts)
	login(t, wendy, "wendy", "wendy-pw-77") // gitleaks:allow (test fixture)

	t.Run("writer authors only", func(t *testing.T) {
		wendy := wendy.in(t)
		if s := wendy.do("POST", "/api/admin/adventures", map[string]any{"slug": "w-trip", "title": "W"}, nil); s/100 != 2 {
			t.Errorf("writer create adventure = %d, want 2xx", s)
		}
		for _, c := range []struct{ method, path string }{
			{"GET", "/api/admin/adventures"},
			{"GET", "/api/admin/adventures/w-trip/edit"},
			{"GET", "/api/admin/org-themes"},
			{"GET", "/api/admin/me/sessions"},
		} {
			if s := wendy.do(c.method, c.path, nil, nil); s != 200 {
				t.Errorf("writer %s %s = %d, want 200", c.method, c.path, s)
			}
		}
		for _, c := range []struct{ method, path string }{
			{"GET", "/api/admin/events"},
			{"POST", "/api/admin/events"},
			{"GET", "/api/admin/assignments"},
			{"GET", "/api/admin/purchases"},
			{"POST", "/api/admin/purchases"},
			{"GET", "/api/admin/ticket-purchases"},
			{"GET", "/api/admin/redemptions"},
			{"GET", "/api/admin/party-offers"},
			{"GET", "/api/admin/webhooks"},
			{"GET", "/api/admin/orgs/me/profile"},
			{"POST", "/api/admin/adventures/w-trip/host"},
			{"POST", "/api/admin/me/portal-session"},
			{"GET", org},
			{"POST", org + "/members"},
		} {
			if s := wendy.do(c.method, c.path, nil, nil); s != 403 {
				t.Errorf("writer %s %s = %d, want 403", c.method, c.path, s)
			}
		}
	})

	t.Run("role changes in place", func(t *testing.T) {
		boss := boss.in(t)
		path := org + "/members/" + itoa(wendyID) + "/role"
		if s := boss.do("PUT", path, map[string]string{"role": "org_member"}, nil); s != 401 {
			t.Fatalf("role change without password = %d, want 401", s)
		}
		if s := boss.doWithConfirm("PUT", path, "s3cret", map[string]string{"role": "wizard"}, nil); s != 400 {
			t.Fatalf("bad role = %d, want 400", s)
		}
		if s := wendy.in(t).do("PUT", path, map[string]string{"role": "org_admin"}, nil); s != 403 {
			t.Fatalf("writer changing roles = %d, want 403", s)
		}
		if s := boss.doWithConfirm("PUT", path, "s3cret", map[string]string{"role": "org_member"}, nil); s != 200 {
			t.Fatalf("make event host = %d, want 200", s)
		}
		// Takes effect on the writer's live session.
		if s := wendy.in(t).do("GET", "/api/admin/events", nil, nil); s != 200 {
			t.Fatalf("event host lists events = %d, want 200", s)
		}
		var bossID int64
		if err := db.QueryRow(`SELECT id FROM users WHERE username = 'boss'`).Scan(&bossID); err != nil {
			t.Fatal(err)
		}
		if s := boss.doWithConfirm("PUT", org+"/members/"+itoa(bossID)+"/role", "s3cret", map[string]string{"role": "org_member"}, nil); s != 409 {
			t.Fatalf("own role = %d, want 409", s)
		}
		// The last admin can't be demoted, by a global admin either.
		root := newClient(t, ts)
		login(t, root, "root", "rootpw")
		if s := root.doWithConfirm("PUT", org+"/members/"+itoa(bossID)+"/role", "rootpw", map[string]string{"role": "org_writer"}, nil); s != 409 {
			t.Fatalf("demote last admin = %d, want 409", s)
		}
	})
}
