package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
)

// pricing is the platform's prices in force for this request.
func (h *Handlers) pricing() settings.Pricing {
	if h.Pricing != nil {
		return h.Pricing()
	}
	return settings.CurrentPricing()
}

// platformFee is the cut of tickets and parties orgs sell; zero disables
// the marketplace flow (no Connect gate, no application fee).
func (h *Handlers) platformFee() payments.PlatformFee { return payments.FeeFrom(h.pricing()) }

// hosted is the hosted event pass, per block of teams: see hosted_pricing.go.
func (h *Handlers) hosted() payments.HostedPass { return payments.HostedFrom(h.pricing()) }

// pricingResponse is GET/PUT /api/admin/pricing: the prices in force
// and the one currency they are in.
type pricingResponse struct {
	settings.Pricing
	Currency string `json:"currency"`
}

// GetPricing serves the platform's prices. Admin-only at the route level.
func (h *Handlers) GetPricing(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, pricingResponse{settings.CurrentPricing(), settings.Currency})
}

// SetPricing saves every price at once; the next checkout charges them.
// Purchases already made or opened keep their amounts. Logged with the
// old and new prices. Admin-only at the route level.
func (h *Handlers) SetPricing(w http.ResponseWriter, r *http.Request) {
	var p settings.Pricing
	if !httpx.DecodeJSON(w, r, &p) {
		return
	}
	if err := p.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	before := settings.CurrentPricing()
	if err := settings.SetPricing(r.Context(), h.DB, p); err != nil {
		logx.Error("admin.pricing.set_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "could not save the prices")
		return
	}
	me, _ := auth.FromContext(r.Context())
	logx.Info("admin.pricing", "by", me.Username, "before", before, "after", p)
	httpx.JSON(w, http.StatusOK, pricingResponse{p, settings.Currency})
}
