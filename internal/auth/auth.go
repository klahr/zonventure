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
// handlers. OrgID is nil for admins (they are not scoped to any
// organization); every other role has exactly one.
type User struct {
	ID       int64
	Username string
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
}

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

// Login validates credentials and creates a new session row. Returns
// ErrPasswordNotSet when the user exists but never set a password — the
// caller should route that user through SetInitialPassword.
func Login(ctx context.Context, db *sql.DB, username, password string) (User, string, error) {
	var (
		u     User
		hash  sql.NullString
		orgID sql.NullInt64
	)
	err := db.QueryRowContext(ctx, `
		SELECT id, username, role, password_hash, org_id, COALESCE(email,''), (email_verified <> 0) FROM users WHERE username = $1
	`, username).Scan(&u.ID, &u.Username, &u.Role, &hash, &orgID, &u.Email, &u.EmailVerified)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", err
	}
	if orgID.Valid {
		v := orgID.Int64
		u.OrgID = &v
	}
	if !hash.Valid {
		return u, "", ErrPasswordNotSet
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(password)); err != nil {
		return User{}, "", ErrInvalidCredentials
	}
	token, err := newSession(ctx, db, u.ID, "", "")
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
		u     User
		hash  sql.NullString
		orgID sql.NullInt64
	)
	err := db.QueryRowContext(ctx, `
		SELECT id, username, role, password_hash, org_id, COALESCE(email,''), (email_verified <> 0) FROM users WHERE username = $1
	`, username).Scan(&u.ID, &u.Username, &u.Role, &hash, &orgID, &u.Email, &u.EmailVerified)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", err
	}
	if orgID.Valid {
		v := orgID.Int64
		u.OrgID = &v
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
	token, err := newSession(ctx, db, u.ID, "", "")
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
func newSession(ctx context.Context, w sessionWriter, userID int64, ua, ip string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	expires := time.Now().Add(SessionTTL).UTC().Format(time.RFC3339)
	if _, err := w.ExecContext(ctx, `
		INSERT INTO sessions(token, user_id, expires_at, user_agent, ip, last_used_at)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6)
	`, token, userID, expires, ua, ip, now); err != nil {
		return "", err
	}
	return token, nil
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
func Lookup(ctx context.Context, db *sql.DB, token string) (User, bool, error) {
	if token == "" {
		return User{}, false, nil
	}
	var (
		u       User
		orgID   sql.NullInt64
		expires string
	)
	var schedDel sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.org_id, COALESCE(u.email,''), (u.email_verified <> 0),
		       u.scheduled_deletion_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = $1
	`, token).Scan(&u.ID, &u.Username, &u.Role, &orgID, &u.Email, &u.EmailVerified, &schedDel, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	if orgID.Valid {
		v := orgID.Int64
		u.OrgID = &v
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
