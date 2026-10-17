package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/metrics"
)

type loginTOTPRequest struct {
	Challenge    string `json:"challenge"`
	Code         string `json:"code,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}

// LoginTOTP completes a login that was suspended at the TOTP step.
// The body carries the challenge token returned from /api/auth/login
// plus either a TOTP code or a single-use recovery code. Success
// mints the session cookie and returns the standard whoami payload —
// from the front-end's perspective this is just the second leg of
// the login.
//
// Per-(ip, user) rate-limited via h.TOTPLimiter so a brute-force
// attempt against the 6-digit code space gets locked out after a
// handful of misses without affecting the password limiter.
func (h *Handlers) LoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req loginTOTPRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ip := ClientIP(r)
	ua := UserAgent(r)
	if req.Challenge == "" {
		httpx.Error(w, http.StatusBadRequest, "challenge required")
		return
	}
	if req.Code == "" && req.RecoveryCode == "" {
		httpx.Error(w, http.StatusBadRequest, "code or recovery_code required")
		return
	}
	// Lock out per user, not per challenge: "back to log in" plus the
	// password mints a fresh challenge, which must not reset the count.
	limitKey := req.Challenge
	uid, uidErr := TOTPChallengeUserID(r.Context(), h.DB, req.Challenge)
	if uidErr == nil {
		limitKey = "uid:" + strconv.FormatInt(uid, 10)
	}
	if ok, retry := h.TOTPLimiter.Allow(ip, limitKey); !ok {
		metrics.AuthSecondFactor.WithLabelValues("rate_limited").Inc()
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventTOTPVerify, OK: false, IP: ip, UserAgent: ua,
			Reason: ReasonRateLimited,
		})
		writeRateLimited(w, retry)
		return
	}
	if h.TOTPKey == nil {
		// TOTP not configured at the server level but a user has
		// somehow reached this endpoint. Fail closed — the operator
		// has misconfigured the deployment.
		httpx.Error(w, http.StatusServiceUnavailable, "totp is not configured on this server")
		return
	}
	res, err := ConsumeTOTPChallenge(r.Context(), h.DB, h.TOTPKey, req.Challenge, req.Code, req.RecoveryCode)
	switch {
	case errors.Is(err, ErrChallengeUnknown):
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventTOTPVerify, OK: false, IP: ip, UserAgent: ua,
			Reason: ReasonUnknownToken,
		})
		httpx.Error(w, http.StatusBadRequest, "challenge is invalid")
		return
	case errors.Is(err, ErrChallengeExpired):
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventTOTPVerify, OK: false, IP: ip, UserAgent: ua,
			Reason: ReasonTOTPChallengeExpired,
		})
		httpx.Error(w, http.StatusBadRequest, "challenge has expired — start over")
		return
	case errors.Is(err, ErrTOTPInvalid), errors.Is(err, ErrTOTPReused):
		_, retry := h.TOTPLimiter.RecordFailure(ip, limitKey)
		metrics.AuthSecondFactor.WithLabelValues("fail").Inc()
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventTOTPVerify, OK: false, IP: ip, UserAgent: ua,
			Reason: ReasonTOTPInvalid,
		})
		if retry > 0 {
			// The pair-bucket caller couldn't tell from the error path
			// whether RecordFailure has just locked them — the helper
			// also returns retry > 0 for pre-existing locks, but the
			// client sees a simple "wrong code" either way.
			_ = retry
		}
		// A code that was right but already redeemed: the next one works.
		if errors.Is(err, ErrTOTPReused) {
			httpx.Error(w, http.StatusUnauthorized, "That code was already used. Wait for the next code in your authenticator app.")
			return
		}
		httpx.Error(w, http.StatusUnauthorized, "Wrong code. Enter the current code from your authenticator app.")
		return
	case errors.Is(err, ErrRecoveryCodeInvalid):
		h.TOTPLimiter.RecordFailure(ip, limitKey)
		metrics.AuthSecondFactor.WithLabelValues("fail").Inc()
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventTOTPRecoveryCodeUse, OK: false, IP: ip, UserAgent: ua,
			Reason: ReasonRecoveryCodeInvalid,
		})
		httpx.Error(w, http.StatusUnauthorized, "Wrong recovery code, or one that was already used.")
		return
	case errors.Is(err, ErrTOTPNotEnrolled):
		// Race: the user disabled TOTP between login leg 1 and leg 2.
		// Surface as challenge-invalid rather than leaking the new state.
		httpx.Error(w, http.StatusBadRequest, "challenge is invalid")
		return
	case errors.Is(err, ErrTOTPSecretCorrupt):
		// Stored ciphertext can't be decrypted with the current key.
		// Almost always means ZV_TOTP_KEY was rotated/lost. Fail
		// closed — the operator's audit log will show the pattern.
		// res is empty on this path, so the user comes from the challenge.
		logx.Error("auth.totp.secret_corrupt", "user_id", uid,
			"hint", "the stored 2FA secret does not decrypt with the current ZV_TOTP_KEY; was the key changed or lost (e.g. after a restore)? Recovery codes still work")
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Two-factor sign-in is unavailable on this server. Use a recovery code or contact your admin.",
			"code":  "totp_unavailable",
		})
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "totp verify failed")
		return
	}
	h.TOTPLimiter.RecordSuccess(ip, limitKey)
	// Reset password limiter too — same successful login, just with
	// the second factor.
	h.Limiter.RecordSuccess(ip, res.User.Username)
	var active sql.NullInt64
	if res.User.OrgID != nil {
		active = sql.NullInt64{Int64: *res.User.OrgID, Valid: true}
	}
	first := FactorPassword
	if res.Origin == ChallengeOIDC {
		first = FactorIdPPrimary
	}
	factors := []Factor{first, res.Factor}
	token, err := CompleteLogin(r.Context(), h.DB, res.User, factors, ua, ip, active)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session create failed")
		return
	}
	metrics.AuthLogins.WithLabelValues("success").Inc()
	metrics.AuthSecondFactor.WithLabelValues("success").Inc()
	verifyEvent := EventTOTPVerify
	if res.Factor == FactorRecoveryCode {
		verifyEvent = EventTOTPRecoveryCodeUse
	}
	Record(r.Context(), h.DB, AuditEvent{
		Event: verifyEvent, OK: true, UserID: &res.User.ID, Username: res.User.Username,
		IP: ip, UserAgent: ua, Reason: ReasonSuccess,
	})
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventLogin, OK: true, UserID: &res.User.ID, Username: res.User.Username,
		IP: ip, UserAgent: ua, Reason: ReasonSuccess,
	})
	SetCookie(w, token, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(r.Context(), res.User))
}
