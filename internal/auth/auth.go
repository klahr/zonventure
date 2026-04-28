// Package auth implements password hashing and cookie-backed sessions.
//
// Sessions are opaque random tokens stored in the `sessions` table. The
// cookie is HttpOnly and SameSite=Lax so it works for same-site form posts
// while still being unreadable from JavaScript.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/tokenx"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName is the session cookie key.
	CookieName = "zv_session"

	// RolePlayer is the role for adventure participants.
	RolePlayer = "player"
	// RoleOrgMember is a user belonging to one organization — runs the
	// games: manages players, schedules events (consuming assignments),
	// manages teams, etc.
	RoleOrgMember = "org_member"
	// RoleOrgAdmin has every org_member power plus the right to invite and
	// remove members of their own organization.
	RoleOrgAdmin = "org_admin"
	// RoleAdmin is the global tier: creates and manages organizations, mints
	// assignments, and administers other admins. Belongs to no organization.
	RoleAdmin = "admin"
)

// SessionTTL is how long a new staff session remains valid. Default 30
// days; override with SetSessionTTL at startup (driven by a CLI flag in
// main). Kept as a package-level var rather than a const so the operator
// can tighten it without recompiling.
var SessionTTL = 30 * 24 * time.Hour

// SetSessionTTL sets the staff-session TTL. Idempotent. Values <=0 are
// ignored — refusing rather than disabling sessions silently is the
// less-foot-gunny default.
func SetSessionTTL(d time.Duration) {
	if d <= 0 {
		return
	}
	SessionTTL = d
}

// User is the minimal authenticated-user view used by middleware and
// handlers. OrgID is the *active* organisation for this request — not
// the user's primary or only org, but whichever one the session has
// pinned via sessions.active_org_id. Admins have nil OrgID (no org
// context). Multi-org arrangers have nil OrgID only when they
// haven't picked yet (the org-picker modal fires in that case);
// otherwise it carries whichever org they're currently acting as.
//
// Role is derived: 'admin' when IsAdmin is true, otherwise the
// org_memberships.role for the active org (empty when a multi-org
// arranger hasn't picked yet). Most handlers compare Role against
// the Role* constants; new code can also test IsAdmin directly to
// distinguish "global admin" from "org user with admin role in some
// org" without reaching for the membership table.
type User struct {
	ID       int64
	Username string
	IsAdmin  bool
	Role     string
	OrgID    *int64
	// EmailVerified is true when the user has either confirmed their
	// signup email OR was grandfathered (admin-seeded users without
	// email default to true). Money-touching actions (CreateEvent,
	// CreatePurchase) check this before letting work through.
	EmailVerified bool
	// Email is the address tied to this account, or "" for
	// grandfathered users. Surfaced via /whoami so the UI can show
	// the verified-or-not banner.
	Email string
	// ScheduledDeletionAt is non-nil when the user has requested
	// account deletion. The retention sweeper hard-deletes once this
	// timestamp passes; until then the user can call
	// /api/admin/me/cancel-deletion to clear it. Surfaced via
	// /whoami so the UI can show a banner with the date.
	ScheduledDeletionAt *time.Time
	// TOTPEnabled is true when the user has completed TOTP enrolment.
	// VerifyPassword populates this so the login handler can fork
	// into the second-factor challenge before any session is minted.
	// An unfinished enrolment leaves totp_secret_encrypted set with
	// totp_enabled=false, and the login flow ignores it (else
	// abandoning enrolment would lock the account).
	TOTPEnabled bool
}

// Factor names a single authentication factor that contributed to
// completing a login. CompleteLogin takes a slice of these so the
// caller (login handler today, future OIDC handler) can record the
// path that produced the session, and so policy decisions ("require
// TOTP regardless of backend") have something concrete to consult.
type Factor string

const (
	// FactorPassword is the local username+password challenge.
	FactorPassword Factor = "password"
	// FactorTOTP is the RFC 6238 second-factor code.
	FactorTOTP Factor = "totp"
	// FactorRecoveryCode is one of the single-use codes generated at
	// TOTP enrolment, redeemed in lieu of a TOTP code when the user
	// has lost their authenticator. Distinguished from FactorTOTP so
	// "logged in via recovery code" is visible in audit.
	FactorRecoveryCode Factor = "recovery_code"
	// FactorIdPPrimary is the primary credential when the user logged
	// in via an external IdP (OIDC / SAML / Google Sign-in). Reserved
	// for the future plugin work tracked in TODO #18 — no producer
	// today.
	FactorIdPPrimary Factor = "idp_primary"
	// FactorIdPMFA is set alongside FactorIdPPrimary when the IdP
	// asserted that an MFA step took place (OIDC `amr` includes
	// "mfa", or the SAML AuthnContext class indicates a multi-factor
	// authentication method). A future per-user policy can choose
	// to trust this in lieu of a local TOTP step.
	FactorIdPMFA Factor = "idp_mfa"
)

// ctxKey is a private type so handler contexts can't collide with other
// packages that use string keys.
type ctxKey struct{}

var userKey = ctxKey{}

// ErrInvalidCredentials is returned when login fails. Callers should return
// a generic 401 and never leak whether the username or password was wrong.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrPasswordNotSet is returned by Login when the user exists but has no
// password yet. Callers should steer the client into the set-password flow
// rather than treating this as bad credentials.
var ErrPasswordNotSet = errors.New("password not set")

// ErrPasswordAlreadySet is returned by SetInitialPassword when the user
// already has a password — they must use Login (or an arranger-driven
// reset) instead.
var ErrPasswordAlreadySet = errors.New("password already set")

// HashPassword returns a bcrypt hash suitable for storing in users.password_hash.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword checks the username+password credential and returns
// the resolved User without minting a session. The caller (the login
// handler) decides whether to immediately CompleteLogin or to fork
// into the TOTP challenge based on User.TOTPEnabled.
//
// Returns ErrPasswordNotSet when the user exists but never set a
// password — the caller should route that user through
// SetInitialPassword.
//
// User.OrgID and User.Role are populated the same way Login used to
// populate them: the only-membership pin for single-org users, NULL
// for multi-org and admins (the UI picker takes over).
func VerifyPassword(ctx context.Context, db *sql.DB, username, password string) (User, error) {
	var (
		u    User
		hash sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT id, username, is_admin, password_hash, COALESCE(email,''), (email_verified <> 0), totp_enabled
		FROM users WHERE username = $1
	`, username).Scan(&u.ID, &u.Username, &u.IsAdmin, &hash, &u.Email, &u.EmailVerified, &u.TOTPEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !hash.Valid {
		return u, ErrPasswordNotSet
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	active, role, err := resolveInitialActiveOrg(ctx, db, u.ID)
	if err != nil {
		return User{}, err
	}
	if active.Valid {
		v := active.Int64
		u.OrgID = &v
	}
	if u.IsAdmin {
		u.Role = RoleAdmin
	} else {
		u.Role = role
	}
	return u, nil
}

// CompleteLogin mints a session row for a user whose authentication
// has already succeeded, recording the asserted factors that
// produced it. The login handler calls this after VerifyPassword
// (and, when TOTP is enrolled, after the TOTP challenge is also
// consumed). Future OIDC handlers will reuse the same seam,
// passing FactorIdPPrimary (and FactorIdPMFA if the IdP asserted
// MFA) instead of FactorPassword.
//
// activeOrgID is the same NullInt64 newSession takes — pass the
// only-membership pin for single-org users, an invalid value for
// multi-org users (the UI picker fires on first load) and admins.
//
// factors is currently observational — it is recorded in audit log
// entries by the calling handler but not yet persisted on the
// session row. Reserved for the future per-user "require TOTP
// regardless of backend" policy.
func CompleteLogin(ctx context.Context, w sessionWriter, u User, factors []Factor, ua, ip string, activeOrgID sql.NullInt64) (string, error) {
	_ = factors // reserved for future use; see doc comment.
	return newSession(ctx, w, u.ID, ua, ip, activeOrgID)
}

// Login validates credentials and creates a new session row. Thin
// shim around VerifyPassword + CompleteLogin so existing callers
// (set-password flow, tests) keep compiling. The login handler now
// uses the two-step variant directly so it can interpose a TOTP
// challenge between verification and session minting.
func Login(ctx context.Context, db *sql.DB, username, password string) (User, string, error) {
	u, err := VerifyPassword(ctx, db, username, password)
	if err != nil {
		return u, "", err
	}
	var active sql.NullInt64
	if u.OrgID != nil {
		active = sql.NullInt64{Int64: *u.OrgID, Valid: true}
	}
	token, err := CompleteLogin(ctx, db, u, []Factor{FactorPassword}, "", "", active)
	if err != nil {
		return User{}, "", err
	}
	return u, token, nil
}

// SetInitialPassword assigns a first password to an account that was
// created without one and immediately creates a session, so the caller can
// issue the cookie in the same round-trip. Refuses if a password is already
// set — to change a password, an arranger must reset it first.
func SetInitialPassword(ctx context.Context, db *sql.DB, username, password string) (User, string, error) {
	var (
		u    User
		hash sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT id, username, is_admin, password_hash, COALESCE(email,''), (email_verified <> 0) FROM users WHERE username = $1
	`, username).Scan(&u.ID, &u.Username, &u.IsAdmin, &hash, &u.Email, &u.EmailVerified)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", err
	}
	if hash.Valid {
		return User{}, "", ErrPasswordAlreadySet
	}
	h, err := HashPassword(password)
	if err != nil {
		return User{}, "", err
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, h, u.ID); err != nil {
		return User{}, "", err
	}
	active, role, err := resolveInitialActiveOrg(ctx, db, u.ID)
	if err != nil {
		return User{}, "", err
	}
	if active.Valid {
		v := active.Int64
		u.OrgID = &v
	}
	if u.IsAdmin {
		u.Role = RoleAdmin
	} else {
		u.Role = role
	}
	token, err := newSession(ctx, db, u.ID, "", "", active)
	if err != nil {
		return User{}, "", err
	}
	return u, token, nil
}

// sessionWriter is the narrow interface newSession needs — both
// *sql.DB and *sql.Tx satisfy it, so Signup (which already runs inside
// a tx) can mint a session in the same transaction without
// duplicating the INSERT.
type sessionWriter interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// txAsExecutor adapts *sql.Tx to sessionWriter. *sql.Tx already has
// ExecContext, so this is just a typed alias for clarity at call
// sites.
type txAsExecutor struct{ tx *sql.Tx }

func (t txAsExecutor) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, q, args...)
}

// newSession mints a random session token, persists it against the
// user, and returns it. UA + IP are stored alongside the session so
// the settings UI can distinguish "Phone Safari from 192.0.2.5" from
// other devices; pass empty strings when the call site doesn't know
// (e.g. inside the signup tx — StampSessionMeta backfills after
// commit).
//
// activeOrgID seeds sessions.active_org_id. Pass a NullInt64 with
// Valid=false for admins (no org context) and for multi-org users
// who haven't picked yet — Lookup returns User.OrgID = nil, the UI
// catches that and shows the picker. For single-org users, pin it
// to the only membership so the user lands directly in their org.
func newSession(ctx context.Context, w sessionWriter, userID int64, ua, ip string, activeOrgID sql.NullInt64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	expires := time.Now().Add(SessionTTL).UTC().Format(time.RFC3339)
	if _, err := w.ExecContext(ctx, `
		INSERT INTO sessions(token, user_id, expires_at, user_agent, ip, last_used_at, active_org_id)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6, $7)
	`, token, userID, expires, ua, ip, now, activeOrgID); err != nil {
		return "", err
	}
	return token, nil
}

// resolveInitialActiveOrg picks the active_org_id to seed onto a
// freshly-minted session and the role to populate User.Role with.
// Single membership → (that org, that role). Multi-membership →
// (null, "") so the UI fires its picker. Admin or no memberships at
// all → also (null, ""); the caller decides whether IsAdmin maps to
// Role="admin" instead.
//
// One round-trip: COUNT decides whether to use the MIN values. With
// COUNT==1, MIN(org_id)/MIN(role) come from the only row; with any
// other count we discard them.
func resolveInitialActiveOrg(ctx context.Context, db *sql.DB, userID int64) (sql.NullInt64, string, error) {
	var (
		only sql.NullInt64
		role sql.NullString
		cnt  int
	)
	err := db.QueryRowContext(ctx, `
		SELECT MIN(org_id), MIN(role), COUNT(*) FROM org_memberships WHERE user_id = $1
	`, userID).Scan(&only, &role, &cnt)
	if err != nil {
		return sql.NullInt64{}, "", err
	}
	if cnt == 1 {
		return only, role.String, nil
	}
	return sql.NullInt64{}, "", nil
}

// StampSessionMeta best-effort writes UA + IP onto an existing
// session row. Used right after Login / SetInitialPassword / signup
// to backfill the request-context metadata that the lower-level
// session creator can't see. Errors are swallowed — these columns
// are surface state, not auth-critical.
func StampSessionMeta(ctx context.Context, db *sql.DB, token, ua, ip string) {
	if token == "" {
		return
	}
	_, _ = db.ExecContext(ctx, `
		UPDATE sessions
		   SET user_agent = NULLIF($1,''), ip = NULLIF($2,'')
		 WHERE token = $3
	`, ua, ip, token)
}

// Logout deletes the session row for the given token. A missing token is
// not treated as an error.
func Logout(ctx context.Context, db *sql.DB, token string) error {
	if token == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// VerifyUserPassword returns nil if the given plaintext password matches
// the stored hash for the user. Returns ErrInvalidCredentials on mismatch
// or if the account has no password set (cannot confirm without one).
func VerifyUserPassword(ctx context.Context, db *sql.DB, userID int64, password string) error {
	var hash sql.NullString
	err := db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) || !hash.Valid {
		return ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// Lookup returns the user backing a session token, or (User{}, false) if the
// token is unknown or expired.
//
// User.OrgID reflects the *session's* active_org_id, not any legacy
// user-level pin (the column was dropped in Phase 4). Role is
// derived: 'admin' when u.is_admin is set; else the active org's
// membership role; else "" (multi-org user who hasn't picked yet —
// the UI catches the empty Role + populated Memberships and shows
// the picker before any view paints).
func Lookup(ctx context.Context, db *sql.DB, token string) (User, bool, error) {
	if token == "" {
		return User{}, false, nil
	}
	var (
		u           User
		activeOrgID sql.NullInt64
		membRole    sql.NullString
		expires     string
	)
	var schedDel sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_admin, m.role, s.active_org_id,
		       COALESCE(u.email,''), (u.email_verified <> 0),
		       u.scheduled_deletion_at, u.totp_enabled, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		LEFT JOIN org_memberships m ON m.user_id = u.id AND m.org_id = s.active_org_id
		WHERE s.token = $1
	`, token).Scan(&u.ID, &u.Username, &u.IsAdmin, &membRole, &activeOrgID,
		&u.Email, &u.EmailVerified, &schedDel, &u.TOTPEnabled, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	if activeOrgID.Valid {
		v := activeOrgID.Int64
		u.OrgID = &v
	}
	if u.IsAdmin {
		u.Role = RoleAdmin
	} else if membRole.Valid {
		u.Role = membRole.String
	}
	if schedDel.Valid {
		t := schedDel.Time
		u.ScheduledDeletionAt = &t
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err == nil && time.Now().After(exp) {
		_, _ = db.ExecContext(ctx, `DELETE FROM sessions WHERE token = $1`, token)
		return User{}, false, nil
	}
	return u, true, nil
}

// SetCookie writes the session cookie on the response.
func SetCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(SessionTTL),
	})
}

// ClearCookie expires the session cookie on the client.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// WithUser returns a new context carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// FromContext returns the user previously stored via WithUser, or
// (User{}, false) when none is present.
func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	return u, ok
}

// newToken returns a 256-bit random session token. Centralised in
// tokenx; the wrapper keeps the call sites (newSession, newSlotSession)
// readable without leaking the byte count into auth's body.
func newToken() (string, error) { return tokenx.New(32) }

// Membership is one row in a user's org-membership list — the
// organisation's id + name + the user's role inside it. Joined
// against `organizations` so callers don't have to re-query for
// the display name.
type Membership struct {
	OrgID   int64
	OrgName string
	Role    string
}

// ListMemberships returns every (org, role) the user belongs to,
// ordered by org name. Empty slice for admins (admin is a global
// role with no membership rows). Used to populate the login-time
// org picker and the hamburger switcher.
func ListMemberships(ctx context.Context, db *sql.DB, userID int64) ([]Membership, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.org_id, o.name, m.role
		FROM org_memberships m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1
		ORDER BY LOWER(o.name)
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.OrgID, &m.OrgName, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SwitchActiveOrg pins the session's active_org_id, validating that
// the user is actually a member of the target org first. Returns
// ErrNotAMember if not. The token resolves to the calling session;
// admins should never call this (they have no org context to pin).
func SwitchActiveOrg(ctx context.Context, db *sql.DB, userID, orgID int64, sessionToken string) error {
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2
	`, userID, orgID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotAMember
	}
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE sessions SET active_org_id = $1 WHERE token = $2 AND user_id = $3
	`, orgID, sessionToken, userID)
	return err
}

// ErrNotAMember is returned by SwitchActiveOrg when the caller tries
// to pin a session to an org they don't belong to. Surfaced as a 403
// by the handler.
var ErrNotAMember = errors.New("not a member of that organization")
