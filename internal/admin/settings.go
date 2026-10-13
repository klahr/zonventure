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
	// AdminTOTPRequired: every global admin and Support user must use
	// two-factor authentication.
	AdminTOTPRequired bool `json:"admin_totp_required"`
	// TOTPAvailable: the server has ZV_TOTP_KEY, so 2FA can be required.
	TOTPAvailable bool `json:"totp_available"`
}

// GetSettings returns the operator-tunable admin settings. Admin-only
// at the route level. Cheap enough to read fresh per request — single
// PK lookup against a tiny KV table — so no caching layer.
func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.buildSettingsResponse(r))
}

func (h *Handlers) buildSettingsResponse(r *http.Request) settingsResponse {
	return settingsResponse{
		SignupEnabled:     settings.SignupEnabled(r.Context(), h.DB),
		AdminTOTPRequired: settings.AdminTOTPRequired(r.Context(), h.DB),
		TOTPAvailable:     h.TOTPKey != nil,
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
	noteAction(r, onOff(req.Enabled))
	httpx.JSON(w, http.StatusOK, h.buildSettingsResponse(r))
}

// SetAdminTOTPRequired flips whether every global admin (full or
// Support) must use two-factor authentication. Full-admin only at the
// route level, which also writes it to the admin action log. Turning
// it on needs ZV_TOTP_KEY, else nobody could comply, and the admin's
// own 2FA; turning it off needs the admin's password.
func (h *Handlers) SetAdminTOTPRequired(w http.ResponseWriter, r *http.Request) {
	var req setBoolRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Enabled && h.TOTPKey == nil {
		httpx.Error(w, http.StatusConflict, "two-factor authentication is not available on this server")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if req.Enabled && refuseWithoutOwnTOTP(w, me) {
		return
	}
	if !req.Enabled && !h.requirePasswordConfirm(w, r) {
		return
	}
	if err := settings.SetAdminTOTPRequired(r.Context(), h.DB, req.Enabled); err != nil {
		logx.Error("admin.settings.admin_totp_set_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "could not save setting")
		return
	}
	logx.Info("admin.settings.admin_totp", "state", boolState(req.Enabled), "by", me.Username)
	noteAction(r, onOff(req.Enabled))
	httpx.JSON(w, http.StatusOK, h.buildSettingsResponse(r))
}

// refuseWithoutOwnTOTP answers 409 when me, who has no second factor,
// would require one: the switch would lock them into the setup at once,
// with no way back to it to undo.
func refuseWithoutOwnTOTP(w http.ResponseWriter, me auth.User) bool {
	if me.TOTPEnabled || me.ViaSSO {
		return false
	}
	httpx.JSON(w, http.StatusConflict, map[string]string{
		"error": "Set up two-factor authentication for yourself first.",
		"code":  "totp_self_first",
	})
	return true
}

func boolState(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}
