// Package auth implements password hashing and cookie-backed sessions.
//
// Sessions are opaque random tokens stored in the `sessions` table. The
// cookie is HttpOnly and SameSite=Lax so it works for same-site form posts
// while still being unreadable from JavaScript.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName is the session cookie key.
	CookieName = "zv_session"
	// SessionTTL is how long a new session remains valid.
	SessionTTL = 30 * 24 * time.Hour

	// RolePlayer is the role for adventure participants.
	RolePlayer = "player"
	// RoleOrgMember is a user belonging to one organization — runs the
	// games: manages players, schedules runs (consuming assignments),
	// manages teams, etc.
	RoleOrgMember = "org_member"
	// RoleOrgAdmin has every org_member power plus the right to invite and
	// remove members of their own organization.
	RoleOrgAdmin = "org_admin"
	// RoleAdmin is the global tier: creates and manages organizations, mints
	// assignments, and administers other admins. Belongs to no organization.
	RoleAdmin = "admin"
)

// User is the minimal authenticated-user view used by middleware and
// handlers. OrgID is nil for admins (they are not scoped to any
// organization); every other role has exactly one.
type User struct {
	ID       int64
	Username string
	Role     string
	OrgID    *int64
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
		SELECT id, username, role, password_hash, org_id FROM users WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.Role, &hash, &orgID)
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
	token, err := newSession(ctx, db, u.ID)
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
		SELECT id, username, role, password_hash, org_id FROM users WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.Role, &hash, &orgID)
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
	if _, err := db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, h, u.ID); err != nil {
		return User{}, "", err
	}
	token, err := newSession(ctx, db, u.ID)
	if err != nil {
		return User{}, "", err
	}
	return u, token, nil
}

// newSession mints a random token, persists it against the user, and
// returns it.
func newSession(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(SessionTTL).UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sessions(token, user_id, expires_at) VALUES (?, ?, ?)
	`, token, userID, expires); err != nil {
		return "", err
	}
	return token, nil
}

// Logout deletes the session row for the given token. A missing token is
// not treated as an error.
func Logout(ctx context.Context, db *sql.DB, token string) error {
	if token == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// VerifyUserPassword returns nil if the given plaintext password matches
// the stored hash for the user. Returns ErrInvalidCredentials on mismatch
// or if the account has no password set (cannot confirm without one).
func VerifyUserPassword(ctx context.Context, db *sql.DB, userID int64, password string) error {
	var hash sql.NullString
	err := db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
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
	err := db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.org_id, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?
	`, token).Scan(&u.ID, &u.Username, &u.Role, &orgID, &expires)
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
	exp, err := time.Parse(time.RFC3339, expires)
	if err == nil && time.Now().After(exp) {
		_, _ = db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
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

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
