package mailer

import "context"

// ExportedTick exposes Outbox.tick to the external _test package so
// regression tests can drive a single drain pass without spinning
// up the periodic worker.
func ExportedTick(o *Outbox, ctx context.Context) error {
	return o.tick(ctx)
}
