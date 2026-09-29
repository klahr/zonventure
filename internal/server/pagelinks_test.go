package server

import (
	"net/http"
	"net/http/httptest"
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
	// No legal identity: /privacy and /terms are unavailable; the
	// landing pinned off by env makes /pricing unavailable too.
	t.Setenv("ZV_LANDING_ENABLED", "0")
	mw := hideUnavailableLinks(nil, LegalConfig{})

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
