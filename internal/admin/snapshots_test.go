package admin

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSnapshotsShareOneBuild(t *testing.T) {
	var s snapshots
	var builds atomic.Int32
	release := make(chan struct{})
	build := func() ([]byte, error) {
		builds.Add(1)
		<-release
		return []byte("frame"), nil
	}
	before := time.Now().Add(-time.Second)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if f, err := s.get("detail:1", before, build); err != nil || string(f) != "frame" {
				t.Errorf("get = %q, %v", f, err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Errorf("builds = %d, want one shared by all watchers", n)
	}
}

func TestSnapshotsRebuildAfterAPublish(t *testing.T) {
	var s snapshots
	n := 0
	build := func() ([]byte, error) { n++; return []byte{byte(n)}, nil }
	s.get("k", time.Time{}, build)
	// Nothing published since: the snapshot is current.
	s.get("k", time.Time{}, build)
	if n != 1 {
		t.Fatalf("builds = %d, want 1", n)
	}
	// A publish after it was built makes it stale.
	f, _ := s.get("k", time.Now(), build)
	if n != 2 || f[0] != 2 {
		t.Fatalf("after a publish: builds = %d, frame %v", n, f)
	}
}

func TestSnapshotsDoNotKeepFailures(t *testing.T) {
	var s snapshots
	n := 0
	fail := func() ([]byte, error) { n++; return nil, errors.New("db down") }
	if _, err := s.get("k", time.Time{}, fail); err == nil {
		t.Fatal("want the build's error")
	}
	s.get("k", time.Time{}, fail)
	if n != 2 {
		t.Errorf("builds = %d, want a retry after a failure", n)
	}
}
