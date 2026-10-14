package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/domains"
)

func stageDomains(t *testing.T, es ...domains.Entry) {
	t.Helper()
	domains.Set(es)
	t.Cleanup(func() { domains.Set(nil) })
}

func TestCustomDomainRouting(t *testing.T) {
	until := time.Now().Add(30 * 24 * time.Hour)
	stageDomains(t,
		domains.Entry{OrgID: 7, Slug: "acme", Name: "Acme Quests", Host: "play.acme.se", Status: domains.StatusLive, Until: until, HasLogo: true},
		domains.Entry{OrgID: 8, Slug: "gone", Name: "Gone", Host: "play.gone.se", Status: domains.StatusLive, Until: time.Now().Add(-40 * 24 * time.Hour)},
		domains.Entry{OrgID: 9, Slug: "quit", Name: "Quit", Host: "play.quit.se", Status: domains.StatusRemoved, Until: until},
	)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, wl := whiteLabelFrom(r.Context())
		w.Header().Set("X-Path", r.URL.Path)
		if wl {
			w.Header().Set("X-WL", "1")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<title>Store · Zonventure</title><!--platform--><a href="/manual">Manual</a><!--/platform--><link rel="icon" href="/favicon.svg" type="image/svg+xml">`))
	})
	h := customDomains("https://zonventure.com")(next)
	do := func(host, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	w := do("play.acme.se", "/")
	if w.Header().Get("X-Path") != "/o/acme" || w.Header().Get("X-WL") != "1" {
		t.Fatalf("/ on own domain: path %q wl %q", w.Header().Get("X-Path"), w.Header().Get("X-WL"))
	}
	body := w.Body.String()
	if strings.Contains(body, "Zonventure") || strings.Contains(body, "/manual") || !strings.Contains(body, "Acme Quests") || !strings.Contains(body, `href="/favicon.ico">`) {
		t.Errorf("white-label body: %s", body)
	}

	if w := do("play.acme.se", "/redeem"); w.Header().Get("X-Path") != "/o/acme/redeem" {
		t.Errorf("/redeem: %q", w.Header().Get("X-Path"))
	}
	if w := do("play.acme.se", "/o/acme"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/" {
		t.Errorf("/o/acme: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do("play.acme.se", "/o/other"); w.Code != http.StatusNotFound {
		t.Errorf("another org's page: %d", w.Code)
	}
	if w := do("play.acme.se", "/app/arrange/"); w.Code != http.StatusFound || w.Header().Get("Location") != "https://zonventure.com/app/arrange/" {
		t.Errorf("console: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do("play.acme.se", "/pricing"); w.Header().Get("Location") != "/" {
		t.Errorf("pricing: %q", w.Header().Get("Location"))
	}
	if w := do("play.acme.se", "/favicon.ico"); w.Header().Get("X-Path") != "/org-logo/7" {
		t.Errorf("favicon: %q", w.Header().Get("X-Path"))
	}
	if w := do("play.acme.se", domains.ProbePath); w.Body.String() != "play.acme.se" {
		t.Errorf("probe: %q", w.Body.String())
	}
	if w := do("play.acme.se", "/play"); w.Header().Get("X-Path") != "/play" || w.Header().Get("X-WL") != "1" {
		t.Errorf("/play should pass through in the org's name")
	}

	// Past the grace: back to the instance's store.
	if w := do("play.gone.se", "/"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "https://zonventure.com/o/gone" {
		t.Errorf("lapsed: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do("play.gone.se", "/play?t=x"); w.Header().Get("Location") != "https://zonventure.com/play?t=x" {
		t.Errorf("lapsed play link: %q", w.Header().Get("Location"))
	}

	// Given up: links already sent go back to the instance's store.
	if w := do("play.quit.se", "/"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "https://zonventure.com/o/quit" {
		t.Errorf("removed: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do("zonventure.com", "/o/quit"); w.Code != http.StatusOK {
		t.Errorf("a removed domain's org page stays here: %d", w.Code)
	}

	// The instance's own host: a live org's pages move, play links don't.
	if w := do("zonventure.com", "/o/acme"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "https://play.acme.se/" {
		t.Errorf("main /o/acme: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do("zonventure.com", "/o/acme/store/event/3?x=1"); w.Header().Get("Location") != "https://play.acme.se/o/acme/store/event/3?x=1" {
		t.Errorf("main event page: %q", w.Header().Get("Location"))
	}
	if w := do("zonventure.com", "/play"); w.Code != http.StatusOK || w.Header().Get("X-WL") != "" {
		t.Errorf("main /play: %d", w.Code)
	}
	if w := do("zonventure.com", "/o/gone"); w.Code != http.StatusOK {
		t.Errorf("a lapsed org's page stays: %d", w.Code)
	}
}

func TestTLSAllowed(t *testing.T) {
	until := time.Now().Add(24 * time.Hour)
	stageDomains(t,
		domains.Entry{OrgID: 1, Slug: "a", Host: "ok.a.se", Status: domains.StatusCertificate, Until: until},
		domains.Entry{OrgID: 2, Slug: "b", Host: "waiting.b.se", Status: domains.StatusWaiting, Until: until},
		domains.Entry{OrgID: 3, Slug: "c", Host: "lapsed.c.se", Status: domains.StatusLive},
	)
	h := customDomains("https://zonventure.com")(http.NotFoundHandler())
	for host, want := range map[string]int{"ok.a.se": 200, "waiting.b.se": 404, "lapsed.c.se": 404, "unknown.se": 404} {
		req := httptest.NewRequest(http.MethodGet, "/internal/tls-allowed?domain="+host, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("%s: %d, want %d", host, w.Code, want)
		}
	}
}

// A look font resolves to its stylesheet once the player is built: the
// build manifest names the files under fonts/.
func TestLookFontURL(t *testing.T) {
	if playAsset("fonts/fredoka-latin.woff2") == "" {
		t.Skip("no player build (make web)")
	}
	if got := lookFontURL("fredoka"); got != "/assets/look-font/fredoka.css" {
		t.Errorf("lookFontURL(fredoka) = %q", got)
	}
	if got := lookFontURL("baloo-2"); got == "" {
		t.Error("baloo-2 should resolve through its file stem")
	}
}

// An organisation with its own colours: its brand block must not land
// inside another <style>, or the page's CSS shows as text.
func TestOrgPageStylesIntact(t *testing.T) {
	org := orgProfile{ID: 7, Slug: "acme", Name: "Acme",
		Accent: sql.NullString{String: "#495369", Valid: true}, Accent2: sql.NullString{String: "#f59e0b", Valid: true}}
	for _, wl := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if wl {
			r = r.WithContext(domains.WithEntry(r.Context(), domains.Entry{OrgID: 7, Slug: "acme", Name: "Acme", Host: "play.acme.se"}))
		}
		w := httptest.NewRecorder()
		renderOrgPage(w, r, org)
		body := w.Body.String()
		if !strings.Contains(body, "--accent:#495369") {
			t.Fatalf("brand colours missing (white-label %v)", wl)
		}
		depth := 0
		for _, tok := range regexp.MustCompile(`</?style\b`).FindAllString(body, -1) {
			if tok == "<style" {
				depth++
			} else {
				depth--
			}
			if depth < 0 || depth > 1 {
				t.Fatalf("nested or stray <style> (white-label %v)", wl)
			}
		}
		if depth != 0 {
			t.Fatalf("unclosed <style> (white-label %v)", wl)
		}
	}
}
