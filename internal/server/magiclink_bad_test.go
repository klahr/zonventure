package server_test

import (
	"net/http"
	"testing"
)

// TestMagicLink_BadTokenSaysWhy locks in that a magic link the server
// can't honour leaves /play with the token stripped and a reason the
// page can show, rather than asking the player to open the link they
// just opened.
func TestMagicLink_BadTokenSaysWhy(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(ts.URL + "/play?t=not-a-real-token")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/play?link=invalid" {
		t.Fatalf("Location = %q, want /play?link=invalid", got)
	}
}
