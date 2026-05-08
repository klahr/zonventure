package admin

import (
	"context"

	"git.sr.ht/~klahr/zv/internal/payments"
)

// Test-only re-exports so external_test files (package admin_test)
// can drive the unexported webhook handlers directly. Compiled only
// during `go test` thanks to the _test.go suffix.

func ExportedHandleTicketCheckoutCompleted(h *Handlers, ctx context.Context, ev payments.Event) error {
	return h.handleTicketCheckoutCompleted(ctx, ev)
}

func ExportedHandleChargeRefunded(h *Handlers, ctx context.Context, ev payments.Event) error {
	return h.handleChargeRefunded(ctx, ev)
}

// ExportedCreatePurchaseCheckoutSession exposes the unexported helper
// so tests can assert request shape (notably IdempotencyKey).
func ExportedCreatePurchaseCheckoutSession(h *Handlers, ctx context.Context, purchaseID int64,
	advSlug, advTitle string, maxPlayers, amountCents int, customerEmail, existingCustomerID string,
) (string, string, error) {
	return h.createPurchaseCheckoutSession(ctx, purchaseID, advSlug, advTitle, maxPlayers, amountCents, customerEmail, existingCustomerID)
}
