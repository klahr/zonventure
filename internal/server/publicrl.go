package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/auth"
)

// publicLimiter caps requests-per-minute per IP across the public,
// no-auth surface (`/live/{token}`, `/team/{token}`, `/qr/team/{token}`,
// `/api/public/*`, `/ws/live/{token}`). The tokens these routes accept
// are unguessable, but the routes still hit the database on every probe
// — without a cap, a script can hammer them at line rate to enumerate
// or DoS.
//
// Generous cap (120/min) since a legitimate viewer's WebSocket
// reconnect storm or a captain's repeated /qr/team fetch shouldn't
// trip it. A real attacker mining tokens hits the wall fast.
//
// The live board (`/live`, `/ws/live`, `/api/public/live`) counts
// against its own bucket per IP. A venue puts the TV and every
// player's phone behind one NAT address, and the board's reconnects
// and poll fallback must not use up what the players' share pages
// need, or the other way round.
//
// In-memory only; resets on restart. Move to a shared store when we
// run more than one replica.
type publicLimiter struct {
	mu     sync.Mutex
	counts map[string]*publicCount
	now    func() time.Time
}

type publicCount struct {
	n           int
	windowStart time.Time
}

const (
	publicRLWindow      = time.Minute
	publicRLMaxRequests = 120
)

func newPublicLimiter(ctx context.Context) *publicLimiter {
	l := &publicLimiter{counts: map[string]*publicCount{}, now: time.Now}
	go l.sweepLoop(ctx)
	return l
}

// sweepLoop drops stale buckets so the map doesn't grow unboundedly
// across the lifetime of the server. Five-minute cadence is plenty —
// we just want to prune inactive IPs, not maintain second-level
// freshness. Exits cleanly when ctx is cancelled (graceful shutdown).
func (l *publicLimiter) sweepLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.sweep()
		}
	}
}

func (l *publicLimiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-2 * publicRLWindow)
	for k, c := range l.counts {
		if c.windowStart.Before(cutoff) {
			delete(l.counts, k)
		}
	}
}

func (l *publicLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.counts[ip]
	if c == nil || now.Sub(c.windowStart) >= publicRLWindow {
		l.counts[ip] = &publicCount{n: 1, windowStart: now}
		return true
	}
	if c.n >= publicRLMaxRequests {
		return false
	}
	c.n++
	return true
}

// publicRLMiddleware wraps a router/handler with the per-IP cap. 429s
// carry a Retry-After hint matching the bucket window so well-behaved
// clients back off correctly.
func publicRLMiddleware(l *publicLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(publicRLBucket(r.URL.Path) + auth.ClientIP(r)) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "rate limit — try again in a minute", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// publicRLBucket names the bucket a public path counts against, as a
// prefix for the client IP.
func publicRLBucket(path string) string {
	if strings.HasPrefix(path, "/live/") || strings.HasPrefix(path, "/ws/live/") ||
		strings.HasPrefix(path, "/api/public/live/") {
		return "live|"
	}
	return "public|"
}
