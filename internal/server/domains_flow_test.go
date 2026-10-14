package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/domains"
)

type domainView struct {
	Enabled  bool `json:"enabled"`
	Entitled bool `json:"entitled"`
	Domain   *struct {
		Host   string `json:"host"`
		Name   string `json:"name"`
		Value  string `json:"value"`
		Status string `json:"status"`
	} `json:"domain"`
}

// An organisation sets up its own domain: refused until granted, a bare
// domain refused, a dev *.localhost name live at once; its store then
// answers there in its name only, and after it gives the domain up,
// links to it go back to the instance.
func TestOwnDomainFlow(t *testing.T) {
	t.Setenv("ZV_DOMAIN_CNAME_SUFFIX", "cname.zv.test")
	t.Setenv("ZV_ENV", "dev")
	boss, db, hc, orgID, _, cleanup := redemptionStack(t)
	defer cleanup()
	defer domains.Set(nil)

	var d domainView
	if s := boss.do("GET", "/api/admin/orgs/me/domain", nil, &d); s != 200 || !d.Enabled || d.Entitled || d.Domain != nil {
		t.Fatalf("before: %d %+v", s, d)
	}
	if s, e := doErr(boss, "PUT", "/api/admin/orgs/me/domain", map[string]string{"host": "play.testorg.localhost"}); s != 403 || e.Reason != "not_entitled" {
		t.Fatalf("not granted: %d %+v", s, e)
	}
	if _, err := db.Exec(`UPDATE organizations SET custom_domain_until = current_date + 365, name = 'Acme Quests' WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	if s, e := doErr(boss, "PUT", "/api/admin/orgs/me/domain", map[string]string{"host": "testorg.se"}); s != 400 || e.Reason != "apex" {
		t.Fatalf("bare domain: %d %+v", s, e)
	}
	if s := boss.do("PUT", "/api/admin/orgs/me/domain", map[string]string{"host": "https://Play.Testorg.localhost/"}, &d); s != 200 || d.Domain == nil {
		t.Fatalf("claim: %d %+v", s, d)
	}
	if d.Domain.Host != "play.testorg.localhost" || d.Domain.Name != "play" || d.Domain.Value != "testorg.cname.zv.test" || d.Domain.Status != "live" {
		t.Fatalf("claimed: %+v", *d.Domain)
	}
	var mails int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mail_outbox WHERE kind = 'domain' AND to_addr = 'boss@org.test'`).Scan(&mails); err != nil || mails != 1 {
		t.Fatalf("live email: %d %v", mails, err)
	}

	get := func(path string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", boss.ts.URL+path, nil)
		req.Host = "play.testorg.localhost"
		c := *hc
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	resp, body := get("/")
	if resp.StatusCode != 200 || !strings.Contains(body, "Acme Quests") || strings.Contains(body, "Zonventure") || strings.Contains(body, `href="/manual"`) {
		t.Fatalf("store on own domain: %d\n%s", resp.StatusCode, body)
	}
	if s, e := doErr(boss, "PUT", "/api/admin/orgs/me/domain/brand", map[string]string{"display_name": "Acme", "font": "comic-sans", "terms_url": "", "privacy_url": ""}); s != 400 || e.Reason != "invalid_font" {
		t.Fatalf("bad font: %d %+v", s, e)
	}
	if s, e := doErr(boss, "PUT", "/api/admin/orgs/me/domain/brand", map[string]string{"display_name": "Acme", "font": "", "terms_url": "http://acme.test/terms", "privacy_url": ""}); s != 400 || e.Reason != "invalid_url" {
		t.Fatalf("plain http terms: %d %+v", s, e)
	}
	if s := boss.do("PUT", "/api/admin/orgs/me/domain/brand", map[string]string{"display_name": "Acme Adventures", "font": "fredoka", "terms_url": "https://acme.test/terms", "privacy_url": ""}, nil); s != 200 {
		t.Fatalf("brand: %d", s)
	}
	_, body = get("/")
	if !strings.Contains(body, "Acme Adventures") || !strings.Contains(body, `href="https://acme.test/terms"`) || strings.Contains(body, "Acme Quests") || !strings.Contains(body, "<title>Acme Adventures — Store</title>") {
		t.Fatalf("branded store:\n%s", body)
	}
	if resp, _ := get("/app/arrange/"); resp.StatusCode != 302 || resp.Header.Get("Location") != "https://zv.test/app/arrange/" {
		t.Fatalf("console: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if base := domains.Base(orgID, "https://zv.test"); !strings.HasPrefix(base, "http://play.testorg.localhost") {
		t.Fatalf("links: %q", base)
	}

	if s := boss.do("DELETE", "/api/admin/orgs/me/domain", nil, &d); s != 200 || d.Domain != nil {
		t.Fatalf("remove: %d %+v", s, d)
	}
	if resp, _ := get("/"); resp.StatusCode != 301 || resp.Header.Get("Location") != "https://zv.test/o/testorg" {
		t.Fatalf("after removal: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if base := domains.Base(orgID, "https://zv.test"); base != "https://zv.test" {
		t.Fatalf("links after removal: %q", base)
	}

	// A name under the instance's own domain: only with no end date.
	if s, e := doErr(boss, "PUT", "/api/admin/orgs/me/domain", map[string]string{"host": "adventures.zv.test"}); s != 400 || e.Reason != "ours" {
		t.Fatalf("own zone, yearly: %d %+v", s, e)
	}
	if _, err := db.Exec(`UPDATE organizations SET custom_domain_until = '9999-12-31' WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	if s := boss.do("PUT", "/api/admin/orgs/me/domain", map[string]string{"host": "adventures.zv.test"}, &d); s != 200 || d.Domain == nil || d.Domain.Host != "adventures.zv.test" {
		t.Fatalf("own zone, no end date: %d %+v", s, d)
	}
}
