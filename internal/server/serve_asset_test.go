package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServeAsset_ETagAndCache asserts the cache-friendly headers on
// the asset handler — ETag present, Cache-Control public+max-age,
// 304 returned when If-None-Match matches.
func TestServeAsset_ETagAndCache(t *testing.T) {
	handler := serveAsset("web/topbar.css", "text/css; charset=utf-8")

	// First request: 200 with ETag + Cache-Control.
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "/assets/topbar.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag missing on first response")
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "public") || !strings.Contains(cc, "max-age=") {
		t.Errorf("Cache-Control = %q, want public + max-age", cc)
	}
	if rec.Body.Len() == 0 {
		t.Error("body empty on first response")
	}

	// Conditional request with matching ETag: 304, no body.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/assets/topbar.css", nil)
	req2.Header.Set("If-None-Match", etag)
	handler(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional request status = %d, want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 body length = %d, want 0", rec2.Body.Len())
	}

	// Conditional request with stale ETag: 200 with body.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/assets/topbar.css", nil)
	req3.Header.Set("If-None-Match", `"stale"`)
	handler(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("stale ETag status = %d, want 200", rec3.Code)
	}
}
