package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/settings"
)

// reachedHandler is a sentinel inner handler the redirect middleware
// is wrapped around in these tests. When the request 301s, this
// handler must NOT be reached; when the path is allowlisted, it must.
type reachedHandler struct{ called bool }

func (h *reachedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

// pinRedirectTarget seeds the in-process redirect target via the env
// var so the test doesn't need a Postgres handle. t.Setenv handles
// cleanup. LoadRedirectTarget reads env first, so the pin sticks for
// the lifetime of the test.
func pinRedirectTarget(t *testing.T, target string) {
	t.Helper()
	t.Setenv("ZV_REDIRECT_TARGET", target)
	settings.LoadRedirectTarget(context.Background(), nil)
	t.Cleanup(func() {
		t.Setenv("ZV_REDIRECT_TARGET", "")
		settings.LoadRedirectTarget(context.Background(), nil)
	})
}

// TestRedirectMode_PathsAndAllowlist locks in the contract: every
// user-facing path 301s to the hosted target with the original path
// + query preserved, and the allowlist (/api/health, /api/version,
// /api/webhooks/stripe, /api/admin/migrate/redirect-mode) does not
// redirect. The Stripe webhook gets a special-cased 200 OK so the
// upstream never replays into the new instance.
func TestRedirectMode_PathsAndAllowlist(t *testing.T) {
	pinRedirectTarget(t, "https://hosted.example")

	cases := []struct {
		name       string
		path       string
		query      string
		method     string
		wantStatus int
		wantLoc    string // empty = no Location header expected
		wantInner  bool   // true = the inner handler must run
	}{
		{
			name:       "team-token redirects",
			path:       "/team/abc123",
			method:     http.MethodGet,
			wantStatus: http.StatusMovedPermanently,
			wantLoc:    "https://hosted.example/team/abc123",
		},
		{
			name:       "play-token preserves query",
			path:       "/play",
			query:      "t=xyz789",
			method:     http.MethodGet,
			wantStatus: http.StatusMovedPermanently,
			wantLoc:    "https://hosted.example/play?t=xyz789",
		},
		{
			name:       "live-token redirects",
			path:       "/live/abc",
			method:     http.MethodGet,
			wantStatus: http.StatusMovedPermanently,
			wantLoc:    "https://hosted.example/live/abc",
		},
		{
			name:       "store redirects",
			path:       "/store",
			method:     http.MethodGet,
			wantStatus: http.StatusMovedPermanently,
			wantLoc:    "https://hosted.example/store",
		},
		{
			name:       "root redirects",
			path:       "/",
			method:     http.MethodGet,
			wantStatus: http.StatusMovedPermanently,
			wantLoc:    "https://hosted.example/",
		},
		{
			name:       "arrange redirects",
			path:       "/arrange",
			method:     http.MethodGet,
			wantStatus: http.StatusMovedPermanently,
			wantLoc:    "https://hosted.example/arrange",
		},
		{
			name:       "health is allowlisted",
			path:       "/api/health",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantInner:  true,
		},
		{
			name:       "version is allowlisted",
			path:       "/api/version",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantInner:  true,
		},
		{
			name: "stripe webhook gets 200 without reaching inner handler",
			// The redirect middleware short-circuits the webhook with
			// 200 so Stripe stops retrying — we don't pass the body
			// through to the verifier (the operator has likely already
			// rotated the webhook secret).
			path:       "/api/webhooks/stripe",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantInner:  false,
		},
		{
			name:       "redirect-mode toggle is allowlisted",
			path:       "/api/admin/migrate/redirect-mode",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantInner:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &reachedHandler{}
			handler := redirectMode(inner)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.query != "" {
				req.URL.RawQuery = tc.query
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantLoc != "" {
				gotLoc := rec.Header().Get("Location")
				if gotLoc != tc.wantLoc {
					t.Errorf("Location: got %q, want %q", gotLoc, tc.wantLoc)
				}
			}
			if inner.called != tc.wantInner {
				t.Errorf("inner reached: got %v, want %v", inner.called, tc.wantInner)
			}
		})
	}
}

// TestRedirectMode_HotFlip exercises the in-memory atomic pointer:
// flipping the target on/off via the env-pin path takes effect
// immediately on the same handler instance. The DB-backed setter
// (SetRedirectTarget) does the same in production; we test the
// pointer mechanic, not the SQL round-trip, since the latter needs
// a Postgres handle and the contract is the same either way.
func TestRedirectMode_HotFlip(t *testing.T) {
	inner := &reachedHandler{}
	handler := redirectMode(inner)

	// Off: requests pass through.
	t.Setenv("ZV_REDIRECT_TARGET", "")
	settings.LoadRedirectTarget(context.Background(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/abc", nil))
	if rec.Code != http.StatusOK || !inner.called {
		t.Fatalf("expected pass-through with redirect mode off, got status=%d inner=%v",
			rec.Code, inner.called)
	}

	// Flip on: same handler now redirects.
	inner.called = false
	t.Setenv("ZV_REDIRECT_TARGET", "https://hosted.example")
	settings.LoadRedirectTarget(context.Background(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/abc", nil))
	if rec.Code != http.StatusMovedPermanently || inner.called {
		t.Fatalf("expected 301 with redirect mode on, got status=%d inner=%v",
			rec.Code, inner.called)
	}
	if got := rec.Header().Get("Location"); got != "https://hosted.example/team/abc" {
		t.Errorf("Location: got %q, want https://hosted.example/team/abc", got)
	}

	// Flip off: pass-through again.
	inner.called = false
	t.Setenv("ZV_REDIRECT_TARGET", "")
	settings.LoadRedirectTarget(context.Background(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/abc", nil))
	if rec.Code != http.StatusOK || !inner.called {
		t.Fatalf("expected pass-through after flip off, got status=%d inner=%v",
			rec.Code, inner.called)
	}
}

// TestRedirectMode_DBLessFastPath confirms the redirect path makes
// no DB calls — the self-host's DB can be on the way down during the
// cutover and the 301 still fires. We satisfy this by passing a nil
// *sql.DB to LoadRedirectTarget while ZV_REDIRECT_TARGET pins the
// value; the middleware then reads exclusively from the in-memory
// atomic pointer. If anything in the redirect handler reached for
// the DB, this test would panic on the nil deref.
func TestRedirectMode_DBLessFastPath(t *testing.T) {
	pinRedirectTarget(t, "https://hosted.example")

	inner := &reachedHandler{}
	handler := redirectMode(inner)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/abc?x=1", nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status: got %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, "https://hosted.example/team/abc") {
		t.Errorf("Location: got %q, want prefix https://hosted.example/team/abc", got)
	}
	if inner.called {
		t.Error("inner handler reached on a redirected request")
	}
}
