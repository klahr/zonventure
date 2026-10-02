package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/klahr/zonventure/internal/emailtpl"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
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
		`UPDATE users SET password_hash = $1, password_is_default = 0 WHERE id = $2`,
		hash, me.ID); err != nil {
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
	// A password change revokes every API/MCP token too — they were
	// minted under the old credential and must not survive it.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM mcp_tokens WHERE user_id = $1`, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token cleanup failed")
		return
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
		// In the language they use the console in, else their browser's.
		lang := emailtpl.LangFromRequest(r)
		var own sql.NullString
		if h.DB.QueryRowContext(r.Context(), `SELECT language FROM users WHERE id = $1`, me.ID).Scan(&own) == nil && own.Valid {
			lang = emailtpl.Lang(own.String)
		}
		subject, c := emailtpl.EmailChange(lang, link)
		err := h.Mailer.Send(r.Context(), emailtpl.Message([]string{newEmail}, subject, emailtpl.Classic, emailtpl.Zonventure(), c))
		if err != nil {
			logx.Warn("admin.me.email.mail_failed", "user_id", me.ID, "err", err.Error())
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "sent", "to": newEmail})
}

type changeUsernameRequest struct {
	Username string `json:"username"`
}

// usernameRe is the shape a self-chosen username must have. No "@",
// so it can never equal anyone's email — login accepts either, and a
// username that looked like another user's address would be ambiguous.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

// ChangeMyUsername sets the caller's username. Uniqueness is the
// CITEXT constraint's job, so a case-only change of your own name
// goes through and a clash with anyone else is a 409. Sessions are
// keyed by user id and auth.Lookup reads the row per request, so
// nothing needs re-issuing.
func (h *Handlers) ChangeMyUsername(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var req changeUsernameRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if !usernameRe.MatchString(username) {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{
			"error": "username must be 3–32 letters, digits, '.', '_' or '-'",
			"code":  "username_invalid",
		})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET username = $1 WHERE id = $2`, username, me.ID); err != nil {
		if isUniqueViolation(err) {
			httpx.JSON(w, http.StatusConflict, map[string]string{
				"error": "that username is already taken",
				"code":  "username_taken",
			})
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventSetUsername, OK: true, UserID: &me.ID, Username: username,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonSuccess,
	})
	logx.Info("admin.me.username.changed", "user_id", me.ID, "from", me.Username, "to", username)
	httpx.JSON(w, http.StatusOK, map[string]any{"id": me.ID, "username": username})
}

// buildChangeEmailLink builds the email-change confirmation URL
// using only the configured publicURL. Request headers are not
// consulted; see buildResetLink for rationale.
func buildChangeEmailLink(publicURL string, _ *http.Request, token string) string {
	base := strings.TrimRight(publicURL, "/")
	q := url.Values{"token": []string{token}}
	return base + "/arrange#change-email?" + q.Encode()
}

// AccountDeletionGracePeriod is how long after a user requests
// account deletion before the retention sweeper actually runs the
// cascade. Long enough to recover from a misclick, short enough to
// not leave abandoned data sitting around indefinitely.
const AccountDeletionGracePeriod = 7 * 24 * time.Hour

// DeleteMyAccount marks the calling user for deletion in
// AccountDeletionGracePeriod. The retention sweeper runs the actual
// cascade once the timestamp passes; until then the user can call
// /api/admin/me/cancel-deletion to clear it. Refused for the last
// global admin so the system can't be left rudderless.
//
// Returns 202 Accepted with the scheduled timestamp so the client UI
// can render a confirmation. Sessions are dropped immediately — the
// user can log back in to cancel, but a stolen cookie shouldn't be
// able to pivot during the grace window.
func (h *Handlers) DeleteMyAccount(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if !h.requirePasswordConfirm(w, r) {
		return
	}

	if me.Role == auth.RoleAdmin {
		var others int
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM users WHERE is_admin = TRUE AND id <> $1`, me.ID).Scan(&others); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if others == 0 {
			httpx.Error(w, http.StatusConflict,
				"you're the last global admin — promote another admin before deleting yourself")
			return
		}
	}

	scheduled := time.Now().Add(AccountDeletionGracePeriod)
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET scheduled_deletion_at = $1 WHERE id = $2`,
		scheduled, me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "schedule deletion failed", err)
		return
	}
	// Drop every session so a cookie that's already in the wild can't
	// keep using the account through the grace window. The user can
	// log back in to cancel — that's fine, login still works.
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE user_id = $1`, me.ID); err != nil {
		logx.Warn("admin.me.delete.session_clear_failed", "user_id", me.ID, "err", err.Error())
	}
	// Revoke API/MCP tokens too so a scheduled-for-deletion account
	// can't keep being reached through a long-lived bearer token during
	// the grace window.
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM mcp_tokens WHERE user_id = $1`, me.ID); err != nil {
		logx.Warn("admin.me.delete.token_clear_failed", "user_id", me.ID, "err", err.Error())
	}
	logx.Info("admin.me.delete.scheduled",
		"user_id", me.ID, "username", me.Username,
		"scheduled_for", scheduled.UTC().Format(time.RFC3339))
	auth.ClearCookie(w, auth.CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusAccepted, map[string]string{
		"scheduled_deletion_at": scheduled.UTC().Format(time.RFC3339),
	})
}

// CancelMyDeletion clears scheduled_deletion_at on the calling user.
// Idempotent — calling it on an account that wasn't scheduled is a
// 204, not an error, so the UI can show the affordance unconditionally
// without worrying about race conditions.
func (h *Handlers) CancelMyDeletion(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET scheduled_deletion_at = NULL WHERE id = $1`, me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "cancel failed", err)
		return
	}
	logx.Info("admin.me.delete.cancelled", "user_id", me.ID, "username", me.Username)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// sessionDTO mirrors a row in the sessions table for the settings UI.
// We never return the raw token — exposing it to JS would let any XSS
// pivot into a takeover. Instead, an opaque short ID derived from a
// hash of the token is the public handle, and revocation looks the
// matching token up server-side. Cheap because there's at most ~10
// active sessions per user in practice.
type sessionDTO struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	UserAgent  string `json:"user_agent,omitempty"`
	IP         string `json:"ip,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	Current    bool   `json:"current"`
}

func sessionShortID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// ListMySessions returns every active session for the calling user.
func (h *Handlers) ListMySessions(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	curTok := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		curTok = c.Value
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT token, created_at, expires_at,
		       COALESCE(user_agent,''), COALESCE(ip,''), COALESCE(last_used_at,'')
		FROM sessions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []sessionDTO{}
	for rows.Next() {
		var (
			tok string
			s   sessionDTO
		)
		if err := rows.Scan(&tok, &s.CreatedAt, &s.ExpiresAt, &s.UserAgent, &s.IP, &s.LastUsedAt); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		s.ID = sessionShortID(tok)
		s.Current = tok != "" && tok == curTok
		out = append(out, s)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// RevokeMySession deletes a single session by its opaque short ID.
// Always 204 on success; 404 if no session of the caller's matches the
// supplied id (so an attacker probing IDs gets nothing useful).
func (h *Handlers) RevokeMySession(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	target := chi.URLParam(r, "token")
	if target == "" {
		httpx.Error(w, http.StatusBadRequest, "bad id")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT token FROM sessions WHERE user_id = $1`, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	defer rows.Close()
	var match sql.NullString
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if sessionShortID(t) == target {
			match.String = t
			match.Valid = true
			break
		}
	}
	if !match.Valid {
		httpx.Error(w, http.StatusNotFound, "session not found")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE token = $1`, match.String); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RevokeMyOtherSessions deletes every session for the caller except
// the one carried by the current request. The "log out everywhere
// else" button.
func (h *Handlers) RevokeMyOtherSessions(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	curTok := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		curTok = c.Value
	}
	if curTok == "" {
		// Without a current cookie we'd nuke everything, including the
		// session that's making this request. Refuse.
		httpx.Error(w, http.StatusBadRequest, "no current session")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE user_id = $1 AND token <> $2`, me.ID, curTok); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// EmailRevertWindow is how long after a confirmed email change the
// user can still revert it from settings. Long enough to cover the
// "I typo'd the new address and didn't notice for a couple of days"
// case; short enough that a stolen-then-recovered account doesn't
// give the attacker an open undo window.
const emailRevertWindowDays = 7

// emailChangeRevertStatus tells the settings UI whether a recent
// email change is still revertable, what the previous address was,
// and when the window expires.
type emailChangeRevertStatus struct {
	Available     bool   `json:"available"`
	PreviousEmail string `json:"previous_email,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

// MyEmailChangeStatus returns the revertability of the most recent
// email change for the calling user.
func (h *Handlers) MyEmailChangeStatus(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	prev, setAt, ok := loadPreviousEmail(r.Context(), h.DB, me.ID)
	if !ok {
		httpx.JSON(w, http.StatusOK, emailChangeRevertStatus{Available: false})
		return
	}
	expiresAt := setAt.Add(time.Duration(emailRevertWindowDays) * 24 * time.Hour)
	if time.Now().After(expiresAt) {
		// Window expired — clear it lazily so the next read is fast.
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE users SET previous_email = NULL, previous_email_set_at = NULL WHERE id = $1`, me.ID)
		httpx.JSON(w, http.StatusOK, emailChangeRevertStatus{Available: false})
		return
	}
	httpx.JSON(w, http.StatusOK, emailChangeRevertStatus{
		Available:     true,
		PreviousEmail: prev,
		ExpiresAt:     expiresAt.UTC().Format(time.RFC3339),
	})
}

// RevertMyEmailChange flips the email back to the stored previous
// value if the grace window hasn't expired. Refuses if the previous
// email is now in use by another account (rare, but possible if
// someone else signed up with that address in the meantime).
func (h *Handlers) RevertMyEmailChange(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	prev, setAt, ok := loadPreviousEmail(r.Context(), h.DB, me.ID)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no recent email change to revert")
		return
	}
	expiresAt := setAt.Add(time.Duration(emailRevertWindowDays) * 24 * time.Hour)
	if time.Now().After(expiresAt) {
		httpx.Error(w, http.StatusGone, "revert window has closed")
		return
	}
	// Refuse if the previous address is no longer free — better to
	// surface a clear error than leave the user with a half-broken
	// state.
	var n int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE email = $1 AND id <> $2`, prev, me.ID).Scan(&n); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if n > 0 {
		httpx.Error(w, http.StatusConflict, "previous email is taken — contact support")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE users SET
		  email                 = previous_email,
		  username              = CASE WHEN username = email THEN previous_email ELSE username END,
		  previous_email        = NULL,
		  previous_email_set_at = NULL
		WHERE id = $1
	`, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "revert failed")
		return
	}
	logx.Info("admin.me.email.reverted", "user_id", me.ID, "to", prev)
	httpx.JSON(w, http.StatusOK, map[string]string{"email": prev})
}

func loadPreviousEmail(ctx context.Context, db *sql.DB, userID int64) (string, time.Time, bool) {
	var (
		prev  sql.NullString
		setAt sql.NullString
	)
	err := db.QueryRowContext(ctx,
		`SELECT previous_email, previous_email_set_at FROM users WHERE id = $1`, userID).Scan(&prev, &setAt)
	if err != nil || !prev.Valid || !setAt.Valid {
		return "", time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, setAt.String)
	if err != nil {
		return "", time.Time{}, false
	}
	return prev.String, t, true
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
