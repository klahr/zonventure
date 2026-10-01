package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// bearer issues a cookie-less request authenticated only by tok —
// the shape a script or agent uses against /api/admin.
func bearer(t *testing.T, ts *httptest.Server, tok, method, path string, body any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestAPIToken_AdminREST covers bearer tokens on /api/admin: no CSRF
// header needed, scopes gate by method, and credential management
// stays session-only.
func TestAPIToken_AdminREST(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	mint := func(scopes []string) string {
		var out struct {
			Token string `json:"token"`
		}
		if s := arr.do("POST", "/api/admin/me/mcp-tokens",
			map[string]any{"name": "agent", "scopes": scopes}, &out); s != 201 {
			t.Fatalf("mint = %d", s)
		}
		return out.Token
	}
	rw := mint([]string{"read", "write"})
	ro := mint([]string{"read"})

	cases := []struct {
		name, tok, method, path string
		body                    any
		want                    int
	}{
		{"no auth", "", "GET", "/api/admin/adventures", nil, 401},
		{"unknown token", "zvmcp_nope", "GET", "/api/admin/adventures", nil, 401},
		{"rw list", rw, "GET", "/api/admin/adventures", nil, 200},
		{"rw create without csrf", rw, "POST", "/api/admin/adventures",
			map[string]string{"slug": "via-token", "title": "Via token"}, 201},
		{"ro list", ro, "GET", "/api/admin/adventures", nil, 200},
		{"ro create", ro, "POST", "/api/admin/adventures",
			map[string]string{"slug": "ro-token", "title": "RO"}, 403},
		{"token cannot mint tokens", rw, "POST", "/api/admin/me/mcp-tokens",
			map[string]any{"name": "more"}, 403},
		{"token cannot list tokens", rw, "GET", "/api/admin/me/mcp-tokens", nil, 403},
		{"token cannot change password", rw, "POST", "/api/admin/me/password",
			map[string]string{"current": "s3cret", "new": "0therPassw0rd!"}, 403},
		{"token cannot change username", rw, "POST", "/api/admin/me/username",
			map[string]string{"username": "renamed"}, 403},
	}
	for _, c := range cases {
		if s := bearer(t, ts, c.tok, c.method, c.path, c.body); s != c.want {
			t.Errorf("%s: %s %s = %d, want %d", c.name, c.method, c.path, s, c.want)
		}
	}
}
