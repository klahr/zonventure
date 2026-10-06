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

	fired := make(chan struct{}, 1)
	done := Start(ctx, time.Hour, func(_ context.Context) {
		atomic.AddInt32(&calls, 1)
		select {
		case fired <- struct{}{}:
		default:
		}
	})

	// Wait for the immediate pass rather than sleeping a guessed
	// amount; the 1h interval guarantees no second invocation
	// reaches us first.
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("fn not invoked within 5s of Start")
	}
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

	ticked := make(chan struct{})
	done := Start(ctx, 10*time.Millisecond, func(_ context.Context) {
		if atomic.AddInt32(&calls, 1) == 2 {
			close(ticked)
		}
	})

	// Wait for the immediate pass plus one tick instead of sleeping.
	select {
	case <-ticked:
	case <-time.After(5 * time.Second):
		t.Fatal("no tick within 5s of Start")
	}
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
