package player

import (
	"sync"
	"time"
)

// playLimiter applies in-process rate limits to the slot-authenticated
// endpoints. Two buckets, both keyed by slot id:
//
//   - Answer: rolling window. Up to AnswerBurst attempts in
//     AnswerWindow. Crucial because puzzle answers are short ("hi",
//     "blue") and trivially brute-forceable; without a rate cap a
//     scripted attacker could enumerate the accept list in seconds.
//
//   - Location: throttled. At most one ping per LocationMinInterval.
//     Real clients post every 15 seconds; anything faster than 5s is
//     either clock skew or a misbehaving page.
//
// State is per-process and resets on restart — fine for a single
// instance. Bounded growth: a slot that stops calling these endpoints
// leaves a small entry behind; we accept the leak (a long-lived
// process across many runs adds up to maybe a few KB) rather than
// adding a sweeper goroutine for a feature this trivial.
type playLimiter struct {
	answers   sync.Map // key: slotID → []time.Time (recent attempts, oldest first)
	answersMu sync.Mutex
	locations sync.Map // key: slotID → time.Time (last ping)
}

const (
	AnswerBurst         = 6
	AnswerWindow        = time.Minute
	LocationMinInterval = 5 * time.Second
)

// AllowAnswer returns (ok, retryAfter). ok=false means the slot has
// exhausted AnswerBurst attempts in AnswerWindow and must wait
// retryAfter before its next attempt.
func (l *playLimiter) AllowAnswer(slotID int64) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := time.Now()

	l.answersMu.Lock()
	defer l.answersMu.Unlock()

	var hits []time.Time
	if v, ok := l.answers.Load(slotID); ok {
		hits = v.([]time.Time)
	}
	cutoff := now.Add(-AnswerWindow)
	// Drop expired hits in-place (cheap; lists are tiny).
	pruned := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= AnswerBurst {
		// Lockout until the oldest hit ages out of the window.
		oldest := pruned[0]
		retry := AnswerWindow - now.Sub(oldest)
		if retry < time.Second {
			retry = time.Second
		}
		l.answers.Store(slotID, pruned)
		return false, retry.Round(time.Second)
	}
	pruned = append(pruned, now)
	l.answers.Store(slotID, pruned)
	return true, 0
}

// AllowLocation reports whether the slot may post a fresh location
// ping. Returns retryAfter when not allowed, rounded up to the next
// whole second.
func (l *playLimiter) AllowLocation(slotID int64) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := time.Now()
	v, ok := l.locations.Load(slotID)
	if !ok {
		l.locations.Store(slotID, now)
		return true, 0
	}
	last := v.(time.Time)
	gap := now.Sub(last)
	if gap < LocationMinInterval {
		retry := LocationMinInterval - gap
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry.Round(time.Second)
	}
	l.locations.Store(slotID, now)
	return true, 0
}
