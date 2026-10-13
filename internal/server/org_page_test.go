package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// /o/{slug} is an organisation's one page; the shop's old addresses
// lead to it, and /store to the site's own organisation — never to a
// store of every organisation's adventures.
func TestOrgPageAndStoreRedirects(t *testing.T) {
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(base, path string) (int, string, string) {
		t.Helper()
		resp, err := noFollow.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp.StatusCode, resp.Header.Get("Location"), b.String()
	}

	ts, _, cleanup := newTestServerWith(t, server.Config{HomeOrg: "testorg"})
	defer cleanup()
	if code, loc, _ := get(ts.URL, "/store?paid=7&session_id=cs_1"); code != http.StatusMovedPermanently || loc != "/o/testorg?paid=7&session_id=cs_1" {
		t.Errorf("/store = %d → %q, want the home organisation's page with the query kept", code, loc)
	}
	if code, loc, _ := get(ts.URL, "/o/testorg/store?cancelled=3"); code != http.StatusMovedPermanently || loc != "/o/testorg?cancelled=3" {
		t.Errorf("/o/{slug}/store = %d → %q", code, loc)
	}
	if code, _, body := get(ts.URL, "/o/testorg"); code != http.StatusOK || !strings.Contains(body, "/api/o/testorg/store") {
		t.Errorf("/o/{slug} = %d, want the page reading the organisation's adventures", code)
	}
	if _, _, body := get(ts.URL, "/o/testorg"); !strings.Contains(body, "<h1>TestOrg</h1>") || strings.Contains(body, "{{") {
		t.Errorf("/o/{slug} lacks the organisation's name or has raw placeholders")
	}
	if code, _, body := get(ts.URL, "/o/no-such-org"); code != http.StatusNotFound || strings.Contains(body, "{{") {
		t.Errorf("/o/no-such-org = %d, want a rendered 404", code)
	}
	if code, _, _ := get(ts.URL, "/api/store/items"); code == http.StatusOK {
		t.Errorf("the store of every organisation's adventures still answers")
	}

	bare, _, cleanup2 := newTestServerWith(t, server.Config{})
	defer cleanup2()
	if code, _, body := get(bare.URL, "/store"); code != http.StatusNotFound || strings.Contains(body, "{{") {
		t.Errorf("/store without a home organisation = %d, want a rendered 404", code)
	}
}
