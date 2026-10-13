package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/server"
)

// totpWorld is a server with two organisations and the users the 2FA
// enforcement tests need, plus the database to look behind it.
type totpWorld struct {
	ts         *httptest.Server
	db         *sql.DB
	orgA, orgB int64
	users      map[string]int64
}

// newTOTPWorld seeds org A (boss: org_admin, mia: org_member), org B
// (olle: org_member), multi (member of both), root (global admin) and
// sam (Support). Every password is "s3cret". key is the TOTP key, nil
// for a server without one.
func newTOTPWorld(t *testing.T, key []byte) *totpWorld {
	t.Helper()
	db, dbCleanup := dbtest.Open(t)
	w := &totpWorld{db: db, users: map[string]int64{}}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Org A') RETURNING id`).Scan(&w.orgA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Org B') RETURNING id`).Scan(&w.orgB); err != nil {
		t.Fatal(err)
	}
	hash := fixtureHash(t, "s3cret")
	seed := func(name string, admin, support bool, memberships map[int64]string) {
		var id int64
		if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified, is_admin, is_support)
			VALUES ($1, $2, 1, $3, $4) RETURNING id`, name, hash, admin, support).Scan(&id); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		for org, role := range memberships {
			if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, $3)`, id, org, role); err != nil {
				t.Fatalf("seed %s membership: %v", name, err)
			}
		}
		w.users[name] = id
	}
	seed("boss", false, false, map[int64]string{w.orgA: "org_admin"})
	seed("mia", false, false, map[int64]string{w.orgA: "org_member"})
	seed("olle", false, false, map[int64]string{w.orgB: "org_member"})
	seed("multi", false, false, map[int64]string{w.orgA: "org_member", w.orgB: "org_member"})
	seed("root", true, false, nil)
	seed("sam", true, true, nil)
	w.ts = httptest.NewServer(server.New(server.Config{DB: db, Library: adventure.New(db), TOTPKey: key}))
	t.Cleanup(func() {
		w.ts.Close()
		dbCleanup()
	})
	return w
}

// login signs name in with the password.
func (w *totpWorld) login(t *testing.T, name string) *client {
	t.Helper()
	c := newClient(t, w.ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": name, "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login %s = %d", name, s)
	}
	return c
}

// sso gives name a session as an SSO (OIDC) sign-in mints it.
func (w *totpWorld) sso(t *testing.T, name string, org int64) *client {
	t.Helper()
	tok, err := auth.CompleteLogin(context.Background(), w.db, auth.User{ID: w.users[name]},
		[]auth.Factor{auth.FactorIdPPrimary}, "", "", sql.NullInt64{Int64: org, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, w.ts)
	u, _ := url.Parse(w.ts.URL)
	c.c.Jar.SetCookies(u, []*http.Cookie{{Name: auth.CookieName, Value: tok, Path: "/"}})
	return c
}

// call is c.do that also returns the error code of a refusal; pw, when
// given, goes in X-Confirm-Password.
func call(t *testing.T, c *client, method, path string, body any, pw ...string) (int, string) {
	t.Helper()
	var rdr bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&rdr).Encode(body)
	}
	req, _ := http.NewRequest(method, c.ts.URL+path, &rdr)
	req.Header.Set("Content-Type", "application/json")
	if len(pw) > 0 {
		req.Header.Set("X-Confirm-Password", pw[0])
	}
	if tok := c.csrfCookie(); tok != "" && method != "GET" {
		req.Header.Set(auth.CSRFHeaderName, tok)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Code
}

// bearerCode is bearer that also returns the error code of a refusal.
func bearerCode(t *testing.T, ts *httptest.Server, tok, method, path string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Code
}

func wantGated(t *testing.T, who string, s int, code string) {
	t.Helper()
	if s != http.StatusForbidden || code != "totp_setup_required" {
		t.Errorf("%s: %d %q, want 403 totp_setup_required", who, s, code)
	}
}

// enrol sets up and confirms 2FA for c's user.
func enrol(t *testing.T, c *client) {
	t.Helper()
	var setup struct {
		Secret string `json:"secret_base32"`
	}
	if s := c.do("POST", "/api/admin/me/totp/setup", nil, &setup); s != 200 {
		t.Fatalf("totp setup = %d", s)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var done struct {
		Codes []string `json:"recovery_codes"`
	}
	if s := c.do("POST", "/api/admin/me/totp/confirm", map[string]string{"code": code}, &done); s != 200 || len(done.Codes) == 0 {
		t.Fatalf("totp confirm = %d, %d codes", s, len(done.Codes))
	}
}

func mintToken(t *testing.T, c *client) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	if s := c.do("POST", "/api/admin/me/mcp-tokens", map[string]any{"name": "agent", "scopes": []string{"read", "write"}}, &out); s != 201 {
		t.Fatalf("mint = %d", s)
	}
	return out.Token
}

// An organisation that requires 2FA gates its members' password
// sessions and API tokens until they enrol; SSO sessions and other
// organisations' members are not gated.
func TestTOTPEnforcement_Org(t *testing.T) {
	t.Setenv("ZV_MCP_ENABLED", "1")
	w := newTOTPWorld(t, make([]byte, 32))
	boss, mia, olle := w.login(t, "boss"), w.login(t, "mia"), w.login(t, "olle")
	multi := w.login(t, "multi")
	if s := multi.do("POST", "/api/auth/switch-org", map[string]int64{"org_id": w.orgB}, nil); s != 200 && s != 204 {
		t.Fatalf("multi switch-org = %d", s)
	}
	tok := mintToken(t, mia)
	toggle := "/api/admin/organizations/" + itoa(w.orgA) + "/require-totp"

	if s, _ := call(t, mia, "PUT", toggle, map[string]bool{"enabled": true}); s != http.StatusForbidden {
		t.Errorf("org member turning it on = %d, want 403", s)
	}
	if s, _ := call(t, olle, "PUT", toggle, map[string]bool{"enabled": true}); s != http.StatusForbidden {
		t.Errorf("other org's member turning it on = %d, want 403", s)
	}
	// Requiring it without using it would lock the admin out at once.
	if s, code := call(t, boss, "PUT", toggle, map[string]bool{"enabled": true}); s != http.StatusConflict || code != "totp_self_first" {
		t.Fatalf("org admin without 2FA turning it on = %d %q, want 409 totp_self_first", s, code)
	}
	enrol(t, boss)
	if s, _ := call(t, boss, "PUT", toggle, map[string]bool{"enabled": true}); s != 200 {
		t.Fatalf("org admin turning it on = %d", s)
	}

	if s, _ := call(t, boss, "GET", "/api/admin/adventures", nil); s != 200 {
		t.Errorf("boss with 2FA = %d, want 200", s)
	}
	s, code := call(t, mia, "GET", "/api/admin/adventures", nil)
	wantGated(t, "mia", s, code)
	s, code = call(t, multi, "GET", "/api/admin/adventures", nil)
	wantGated(t, "multi (acting in org B, member of A)", s, code)
	if s, _ := call(t, olle, "GET", "/api/admin/adventures", nil); s != 200 {
		t.Errorf("olle (org B only) = %d, want 200", s)
	}
	var who struct {
		Required bool `json:"totp_setup_required"`
	}
	if s := mia.do("GET", "/api/auth/whoami", nil, &who); s != 200 || !who.Required {
		t.Errorf("mia whoami = %d, totp_setup_required %v, want true", s, who.Required)
	}
	var st struct {
		RequiredBy string `json:"required_by"`
	}
	if s := mia.do("GET", "/api/admin/me/totp", nil, &st); s != 200 || st.RequiredBy != "org" {
		t.Errorf("mia reading her 2FA status = %d, required_by %q, want 200 \"org\"", s, st.RequiredBy)
	}

	s, code = bearerCode(t, w.ts, tok, "GET", "/api/admin/adventures")
	wantGated(t, "mia's token", s, code)
	s, code = bearerCode(t, w.ts, tok, "POST", "/mcp")
	wantGated(t, "mia's token on /mcp", s, code)

	if s, _ := call(t, w.sso(t, "mia", w.orgA), "GET", "/api/admin/adventures", nil); s != 200 {
		t.Errorf("mia over SSO = %d, want 200", s)
	}

	enrol(t, mia)
	if s, _ := call(t, mia, "GET", "/api/admin/adventures", nil); s != 200 {
		t.Errorf("mia after enrolling = %d, want 200", s)
	}
	if s, _ := bearerCode(t, w.ts, tok, "GET", "/api/admin/adventures"); s != 200 {
		t.Errorf("mia's token after enrolling = %d, want 200", s)
	}

	// A required 2FA stays on; new recovery codes are fine.
	if s, code := call(t, mia, "POST", "/api/admin/me/totp/disable", nil, "s3cret"); s != http.StatusConflict || code != "totp_required_by_org" {
		t.Errorf("mia turning 2FA off = %d %q, want 409 totp_required_by_org", s, code)
	}
	if s, _ := call(t, mia, "POST", "/api/admin/me/totp/recovery-codes/regenerate", nil, "s3cret"); s != 200 {
		t.Errorf("mia regenerating recovery codes = %d, want 200", s)
	}

	// A full global admin may set any org's requirement (admin action
	// log); Support may not.
	root, sam := w.login(t, "root"), w.login(t, "sam")
	toggleB := "/api/admin/organizations/" + itoa(w.orgB) + "/require-totp"
	if s, _ := call(t, sam, "PUT", toggleB, map[string]bool{"enabled": true}); s != http.StatusForbidden {
		t.Errorf("Support turning org B on = %d, want 403", s)
	}
	enrol(t, root)
	if s, _ := call(t, root, "PUT", toggleB, map[string]bool{"enabled": true}); s != 200 {
		t.Fatalf("admin turning org B on = %d", s)
	}
	var logged int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE action = 'PUT /api/admin/organizations/{orgID}/require-totp (on)'`).Scan(&logged); err != nil || logged != 1 {
		t.Errorf("admin action log rows = %d (%v), want 1 (root's only)", logged, err)
	}
	s, code = call(t, olle, "GET", "/api/admin/adventures", nil)
	wantGated(t, "olle after root required it of org B", s, code)

	// Turning off needs the password.
	if s, _ := call(t, boss, "PUT", toggle, map[string]bool{"enabled": false}); s != http.StatusUnauthorized {
		t.Errorf("turning org A off without password = %d, want 401", s)
	}
	if s, _ := call(t, boss, "PUT", toggle, map[string]bool{"enabled": false}, "wrong"); s != http.StatusUnauthorized {
		t.Errorf("turning org A off with a wrong password = %d, want 401", s)
	}
	if s, _ := call(t, boss, "PUT", toggle, map[string]bool{"enabled": false}, "s3cret"); s != 200 {
		t.Errorf("turning org A off with password = %d, want 200", s)
	}

	// Settings → Audit: org A's admin sees org A's switches (on, off),
	// not org B's, though root flipped that one.
	var audit struct {
		Events []struct {
			Username string `json:"username"`
			Reason   string `json:"reason"`
		} `json:"events"`
	}
	if s := boss.do("GET", "/api/admin/auth-events?event=org_require_totp", nil, &audit); s != 200 {
		t.Fatalf("boss audit = %d", s)
	}
	if len(audit.Events) != 2 || audit.Events[0].Reason != "require_totp_off" || audit.Events[1].Reason != "require_totp_on" || audit.Events[0].Username != "boss" {
		t.Errorf("org A audit = %+v, want boss off then on", audit.Events)
	}
	var orgBRows int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE event = 'org_require_totp' AND org_id = $1 AND username = 'root'`, w.orgB).Scan(&orgBRows); err != nil || orgBRows != 1 {
		t.Errorf("org B audit rows by root = %d (%v), want 1", orgBRows, err)
	}
}

// The admin switch covers global admins and Support, nobody else, and
// only a full admin may flip it; turning it on is in the action log.
func TestTOTPEnforcement_Admins(t *testing.T) {
	w := newTOTPWorld(t, make([]byte, 32))
	root, sam, boss := w.login(t, "root"), w.login(t, "sam"), w.login(t, "boss")
	const toggle = "/api/admin/settings/admin-totp"

	if s, _ := call(t, sam, "PUT", toggle, map[string]bool{"enabled": true}); s != http.StatusForbidden {
		t.Errorf("Support turning it on = %d, want 403", s)
	}
	if s, _ := call(t, boss, "PUT", toggle, map[string]bool{"enabled": true}); s != http.StatusForbidden {
		t.Errorf("org admin turning it on = %d, want 403", s)
	}
	if s, code := call(t, root, "PUT", toggle, map[string]bool{"enabled": true}); s != http.StatusConflict || code != "totp_self_first" {
		t.Fatalf("admin without 2FA turning it on = %d %q, want 409 totp_self_first", s, code)
	}
	enrol(t, root)
	if s, _ := call(t, root, "PUT", toggle, map[string]bool{"enabled": true}); s != 200 {
		t.Fatalf("admin turning it on = %d", s)
	}
	var logged int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE action = 'PUT /api/admin/settings/admin-totp (on)'`).Scan(&logged); err != nil || logged != 1 {
		t.Errorf("admin action log rows = %d (%v), want 1", logged, err)
	}

	s, code := call(t, sam, "GET", "/api/admin/organizations", nil)
	wantGated(t, "sam", s, code)
	if s, _ := call(t, boss, "GET", "/api/admin/adventures", nil); s != 200 {
		t.Errorf("boss = %d, want 200", s)
	}
	if s, _ := call(t, root, "GET", "/api/admin/organizations", nil); s != 200 {
		t.Errorf("root with 2FA = %d, want 200", s)
	}
	if s, code := call(t, root, "POST", "/api/admin/me/totp/disable", nil, "s3cret"); s != http.StatusConflict || code != "totp_required_for_admins" {
		t.Errorf("root turning 2FA off = %d %q, want 409 totp_required_for_admins", s, code)
	}
	if s, _ := call(t, root, "PUT", toggle, map[string]bool{"enabled": false}); s != http.StatusUnauthorized {
		t.Errorf("turning it off without password = %d, want 401", s)
	}
	if s, _ := call(t, root, "PUT", toggle, map[string]bool{"enabled": false}, "s3cret"); s != 200 {
		t.Errorf("turning it off with password = %d, want 200", s)
	}
	if s, _ := call(t, sam, "GET", "/api/admin/organizations", nil); s != 200 {
		t.Errorf("sam once it is off = %d, want 200", s)
	}
}

// Without a TOTP key nobody could enrol: neither switch turns on, and a
// requirement already stored is not enforced.
func TestTOTPEnforcement_NoKey(t *testing.T) {
	w := newTOTPWorld(t, nil)
	root, boss := w.login(t, "root"), w.login(t, "boss")
	if s, _ := call(t, boss, "PUT", "/api/admin/organizations/"+itoa(w.orgA)+"/require-totp", map[string]bool{"enabled": true}); s != http.StatusConflict {
		t.Errorf("org switch without key = %d, want 409", s)
	}
	if s, _ := call(t, root, "PUT", "/api/admin/settings/admin-totp", map[string]bool{"enabled": true}); s != http.StatusConflict {
		t.Errorf("admin switch without key = %d, want 409", s)
	}
	if _, err := w.db.Exec(`UPDATE organizations SET require_totp = TRUE WHERE id = $1`, w.orgA); err != nil {
		t.Fatal(err)
	}
	if s, _ := call(t, boss, "GET", "/api/admin/adventures", nil); s != 200 {
		t.Errorf("boss with a stored requirement but no key = %d, want 200", s)
	}
}
