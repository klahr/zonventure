package server_test

import (
	"net/url"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// TestListAuthEvents_Filters seeds a handful of auth_events rows and
// exercises the four filter knobs the audit UI uses (event kind, ok
// flag, username substring, ip substring). The point is to prove each
// query parameter actually narrows the result set rather than to
// re-test pagination.
func TestListAuthEvents_Filters(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	rows := []struct {
		event, username, ip, reason string
		ok                          int
	}{
		{"login", "boss", "10.0.0.1", "success", 1},
		{"login", "boss", "10.0.0.2", "invalid_credentials", 0},
		{"logout", "boss", "10.0.0.1", "", 1},
		{"set_password", "rival", "192.168.1.5", "success", 1},
		{"login", "rival", "192.168.1.5", "success", 1},
	}
	for _, r := range rows {
		if _, err := db.Exec(`
			INSERT INTO auth_events(event, ok, username, ip, reason)
			VALUES ($1, $2, $3, $4, $5)
		`, r.event, r.ok, r.username, r.ip, r.reason); err != nil {
			t.Fatalf("seed auth_event: %v", err)
		}
	}

	root := newClient(t, ts)
	if s := root.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	type auditResp struct {
		Events []struct {
			Event    string `json:"event"`
			OK       bool   `json:"ok"`
			Username string `json:"username"`
			IP       string `json:"ip"`
		} `json:"events"`
	}

	get := func(qs url.Values) auditResp {
		t.Helper()
		var resp auditResp
		path := "/api/admin/auth-events"
		if qs != nil {
			path += "?" + qs.Encode()
		}
		if s := root.do("GET", path, nil, &resp); s != 200 {
			t.Fatalf("GET %s = %d", path, s)
		}
		return resp
	}

	// No filter: all five seed rows plus whatever login attempt the
	// test client itself produced. Lower bound is enough.
	if got := get(nil); len(got.Events) < 5 {
		t.Fatalf("unfiltered = %d, want >= 5", len(got.Events))
	}

	// event=login: three rows (two boss, one rival) plus the test
	// client's own successful login.
	if got := get(url.Values{"event": {"login"}}); len(got.Events) < 3 {
		t.Fatalf("event=login = %d, want >= 3", len(got.Events))
	} else {
		for _, e := range got.Events {
			if e.Event != "login" {
				t.Fatalf("event filter leaked %q", e.Event)
			}
		}
	}

	// ok=0: only the failed login.
	got := get(url.Values{"ok": {"0"}})
	if len(got.Events) != 1 || got.Events[0].Event != "login" || got.Events[0].OK {
		t.Fatalf("ok=0 = %+v, want one failed login", got.Events)
	}

	// username substring: "riv" matches rival's two rows.
	got = get(url.Values{"username": {"riv"}})
	if len(got.Events) != 2 {
		t.Fatalf("username~=riv = %d, want 2", len(got.Events))
	}
	for _, e := range got.Events {
		if e.Username != "rival" {
			t.Fatalf("username filter leaked %q", e.Username)
		}
	}

	// ip substring: "192.168" matches rival's two rows.
	got = get(url.Values{"ip": {"192.168"}})
	if len(got.Events) != 2 {
		t.Fatalf("ip~=192.168 = %d, want 2", len(got.Events))
	}

	// Combined: event=login + ok=1 + username=boss → exactly one
	// (the seeded boss success; the test client's login is the
	// admin "root", which the username substring excludes).
	got = get(url.Values{"event": {"login"}, "ok": {"1"}, "username": {"boss"}})
	if len(got.Events) != 1 {
		t.Fatalf("combined filter = %d, want 1", len(got.Events))
	}
}
