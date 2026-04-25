package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LoginLimiter throttles login attempts to slow down credential stuffing
// without DoS-ing real users behind typos. Two independent buckets:
//
//   - Per (ip, username): tight. After PairThreshold failures the pair
//     is locked, starting at PairBaseLockout and doubling each
//     subsequent failure up to PairMaxLockout. A successful login
//     clears the pair; ageing failures off the rolling PairWindow
//     resets the count but keeps the escalation level so a patient
//     attacker doesn't get a clean slate by waiting.
//
//   - Per ip: loose. After IPThreshold failures across any usernames
//     in IPWindow the whole ip is parked for IPLockout, blocking the
//     "rotate the username on every attempt" pattern.
//
// State is in-memory only — fine for a single instance, resets on
// restart. Move to a shared store when we run more than one replica.
type LoginLimiter struct {
	mu      sync.Mutex
	perPair map[string]*pairState // key = ip + "\x00" + lower(username)
	perIP   map[string]*ipState
	now     func() time.Time
}

type pairState struct {
	fails       int
	level       int // doubles the lockout each step
	lockedUntil time.Time
	lastFail    time.Time
}

type ipState struct {
	fails       int
	lockedUntil time.Time
	lastFail    time.Time
}

const (
	PairThreshold   = 5
	PairWindow      = time.Hour
	PairBaseLockout = 60 * time.Second
	PairMaxLockout  = 30 * time.Minute
	IPThreshold     = 50
	IPWindow        = time.Hour
	IPLockout       = time.Hour
)

// NewLoginLimiter returns a fresh limiter using time.Now as its clock.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		perPair: map[string]*pairState{},
		perIP:   map[string]*ipState{},
		now:     time.Now,
	}
}

// Allow reports whether (ip, username) may currently attempt to log in.
// When false, retryAfter is how long the caller must wait — surface it
// as the Retry-After header so the client throttles correctly.
func (l *LoginLimiter) Allow(ip, username string) (ok bool, retryAfter time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if p, ok := l.perPair[pairKey(ip, username)]; ok && now.Before(p.lockedUntil) {
		return false, ceilSecond(p.lockedUntil.Sub(now))
	}
	if i, ok := l.perIP[ip]; ok && now.Before(i.lockedUntil) {
		return false, ceilSecond(i.lockedUntil.Sub(now))
	}
	return true, 0
}

// RecordFailure logs a failed login against both buckets and returns
// whether this attempt pushed (ip, username) into a fresh lockout. The
// caller already wrote the 401; the boolean lets it upgrade to 429
// instead so the client sees the new Retry-After immediately.
func (l *LoginLimiter) RecordFailure(ip, username string) (locked bool, retryAfter time.Duration) {
	if l == nil {
		return false, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	pk := pairKey(ip, username)
	p := l.perPair[pk]
	if p == nil {
		p = &pairState{}
		l.perPair[pk] = p
	}
	if !p.lastFail.IsZero() && now.Sub(p.lastFail) > PairWindow {
		p.fails = 0
	}
	p.fails++
	p.lastFail = now
	if p.fails >= PairThreshold {
		d := PairBaseLockout << p.level
		if d <= 0 || d > PairMaxLockout {
			d = PairMaxLockout
		}
		p.lockedUntil = now.Add(d)
		p.fails = 0
		p.level++
		locked = true
		retryAfter = ceilSecond(d)
	}

	i := l.perIP[ip]
	if i == nil {
		i = &ipState{}
		l.perIP[ip] = i
	}
	if !i.lastFail.IsZero() && now.Sub(i.lastFail) > IPWindow {
		i.fails = 0
	}
	i.fails++
	i.lastFail = now
	if i.fails >= IPThreshold {
		i.lockedUntil = now.Add(IPLockout)
		i.fails = 0
	}
	return locked, retryAfter
}

// RecordSuccess clears the (ip, username) pair so a returning user with
// the correct password gets full latitude back. The per-IP bucket is
// intentionally left in place — a successful login doesn't absolve an
// IP that's been brute-forcing OTHER usernames.
func (l *LoginLimiter) RecordSuccess(ip, username string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.perPair, pairKey(ip, username))
}

func pairKey(ip, username string) string {
	return ip + "\x00" + strings.ToLower(username)
}

// ceilSecond rounds a duration UP to the nearest second so the
// Retry-After header is integer-valued and never under-promises.
func ceilSecond(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Second
	}
	if r := d % time.Second; r != 0 {
		d += time.Second - r
	}
	return d
}

// ClientIP returns the request's source IP. Trusts the leftmost
// X-Forwarded-For entry when set — assumes the server runs behind a
// trusted reverse proxy in production and is reachable directly only
// in dev. Do NOT honour XFF without a proxy in front; an attacker can
// forge the header and dodge the per-IP bucket.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
