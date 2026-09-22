package admin

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"

	"github.com/pquerna/otp"
)

// TOTP-enrolment endpoints. These all sit under /api/admin/me/totp/*
// and follow the same pattern as ChangeMyPassword in me.go: the
// caller must have a session, and any state-mutating action requires
// password reauth via the X-Confirm-Password header (the helper at
// internal/admin/users.go:requirePasswordConfirm).

type totpStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	EnrolledAt  string `json:"enrolled_at,omitempty"`
	ServerReady bool   `json:"server_ready"`
}

// TOTPStatus reports whether the calling user has TOTP enabled, when
// they enrolled, and whether the server is configured to accept new
// enrolments at all (true when ZV_TOTP_KEY is set). The settings UI
// uses ServerReady to decide whether to show the "Set up 2FA"
// button or a "TOTP isn't configured on this server — ask your
// operator to set ZV_TOTP_KEY" placeholder.
func (h *Handlers) TOTPStatus(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	st, err := auth.LoadTOTPStatus(r.Context(), h.DB, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	httpx.JSON(w, http.StatusOK, totpStatusResponse{
		Enabled:     st.Enabled,
		EnrolledAt:  st.EnrolledAt,
		ServerReady: h.TOTPKey != nil,
	})
}

type totpSetupResponse struct {
	OTPAuthURL   string `json:"otpauth_url"`
	SecretBase32 string `json:"secret_base32"`
	// QRDataURL is a base64 data: URL the front-end can drop into an
	// <img src> tag without depending on a JS QR encoder. Rendered
	// from the same otpauth:// URL via pquerna/otp's image helper —
	// 240×240 PNG, well below the 1 MB request-line cap.
	QRDataURL string `json:"qr_data_url"`
}

// SetupTOTP starts an enrolment: generates a fresh secret, stores it
// pending (totp_enabled=false), and returns the otpauth:// URL the
// front-end renders as a QR code plus the manual secret. Idempotent —
// re-calling before confirm replaces the pending secret.
//
// No password reauth on this leg: the secret is useless until
// ConfirmTOTP commits it. Disable / regenerate-codes do require
// reauth, since they affect a live enrolment.
func (h *Handlers) SetupTOTP(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if h.TOTPKey == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "TOTP is not configured on this server")
		return
	}
	setup, err := auth.EnrolStart(r.Context(), h.DB, h.TOTPKey, me.ID, me.Username)
	if errors.Is(err, auth.ErrTOTPAlreadyEnrolled) {
		httpx.Error(w, http.StatusConflict, "TOTP is already enabled — disable it first to re-enrol")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "setup failed")
		return
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventTOTPSetup, OK: true, UserID: &me.ID, Username: me.Username,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonSuccess,
	})
	qr, err := renderTOTPQR(setup.OTPAuthURL)
	if err != nil {
		// QR is a convenience — fall back to URL-only if image
		// generation fails, rather than blowing up enrolment.
		qr = ""
	}
	httpx.JSON(w, http.StatusOK, totpSetupResponse{
		OTPAuthURL:   setup.OTPAuthURL,
		SecretBase32: setup.SecretBase32,
		QRDataURL:    qr,
	})
}

// renderTOTPQR returns a data:image/png;base64,... URL for the
// supplied otpauth:// URL. 240px is large enough to scan from a
// laptop screen with a phone camera and small enough to keep the
// JSON response under ~10 KB.
func renderTOTPQR(otpauthURL string) (string, error) {
	k, err := otp.NewKeyFromURL(otpauthURL)
	if err != nil {
		return "", err
	}
	img, err := k.Image(240, 240)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

type totpConfirmRequest struct {
	Code string `json:"code"`
}

type totpConfirmResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// ConfirmTOTP verifies the first code against the pending secret,
// flips totp_enabled=true, and returns the freshly minted recovery
// codes (one-time display).
func (h *Handlers) ConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if h.TOTPKey == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "TOTP is not configured on this server")
		return
	}
	var req totpConfirmRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	codes, err := auth.EnrolConfirm(r.Context(), h.DB, h.TOTPKey, me.ID, req.Code)
	switch {
	case errors.Is(err, auth.ErrTOTPNotEnrolled):
		httpx.Error(w, http.StatusBadRequest, "no pending enrolment — call /me/totp/setup first")
		return
	case errors.Is(err, auth.ErrTOTPAlreadyEnrolled):
		httpx.Error(w, http.StatusConflict, "TOTP is already enabled")
		return
	case errors.Is(err, auth.ErrTOTPInvalid):
		auth.Record(r.Context(), h.DB, auth.AuditEvent{
			Event: auth.EventTOTPConfirm, OK: false, UserID: &me.ID, Username: me.Username,
			IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonTOTPInvalid,
		})
		httpx.Error(w, http.StatusUnauthorized, "invalid code")
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "confirm failed")
		return
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventTOTPConfirm, OK: true, UserID: &me.ID, Username: me.Username,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonSuccess,
	})
	httpx.JSON(w, http.StatusOK, totpConfirmResponse{RecoveryCodes: codes})
}

// DisableTOTP clears the user's TOTP enrolment + recovery codes.
// Requires X-Confirm-Password reauth (so a stolen session can't pivot
// into removing the second factor).
//
// Disabling TOTP is a security-state DOWNGRADE: any other live
// staff session for this user was previously protected by 2FA, and
// the user almost certainly wants those forced to re-auth at the
// new (weaker) factor level. Same shape as ChangeMyPassword —
// preserve the current request's cookie so the actor doesn't
// accidentally log themselves out, drop everything else.
func (h *Handlers) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	if err := auth.DisableTOTP(r.Context(), h.DB, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "disable failed")
		return
	}
	// Drop every other session for this user. Best-effort: if the
	// current cookie is missing for some reason, bail out and
	// drop EVERY session including the caller's — better to log
	// them out and force a fresh login than leave a stale-2FA
	// session anywhere.
	if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
		_, _ = h.DB.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id = $1 AND token <> $2`, me.ID, c.Value)
	} else {
		_, _ = h.DB.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id = $1`, me.ID)
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventTOTPDisable, OK: true, UserID: &me.ID, Username: me.Username,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonSuccess,
	})
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RegenerateTOTPRecoveryCodes drops every existing recovery code and
// returns a fresh set. Requires reauth — same threat model as
// DisableTOTP, since regenerating invalidates the codes the user
// previously printed.
func (h *Handlers) RegenerateTOTPRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	codes, err := auth.RegenerateRecoveryCodes(r.Context(), h.DB, me.ID)
	if errors.Is(err, auth.ErrTOTPNotEnrolled) {
		httpx.Error(w, http.StatusBadRequest, "TOTP is not enabled")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "regenerate failed")
		return
	}
	httpx.JSON(w, http.StatusOK, totpConfirmResponse{RecoveryCodes: codes})
}
