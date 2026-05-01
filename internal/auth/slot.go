package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/tokenx"
)

// Slot identifies a player's team slot. It's attached to the request
// context by SlotMiddleware and is the "who is this player" value that
// player endpoints use.
type Slot struct {
	ID       int64
	Name     string
	TeamID   int64
	EventID    int64
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

// ErrSlotTokenExpired is returned when the magic-link token resolved
// but its player_slots.expires_at is in the past. Callers translate
// this into a generic "open your link again" UX and let the arranger
// rotate.
var ErrSlotTokenExpired = errors.New("slot token expired")

// slotCtxKey is the context key used to stash the Slot.
type slotCtxKey struct{}

// LoginSlotByToken exchanges a magic-link (player_slots.token) for a
// new slot session, returning the session token the caller should set on
// the client as a cookie. Returns ErrSlotTokenExpired when the slot's
// expires_at is in the past or claimed_at is non-null — the row stays
// in place so the arranger can rotate, but no new sessions are issued
// from a dead or already-claimed token.
//
// Single-use: claimed_at is NULL on a fresh slot and gets stamped on
// the first successful exchange. Subsequent exchanges of the same
// token return ErrSlotTokenExpired so a leaked link can't mint
// parallel sessions on multiple devices. Players who legitimately
// need a new device after the token's been claimed go through the
// lost-link recovery flow to mint a fresh token.
func LoginSlotByToken(ctx context.Context, db *sql.DB, magic string) (Slot, string, error) {
	if magic == "" {
		return Slot{}, "", ErrSlotTokenUnknown
	}
	var (
		s         Slot
		expiresAt sql.NullString
		claimedAt sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT ps.id, ps.name, ps.team_id, t.event_id, ps.expires_at, ps.claimed_at
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE ps.token = $1
	`, magic).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID, &expiresAt, &claimedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, "", ErrSlotTokenUnknown
	}
	if err != nil {
		return Slot{}, "", err
	}
	if expiresAt.Valid && expiresAt.String != "" {
		if t, perr := parseSlotTimestamp(expiresAt.String); perr == nil && time.Now().After(t) {
			return Slot{}, "", ErrSlotTokenExpired
		}
	}
	if claimedAt.Valid && claimedAt.String != "" {
		// Token already exchanged once — refuse the second claim.
		// The original session's cookie still works; this gate is
		// only on minting fresh sessions from the same token.
		return Slot{}, "", ErrSlotTokenExpired
	}
	session, err := newSlotSession(ctx, db, s.ID)
	if err != nil {
		return Slot{}, "", err
	}
	// Stamp claimed_at + last_login_at atomically. The claim is what
	// makes the token single-use; last_login_at is informational so
	// the arranger can see when the player actually came online.
	_, _ = db.ExecContext(ctx,
		`UPDATE player_slots SET last_login_at = now_text(), claimed_at = now_text() WHERE id = $1`, s.ID)
	return s, session, nil
}

// parseSlotTimestamp accepts both the wire format we write
// ("YYYY-MM-DD HH:MM:SS" UTC) and RFC3339 since scheduled_at-based
// expiries inherit whichever shape the front-end sent us. Returns
// the parsed time in UTC.
func parseSlotTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// DefaultSlotExpiresAt computes the default expires_at value for a
// newly-created slot. Uses the event's scheduled_at + 7 days when the event
// is scheduled, else now + 30 days. Returned as the wire-format string
// admin queries pass directly to the INSERT.
func DefaultSlotExpiresAt(ctx context.Context, db *sql.DB, eventID int64) string {
	const layout = "2006-01-02 15:04:05"
	var sched sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT scheduled_at FROM events WHERE id = $1`, eventID).Scan(&sched); err == nil {
		if sched.Valid && sched.String != "" {
			if t, err := parseSlotTimestamp(sched.String); err == nil {
				return t.Add(7 * 24 * time.Hour).UTC().Format(layout)
			}
		}
	}
	return time.Now().UTC().Add(30 * 24 * time.Hour).Format(layout)
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
		SELECT ps.id, ps.name, ps.team_id, t.event_id, ss.expires_at
		FROM slot_sessions ss
		JOIN player_slots ps ON ps.id = ss.slot_id
		JOIN teams t ON t.id = ps.team_id
		WHERE ss.token = $1
	`, token).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, false, nil
	}
	if err != nil {
		return Slot{}, false, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err == nil && time.Now().After(exp) {
		_, _ = db.ExecContext(ctx, `DELETE FROM slot_sessions WHERE token = $1`, token)
		return Slot{}, false, nil
	}
	return s, true, nil
}

// LogoutSlot invalidates a slot session.
func LogoutSlot(ctx context.Context, db *sql.DB, token string) error {
	if token == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DELETE FROM slot_sessions WHERE token = $1`, token)
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
// 18 bytes = 144 bits — short enough to keep magic-link URLs human-
// scannable, long enough that brute-forcing one is hopeless.
func NewSlotToken() (string, error) { return tokenx.New(18) }

func newSlotSession(ctx context.Context, db *sql.DB, slotID int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(SlotSessionTTL).UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO slot_sessions(token, slot_id, expires_at) VALUES ($1, $2, $3)
	`, token, slotID, expires); err != nil {
		return "", err
	}
	return token, nil
}
