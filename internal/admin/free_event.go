package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
)

// updateEventBilling applies UpdateEvent's billing: an admin of the
// site's own organisation (HomeOrg) makes its self-guided event free
// (price 0) or charged per team again (no price, to be set). Another
// organisation's free tickets would be the platform's cost, so only the
// home one gives them. A free event's price can't be set; pricing says
// the request sets one. false when it answered.
func (h *Handlers) updateEventBilling(w http.ResponseWriter, r *http.Request, eventID int64, billing *string, pricing bool) bool {
	var (
		current, orgSlug string
		forSale          bool
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT e.billing, e.public_for_sale, COALESCE(o.slug, '')
		FROM events e JOIN organizations o ON o.id = e.org_id WHERE e.id = $1
	`, eventID).Scan(&current, &forSale, &orgSlug); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return false
	}
	if billing == nil {
		if pricing && current == "free" {
			httpx.Error(w, http.StatusConflict, "a free event has no price: set billing to teams first")
			return false
		}
		return true
	}
	if me, _ := auth.FromContext(r.Context()); me.Role != auth.RoleOrgAdmin || h.HomeOrg == "" || orgSlug != h.HomeOrg {
		httpx.Error(w, http.StatusForbidden, "only the site's own organisation's admins set an event's billing")
		return false
	}
	switch {
	case *billing != "free" && *billing != "teams":
		httpx.Error(w, http.StatusBadRequest, "billing must be free or teams")
		return false
	case *billing == current:
		return true
	case current == "prepaid":
		httpx.Error(w, http.StatusConflict, "a prepaid event's billing can't be changed")
		return false
	case !forSale:
		httpx.Error(w, http.StatusConflict, "only a self-guided event can be free")
		return false
	case pricing:
		httpx.Error(w, http.StatusConflict, "set the billing or the price, not both")
		return false
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE events SET billing = $1, ticket_currency = $3,
		       ticket_price_cents = CASE WHEN $1 = 'free' THEN 0 END
		WHERE id = $2
	`, *billing, eventID, settings.Currency); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return false
	}
	logx.Info("admin.event.billing", "event_id", eventID, "billing", *billing)
	return true
}

// giveStoreFreeTicket is the store's "buy" on a free event: no
// checkout, the team made at once and its link mailed, as a gift from
// the organisation. The buyer's browser goes straight to the team page.
// One ticket per email address and event, so the form can't be used to
// mail someone over and over; the route's per-IP limiter does the rest.
func (h *Handlers) giveStoreFreeTicket(w http.ResponseWriter, r *http.Request, eventID int64, email string, teamSize int) {
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "free ticket failed", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var had bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM teams WHERE event_id = $1 AND email = $2)`, eventID, email).Scan(&had); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "free ticket failed", err)
		return
	}
	if had {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this email address already has a ticket", "code": "already_have"})
		return
	}
	teamID, teamToken, voucher, err := createTicketTeam(ctx, tx, eventID, email, teamSize)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "free ticket failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "free ticket failed", err)
		return
	}
	logx.Info("admin.store.free_ticket", "event_id", eventID, "team_id", teamID)
	if err := h.sendTicketTeamLink(ctx, eventID, email, teamToken, voucher, "", nil, true); err != nil {
		logx.Warn("admin.store.free_ticket.mail", "event_id", eventID, "team_id", teamID, "err", err.Error())
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, BuyTicketResponse{CheckoutURL: "/team/" + teamToken})
}
