package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/tokenx"
)

// Slot identifies a player's seat on a team (one row in player_slots).
// It's attached to the request
// context by SlotMiddleware and is the "who is this player" value that
// player endpoints use.
type Slot struct {
	ID      int64
	Name    string
	TeamID  int64
	EventID int64
	// Ended says the slot's event has ended. Such a session only erases
	// or signs out; see RequireSlotOrEnded.
	Ended bool
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
// expires_at is in the past — the row stays in place so the arranger
// can rotate, but no new sessions are issued from a dead token.
//
// The token is deliberately reusable. Single-use was tried and reverted:
// link previewers (Slack, iMessage, WhatsApp, etc.) fetch the URL when
// a player shares the link to a teammate, consuming the token before
// the human ever taps it. The remaining defences are: 144-bit token
// entropy, expires_at, the per-IP rate limit on the exchange, and the
// arranger's ability to rotate via the lost-link flow.
//
// claimed_at is still stamped (on first claim) as forensic record of
// when the slot first activated, but is never read as a gate.
func LoginSlotByToken(ctx context.Context, db *sql.DB, magic string) (Slot, string, error) {
	if magic == "" {
		return Slot{}, "", ErrSlotTokenUnknown
	}
	var (
		s         Slot
		expiresAt sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT ps.id, ps.name, ps.team_id, t.event_id, ps.expires_at
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE ps.token = $1
	`, magic).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID, &expiresAt)
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

	// Stamp last_login_at every exchange; stamp claimed_at only the
	// first time so it preserves the original activation moment for
	// audit. COALESCE keeps the existing value once set.
	if _, err := db.ExecContext(ctx,
		`UPDATE player_slots
		    SET last_login_at = now_text(),
		        claimed_at    = COALESCE(claimed_at, now_text())
		  WHERE id = $1`, s.ID); err != nil {
		return Slot{}, "", err
	}
	session, err := newSlotSession(ctx, db, s.ID)
	if err != nil {
		return Slot{}, "", err
	}
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

// rowQuerier is the subset of *sql.DB and *sql.Tx the slot helpers
// need. Lets DefaultSlotExpiresAt run inside or outside a tx without
// the caller juggling two APIs.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DefaultSlotExpiresAt computes the default expires_at value for a
// newly-created slot. Uses the event's scheduled_at + 7 days when the event
// is scheduled, else now + 30 days. Returned as the wire-format string
// admin queries pass directly to the INSERT.
func DefaultSlotExpiresAt(ctx context.Context, db rowQuerier, eventID int64) string {
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
		SELECT ps.id, ps.name, ps.team_id, t.event_id, ss.expires_at, ss.ended
		FROM slot_sessions ss
		JOIN player_slots ps ON ps.id = ss.slot_id
		JOIN teams t ON t.id = ps.team_id
		WHERE ss.token = $1
	`, token).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID, &expires, &s.Ended)
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
			// An ended session is kept apart, so nothing but
			// RequireSlotOrEnded takes it for a signed-in player.
			switch {
			case ok && s.Ended:
				r = r.WithContext(context.WithValue(r.Context(), endedSlotCtxKey{}, s))
			case ok:
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

// RequireSlotOrEnded is RequireSlot that also takes a session whose
// event has ended, for what a player may still do then: erase their
// data and sign out.
func RequireSlotOrEnded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SlotFromContext(r.Context()); !ok {
			s, ended := r.Context().Value(endedSlotCtxKey{}).(Slot)
			if !ended {
				httpx.Error(w, http.StatusUnauthorized, "open your magic link to log in")
				return
			}
			r = r.WithContext(WithSlot(r.Context(), s))
		}
		next.ServeHTTP(w, r)
	})
}

// endedSlotCtxKey holds a session whose event has ended.
type endedSlotCtxKey struct{}

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

// ErrTeamFull is returned by ClaimFreeSlot when every seat on the team
// has already been claimed.
var ErrTeamFull = errors.New("team full")

// ClaimFreeSlot takes the next unclaimed seat on a team and signs the
// caller into it: it stamps claimed_at, mints a slot session, and
// returns the slot plus the session token. This backs the "scan to
// join" flow on the self-guided team page, where a player should just
// get a seat rather than pick one. Atomic — FOR UPDATE SKIP LOCKED means
// two people tapping Start at the same moment never land on the same
// seat. ErrTeamFull when none are free.
func ClaimFreeSlot(ctx context.Context, db *sql.DB, teamID int64) (Slot, string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Slot{}, "", err
	}
	defer tx.Rollback() //nolint:errcheck

	var s Slot
	err = tx.QueryRowContext(ctx, `
		SELECT ps.id, ps.name, ps.team_id, t.event_id
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE ps.team_id = $1 AND ps.claimed_at IS NULL
		ORDER BY ps.id
		FOR UPDATE OF ps SKIP LOCKED
		LIMIT 1
	`, teamID).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, "", ErrTeamFull
	}
	if err != nil {
		return Slot{}, "", err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE player_slots SET last_login_at = now_text(), claimed_at = now_text() WHERE id = $1`, s.ID); err != nil {
		return Slot{}, "", err
	}
	token, err := newToken()
	if err != nil {
		return Slot{}, "", err
	}
	expires := time.Now().Add(SlotSessionTTL).UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO slot_sessions(token, slot_id, expires_at) VALUES ($1, $2, $3)`,
		token, s.ID, expires); err != nil {
		return Slot{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Slot{}, "", err
	}
	return s, token, nil
}

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
