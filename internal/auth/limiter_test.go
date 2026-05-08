package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

// fakeClock returns a *time.Time-backed time.Now stand-in so tests can
// step through lockout windows deterministically.
func fakeClock(start time.Time) (*time.Time, func() time.Time) {
	now := start
	return &now, func() time.Time { return now }
}

func TestLimiter_AllowsInitialAttempts(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < PairThreshold-1; i++ {
		if ok, _ := l.Allow("1.1.1.1", "alice"); !ok {
			t.Fatalf("attempt %d unexpectedly denied", i)
		}
		l.RecordFailure("1.1.1.1", "alice")
	}
	// Still inside the threshold — Allow stays true.
	if ok, _ := l.Allow("1.1.1.1", "alice"); !ok {
		t.Fatalf("Allow should be true just below threshold")
	}
}

func TestLimiter_LocksAtThreshold(t *testing.T) {
	now, clock := fakeClock(time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC))
	l := NewLoginLimiter()
	l.now = clock

	for i := 0; i < PairThreshold-1; i++ {
		if locked, _ := l.RecordFailure("1.1.1.1", "alice"); locked {
			t.Fatalf("locked early at attempt %d", i)
		}
	}
	locked, retry := l.RecordFailure("1.1.1.1", "alice")
	if !locked {
		t.Fatalf("expected lock at attempt %d", PairThreshold)
	}
	if retry < PairBaseLockout {
		t.Fatalf("retry %s < base %s", retry, PairBaseLockout)
	}

	// Allow now reports denied with same retry.
	ok, after := l.Allow("1.1.1.1", "alice")
	if ok || after <= 0 {
		t.Fatalf("expected denied with retry, got ok=%v after=%s", ok, after)
	}

	// Step past the lockout — fresh allow.
	*now = now.Add(retry + time.Second)
	if ok, _ := l.Allow("1.1.1.1", "alice"); !ok {
		t.Fatalf("expected Allow to recover after lockout")
	}
}

func TestLimiter_BackoffEscalates(t *testing.T) {
	now, clock := fakeClock(time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC))
	l := NewLoginLimiter()
	l.now = clock

	// First lockout.
	for i := 0; i < PairThreshold; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	first := mustLocked(t, l, "1.1.1.1", "alice")
	*now = now.Add(first + time.Second)

	// Second wave of failures should hit a longer lockout.
	for i := 0; i < PairThreshold; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	second := mustLocked(t, l, "1.1.1.1", "alice")
	if second <= first {
		t.Fatalf("expected escalating backoff: first=%s second=%s", first, second)
	}
}

func TestLimiter_SuccessClearsPair(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < PairThreshold-1; i++ {
		l.RecordFailure("1.1.1.1", "alice")
	}
	l.RecordSuccess("1.1.1.1", "alice")
	// Counter cleared — one more failure shouldn't lock.
	if locked, _ := l.RecordFailure("1.1.1.1", "alice"); locked {
		t.Fatalf("RecordSuccess should have cleared the pair")
	}
}

func TestLimiter_PerIPBucketTripsAcrossUsers(t *testing.T) {
	_, clock := fakeClock(time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC))
	l := NewLoginLimiter()
	l.now = clock

	// Rotate username so the per-pair bucket never trips.
	for i := 0; i < IPThreshold; i++ {
		l.RecordFailure("1.1.1.1", makeUser(i))
	}
	if ok, retry := l.Allow("1.1.1.1", "anyone-else"); ok || retry <= 0 {
		t.Fatalf("expected per-IP lockout after %d rotated misses; got ok=%v retry=%s",
			IPThreshold, ok, retry)
	}
}

func TestLimiter_NilSafe(t *testing.T) {
	var l *LoginLimiter
	if ok, _ := l.Allow("x", "y"); !ok {
		t.Fatalf("nil limiter should be permissive")
	}
	if locked, _ := l.RecordFailure("x", "y"); locked {
		t.Fatalf("nil limiter should never lock")
	}
	l.RecordSuccess("x", "y") // must not panic
}

func TestClientIP_TrustsXFFFromTrustedPeer(t *testing.T) {
	withTrustedProxies(t, "127.0.0.0/8")
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q, want 203.0.113.7", got)
	}
}

// TestClientIP_IgnoresXFFFromUntrustedPeer is the C6 regression
// guard. Without a trusted-proxy gate, an attacker on a directly-
// reachable deployment forges X-Forwarded-For and dodges per-IP
// rate limiting + audit logging. ClientIP must fall back to the
// real remote address whenever the immediate peer isn't whitelisted.
func TestClientIP_IgnoresXFFFromUntrustedPeer(t *testing.T) {
	withTrustedProxies(t) // empty: trust nothing
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "203.0.113.99:54321"
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.0.1")
	if got := ClientIP(r); got != "203.0.113.99" {
		t.Fatalf("ClientIP = %q, want 203.0.113.99 (XFF from untrusted peer must be ignored)", got)
	}
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	withTrustedProxies(t, "127.0.0.0/8")
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	if got := ClientIP(r); got != "127.0.0.1" {
		t.Fatalf("ClientIP = %q, want 127.0.0.1", got)
	}
}

func TestCeilSecond(t *testing.T) {
	cases := []struct {
		in, want time.Duration
	}{
		{500 * time.Millisecond, time.Second},
		{time.Second, time.Second},
		{1500 * time.Millisecond, 2 * time.Second},
		{0, time.Second},
	}
	for _, c := range cases {
		if got := ceilSecond(c.in); got != c.want {
			t.Fatalf("ceilSecond(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func mustLocked(t *testing.T, l *LoginLimiter, ip, user string) time.Duration {
	t.Helper()
	ok, retry := l.Allow(ip, user)
	if ok {
		t.Fatalf("expected (%s,%s) locked; was open", ip, user)
	}
	if retry <= 0 {
		t.Fatalf("retry must be positive while locked, got %s", retry)
	}
	return retry
}

func makeUser(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return string(letters[i%len(letters)]) + string(letters[(i/len(letters))%len(letters)])
}

// TestLimiter_SweepDropsExpiredEntries confirms the periodic sweeper
// keeps the in-memory maps from growing unboundedly: a patient
// attacker rotating fake (ip, user) pairs would otherwise accumulate
// state forever, eventually OOMing the process. After the lockout
// window plus the per-pair window have elapsed since the last fail,
// Sweep should drop the entry.
func TestLimiter_SweepDropsExpiredEntries(t *testing.T) {
	now, clock := fakeClock(time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC))
	l := NewLoginLimiter()
	l.now = clock

	for range PairThreshold {
		l.RecordFailure("1.1.1.1", "alice")
	}
	if len(l.perPair) != 1 || len(l.perIP) != 1 {
		t.Fatalf("setup: want 1 entry each, got pair=%d ip=%d", len(l.perPair), len(l.perIP))
	}

	// Within the windows — Sweep keeps the rows.
	*now = now.Add(time.Minute)
	l.Sweep()
	if len(l.perPair) != 1 {
		t.Fatalf("sweep evicted active entry")
	}

	// Past the lockout AND past PairWindow — Sweep should drop both.
	*now = now.Add(2 * PairMaxLockout).Add(2 * PairWindow)
	l.Sweep()
	if len(l.perPair) != 0 || len(l.perIP) != 0 {
		t.Fatalf("sweep failed to drop expired entries: pair=%d ip=%d",
			len(l.perPair), len(l.perIP))
	}
}
