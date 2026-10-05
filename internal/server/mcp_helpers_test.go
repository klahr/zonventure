package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mcpTool calls MCP tools over /mcp with a read-write token minted by
// the logged-in client c, as Claude does: it returns each call's text
// and whether it was an error.
func mcpTool(t *testing.T, ts *httptest.Server, c *client) func(name string, args any) (string, bool) {
	t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	if s := c.do("POST", "/api/admin/me/mcp-tokens", map[string]any{"name": "agent", "scopes": []string{"read", "write"}}, &tok); s != 201 {
		t.Fatalf("mint = %d", s)
	}
	return func(name string, args any) (string, bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		req, _ := http.NewRequest("POST", ts.URL+"/mcp", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("%s: bad response (%v)", name, err)
		}
		if out.Error != nil {
			return out.Error.Message, true
		}
		if len(out.Result.Content) == 0 {
			return "", out.Result.IsError
		}
		return out.Result.Content[0].Text, out.Result.IsError
	}
}
