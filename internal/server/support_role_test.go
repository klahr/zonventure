package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/server"
)

// TestSupportRole: a full admin creates a Support admin, who reads the
// admin console, may verify an email and reset 2FA (both in the admin
// action log under their name), and is refused everything else. Only a
// full admin changes an admin's role, never their own.
func TestSupportRole(t *testing.T) {
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	root := newClient(t, ts)
	login(t, root, "root", "rootpw")
	var created struct {
		ID   int64  `json:"id"`
		Role string `json:"role"`
	}
	if s := root.doWithConfirm("POST", "/api/admin/admins", "rootpw", map[string]string{"username": "anna", "password": "Lantern-Fjord-42", "role": "support"}, &created); s != 201 || created.Role != "support" { // gitleaks:allow (test fixture)
		t.Fatalf("create support = %d %+v", s, created)
	}
	if s := root.doWithConfirm("POST", "/api/admin/admins", "rootpw", map[string]string{"username": "bad", "password": "Lantern-Fjord-42", "role": "owner"}, nil); s != 400 { // gitleaks:allow (test fixture)
		t.Fatalf("create unknown role = %d, want 400", s)
	}

	anna := newClient(t, ts)
	login(t, anna, "anna", "Lantern-Fjord-42")
	var who struct {
		Role string `json:"role"`
	}
	if s := anna.do("GET", "/api/auth/whoami", nil, &who); s != 200 || who.Role != "support" {
		t.Fatalf("whoami = %d %+v", s, who)
	}

	var bossID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE username = 'boss'`).Scan(&bossID); err != nil {
		t.Fatal(err)
	}
	org := "/api/admin/organizations/" + itoa(orgID)
	for _, path := range []string{
		"/api/admin/organizations", org, org + "/events", "/api/admin/auth-events", "/api/admin/admin-actions",
		"/api/admin/logs", "/api/admin/listing-reviews", "/api/admin/listing-reviews/log", "/api/admin/purchases",
		"/api/admin/ticket-purchases", "/api/admin/catalog", "/api/admin/settings",
	} {
		if s := anna.do("GET", path, nil, nil); s != 200 {
			t.Errorf("support GET %s = %d, want 200", path, s)
		}
	}
	// A logged mail (no SMTP) carries sign-in and reset links: only a
	// full admin reads it on Logs.
	logx.Info("mail.body", "to", "x@example.com", "text", "support-must-not-see")
	type logsResp struct {
		Entries []struct {
			Msg string `json:"msg"`
		} `json:"entries"`
	}
	hasMail := func(c *client) bool {
		var l logsResp
		if s := c.do("GET", "/api/admin/logs", nil, &l); s != 200 {
			t.Fatalf("logs = %d", s)
		}
		for _, e := range l.Entries {
			if e.Msg == "mail.body" {
				return true
			}
		}
		return false
	}
	if !hasMail(root) || hasMail(anna) {
		t.Errorf("mail.body on Logs: admin %v, support %v; want true, false", hasMail(root), hasMail(anna))
	}
	// Made a listing reviewer, Support still never decides.
	if _, err := db.Exec(`UPDATE users SET listing_reviewer = TRUE WHERE username = 'anna'`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/admin/organizations"},
		{"PATCH", org},
		{"POST", org + "/trusted"},
		{"POST", org + "/verified-creator"},
		{"POST", org + "/members"},
		{"PUT", "/api/admin/pricing"},
		{"GET", "/api/admin/pricing"},
		{"PUT", "/api/admin/legal"},
		{"GET", "/api/admin/admins"},
		{"POST", "/api/admin/admins"},
		{"PUT", "/api/admin/admins/" + itoa(created.ID) + "/role"},
		{"POST", "/api/admin/migrate/maintenance"},
		{"POST", "/api/admin/system-update/check"},
		{"PUT", "/api/admin/logs/level"},
		{"POST", "/api/admin/purchases/1/void"},
		{"POST", "/api/admin/listing-reviewers"},
		{"POST", "/api/admin/listing-reviews/event/1/approve"},
		{"POST", "/api/admin/users/" + itoa(bossID) + "/password"},
	} {
		if s := anna.doWithConfirm(c.method, c.path, "Lantern-Fjord-42", map[string]any{}, nil); s != 403 {
			t.Errorf("support %s %s = %d, want 403", c.method, c.path, s)
		}
	}

	if s := anna.do("POST", "/api/admin/users/"+itoa(bossID)+"/verify-email", nil, nil); s >= 300 {
		t.Fatalf("support verify-email = %d", s)
	}
	if s := anna.doWithConfirm("POST", "/api/admin/users/"+itoa(bossID)+"/totp/disable", "Lantern-Fjord-42", nil, nil); s != 204 {
		t.Fatalf("support totp reset = %d, want 204", s)
	}
	var actions struct {
		Actions []struct {
			Username string `json:"username"`
			Action   string `json:"action"`
		} `json:"actions"`
	}
	if s := root.do("GET", "/api/admin/admin-actions", nil, &actions); s != 200 {
		t.Fatalf("admin-actions = %d", s)
	}
	logged := map[string]bool{}
	for _, a := range actions.Actions {
		if a.Username == "anna" {
			logged[a.Action] = true
		}
	}
	for _, want := range []string{"POST /api/admin/users/{userID}/verify-email", "POST /api/admin/users/{userID}/totp/disable"} {
		if !logged[want] {
			t.Errorf("admin action %q by anna not logged; got %+v", want, actions.Actions)
		}
	}

	// Every global admin reads the review queue, reviewer or not.
	if _, err := db.Exec(`UPDATE users SET listing_reviewer = FALSE WHERE username = 'root'`); err != nil {
		t.Fatal(err)
	}
	if s := root.do("GET", "/api/admin/listing-reviews", nil, nil); s != 200 {
		t.Fatalf("admin GET listing-reviews = %d, want 200", s)
	}

	var rootID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE username = 'root'`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if s := root.doWithConfirm("PUT", "/api/admin/admins/"+itoa(rootID)+"/role", "rootpw", map[string]string{"role": "support"}, nil); s != 409 {
		t.Fatalf("own role change = %d, want 409", s)
	}
	if s := root.doWithConfirm("PUT", "/api/admin/admins/"+itoa(bossID)+"/role", "rootpw", map[string]string{"role": "support"}, nil); s != 404 {
		t.Fatalf("non-admin role change = %d, want 404", s)
	}
	if s := root.doWithConfirm("PUT", "/api/admin/admins/"+itoa(created.ID)+"/role", "rootpw", map[string]string{"role": "admin"}, nil); s != 204 {
		t.Fatalf("promote = %d, want 204", s)
	}
	var list struct {
		Admins []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"admins"`
	}
	if s := root.do("GET", "/api/admin/admins", nil, &list); s != 200 {
		t.Fatalf("list = %d", s)
	}
	for _, a := range list.Admins {
		if a.Role != "admin" {
			t.Errorf("admin %s role = %q, want admin", a.Username, a.Role)
		}
	}
	// The promotion applies to Anna's open session at once.
	if s := anna.do("GET", "/api/admin/pricing", nil, nil); s != 200 {
		t.Fatalf("promoted GET pricing = %d, want 200", s)
	}
}
