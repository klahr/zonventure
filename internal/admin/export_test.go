package admin

import (
	"context"

	stripe "github.com/stripe/stripe-go/v82"
)

// Test-only re-exports so external_test files (package admin_test)
// can drive the unexported webhook handlers directly. Compiled only
// during `go test` thanks to the _test.go suffix.

func ExportedHandleTicketCheckoutCompleted(h *Handlers, ctx context.Context, ev stripe.Event) error {
	return h.handleTicketCheckoutCompleted(ctx, ev)
}

func ExportedHandleChargeRefunded(h *Handlers, ctx context.Context, ev stripe.Event) error {
	return h.handleChargeRefunded(ctx, ev)
}
