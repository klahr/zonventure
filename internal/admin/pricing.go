package admin

import (
	"context"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
)

// pricing is the platform's prices in force for this request.
func (h *Handlers) pricing() settings.Pricing {
	if h.Pricing != nil {
		return h.Pricing()
	}
	return settings.CurrentPricing()
}

// pricingResponse is GET/PUT /api/admin/pricing: the prices in force,
// the one currency they are in, and the orgs with a listing up that
// can't sell until they connect a Stripe account of their own.
type pricingResponse struct {
	settings.Pricing
	Currency           string              `json:"currency"`
	UnconnectedSellers []unconnectedSeller `json:"unconnected_sellers"`
}

// unconnectedSeller is an org with approved, running store listings
// and no Stripe Connect account that can take charges.
type unconnectedSeller struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Listings int    `json:"listings"`
}

// unconnectedSellers lists the orgs whose listings the store holds
// back until their own Stripe account can take charges
// (public/store.go storeItemRowFilter).
func (h *Handlers) unconnectedSellers(ctx context.Context) ([]unconnectedSeller, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT o.id, o.name, COUNT(*)
		FROM events e
		JOIN event_listings l ON l.event_id = e.id
		JOIN organizations o ON o.id = e.org_id
		WHERE e.public_for_sale
		  AND e.ticket_price_cents IS NOT NULL
		  AND e.state = 'running'
		  AND e.billing <> 'free'
		  AND NOT o.stripe_connect_charges_enabled
		GROUP BY o.id, o.name
		ORDER BY o.name, o.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []unconnectedSeller{}
	for rows.Next() {
		var s unconnectedSeller
		if err := rows.Scan(&s.ID, &s.Name, &s.Listings); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetPricing serves the platform's prices. Admin-only at the route level.
func (h *Handlers) GetPricing(w http.ResponseWriter, r *http.Request) {
	sellers, err := h.unconnectedSellers(r.Context())
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "could not read the prices", err)
		return
	}
	httpx.JSON(w, http.StatusOK, pricingResponse{settings.CurrentPricing(), settings.Currency, sellers})
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
	sellers, err := h.unconnectedSellers(r.Context())
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "could not read the prices", err)
		return
	}
	httpx.JSON(w, http.StatusOK, pricingResponse{p, settings.Currency, sellers})
}
