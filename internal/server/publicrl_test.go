package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The live board and the rest of the public surface count against
// separate buckets, so a TV behind a venue NAT can't use up what the
// players' share pages need from the same address.
func TestPublicRL_LiveBoardHasItsOwnBucket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := publicRLMiddleware(newPublicLimiter(ctx))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	hit := func(path string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "203.0.113.7:4000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < publicRLMaxRequests; i++ {
		if c := hit("/api/public/live/tok"); c != http.StatusOK {
			t.Fatalf("live request %d = %d", i, c)
		}
	}
	if c := hit("/ws/live/tok"); c != http.StatusTooManyRequests {
		t.Fatalf("live request past the cap = %d, want 429", c)
	}
	if c := hit("/team/tok"); c != http.StatusOK {
		t.Fatalf("share page after the live bucket filled = %d, want 200", c)
	}
}
