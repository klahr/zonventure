package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

func TestRedirectAllowed(t *testing.T) {
	s := &Server{RedirectHosts: []string{"claude.ai"}}
	for uri, want := range map[string]bool{
		"https://claude.ai/api/mcp/auth_callback": true,
		"http://localhost:33418/callback":         true,
		"http://127.0.0.1:9/cb":                   true,
		"http://claude.ai/cb":                     false,
		"https://evil.example/cb":                 false,
		"https://claude.ai.evil.example/cb":       false,
		"https://claude.ai/cb#frag":               false,
		"https://user@claude.ai/cb":               false,
		"javascript:alert(1)":                     false,
	} {
		if got := s.redirectAllowed(uri); got != want {
			t.Errorf("redirectAllowed(%q) = %v, want %v", uri, got, want)
		}
	}
}

func TestPKCE(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !pkceMatches(verifier, challenge) {
		t.Error("matching verifier refused")
	}
	if pkceMatches(strings.Repeat("b", 43), challenge) || pkceMatches("short", challenge) {
		t.Error("wrong verifier accepted")
	}
}

// The whole flow a connector goes through, against a real database.
func TestFlow(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	s := New(db, "https://zv.example/")
	if s.Issuer != "https://zv.example" {
		t.Fatalf("issuer %q", s.Issuer)
	}
	const redirect = "https://claude.ai/api/mcp/auth_callback"

	// Register.
	rec := httptest.NewRecorder()
	s.Register(rec, httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"client_name":"Claude","redirect_uris":["`+redirect+`"]}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	rec = httptest.NewRecorder()
	s.Register(rec, httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"redirect_uris":["https://evil.example/cb"]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a redirect off the allowlist should be refused, got %d", rec.Code)
	}

	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {reg.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"state": {"xyz"}, "resource": {"https://zv.example/mcp"},
	}

	// Authorize sends the browser to the console; a bad redirect is an error page, never a redirect.
	rec = httptest.NewRecorder()
	s.Authorize(rec, httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil))
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/app/arrange/#/authorize?") {
		t.Fatalf("authorize: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	s.Request(rec, httptest.NewRequest("GET", "/api/admin/oauth/request?"+q.Encode(), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"verified":true`) {
		t.Errorf("an allowlisted https client should be verified: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.Register(rec, httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"client_name":"Claude","redirect_uris":["http://127.0.0.1:53682/cb"]}`)))
	var local struct {
		ClientID string `json:"client_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &local)
	lq := url.Values{}
	for k, v := range q {
		lq[k] = v
	}
	lq.Set("client_id", local.ClientID)
	lq.Set("redirect_uri", "http://127.0.0.1:53682/cb")
	rec = httptest.NewRecorder()
	s.Request(rec, httptest.NewRequest("GET", "/api/admin/oauth/request?"+lq.Encode(), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"verified":false`) {
		t.Errorf("a loopback client calling itself Claude should be unverified: %d %s", rec.Code, rec.Body)
	}
	bad := url.Values{}
	for k, v := range q {
		bad[k] = v
	}
	bad.Set("redirect_uri", "https://claude.ai/other")
	rec = httptest.NewRecorder()
	s.Authorize(rec, httptest.NewRequest("GET", "/oauth/authorize?"+bad.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unregistered redirect_uri should be refused outright, got %d", rec.Code)
	}

	// The signed-in user approves.
	me := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})
	approve := func(ctx context.Context, yes bool) (int, string) {
		body := map[string]any{"approve": yes}
		for k := range q {
			body[k] = q.Get(k)
		}
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/oauth/approve", strings.NewReader(string(b))).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		s.Approve(rec, req)
		var out struct {
			Redirect string `json:"redirect"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out.Redirect
	}
	if code, _ := approve(auth.WithAPIToken(me), true); code != http.StatusForbidden {
		t.Errorf("an API token must not approve, got %d", code)
	}
	if _, back := approve(me, false); !strings.Contains(back, "error=access_denied") || !strings.Contains(back, "state=xyz") {
		t.Errorf("deny: %s", back)
	}
	_, back := approve(me, true)
	u, _ := url.Parse(back)
	code := u.Query().Get("code")
	if code == "" || u.Query().Get("state") != "xyz" || u.Query().Get("iss") != "https://zv.example" {
		t.Fatalf("approve: %s", back)
	}

	token := func(form url.Values) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		s.Token(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {reg.ClientID}}

	wrong := url.Values{}
	for k, v := range exchange {
		wrong[k] = v
	}
	wrong.Set("code_verifier", strings.Repeat("w", 50))
	if status, _ := token(wrong); status != http.StatusBadRequest {
		t.Errorf("a wrong verifier should be refused, got %d", status)
	}
	// The failed attempt used the code up: codes are single-use.
	exchange.Set("code_verifier", verifier)
	if status, _ := token(exchange); status != http.StatusBadRequest {
		t.Errorf("a used code should be refused, got %d", status)
	}

	// A fresh approval, exchanged properly.
	_, back = approve(me, true)
	u, _ = url.Parse(back)
	exchange.Set("code", u.Query().Get("code"))
	status, tok := token(exchange)
	if status != http.StatusOK || tok["token_type"] != "Bearer" || tok["scope"] != "read write" {
		t.Fatalf("exchange: %d %v", status, tok)
	}
	access, _ := tok["access_token"].(string)
	refresh, _ := tok["refresh_token"].(string)
	if user, scopes, err := admin.LookupMCPToken(context.Background(), db, access); err != nil || user.ID != userID || len(scopes) != 2 {
		t.Fatalf("the access token should work as an MCP token: %v %v %v", user, scopes, err)
	}

	// Refresh rotates both tokens; the old access token stops working.
	status, tok2 := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {reg.ClientID}})
	if status != http.StatusOK {
		t.Fatalf("refresh: %d %v", status, tok2)
	}
	if _, _, err := admin.LookupMCPToken(context.Background(), db, access); err == nil {
		t.Error("the replaced access token still works")
	}
	access2, _ := tok2["access_token"].(string)
	if _, _, err := admin.LookupMCPToken(context.Background(), db, access2); err != nil {
		t.Errorf("the new access token should work: %v", err)
	}

	// Replaying the old refresh token revokes the grant.
	if status, _ := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {reg.ClientID}}); status != http.StatusBadRequest {
		t.Errorf("a replayed refresh token should be refused, got %d", status)
	}
	if _, _, err := admin.LookupMCPToken(context.Background(), db, access2); err == nil {
		t.Error("after a replay the grant should be revoked")
	}

	// Someone in two organizations must choose one, and the token acts
	// only there — until they leave it.
	var org2 int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Beta') RETURNING id`).Scan(&org2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`, userID, org2); err != nil {
		t.Fatal(err)
	}
	if status, _ := approve(me, true); status != http.StatusBadRequest {
		t.Errorf("approving without choosing an organization should be refused, got %d", status)
	}
	withOrg := func(org int64) (int, string) {
		body := map[string]any{"approve": true, "org_id": org}
		for k := range q {
			body[k] = q.Get(k)
		}
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/oauth/approve", strings.NewReader(string(b))).WithContext(me)
		req.Header.Set("Content-Type", "application/json")
		s.Approve(rec, req)
		var out struct {
			Redirect string `json:"redirect"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out.Redirect
	}
	if status, _ := withOrg(org2 + 1000); status != http.StatusBadRequest {
		t.Errorf("an organization the user is not in should be refused, got %d", status)
	}
	_, back = withOrg(org2)
	u, _ = url.Parse(back)
	exchange.Set("code", u.Query().Get("code"))
	_, tokB := token(exchange)
	accessB, _ := tokB["access_token"].(string)
	user, _, err := admin.LookupMCPToken(context.Background(), db, accessB)
	if err != nil || user.OrgID == nil || *user.OrgID != org2 || user.Role != auth.RoleOrgMember {
		t.Fatalf("the token should act in Beta as a member: %+v %v", user, err)
	}
	var name string
	_ = db.QueryRow(`SELECT name FROM mcp_tokens WHERE token_prefix = $1`, accessB[:12]).Scan(&name)
	if name != "Claude (signed in) · Beta" {
		t.Errorf("token name %q", name)
	}
	if _, err := db.Exec(`DELETE FROM org_memberships WHERE user_id = $1 AND org_id = $2`, userID, org2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := admin.LookupMCPToken(context.Background(), db, accessB); err == nil {
		t.Error("a token pinned to an organization the user left should stop working")
	}

	// An expired access token is refused.
	_, back = approve(me, true)
	u, _ = url.Parse(back)
	exchange.Set("code", u.Query().Get("code"))
	_, tok3 := token(exchange)
	access3, _ := tok3["access_token"].(string)
	if _, err := db.Exec(`UPDATE mcp_tokens SET expires_at = now() - interval '1 second' WHERE token_prefix = $1`, access3[:12]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := admin.LookupMCPToken(context.Background(), db, access3); err == nil {
		t.Error("an expired access token still works")
	}
}
