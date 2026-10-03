package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
)

// settingsResponse is the JSON envelope GET /api/admin/settings
// returns. Object-shaped (not a flat bool) so adding the next admin
// toggle later is a field add, not a response-shape break.
type settingsResponse struct {
	SignupEnabled bool `json:"signup_enabled"`
}

// GetSettings returns the operator-tunable admin settings. Admin-only
// at the route level. Cheap enough to read fresh per request — single
// PK lookup against a tiny KV table — so no caching layer.
func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.buildSettingsResponse(r))
}

func (h *Handlers) buildSettingsResponse(r *http.Request) settingsResponse {
	return settingsResponse{
		SignupEnabled: settings.SignupEnabled(r.Context(), h.DB),
	}
}

type setBoolRequest struct {
	Enabled bool `json:"enabled"`
}

// SetSignupEnabled flips the public signup gate. Audit-logged so an
// operator reviewing the logs can see who turned signup off and when.
// Admin-only at the route level.
func (h *Handlers) SetSignupEnabled(w http.ResponseWriter, r *http.Request) {
	var req setBoolRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := settings.SetSignupEnabled(r.Context(), h.DB, req.Enabled); err != nil {
		logx.Error("admin.settings.signup_set_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "could not save setting")
		return
	}
	me, _ := auth.FromContext(r.Context())
	logx.Info("admin.settings.signup", "state", boolState(req.Enabled), "by", me.Username)
	httpx.JSON(w, http.StatusOK, h.buildSettingsResponse(r))
}

func boolState(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}
