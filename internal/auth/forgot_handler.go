package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	mailerpkg "git.sr.ht/~klahr/zv/internal/mailer"
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
	if ok, retry := h.Limiter.Allow(ip, email); !ok {
		writeRateLimited(w, retry)
		return
	}
	// Feed the per-IP bucket regardless of whether the email exists —
	// we don't want a positive lookup to "reset" the throttle and let
	// an attacker keep probing.
	defer h.Limiter.RecordFailure(ip, email)

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
// kills any other live sessions for the user, and drops a fresh session
// cookie so the user is signed in immediately. Returns whoami on
// success.
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u, sessionTok, err := ConsumeForgotPasswordToken(r.Context(), h.DB, req.Token, req.NewPassword)
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
	SetCookie(w, sessionTok, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(u))
}

// sendResetEmail builds the reset link and ships the canned message.
// The link puts the token in a URL fragment so it never lands in
// HTTP Referer headers when the reset page links elsewhere.
func (h *Handlers) sendResetEmail(r *http.Request, to, token string) error {
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	link := buildResetLink(h.PublicURL, r, token)
	subject := "Reset your Zonventure password"
	text := "Someone (hopefully you) asked to reset the password for this Zonventure account.\r\n\r\n" +
		"Open this link to choose a new password:\r\n" +
		link + "\r\n\r\n" +
		"The link expires in 1 hour. If you didn't request this, you can " +
		"ignore the message — your existing password remains valid.\r\n"
	return h.Mailer.Send(r.Context(), mailerpkg.Message{
		To:      []string{to},
		Subject: subject,
		Text:    text,
	})
}

func buildResetLink(publicURL string, r *http.Request, token string) string {
	base := strings.TrimRight(publicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		host := r.Host
		if h := r.Header.Get("X-Forwarded-Host"); h != "" {
			host = h
		}
		base = scheme + "://" + host
	}
	q := url.Values{"token": []string{token}}
	// Fragment-delivery: token after #, never sent to servers in
	// Referer headers when the reset page navigates elsewhere.
	return base + "/arrange#reset?" + q.Encode()
}
