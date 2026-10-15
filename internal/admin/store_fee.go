package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
)

// marketplace reports whether organisations sell in the store into
// Stripe accounts of their own, the platform taking its price for the
// teams out of each sale (PLATFORM_PRICING.md): a server that charges
// for teams, with a provider that connects accounts. Otherwise (a self-
// host without team prices, the simulated provider in dev) a sale goes
// to the platform's own account and nothing is taken out of it.
func (h *Handlers) marketplace() bool {
	_, ok := h.Provider.(payments.MarketplaceProvider)
	return ok && h.pricing().TeamCents > 0
}

// storeFee is the platform's price for what a store sale starts: teams
// teams of phones phones each, every team its team price and every phone
// past those a team includes its phone price. It is the sale's
// application fee in a marketplace, nothing otherwise.
func (h *Handlers) storeFee(teams, phones int) int64 {
	if !h.marketplace() {
		return 0
	}
	p := h.pricing()
	fee := int64(teams) * p.TeamCents
	if extra := int64(phones) - p.TeamPhones; extra > 0 {
		fee += int64(teams) * extra * p.PhoneCents
	}
	return fee
}

// ticketSaleRouting is where a store ticket's money goes: in a
// marketplace, the event's org's own Stripe account, with the
// platform's price for one team of teamSize phones as the application
// fee; else the platform's own account, nothing taken. A ticket priced
// at or below that fee can't be sold. It writes the refusal and
// reports false when the sale can't go ahead.
func (h *Handlers) ticketSaleRouting(w http.ResponseWriter, r *http.Request, eventID, price int64, teamSize int) (string, int64, bool) {
	if !h.marketplace() {
		return "", 0, true
	}
	acct, err := h.stripeConnectAccountForEvent(r.Context(), eventID)
	if errors.Is(err, ErrConnectNotReady) {
		// The store's listing filter hides such an event already; a
		// stale tab can race past it.
		httpx.Error(w, http.StatusConflict, "this adventure is not selling tickets right now")
		return "", 0, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "connect account lookup failed", err)
		return "", 0, false
	}
	fee := h.storeFee(1, teamSize)
	if fee >= price {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":     "this ticket costs less than its team: it can't be sold",
			"code":      "price_below_fee",
			"fee_cents": fee,
		})
		return "", 0, false
	}
	return acct, fee, true
}

// Marketplace is marketplace, for the public store's listing filter.
func (h *Handlers) Marketplace() bool { return h.marketplace() }

// refundUnplayedFee gives back the platform's fee on a store sale the
// org refunded itself, when what it bought never played. Best effort:
// a failure is logged for the operator to settle by hand.
func (h *Handlers) refundUnplayedFee(ctx context.Context, purchaseID int64, account, chargeID string) {
	mp, ok := h.Provider.(payments.MarketplaceProvider)
	if !ok {
		return
	}
	_, played, err := h.saleAccountAndPlay(ctx, purchaseID)
	if err == nil && !played {
		err = mp.RefundApplicationFee(ctx, account, chargeID)
	}
	if err != nil {
		logx.Warn("payments.fee_refund", "ticket_purchase_id", purchaseID, "charge_id", chargeID, "err", err.Error())
	}
}
