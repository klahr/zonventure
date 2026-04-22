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

	// RolePlayer is the default role for adventure participants.
	RolePlayer = "player"
	// RoleArranger is the role allowed to manage runs and teams.
	RoleArranger = "arranger"
	// RoleRoot is the superuser role: can manage arrangers and the
	// adventures they are allowed to arrange. Implicitly has every
	// arranger capability.
	RoleRoot = "root"
)

// User is the minimal authenticated-user view used by middleware and
// handlers.
type User struct {
	ID       int64
	Username string
	Role     string
}

// ctxKey is a private type so handler contexts can't collide with other
// packages that use string keys.
type ctxKey struct{}

var userKey = ctxKey{}

// ErrInvalidCredentials is returned when login fails. Callers should return
// a generic 401 and never leak whether the username or password was wrong.
var ErrInvalidCredentials = errors.New("invalid credentials")

// HashPassword returns a bcrypt hash suitable for storing in users.password_hash.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Login validates credentials and creates a new session row. It returns the
// opaque session token on success.
func Login(ctx context.Context, db *sql.DB, username, password string) (User, string, error) {
	var (
		u    User
		hash string
	)
	err := db.QueryRowContext(ctx, `
		SELECT id, username, role, password_hash FROM users WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.Role, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return User{}, "", ErrInvalidCredentials
	}
	token, err := newToken()
	if err != nil {
		return User{}, "", err
	}
	expires := time.Now().Add(SessionTTL).UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sessions(token, user_id, expires_at) VALUES (?, ?, ?)
	`, token, u.ID, expires); err != nil {
		return User{}, "", err
	}
	return u, token, nil
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

// Lookup returns the user backing a session token, or (User{}, false) if the
// token is unknown or expired.
func Lookup(ctx context.Context, db *sql.DB, token string) (User, bool, error) {
	if token == "" {
		return User{}, false, nil
	}
	var (
		u       User
		expires string
	)
	err := db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?
	`, token).Scan(&u.ID, &u.Username, &u.Role, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
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
