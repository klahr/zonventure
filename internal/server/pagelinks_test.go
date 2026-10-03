package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestHideUnavailableLinks(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><meta charset="utf-8"><title>x</title><script>var s = '</head>';</script></head><body><a href="/privacy">P</a></body></html>`))
	})
	js := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(`console.log(1)`))
	})
	// No legal identity: /privacy and /terms are unavailable; no
	// override pricing.html makes /pricing unavailable too.
	mw := hideUnavailableLinks(LegalConfig{})

	rec := httptest.NewRecorder()
	mw(page).ServeHTTP(rec, httptest.NewRequest("GET", "/arrange", nil))
	body := rec.Body.String()
	if !strings.HasPrefix(body, `<html><head><meta charset="utf-8"><script>(()=>{const u=["/privacy","/terms","/pricing"]`) {
		t.Errorf("html page = %q, want the hide script right after the charset meta", body)
	}
	if rec.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %s, body is %d bytes", rec.Header().Get("Content-Length"), len(body))
	}

	rec = httptest.NewRecorder()
	mw(js).ServeHTTP(rec, httptest.NewRequest("GET", "/i18n.js", nil))
	if rec.Body.String() != `console.log(1)` {
		t.Errorf("non-html body = %q, want it untouched", rec.Body.String())
	}
}

func TestDropSitemapPages(t *testing.T) {
	xml := "<urlset>\n  <url>\n    <loc>https://h/</loc>\n  </url>\n  <url>\n    <loc>https://h/privacy</loc>\n    <lastmod>x</lastmod>\n  </url>\n</urlset>"
	got := dropSitemapPages(xml, "https://h", []string{"/privacy"})
	if strings.Contains(got, "/privacy") || !strings.Contains(got, "<loc>https://h/</loc>") {
		t.Errorf("sitemap = %q", got)
	}
}

func TestUnavailablePages_PricingFollowsOverride(t *testing.T) {
	prev := webOverrideDir
	t.Cleanup(func() { webOverrideDir = prev })
	legal := LegalConfig{}

	webOverrideDir = t.TempDir()
	if got := unavailablePages(legal); !slices.Contains(got, "/pricing") {
		t.Errorf("no override pricing.html: unavailable = %v, want /pricing", got)
	}
	if err := os.WriteFile(filepath.Join(webOverrideDir, "pricing.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := unavailablePages(legal); slices.Contains(got, "/pricing") {
		t.Errorf("override pricing.html: unavailable = %v, want /pricing served", got)
	}
}
