package periodic

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestStart_ImmediateInvocation asserts fn runs once before the first
// tick — that's what scheduler/retention rely on so a long-restarted
// process doesn't wait an interval before catching up.
func TestStart_ImmediateInvocation(t *testing.T) {
	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := Start(ctx, time.Hour, func(_ context.Context) {
		atomic.AddInt32(&calls, 1)
	})

	// Give the goroutine a moment to fire its immediate pass; the
	// 1h interval guarantees no second invocation reaches us first.
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

// TestStart_TicksUntilCancel verifies fn keeps running on the interval
// and that the loop stops when ctx is cancelled — the channel closes
// only after the goroutine exits.
func TestStart_TicksUntilCancel(t *testing.T) {
	var calls int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := Start(ctx, 10*time.Millisecond, func(_ context.Context) {
		atomic.AddInt32(&calls, 1)
	})

	time.Sleep(55 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("done channel not closed within 1s of cancel")
	}

	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("calls = %d, want >= 2 (immediate + at least one tick)", got)
	}
}

// TestStart_PreCancelledCtx is the degenerate case: ctx already done
// at call time. The goroutine must exit without invoking fn.
func TestStart_PreCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls int32
	done := Start(ctx, time.Millisecond, func(_ context.Context) {
		atomic.AddInt32(&calls, 1)
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("done channel not closed within 1s for pre-cancelled ctx")
	}

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}
