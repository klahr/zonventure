package auth

import (
	"database/sql"
	"errors"
	"github.com/klahr/zonventure/internal/emailtpl"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword starts the password-reset flow. Always 204, regardless
// of whether the email exists — that's the standard no-enumeration
// posture for forgot endpoints. The mail is sent best-effort; a send
// failure is logged but doesn't change the response.
//
// Per-IP rate-limited via the existing login limiter (every call is
// recorded as a "failure" for rate-limiting purposes), so a script
// can't spray addresses at line rate to map the customer base.
func (h *Handlers) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		// Surface the validation error inline so a typo gets a quick
		// "fix your address" message; nothing here leaks account
		// existence (no DB lookup happened).
		httpx.Error(w, http.StatusBadRequest, "email is not a valid address")
		return
	}

	ip := ClientIP(r)
	// We use a SEPARATE limiter from login (h.ForgotLimiter, falling
	// back to h.Limiter when not wired). Sharing one bucket made the
	// "I forgot my password and tried twice first" path catastrophic:
	// the per-IP cap on the shared bucket would then block forgot too,
	// stranding the user. The dedicated bucket still enforces a tight
	// per-(ip, email) throttle and a per-IP cap on its own — an
	// enumerator rotating addresses still hits a wall, just not one
	// shared with the login surface.
	limiter := h.ForgotLimiter
	if limiter == nil {
		limiter = h.Limiter
	}
	if ok, retry := limiter.Allow(ip, email); !ok {
		writeRateLimited(w, retry)
		return
	}
	// Feed the bucket regardless of whether the email exists — we don't
	// want a positive lookup to "reset" the throttle and let an attacker
	// keep probing.
	defer limiter.RecordFailure(ip, email)

	var userID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM users WHERE email = $1 AND password_hash IS NOT NULL`, email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		// Don't tell the caller the email is unknown; keep response
		// shape and timing as close to the success path as we can.
		logx.Info("auth.forgot.unknown_email", "email", email, "ip", ip)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		logx.Error("auth.forgot.lookup_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	tok, err := IssueForgotPasswordToken(r.Context(), h.DB, userID)
	if err != nil {
		logx.Error("auth.forgot.token_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	if err := h.sendResetEmail(r, email, tok); err != nil {
		// Don't surface the send failure — the audit log records what
		// happened; the user retries from the form.
		logx.Warn("auth.forgot.mail_failed", "user_id", userID, "err", err.Error())
	}
	logx.Info("auth.forgot.sent", "user_id", userID, "ip", ip)
	w.WriteHeader(http.StatusNoContent)
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ResetPassword consumes a one-shot reset token, sets the new password,
// kills any other live sessions for the user, and either drops a
// fresh session cookie (no TOTP) or returns a totp_required
// challenge token (TOTP enrolled) so the front-end transitions
// inline into the second-factor step.
//
// Either way, the password change has committed by the time this
// function returns — the user can never end up in a state where
// the new password is rejected and the old one still works.
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u, sessionTok, totpRequired, err := ConsumeForgotPasswordToken(r.Context(), h.DB, req.Token, req.NewPassword)
	if errors.Is(err, ErrVerificationUnknown) {
		httpx.Error(w, http.StatusBadRequest, "reset link is invalid")
		return
	}
	if errors.Is(err, ErrVerificationExpired) {
		httpx.Error(w, http.StatusBadRequest, "reset link has expired")
		return
	}
	if errors.Is(err, ErrPasswordTooShort) || errors.Is(err, ErrPasswordTooCommon) {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		logx.Error("auth.reset.failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "reset failed")
		return
	}
	logx.Info("auth.reset.completed", "user_id", u.ID)
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventSetPassword, OK: true, UserID: &u.ID, Username: u.Username,
		IP: ClientIP(r), UserAgent: UserAgent(r), Reason: ReasonSuccess,
	})
	if totpRequired {
		ip := ClientIP(r)
		ua := UserAgent(r)
		tok, err := IssueTOTPChallenge(r.Context(), h.DB, u.ID, "reset", ip, ua)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "challenge issue failed")
			return
		}
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, UserID: &u.ID, Username: u.Username, IP: ip, UserAgent: ua,
			Reason: ReasonTOTPRequired,
		})
		httpx.JSON(w, http.StatusOK, map[string]string{
			"code":      "totp_required",
			"challenge": tok,
		})
		return
	}
	StampSessionMeta(r.Context(), h.DB, sessionTok, UserAgent(r), ClientIP(r))
	SetCookie(w, sessionTok, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(r.Context(), u))
}

// sendResetEmail builds the reset link and ships the canned message.
// The link puts the token in a URL fragment so it never lands in
// HTTP Referer headers when the reset page links elsewhere.
func (h *Handlers) sendResetEmail(r *http.Request, to, token string) error {
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	link := buildResetLink(h.PublicURL, r, token)
	lang := emailtpl.LangFromRequest(r)
	subject, c := emailtpl.PasswordReset(lang, link)
	c.Picture = emailtpl.House(h.PublicURL, lang, emailtpl.PictureReset)
	return h.Mailer.Send(r.Context(), emailtpl.Message([]string{to}, subject, emailtpl.Classic, emailtpl.Platform(), c))
}

// buildResetLink returns the password-reset URL the email body
// embeds. Reads only `publicURL` (from ZV_PUBLIC_URL) — request
// headers like X-Forwarded-Host are intentionally NOT consulted,
// so an attacker behind any reverse proxy can't inject a phishing
// host into the reset email body. When publicURL is unset, returns
// an origin-relative URL; the caller's Send call should fail closed
// because a relative URL in an email body is useless to the
// recipient anyway, and that pushes the operator to set the
// env var rather than ship a broken email.
func buildResetLink(publicURL string, _ *http.Request, token string) string {
	base := strings.TrimRight(publicURL, "/")
	q := url.Values{"token": []string{token}}
	// Fragment-delivery: token after #, never sent to servers in
	// Referer headers when the reset page navigates elsewhere.
	return base + "/arrange#reset?" + q.Encode()
}
