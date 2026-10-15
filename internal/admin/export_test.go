package admin

import (
	"context"
	"time"

	"github.com/klahr/zonventure/internal/payments"
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

// ExportedQuoteStamp signs a request form's load time, as GetQuoteForm
// does, so a test can send a form that loaded long enough ago.
func ExportedQuoteStamp(key []byte, at time.Time) string {
	return quoteStamp(key, at)
}

func ExportedHandleTopUpCompleted(h *Handlers, ctx context.Context, ev payments.Event) error {
	return h.handleTopUpCompleted(ctx, ev)
}

func ExportedHandleCardSaved(h *Handlers, ctx context.Context, ev payments.Event) error {
	return h.handleCardSaved(ctx, ev)
}

func ExportedBillMonth(h *Handlers, ctx context.Context, now time.Time) { h.billLastMonth(ctx, now) }

func ExportedChargeDueBills(h *Handlers, ctx context.Context) { h.chargeDueBills(ctx, 0) }

func ExportedAlertLowBalances(h *Handlers, ctx context.Context) { h.alertLowBalances(ctx) }
