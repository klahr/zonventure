package auth

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	mailerpkg "git.sr.ht/~klahr/zv/internal/mailer"
)

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgName  string `json:"org_name"`
	// TermsAccepted is the consent gate — the signup form must check
	// the Terms / Privacy box before submit. Server-side enforcement
	// keeps a scripted POST honest and gives us a defensible "the user
	// agreed" answer if a dispute lands later.
	TermsAccepted bool `json:"terms_accepted"`
}

// Signup creates a new organization + org_admin user, sends a
// verification email, and drops a session cookie so the new arranger
// is logged in immediately. Money-touching actions (CreateEvent,
// CreatePurchase) stay gated until they click the link in the email.
//
// Rate-limit-friendly: failed signups (bad input, taken email) feed
// the per-IP bucket of h.Limiter so a script can't enumerate emails
// or spray the org table at line rate.
func (h *Handlers) Signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.OrgName = strings.TrimSpace(req.OrgName)
	if !req.TermsAccepted {
		httpx.Error(w, http.StatusBadRequest, "you must accept the Terms of Service and Privacy Policy to sign up")
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		httpx.Error(w, http.StatusBadRequest, "email is not a valid address")
		return
	}
	if req.OrgName == "" {
		httpx.Error(w, http.StatusBadRequest, "organization name required")
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	ip := ClientIP(r)
	if ok, retry := h.Limiter.Allow(ip, req.Email); !ok {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: req.Email, IP: ip,
			UserAgent: UserAgent(r), Reason: ReasonRateLimited,
		})
		writeRateLimited(w, retry)
		return
	}

	res, err := Signup(r.Context(), h.DB, req.Email, req.Password, req.OrgName)
	if err != nil {
		// Treat duplicate-email as a soft failure: feed the limiter
		// (so a scripted dictionary attack is throttled) but never
		// confirm or deny existence to the caller.
		_, _ = h.Limiter.RecordFailure(ip, req.Email)
		switch {
		case errors.Is(err, ErrSignupOrgNameTaken):
			httpx.Error(w, http.StatusConflict, "organization name already in use")
		case errors.Is(err, ErrSignupEmailTaken):
			// Generic message — don't enumerate accounts.
			httpx.Error(w, http.StatusConflict, "could not create account")
		case errors.Is(err, ErrPasswordTooShort), errors.Is(err, ErrPasswordTooCommon):
			httpx.Error(w, http.StatusBadRequest, err.Error())
		default:
			logx.Error("auth.signup.failed", "err", err.Error())
			httpx.Error(w, http.StatusInternalServerError, "signup failed")
		}
		return
	}

	// Audit the success and the verification-email send so an admin
	// reviewing the audit tab can see the same row twice (one
	// "login/success", one "verify-mail-sent" implicit via no row).
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventLogin, OK: true, UserID: &res.User.ID,
		Username: res.User.Username, IP: ip, UserAgent: UserAgent(r),
		Reason: ReasonSuccess,
	})

	if err := h.sendVerificationEmail(r, res.User.Email, res.VerificationToken); err != nil {
		// Don't fail the signup over a send error: the account is
		// created, the user can request a fresh link from the UI.
		// Surface to the operator via warn.
		logx.Warn("auth.signup.mail_failed", "user_id", res.User.ID, "err", err.Error())
	}

	StampSessionMeta(r.Context(), h.DB, res.SessionToken, UserAgent(r), ip)
	SetCookie(w, res.SessionToken, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusCreated, h.userResponse(res.User))
}

// VerifyEmail consumes a one-shot verification token. GET-style so the
// link in the email works on a click; redirects to /arrange with a
// hash that the front-end picks up to flash a toast.
func (h *Handlers) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	u, err := VerifyEmail(r.Context(), h.DB, tok)
	if err != nil {
		// Both error paths land on the same dest with a query flag —
		// the UI shows "this link expired, ask for a new one" and a
		// resend button.
		http.Redirect(w, r, "/arrange#verify-failed", http.StatusFound)
		return
	}
	logx.Info("auth.email_verified", "user_id", u.ID, "username", u.Username)
	http.Redirect(w, r, "/arrange#verify-ok", http.StatusFound)
}

// ResendVerification mints a fresh token and re-sends the email. The
// caller must already be logged in (so we know who to send to);
// rate-limited per IP so a logged-in attacker can't hammer the mail
// provider.
func (h *Handlers) ResendVerification(w http.ResponseWriter, r *http.Request) {
	me, ok := FromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "login required")
		return
	}
	if me.EmailVerified {
		httpx.Error(w, http.StatusConflict, "already verified")
		return
	}
	if me.Email == "" {
		httpx.Error(w, http.StatusBadRequest, "no email on file")
		return
	}
	ip := ClientIP(r)
	if ok, retry := h.Limiter.Allow(ip, me.Username); !ok {
		writeRateLimited(w, retry)
		return
	}
	tok, err := IssueVerificationToken(r.Context(), h.DB, me.ID)
	if err != nil {
		logx.Error("auth.resend.token_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	if err := h.sendVerificationEmail(r, me.Email, tok); err != nil {
		// We did issue a token, so don't 500 — the user can try again
		// or copy from the operator log. Surface as a warn.
		logx.Warn("auth.resend.mail_failed", "user_id", me.ID, "err", err.Error())
		httpx.Error(w, http.StatusBadGateway, "verification email failed to send")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// sendVerificationEmail builds the verify-link URL and ships the
// canned message via h.Mailer. URL host comes from h.PublicURL (set
// at server boot from the -public-url flag / $ZV_PUBLIC_URL env).
// When PublicURL is empty we fall back to the request's Host so a
// dev clone still produces a clickable link.
func (h *Handlers) sendVerificationEmail(r *http.Request, to, token string) error {
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	link := buildVerifyLink(h.PublicURL, r, token)
	subject := "Verify your Zonventure email"
	text := "Welcome to Zonventure!\r\n\r\n" +
		"Confirm your email by opening this link:\r\n" +
		link + "\r\n\r\n" +
		"The link expires in 1 hour. If you didn't sign up, you can " +
		"ignore this message.\r\n"
	return h.Mailer.Send(r.Context(), mailerpkg.Message{
		To:      []string{to},
		Subject: subject,
		Text:    text,
	})
}

func buildVerifyLink(publicURL string, r *http.Request, token string) string {
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
	return base + "/verify?" + q.Encode()
}
