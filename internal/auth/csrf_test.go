package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// passHandler always 200s — used as the inner handler when we want to
// observe whether the middleware let the request through.
var passHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestEnsureCSRF_SetsCookieWhenMissing(t *testing.T) {
	mw := EnsureCSRF(false)(passHandler)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	got := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			got = c.Value
		}
	}
	if got == "" {
		t.Fatalf("expected zv_csrf cookie to be set")
	}
}

func TestEnsureCSRF_LeavesExistingCookie(t *testing.T) {
	mw := EnsureCSRF(false)(passHandler)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "preexisting"})
	mw.ServeHTTP(rec, r)

	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			t.Fatalf("middleware should not overwrite an existing cookie, got %q", c.Value)
		}
	}
}

func TestRequireCSRF_AllowsSafeMethods(t *testing.T) {
	mw := RequireCSRF()(passHandler)
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, httptest.NewRequest(m, "/", nil))
		if rec.Result().StatusCode != 200 {
			t.Fatalf("%s expected to pass without csrf, got %d", m, rec.Result().StatusCode)
		}
	}
}

func TestRequireCSRF_RejectsMissingCookie(t *testing.T) {
	mw := RequireCSRF()(passHandler)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(""))
	mw.ServeHTTP(rec, r)
	if rec.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("missing cookie should 403, got %d", rec.Result().StatusCode)
	}
}

func TestRequireCSRF_RejectsMismatchedHeader(t *testing.T) {
	mw := RequireCSRF()(passHandler)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(""))
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc"})
	r.Header.Set(CSRFHeaderName, "xyz")
	mw.ServeHTTP(rec, r)
	if rec.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("mismatched header should 403, got %d", rec.Result().StatusCode)
	}
}

func TestRequireCSRF_RejectsMissingHeader(t *testing.T) {
	mw := RequireCSRF()(passHandler)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(""))
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc"})
	mw.ServeHTTP(rec, r)
	if rec.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("missing header should 403, got %d", rec.Result().StatusCode)
	}
}

func TestRequireCSRF_AllowsMatchingPair(t *testing.T) {
	mw := RequireCSRF()(passHandler)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(""))
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc"})
	r.Header.Set(CSRFHeaderName, "abc")
	mw.ServeHTTP(rec, r)
	if rec.Result().StatusCode != 200 {
		t.Fatalf("matching header+cookie should pass, got %d", rec.Result().StatusCode)
	}
}
