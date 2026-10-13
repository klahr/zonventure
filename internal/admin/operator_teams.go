package admin

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
)

// operatorTeamDTO is one team as the platform operator sees it on an
// organisation's page: enough to mail it its links again.
type operatorTeamDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Players int    `json:"players"`
}

// ListOrgEventTeams lists one of an org's events' teams for the
// operator (full admin or Support).
//
// GET /api/admin/organizations/{orgID}/events/{eventID}/teams
func (h *Handlers) ListOrgEventTeams(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	eventOrgID, err := h.eventOrg(r.Context(), eventID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && eventOrgID != orgID) {
		httpx.Error(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, COALESCE(t.email, ''),
		       (SELECT COUNT(*) FROM player_slots ps WHERE ps.team_id = t.id)
		FROM teams t WHERE t.event_id = $1 ORDER BY t.id
	`, eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []operatorTeamDTO{}
	for rows.Next() {
		var t operatorTeamDTO
		if err := rows.Scan(&t.ID, &t.Name, &t.Email, &t.Players); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, t)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"teams": out})
}

// OperatorSendTeamInvitation mails one team its links again, as
// SendTeamInvitation does for the org's own users: the support action
// for a team that lost its invitation. Full admins and Support.
//
// POST /api/admin/organizations/{orgID}/teams/{teamID}/invitations/send
func (h *Handlers) OperatorSendTeamInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	h.sendTeamInvitation(w, r, teamID, orgID)
}
