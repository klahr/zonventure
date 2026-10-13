package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// userLang is the language userID uses the console in, else the
// request's.
func (h *Handlers) userLang(r *http.Request, userID int64) string {
	lang := emailtpl.LangFromRequest(r)
	var own sql.NullString
	if h.DB.QueryRowContext(r.Context(), `SELECT language FROM users WHERE id = $1`, userID).Scan(&own) == nil && own.Valid {
		lang = emailtpl.Lang(own.String)
	}
	return lang
}

// sendVerification mails a new account's verify link, as signup does.
// Best-effort: the account exists, and the user can resend from the
// console's banner.
func (h *Handlers) sendVerification(r *http.Request, userID int64, to string) {
	if h.Mailer == nil {
		return
	}
	tok, err := auth.IssueVerificationToken(r.Context(), h.DB, userID)
	if err != nil {
		logx.Warn("admin.member.verify_token_failed", "user_id", userID, "err", err.Error())
		return
	}
	link := strings.TrimRight(h.PublicURL, "/") + "/verify?" + url.Values{"token": []string{tok}}.Encode()
	lang := h.userLang(r, userID)
	subject, c := emailtpl.VerifyEmail(lang, link)
	c.Picture = h.house(lang, emailtpl.PictureVerify)
	if err := h.Mailer.Send(r.Context(), emailtpl.Message([]string{to}, subject, emailtpl.Classic, emailtpl.Platform(), c)); err != nil {
		logx.Warn("admin.member.verify_mail_failed", "user_id", userID, "err", err.Error())
	}
}

// sendOrgInvitation tells userID that orgID invited them, when they
// have an address. The console's banner is the other way they learn.
// An account without a password yet (inviteNewOrgMember) gets a link
// to choose one first.
func (h *Handlers) sendOrgInvitation(r *http.Request, userID, orgID int64) {
	var (
		to, org, role string
		newAccount    bool
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(TRIM(u.email), ''), o.name, i.role, u.password_hash IS NULL FROM org_invitations i
		JOIN users u ON u.id = i.user_id JOIN organizations o ON o.id = i.org_id
		WHERE i.user_id = $1 AND i.org_id = $2
	`, userID, orgID).Scan(&to, &org, &role, &newAccount); err != nil || to == "" || h.Mailer == nil {
		return
	}
	link := strings.TrimRight(h.PublicURL, "/") + "/arrange"
	if newAccount {
		tok, err := auth.IssueInviteToken(r.Context(), h.DB, userID)
		if err != nil {
			logx.Warn("admin.org_invitation.token_failed", "user_id", userID, "err", err.Error())
			return
		}
		link += "#reset?" + url.Values{"token": []string{tok}}.Encode()
	}
	l := h.orgLook(r.Context(), orgID)
	l.Lang = h.userLang(r, userID)
	subject, c := emailtpl.OrgInvitation(l.Lang, org, role, link, newAccount)
	if err := h.send(r.Context(), "org_invitation", l.message([]string{to}, subject, c)); err != nil {
		logx.Warn("admin.org_invitation.mail_failed", "user_id", userID, "org_id", orgID, "err", err.Error())
	}
}

// inviteNewOrgMember creates an account with no password for a new
// address and invites it to orgID (CreateOrgMember): the email's link
// sets the password, then the person accepts as any invitee does.
func (h *Handlers) inviteNewOrgMember(w http.ResponseWriter, r *http.Request, orgID int64, req createOrgMemberRequest) {
	me, _ := auth.FromContext(r.Context())
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var id int64
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO users(username, email, email_verified, created_by_user_id, created_by_org_id)
		VALUES ($1, $2, 0, $3, $4)
		RETURNING id
	`, req.Username, req.Email, me.ID, orgID).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "username or email not available")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO org_invitations(user_id, org_id, role, invited_by_user_id, email) VALUES ($1, $2, $3, $4, $5)
	`, id, orgID, req.Role, me.ID, req.Email); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "invitation insert failed")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	h.sendOrgInvitation(r, id, orgID)
	httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}

// AcceptOrgInvitation makes the caller a member of the organisation
// that invited them, with the role the invitation names.
func (h *Handlers) AcceptOrgInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var role string
	err = tx.QueryRowContext(r.Context(),
		`DELETE FROM org_invitations WHERE user_id = $1 AND org_id = $2 AND declined_at IS NULL RETURNING role`, me.ID, orgID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such invitation")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "invitation lookup failed")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, org_id) DO NOTHING
	`, me.ID, orgID, role); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "membership insert failed")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	logx.Info("admin.org_invitation.accepted", "user_id", me.ID, "org_id", orgID, "role", role)
	httpx.JSON(w, http.StatusOK, map[string]any{"org_id": orgID, "role": role})
}

// DeclineOrgInvitation marks the caller's invitation from the
// organisation declined: its admins see that among their pending
// invitations, and may invite again. Nothing else changes.
func (h *Handlers) DeclineOrgInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE org_invitations SET declined_at = now() WHERE user_id = $1 AND org_id = $2 AND declined_at IS NULL`, me.ID, orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "decline failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "no such invitation")
		return
	}
	logx.Info("admin.org_invitation.declined", "user_id", me.ID, "org_id", orgID)
	w.WriteHeader(http.StatusNoContent)
}
