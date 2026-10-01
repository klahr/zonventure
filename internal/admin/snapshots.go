package admin

import (
	"sync"
	"time"
)

// snapshots shares what the live views build — an event's detail,
// activity, positions, tracks — between every socket watching it. Each
// console used to rebuild them for itself on every wake-up, so M
// watchers cost M times the queries; now the first to ask builds and
// the rest, asking meanwhile or soon after, get the same encoded bytes.
type snapshots struct {
	mu sync.Mutex
	m  map[string]*snapshot
}

type snapshot struct {
	started time.Time
	done    chan struct{}
	frame   []byte
	err     error
}

// snapshotsKept bounds the cache: past it, snapshots older than
// snapshotStale go when the next one is built.
const (
	snapshotsKept = 256
	snapshotStale = time.Minute
)

// get returns the frame under key from a build started after fresh,
// building it now if there is none. Concurrent callers share one build.
// A failed build is not kept: the next caller tries again.
func (s *snapshots) get(key string, fresh time.Time, build func() ([]byte, error)) ([]byte, error) {
	s.mu.Lock()
	if e := s.m[key]; e != nil && e.started.After(fresh) {
		s.mu.Unlock()
		<-e.done
		return e.frame, e.err
	}
	if s.m == nil {
		s.m = map[string]*snapshot{}
	}
	now := time.Now()
	if len(s.m) >= snapshotsKept {
		for k, e := range s.m {
			if now.Sub(e.started) > snapshotStale {
				delete(s.m, k)
			}
		}
	}
	e := &snapshot{started: now, done: make(chan struct{})}
	s.m[key] = e
	s.mu.Unlock()

	e.frame, e.err = build()
	close(e.done)
	if e.err != nil {
		s.mu.Lock()
		if s.m[key] == e {
			delete(s.m, key)
		}
		s.mu.Unlock()
	}
	return e.frame, e.err
}

// latest is the later of two times: a snapshot must postdate both.
func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
