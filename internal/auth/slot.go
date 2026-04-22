package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/httpx"
)

// Slot identifies a player's team slot. It's attached to the request
// context by SlotMiddleware and is the "who is this player" value that
// player endpoints use.
type Slot struct {
	ID       int64
	Name     string
	TeamID   int64
	RunID    int64
}

const (
	// SlotCookieName is the session cookie used for slot-scoped logins,
	// kept separate from the staff cookie so the two audiences can't share
	// credentials accidentally.
	SlotCookieName = "zv_slot"
	// SlotSessionTTL is how long a slot session remains valid.
	SlotSessionTTL = 30 * 24 * time.Hour
)

// ErrSlotTokenUnknown is returned when a magic-link token doesn't resolve
// to a slot.
var ErrSlotTokenUnknown = errors.New("unknown slot token")

// slotCtxKey is the context key used to stash the Slot.
type slotCtxKey struct{}

// LoginSlotByToken exchanges a magic-link (player_slots.token) for a
// new slot session, returning the session token the caller should set on
// the client as a cookie.
func LoginSlotByToken(ctx context.Context, db *sql.DB, magic string) (Slot, string, error) {
	if magic == "" {
		return Slot{}, "", ErrSlotTokenUnknown
	}
	var s Slot
	err := db.QueryRowContext(ctx, `
		SELECT ps.id, ps.name, ps.team_id, t.run_id
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE ps.token = ?
	`, magic).Scan(&s.ID, &s.Name, &s.TeamID, &s.RunID)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, "", ErrSlotTokenUnknown
	}
	if err != nil {
		return Slot{}, "", err
	}
	session, err := newSlotSession(ctx, db, s.ID)
	if err != nil {
		return Slot{}, "", err
	}
	_, _ = db.ExecContext(ctx, `UPDATE player_slots SET last_login_at = datetime('now') WHERE id = ?`, s.ID)
	return s, session, nil
}

// LookupSlot returns the slot backing a session token.
func LookupSlot(ctx context.Context, db *sql.DB, token string) (Slot, bool, error) {
	if token == "" {
		return Slot{}, false, nil
	}
	var (
		s       Slot
		expires string
	)
	err := db.QueryRowContext(ctx, `
		SELECT ps.id, ps.name, ps.team_id, t.run_id, ss.expires_at
		FROM slot_sessions ss
		JOIN player_slots ps ON ps.id = ss.slot_id
		JOIN teams t ON t.id = ps.team_id
		WHERE ss.token = ?
	`, token).Scan(&s.ID, &s.Name, &s.TeamID, &s.RunID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, false, nil
	}
	if err != nil {
		return Slot{}, false, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err == nil && time.Now().After(exp) {
		_, _ = db.ExecContext(ctx, `DELETE FROM slot_sessions WHERE token = ?`, token)
		return Slot{}, false, nil
	}
	return s, true, nil
}

// LogoutSlot invalidates a slot session.
func LogoutSlot(ctx context.Context, db *sql.DB, token string) error {
	if token == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DELETE FROM slot_sessions WHERE token = ?`, token)
	return err
}

// SlotMiddleware attaches the Slot identified by the SlotCookieName cookie
// (if any) to the request context. Endpoints that require a slot should
// compose this with RequireSlot.
func SlotMiddleware(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SlotCookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			s, ok, err := LookupSlot(r.Context(), db, c.Value)
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "slot lookup failed")
				return
			}
			if ok {
				r = r.WithContext(WithSlot(r.Context(), s))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSlot refuses requests that have no authenticated slot.
func RequireSlot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SlotFromContext(r.Context()); !ok {
			httpx.Error(w, http.StatusUnauthorized, "open your magic link to log in")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WithSlot returns a new context carrying s.
func WithSlot(ctx context.Context, s Slot) context.Context {
	return context.WithValue(ctx, slotCtxKey{}, s)
}

// SlotFromContext returns the slot previously stored via WithSlot.
func SlotFromContext(ctx context.Context) (Slot, bool) {
	s, ok := ctx.Value(slotCtxKey{}).(Slot)
	return s, ok
}

// SetSlotCookie writes the slot session cookie on the response.
func SetSlotCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SlotCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(SlotSessionTTL),
	})
}

// ClearSlotCookie expires the slot session cookie on the client.
func ClearSlotCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SlotCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// NewSlotToken returns a new, unguessable magic-link token for a slot.
// Exported so admin code can generate tokens when creating slots.
func NewSlotToken() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func newSlotSession(ctx context.Context, db *sql.DB, slotID int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(SlotSessionTTL).UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO slot_sessions(token, slot_id, expires_at) VALUES (?, ?, ?)
	`, token, slotID, expires); err != nil {
		return "", err
	}
	return token, nil
}
