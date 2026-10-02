// Package periodic runs a function on a fixed interval until the
// supplied context is cancelled. The contract: launch a goroutine,
// invoke fn once immediately, then on each tick of a time.Ticker, and
// expose a "done" channel callers can wait on during process shutdown.
//
// Pulled out of internal/scheduler and internal/retention which were
// reimplementing the exact same shell. fn must respect ctx itself —
// Start does not interrupt a running invocation, only stops scheduling
// new ones once ctx is cancelled.
package periodic

import (
	"context"
	"time"

	"github.com/klahr/zonventure/internal/logx"
)

// Start runs fn once immediately, then every interval, until ctx is
// done. The returned channel is closed after the final invocation
// returns, so cmd/server can sequence its shutdown waits.
//
// If ctx is already cancelled at call time, fn is not invoked at all
// and the done channel closes immediately.
func Start(ctx context.Context, interval time.Duration, fn func(context.Context)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer logx.Guard("periodic")
		if ctx.Err() != nil {
			return
		}
		fn(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				fn(ctx)
			}
		}
	}()
	return done
}
