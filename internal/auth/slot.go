package auth

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/billing"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
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
	// or signs out; see RequireSlotOrEnded. Cancelled says it ended by
	// being cancelled.
	Ended     bool
	Cancelled bool
	// Party says the slot is a hosted party's team; Withdrawn that the
	// team was taken out. Together they say why a party session ended.
	Party     bool
	Withdrawn bool
	// Replaced says the player's link was opened on another phone since,
	// which took the seat over; see activateSlotSession.
	Replaced bool
	// Dev says the slot plays an author's dev ticket.
	Dev    bool
	active bool
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

// ErrOrgCannotPay is a seat that would start a team, or add a phone,
// that the organisation's balance can't pay for (billing.ErrCannotPay):
// the player is told to ask the organiser.
var ErrOrgCannotPay = billing.ErrCannotPay

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
// arranger's ability to rotate links from the console.
//
// claimed_at is still stamped (on first claim) as forensic record of
// when the slot first activated, but is never read as a gate.
func LoginSlotByToken(ctx context.Context, db *sql.DB, magic string) (Slot, string, error) {
	if magic == "" {
		return Slot{}, "", ErrSlotTokenUnknown
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Slot{}, "", err
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		s         Slot
		expiresAt sql.NullString
		claimed   bool
	)
	err = tx.QueryRowContext(ctx, `
		SELECT ps.id, ps.name, ps.team_id, t.event_id, ps.expires_at, ps.claimed_at IS NOT NULL
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE ps.token = $1
		FOR UPDATE OF ps
	`, magic).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID, &expiresAt, &claimed)
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

	// A seat opened for the first time must fit the team's adventure.
	if !claimed {
		if full, err := teamFull(ctx, tx, s.TeamID, s.ID); err != nil {
			return Slot{}, "", err
		} else if full {
			return Slot{}, "", ErrTeamFull
		}
	}

	// Stamp last_login_at every exchange; stamp claimed_at only the
	// first time so it preserves the original activation moment for
	// audit. COALESCE keeps the existing value once set.
	if _, err := tx.ExecContext(ctx,
		`UPDATE player_slots
		    SET last_login_at = now_text(),
		        claimed_at    = COALESCE(claimed_at, now_text())
		  WHERE id = $1`, s.ID); err != nil {
		return Slot{}, "", err
	}
	// The seat's first opening starts its team, or adds a phone: both
	// paid for from the organisation's balance.
	if !claimed {
		if err := billing.ClaimSeat(ctx, tx, s.ID, settings.CurrentPricing()); err != nil {
			return Slot{}, "", err
		}
	}
	session, err := newSlotSession(ctx, tx, s.ID)
	if err != nil {
		return Slot{}, "", err
	}
	if err := tx.Commit(); err != nil {
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
		SELECT ps.id, ps.name, ps.team_id, t.event_id, ss.expires_at, ss.ended, e.state = 'cancelled',
		       e.party, t.withdrawn_at IS NOT NULL, ss.replaced, ss.active, e.dev
		FROM slot_sessions ss
		JOIN player_slots ps ON ps.id = ss.slot_id
		JOIN teams t ON t.id = ps.team_id
		JOIN events e ON e.id = t.event_id
		WHERE ss.token = $1
	`, token).Scan(&s.ID, &s.Name, &s.TeamID, &s.EventID, &expires, &s.Ended, &s.Cancelled, &s.Party, &s.Withdrawn, &s.Replaced, &s.active, &s.Dev)
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

// activateSlotSession makes a session the slot's one active phone the
// first time the player app uses it, and marks every other session of
// the slot replaced. Opening a player's link on a second phone thus
// takes the seat over and signs the first phone out. It waits for the
// app's first API call rather than the link exchange, so a link
// previewer or mail scanner fetching the URL takes nothing over.
func activateSlotSession(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, `
		WITH a AS (
			UPDATE slot_sessions SET active = true
			WHERE token = $1 AND NOT active AND NOT replaced
			RETURNING slot_id
		)
		UPDATE slot_sessions SET replaced = true
		WHERE slot_id IN (SELECT slot_id FROM a) AND token <> $1
	`, token)
	return err
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
// compose this with RequireSlot. lookupFailed is as Middleware's.
func SlotMiddleware(db *sql.DB, lookupFailed http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SlotCookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			s, ok, err := LookupSlot(r.Context(), db, c.Value)
			if err != nil && lookupFailed != nil {
				lookupFailed(w, r)
				return
			}
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "slot lookup failed")
				return
			}
			// An ended session is kept apart, so nothing but
			// RequireSlotOrEnded takes it for a signed-in player.
			switch {
			case ok && s.Ended:
				r = r.WithContext(context.WithValue(r.Context(), endedSlotCtxKey{}, s))
			case ok && s.Replaced:
				r = r.WithContext(context.WithValue(r.Context(), replacedSlotCtxKey{}, s))
			case ok:
				if !s.active && (strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/")) {
					if err := activateSlotSession(r.Context(), db, c.Value); err != nil {
						httpx.Error(w, http.StatusInternalServerError, "slot lookup failed")
						return
					}
				}
				r = r.WithContext(WithSlot(r.Context(), s))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// seenEvery is the most often a slot is stamped as seen (TouchSlot):
// often enough that a phone with its app open never looks away
// (play.away waits minutes), seldom enough that a busy phone costs no
// write per request.
const seenEvery = 30 * time.Second

// seenTick is how often an open WebSocket is stamped: seenEvery, but
// for tests.
var seenTick = seenEvery

// seenJitter is how much sooner than seenTick a socket's first stamp
// comes, at random below seenTick: sockets reconnecting together after
// a restart then spread their lookups and stamps over the tick, not
// all in one second of it. Tests set it to none.
var seenJitter = func(d time.Duration) time.Duration { return rand.N(d) }

// TouchSlot stamps player_slots.last_seen_at, unless it was stamped
// within seenEvery: the phone was just heard from. Stamps are whole
// seconds, so the bar is a second short of seenEvery: a stamp left by
// the socket's last tick, seenEvery ago to the second, is renewed by
// this one rather than the next. A forgotten slot is never stamped: a
// tick or request that found its session just before Forget cleared
// the stamp would otherwise put it back.
func TouchSlot(ctx context.Context, db *sql.DB, slotID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE player_slots SET last_seen_at = now_text()
		WHERE id = $1 AND name <> '(forgotten)' AND (last_seen_at IS NULL OR
		      last_seen_at < to_char((now() - make_interval(secs => $2)) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS'))
	`, slotID, (seenEvery - time.Second).Seconds())
	return err
}

// SeenSlot stamps the signed-in slot as seen (TouchSlot) on each request
// — and, for a WebSocket held open, every seenEvery while it lasts, so a
// phone with the app open and no one tapping still counts as there. The
// connection's own pings close it once the phone is gone. A socket
// whose session has since gone — signed out, erased, its event ended,
// or its seat taken over by another phone — is no longer stamped: its
// request's context is cancelled, which closes the socket, and the
// phone's reconnect is refused as any request of that session would
// be. A failed stamp is logged, and never fails the request. Compose
// after RequireSlot.
func SeenSlot(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, ok := SlotFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			touch := func(ctx context.Context) {
				if err := TouchSlot(ctx, db, s.ID); err != nil && ctx.Err() == nil {
					logx.Warn("auth.slot.seen_failed", "slot", s.ID, "err", err.Error())
				}
			}
			touch(r.Context())
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				r = r.WithContext(ctx)
				token := ""
				if c, err := r.Cookie(SlotCookieName); err == nil {
					token = c.Value
				}
				go func() {
					defer logx.Guard("auth.slot.seen")
					tick := time.NewTimer(seenTick - seenJitter(seenTick))
					defer tick.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-tick.C:
							if !sessionLive(ctx, db, token) {
								cancel()
								return
							}
							touch(ctx)
							tick.Reset(seenTick)
						}
					}
				}()
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sessionLive reports whether a slot session still lets its phone play,
// as SlotMiddleware and RequireSlot would judge it: there, not expired,
// its event not ended, its seat not taken over. A failed lookup counts
// as live — a database hiccup closes no one's socket.
func sessionLive(ctx context.Context, db *sql.DB, token string) bool {
	s, ok, err := LookupSlot(ctx, db, token)
	if err != nil {
		if ctx.Err() == nil {
			logx.Warn("auth.slot.session_check_failed", "err", err.Error())
		}
		return true
	}
	return ok && !s.Ended && !s.Replaced
}

// RequireSlot refuses requests that have no authenticated slot. A
// session its event's end closed is told so by code, for the player app
// to say the event is over rather than ask for the link again. A
// party's team is told whether the host took it out or finished the
// game, the only two ways a party session ends short of a cancel.
func RequireSlot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SlotFromContext(r.Context()); !ok {
			if s, ended := r.Context().Value(endedSlotCtxKey{}).(Slot); ended {
				code := "event_ended"
				switch {
				case s.Cancelled:
					code = "event_cancelled"
				case s.Party && s.Withdrawn:
					code = "taken_out"
				case s.Party:
					code = "game_finished"
				}
				httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "this event has ended", "code": code})
				return
			}
			refuseSlot(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuseSlot answers a request without a usable slot session: a phone
// whose seat was taken over is told so by code, for the player app to
// say the link was opened on another phone.
func refuseSlot(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.Context().Value(replacedSlotCtxKey{}).(Slot); ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "opened on another phone", "code": "session_replaced"})
		return
	}
	httpx.Error(w, http.StatusUnauthorized, "open your magic link to log in")
}

// RequireSlotOrEnded is RequireSlot that also takes a session whose
// event has ended, for what a player may still do then: erase their
// data and sign out.
func RequireSlotOrEnded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SlotFromContext(r.Context()); !ok {
			s, ended := r.Context().Value(endedSlotCtxKey{}).(Slot)
			if !ended {
				refuseSlot(w, r)
				return
			}
			r = r.WithContext(WithSlot(r.Context(), s))
		}
		next.ServeHTTP(w, r)
	})
}

// endedSlotCtxKey holds a session whose event has ended.
type endedSlotCtxKey struct{}

// replacedSlotCtxKey holds a session another phone took over.
type replacedSlotCtxKey struct{}

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

// teamFull reports whether a team already has as many players as its
// adventure takes (the graph's max_players), slot aside.
func teamFull(ctx context.Context, tx *sql.Tx, teamID, slotID int64) (bool, error) {
	var (
		most   sql.NullInt64
		joined int64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT (g.graph->>'max_players')::int,
		       (SELECT COUNT(*) FROM player_slots
		         WHERE team_id = t.id AND claimed_at IS NOT NULL AND id <> $2 AND name <> '(forgotten)')
		FROM teams t
		JOIN events e ON e.id = t.event_id
		LEFT JOIN adventure_graphs g ON g.id = e.graph_id
		WHERE t.id = $1`, teamID, slotID).Scan(&most, &joined)
	if err != nil {
		return false, err
	}
	return most.Valid && most.Int64 > 0 && joined >= most.Int64, nil
}

// ErrTeamFull is returned by ClaimFreeSlot when every seat on the team
// has already been claimed, and by either way in when the team already
// has as many players as its adventure takes.
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
	if full, err := teamFull(ctx, tx, teamID, s.ID); err != nil {
		return Slot{}, "", err
	} else if full {
		return Slot{}, "", ErrTeamFull
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE player_slots SET last_login_at = now_text(), claimed_at = now_text() WHERE id = $1`, s.ID); err != nil {
		return Slot{}, "", err
	}
	if err := billing.ClaimSeat(ctx, tx, s.ID, settings.CurrentPricing()); err != nil {
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

func newSlotSession(ctx context.Context, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, slotID int64) (string, error) {
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
