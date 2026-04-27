package admin

import (
	"net/http"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/settings"
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
	httpx.JSON(w, http.StatusOK, settingsResponse{
		SignupEnabled: settings.SignupEnabled(r.Context(), h.DB),
	})
}

type setSignupRequest struct {
	Enabled bool `json:"enabled"`
}

// SetSignupEnabled flips the public signup gate. Audit-logged so an
// operator reviewing the auth_events table can see who turned signup
// off and when. Admin-only at the route level.
func (h *Handlers) SetSignupEnabled(w http.ResponseWriter, r *http.Request) {
	var req setSignupRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := settings.SetSignupEnabled(r.Context(), h.DB, req.Enabled); err != nil {
		logx.Error("admin.settings.signup_set_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "could not save setting")
		return
	}
	me, _ := auth.FromContext(r.Context())
	state := "disabled"
	if req.Enabled {
		state = "enabled"
	}
	logx.Info("admin.settings.signup", "state", state, "by", me.Username)
	httpx.JSON(w, http.StatusOK, settingsResponse{SignupEnabled: req.Enabled})
}
