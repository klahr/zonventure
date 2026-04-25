package admin

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/mailer"
)

// Account-settings handlers. These all live on /api/admin/me/* and
// require an authenticated staff user; the role doesn't matter (admins
// and org users both manage their own account).

type changePasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// ChangeMyPassword requires the current password (so a stolen session
// can't pivot into a permanent takeover), runs the new password through
// the policy validator, hashes it, and kills every other session for
// the same user — the current session stays so the page the user is on
// keeps working.
func (h *Handlers) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var req changePasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := auth.VerifyUserPassword(r.Context(), h.DB, me.ID, req.Current); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "current password is wrong")
		return
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	// Drop every session except the one the caller is using right now.
	if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id = $1 AND token <> $2`, me.ID, c.Value); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "session cleanup failed")
			return
		}
	} else {
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id = $1`, me.ID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "session cleanup failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventSetPassword, OK: true, UserID: &me.ID, Username: me.Username,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonSuccess,
	})
	httpx.JSON(w, http.StatusNoContent, nil)
}

type changeEmailRequest struct {
	NewEmail string `json:"new_email"`
}

// ChangeMyEmail issues a change_email token and mails the verify link
// to the *new* address (proving the user controls it before we flip
// their login). The actual change happens when the link is clicked
// and ConfirmEmailChange consumes the token.
func (h *Handlers) ChangeMyEmail(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var req changeEmailRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	newEmail := strings.TrimSpace(req.NewEmail)
	if _, err := mail.ParseAddress(newEmail); err != nil {
		httpx.Error(w, http.StatusBadRequest, "new email is not a valid address")
		return
	}
	if strings.EqualFold(newEmail, me.Email) {
		httpx.Error(w, http.StatusConflict, "that's already your email")
		return
	}
	// Refuse early when the new address is already taken by another
	// account — saves the user the round-trip through email and gives
	// us a single clean error to render.
	var n int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE email = $1 AND id <> $2`, newEmail, me.ID).Scan(&n); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if n > 0 {
		httpx.Error(w, http.StatusConflict, "that email is already in use")
		return
	}
	tok, err := auth.IssueEmailChangeToken(r.Context(), h.DB, me.ID, newEmail)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	if h.Mailer != nil {
		link := buildChangeEmailLink(h.PublicURL, r, tok)
		err := h.Mailer.Send(r.Context(), mailer.Message{
			To:      []string{newEmail},
			Subject: "Confirm your new Zonventure email",
			Text: "You asked to change the email on your Zonventure account to this address.\r\n\r\n" +
				"Open this link to confirm:\r\n" +
				link + "\r\n\r\n" +
				"The link expires in 1 hour. If you didn't request this, you can " +
				"ignore the message — nothing changes until you click.\r\n",
		})
		if err != nil {
			logx.Warn("admin.me.email.mail_failed", "user_id", me.ID, "err", err.Error())
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "sent", "to": newEmail})
}

func buildChangeEmailLink(publicURL string, r *http.Request, token string) string {
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
	return base + "/arrange#change-email?" + q.Encode()
}

// DeleteMyAccount removes the calling user. When this leaves an
// organization with zero remaining users, the organization (and its
// runs / teams / adventures / purchases — all on cascade) is removed
// in the same transaction, matching the user's "delete my account"
// expectation. Refused for the last global admin so the system can't
// be left rudderless.
func (h *Handlers) DeleteMyAccount(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}

	if me.Role == auth.RoleAdmin {
		var others int
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND id <> $1`, me.ID).Scan(&others); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if others == 0 {
			httpx.Error(w, http.StatusConflict,
				"you're the last global admin — promote another admin before deleting yourself")
			return
		}
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM users WHERE id = $1`, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed: "+err.Error())
		return
	}
	// Auto-delete the org if this user was its last member. The FK
	// cascade rolls everything (runs, teams, assignments, adventures
	// scoped to the org) along with it.
	if me.OrgID != nil {
		var remaining int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM users WHERE org_id = $1`, *me.OrgID).Scan(&remaining); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "org member count failed")
			return
		}
		if remaining == 0 {
			if _, err := tx.ExecContext(r.Context(),
				`DELETE FROM organizations WHERE id = $1`, *me.OrgID); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "org cascade failed: "+err.Error())
				return
			}
			logx.Info("admin.me.delete.org_collapsed", "org_id", *me.OrgID)
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	logx.Info("admin.me.deleted", "user_id", me.ID, "username", me.Username)
	// Best-effort cookie clear — the session row is already gone
	// (cascaded with the user row), so the next request will get
	// "not logged in" anyway.
	auth.ClearCookie(w, auth.CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ConfirmEmailChange consumes a change_email token. Public route
// (no auth) because the token IS the auth, and the user might be on
// a different device than the one that initiated the change.
//
// Lives in admin/ because it's part of the account-settings flow that
// the rest of the file owns; the route is mounted outside the admin
// auth group in server.go.
func (h *Handlers) ConfirmEmailChange(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodPost && tok == "" {
		// JSON body fallback so the front-end can post the token in
		// the request body instead of the URL when convenient.
		var req struct {
			Token string `json:"token"`
		}
		_ = httpx.DecodeJSON(w, r, &req)
		tok = strings.TrimSpace(req.Token)
	}
	u, err := auth.ConsumeEmailChangeToken(r.Context(), h.DB, tok)
	switch {
	case errors.Is(err, auth.ErrVerificationUnknown):
		httpx.Error(w, http.StatusBadRequest, "change-email link is invalid")
		return
	case errors.Is(err, auth.ErrVerificationExpired):
		httpx.Error(w, http.StatusBadRequest, "change-email link has expired")
		return
	case err != nil:
		// Most likely failure path: another account took the email
		// between the request and the click. Surface as a conflict
		// so the front-end shows "that email is no longer available".
		logx.Warn("admin.me.email.confirm_failed", "err", err.Error())
		httpx.Error(w, http.StatusConflict, "could not change email")
		return
	}
	logx.Info("admin.me.email.changed", "user_id", u.ID, "new_email", u.Email)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "email": u.Email, "username": u.Username,
	})
}
