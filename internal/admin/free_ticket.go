package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

type freeTicketRequest struct {
	Email string `json:"email"`
}

type freeTicketResponse struct {
	TeamID      int64  `json:"team_id"`
	TeamURL     string `json:"team_url"`
	PlayURL     string `json:"play_url"`
	VoucherCode string `json:"voucher_code"`
	// Emailed is false when the team was made but the email could not be
	// queued; the link and code above still work and can be sent by hand.
	Emailed bool `json:"emailed"`
}

// GiveFreeTicket serves POST /api/admin/events/{eventID}/free-ticket: a
// ticket for someone who plays without paying — a tester, a guest. It
// makes the same team a store purchase or a gift box does (the event's
// ticket team size) and sends the same email, with the team link and
// the voucher code to type at /play. The reply carries both too, for
// handing over in person when the mail is slow or bounces.
func (h *Handlers) GiveFreeTicket(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	if !h.requireEventOp(w, r.Context(), eventID, OpCreateTeam) {
		return
	}
	var req freeTicketRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email, err := normaliseTeamEmail(req.Email)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if email == "" {
		httpx.Error(w, http.StatusBadRequest, "email required")
		return
	}
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		httpx.Error(w, http.StatusServiceUnavailable, "free tickets need ZV_PUBLIC_URL for their links")
		return
	}

	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "free ticket failed", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var teamSize int
	if err := tx.QueryRowContext(ctx, `SELECT ticket_team_size FROM events WHERE id = $1`, eventID).Scan(&teamSize); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "event not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "free ticket failed", err)
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

	me, _ := auth.FromContext(ctx)
	logx.Info("admin.free_ticket", "event_id", eventID, "team_id", teamID, "by", me.Username)
	resp := freeTicketResponse{
		TeamID:      teamID,
		TeamURL:     publicURL + "/team/" + teamToken,
		PlayURL:     publicURL + "/play?c=" + voucher,
		VoucherCode: auth.FormatVoucherCode(voucher),
		Emailed:     true,
	}
	if err := h.sendTicketTeamLink(ctx, eventID, email, teamToken, voucher); err != nil {
		logx.Warn("admin.free_ticket.mail", "event_id", eventID, "team_id", teamID, "err", err.Error())
		resp.Emailed = false
	}
	httpx.JSON(w, http.StatusCreated, resp)
}
