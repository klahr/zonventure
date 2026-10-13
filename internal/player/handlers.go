// Package player implements the endpoints a player (slot) uses to play
// through an adventure.
//
// A slot authenticates via a magic-link exchange (see /play?t=<token>)
// which sets the zv_slot cookie. All endpoints in this package require a
// slot context populated by auth.SlotMiddleware + auth.RequireSlot.
package player

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	mailerpkg "github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/storage"
	"github.com/klahr/zonventure/internal/webhooks"
	"github.com/klahr/zonventure/internal/ws"
)

// parsePlayTime accepts both SQLite's "YYYY-MM-DD HH:MM:SS" (UTC) and
// RFC3339 strings — the various writers in this codebase use both.
func parsePlayTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty")
	}
	layouts := []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("unrecognised time format")
}

// wireTime is a stored UTC timestamp as RFC 3339, so the phone reads it
// as UTC rather than as its own local time. Unparseable values pass
// through as they are.
func wireTime(s string) string {
	if t, err := parsePlayTime(s); err == nil {
		return t.Format(time.RFC3339)
	}
	return s
}

// Handlers bundles the dependencies the player endpoints need.
type Handlers struct {
	DB           *sql.DB
	Lib          *adventure.Library
	Hub          *ws.Hub
	SecureCookie bool
	// Mailer + PublicURL power the lost-magic-link recovery flow:
	// the player enters their team's contact email, server mints a
	// fresh single-use token and emails the new link. nil mailer
	// falls back to LogMailer so dev still sees the URL in stderr.
	Mailer    mailerpkg.Mailer
	PublicURL string
	// Engine runs graph adventures. The server sets one shared engine so
	// its graph cache is shared; nil builds a throwaway one per call.
	Engine *play.Engine
	// Storage keeps the team's picture.
	Storage storage.Blobs
	// limiter throttles answer + location attempts per slot. Lazy-init
	// on first use so tests that construct Handlers directly don't
	// need to plumb a value through. nil-safe.
	limiter *playLimiter
}

// rateLimiter returns the lazily-initialised playLimiter so handlers
// don't have to nil-check.
func (h *Handlers) rateLimiter() *playLimiter {
	if h.limiter == nil {
		h.limiter = &playLimiter{}
	}
	return h.limiter
}

// MagicLinkLogin exchanges a player_slots.token (delivered to the player
// via their own message) for a slot session cookie and redirects the
// browser to /play. Handled at GET /play if the ?t= query parameter is
// present; if the token is bad we fall through to the ordinary /play
// page which will tell the player to open their link again.
//
// Both outcomes are audited so an arranger can tell whether a slot link
// was opened (and from where) — useful both for "did the player follow
// the link I sent?" and "is someone scraping tokens?".
func (h *Handlers) MagicLinkLogin(w http.ResponseWriter, r *http.Request) bool {
	token := r.URL.Query().Get("t")
	if token == "" {
		return false
	}
	ip := auth.ClientIP(r)
	ua := auth.UserAgent(r)
	// Per-IP rate limit on the exchange itself. Legitimate users open
	// the link once; abuse cases (script probing tokens, runaway
	// retry loop) burn the budget fast. Cheaper than letting every
	// miss hit the DB + audit log.
	if ok, _ := h.rateLimiter().AllowMagicLink(ip); !ok {
		auth.Record(r.Context(), h.DB, auth.AuditEvent{
			Event: auth.EventMagicLink, OK: false, IP: ip, UserAgent: ua,
			Reason: auth.ReasonRateLimited,
		})
		http.Redirect(w, r, "/app/play/?link=busy", http.StatusFound)
		return true
	}
	slot, session, err := auth.LoginSlotByToken(r.Context(), h.DB, token)
	if err != nil {
		reason := auth.ReasonUnknownToken
		if errors.Is(err, auth.ErrSlotTokenExpired) {
			reason = auth.ReasonExpiredToken
		}
		auth.Record(r.Context(), h.DB, auth.AuditEvent{
			Event: auth.EventMagicLink, OK: false, IP: ip, UserAgent: ua,
			Reason: reason,
		})
		// The link of a finished event has done its job: its holder
		// wants the team's recap, not a sign-in error. A cancelled
		// event's has no recap to show; the player app says so.
		link := "invalid"
		if errors.Is(err, auth.ErrSlotTokenExpired) {
			link = "expired"
			recap, state := h.recapForExpiredLink(r.Context(), token)
			if state == "cancelled" {
				link = "cancelled"
			} else if recap != "" {
				http.Redirect(w, r, "/recap/"+recap, http.StatusFound)
				return true
			}
		}
		// Drop the token from the address bar and tell the player app why
		// no one is signed in, so it can say so instead of asking the
		// player to open the very link they just opened.
		http.Redirect(w, r, "/app/play/?link="+link, http.StatusFound)
		return true
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventMagicLink, OK: true, Username: slot.Name, IP: ip, UserAgent: ua,
		Reason: auth.ReasonSuccess,
	})
	auth.SetSlotCookie(w, session, auth.CookieSecure(r, h.SecureCookie))
	// Straight into the app on a clean URL: the session is set, so there's
	// no need to bounce through /play (and no token left in the address
	// bar to re-trigger the exchange on refresh).
	http.Redirect(w, r, "/app/play/", http.StatusFound)
	return true
}

// recapForExpiredLink is the recap token of the team behind an expired
// magic link and its event's state, when the event is over; empty
// otherwise.
func (h *Handlers) recapForExpiredLink(ctx context.Context, magic string) (recap, state string) {
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(t.recap_token, ''), ar.state
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		JOIN events ar ON ar.id = t.event_id
		WHERE ps.token = $1 AND ar.state IN ('completed', 'cancelled')
	`, magic).Scan(&recap, &state)
	if err != nil {
		return "", ""
	}
	return recap, state
}

// Logout clears the slot session.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SlotCookieName); err == nil {
		_ = auth.LogoutSlot(r.Context(), h.DB, c.Value)
	}
	auth.ClearSlotCookie(w, auth.CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusNoContent, nil)
}

// Forget is the GDPR-style erasure endpoint for a player. Wipes the
// caller slot's personally-identifiable trail (location pings, answer
// attempts, slot name, magic-link token) without deleting the slot
// row itself — keeping the row prevents foreign-key chaos for any
// teammate still mid-event, and the team's progress stays intact.
//
// The slot is effectively dead afterwards: token rotated to an
// unguessable value with expires_at in the past, name replaced with
// "(forgotten)", every active session for the slot dropped. The
// arranger sees an inert row labelled "(forgotten)" in the team
// listing, which is an honest reflection of what happened.
func (h *Handlers) Forget(w http.ResponseWriter, r *http.Request) {
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "open your magic link to log in")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	// A forgotten player's roles go back to be dealt among the rest, and
	// their keys and agreed answers no longer count.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM team_roles WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete roles")
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM team_presses WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete presses")
		return
	}
	// A forgotten saboteur leaves the team without one, not with another.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE team_secrets SET slot_id = NULL WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear secret roles")
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM location_pings WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "wipe pings", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM answer_attempts WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "wipe answers", err)
		return
	}
	// Pictures the player took and their votes. The points an approved
	// picture earned stay: the team keeps its game, not the player's face.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM team_photos WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "wipe photos", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM team_block_votes WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "wipe votes", err)
		return
	}
	// New unguessable token + already-expired expires_at so the row
	// can't be re-logged-into. Name swapped to a literal so the team
	// listing doesn't keep displaying the player's chosen handle.
	deadToken, err := auth.NewSlotToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE player_slots
		   SET name = '(forgotten)', token = $1, expires_at = '1970-01-01 00:00:00'
		 WHERE id = $2
	`, deadToken, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "wipe slot", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM slot_sessions WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "wipe sessions", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	auth.ClearSlotCookie(w, auth.CookieSecure(r, h.SecureCookie))
	// Notify any arranger watching the event so the team list redraws
	// with the (forgotten) name immediately.
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// teamContext is the slot's team/event snapshot used by all the handlers.
type teamContext struct {
	TeamID        int64
	TeamName      string
	TeamToken     string // team-share token, exposed to the player so they can show a join QR for teammates
	EventID       int64
	OrgID         int64
	OrgName       string
	OrgHasLogo    bool // true when the owning org has uploaded a brand logo
	HasAvatar     bool // true when the team has a picture
	AdventureSlug string
	RunState      string
	ScheduledAt   sql.NullString
	StartedAt     sql.NullString
	CompletedAt   sql.NullString
}

// adventureTitle returns the human title for slug, or "" when the
// library doesn't carry it. Used to enrich webhook payloads without
// the same nil-check dance at every publish site.
func (h *Handlers) adventureTitle(slug string) string {
	if adv := h.Lib.Get(slug); adv != nil {
		return adv.Meta.Title
	}
	return ""
}

// publishEventStartedAuto emits event.started for an event that was just
// auto-promoted (player arrived at the scheduled time). Distinct
// from the manual-start webhook in admin/handlers.go via the "manual"
// field on the payload.
func (h *Handlers) publishEventStartedAuto(ctx context.Context, tc teamContext) {
	webhooks.TryPublish(ctx, h.DB, tc.OrgID, webhooks.KindEventStarted, map[string]any{
		"event_id":        tc.EventID,
		"adventure_slug":  tc.AdventureSlug,
		"adventure_title": h.adventureTitle(tc.AdventureSlug),
		"manual":          false,
	})
}

// publishTeamCompleted emits the team.completed webhook after the
// team's last-page UPDATE has committed. Best-effort: a failed
// enqueue logs and is dropped (the player has already seen "you're
// done!").
func (h *Handlers) publishTeamCompleted(ctx context.Context, tc teamContext) {
	webhooks.TryPublish(ctx, h.DB, tc.OrgID, webhooks.KindTeamCompleted, map[string]any{
		"event_id":        tc.EventID,
		"team_id":         tc.TeamID,
		"team_name":       tc.TeamName,
		"adventure_slug":  tc.AdventureSlug,
		"adventure_title": h.adventureTitle(tc.AdventureSlug),
	})
}

// publishEventCompleted emits event.completed when MaybeCompleteEvent
// transitioned the event. Same best-effort discipline.
func (h *Handlers) publishEventCompleted(ctx context.Context, tc teamContext) {
	webhooks.TryPublish(ctx, h.DB, tc.OrgID, webhooks.KindEventCompleted, map[string]any{
		"event_id":        tc.EventID,
		"adventure_slug":  tc.AdventureSlug,
		"adventure_title": h.adventureTitle(tc.AdventureSlug),
	})
}

// maybePromoteScheduled atomically flips a scheduled event to running
// when its scheduled_at has arrived. Returns (true, nil) on a
// successful promotion so the caller can carry on as if the event were
// already running; a lost race (manual start, concurrent sweep) is
// returned as (false, nil). The intent is to remove the "player
// arrived at 00:00 but still waits 15 s for the scheduler" UX hiccup.
//
// On a successful promotion the event's graph is pinned and its teams
// placed. A failure there is logged via the activity feed but doesn't
// roll back the state transition.
func maybePromoteScheduled(ctx context.Context, db *sql.DB, hub *ws.Hub, eventID int64, scheduledAt string) (bool, error) {
	due, err := parsePlayTime(scheduledAt)
	if err != nil {
		return false, nil
	}
	if time.Now().UTC().Before(due) {
		return false, nil
	}
	res, err := db.ExecContext(ctx, `
		UPDATE events
		   SET state = 'running', started_at = now_text()
		 WHERE id = $1 AND state = 'scheduled'
		   AND (graph_id IS NOT NULL OR EXISTS (
		         SELECT 1 FROM adventures a
		          WHERE a.slug = events.adventure_slug AND a.published_graph_id IS NOT NULL))
	`, eventID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if err := play.Start(ctx, db, eventID); err != nil {
		activity.Record(ctx, db, eventID, nil, activity.Actor{},
			"graph.start.failed", map[string]any{"error": err.Error()})
	}
	activity.Record(ctx, db, eventID, nil, activity.Actor{}, activity.EventStarted,
		map[string]any{"auto": true})
	if hub != nil {
		hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	return true, nil
}

func loadTeam(ctx context.Context, db *sql.DB, slot auth.Slot) (teamContext, error) {
	var tc teamContext
	var hasLogo, hasAvatar int
	err := db.QueryRowContext(ctx, `
		SELECT t.id, t.name, t.token, ar.id, ar.org_id, ar.adventure_slug, ar.state,
		       ar.scheduled_at, ar.started_at,
		       t.completed_at,
		       o.name,
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0)::int,
		       (t.avatar_mime_type IS NOT NULL)::int
		FROM teams t
		JOIN events ar ON ar.id = t.event_id
		JOIN organizations o ON o.id = ar.org_id
		WHERE t.id = $1
	`, slot.TeamID).Scan(&tc.TeamID, &tc.TeamName, &tc.TeamToken,
		&tc.EventID, &tc.OrgID, &tc.AdventureSlug,
		&tc.RunState, &tc.ScheduledAt, &tc.StartedAt,
		&tc.CompletedAt,
		&tc.OrgName, &hasLogo, &hasAvatar)
	tc.OrgHasLogo = hasLogo != 0
	tc.HasAvatar = hasAvatar != 0
	return tc, err
}

func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// writeCached replays a previously-recorded response. Content-Type
// is fixed to JSON because that's what every cached endpoint emits;
// this helper would need a stored content-type if we ever started
// caching non-JSON responses.
func writeCached(w http.ResponseWriter, c cachedAction) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(c.Status)
	_, _ = w.Write(c.Body)
}

// LocationRequest is the body of POST /api/player/location.
type LocationRequest struct {
	Lat      float64  `json:"lat"`
	Lng      float64  `json:"lng"`
	Accuracy *float64 `json:"accuracy,omitempty"`
	Speed    *float64 `json:"speed,omitempty"`
	Heading  *float64 `json:"heading,omitempty"`
	ClientTS string   `json:"client_ts,omitempty"`
}

// maxPlausibleSpeedMS caps the implied speed (distance / elapsed)
// between two consecutive pings from the same phone. 60 m/s is ~216
// km/h — comfortably above any realistic foot/bike/bus/train speed
// for a city-wide adventure, low enough to catch the obvious
// teleport cheat where a player POSTs "I'm at the gate" coordinates
// from across town. Real GPS jitter (a 50 m hop in 5 s) clears the
// bar; "I was 1 km away 5 s ago" doesn't.
const maxPlausibleSpeedMS = 60.0

// minLookbackForSpeedSec is the lower bound on elapsed time before
// we even compute implied speed. Below this, the speed math is
// dominated by GPS sample jitter (two pings ~half a second apart
// with 30m accuracy each can imply 60 m/s of "movement" that's
// just sensor noise). Don't reject below this floor.
const minLookbackForSpeedSec = 5.0

// maxLookbackForSpeedSec is the upper bound on elapsed time. A
// long backgrounded app comes back with a fresh fix anywhere on
// the map; we don't want to reject the player's first ping after
// they reopened the tab. Beyond this gap the comparison is moot.
const maxLookbackForSpeedSec = 120.0

// recordLocation inserts a ping row. Returns (statusCode, errMsg).
//
// Plausibility check: the gateReached test treats the latest ping
// as gospel — without a sanity gate, a player still at the start
// could just POST "I'm at the gate" coordinates and walk through
// every gated puzzle from a chair. We compare against the most
// recent ping from the same phone and reject anything that implies
// a teleport above maxPlausibleSpeedMS. Rejections leave the prior
// ping in place so the next legitimate ping has a stable anchor.
//
// Per phone, not per team: two teammates a few streets apart ping in
// turn, and comparing one's fix with the other's reads as a teleport.
func (h *Handlers) recordLocation(ctx context.Context, slot auth.Slot, req LocationRequest) (int, string) {
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		return http.StatusBadRequest, "coordinates out of range"
	}
	if !plausibleNextPing(ctx, h.DB, slot.ID, req.Lat, req.Lng) {
		return http.StatusForbidden, "implausible position jump"
	}
	var acc, speed, heading, cts any
	if req.Accuracy != nil {
		acc = *req.Accuracy
	}
	if req.Speed != nil {
		speed = *req.Speed
	}
	if req.Heading != nil {
		heading = *req.Heading
	}
	if req.ClientTS != "" {
		cts = req.ClientTS
	}
	if _, err := h.DB.ExecContext(ctx, `
		INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, accuracy, speed, heading, client_ts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, slot.ID, slot.TeamID, slot.EventID, req.Lat, req.Lng, acc, speed, heading, cts); err != nil {
		return http.StatusInternalServerError, "save ping failed"
	}
	return http.StatusNoContent, ""
}

// plausibleNextPing returns true when the proposed (lat, lng) is
// reachable from the phone's most recent ping at no more than
// maxPlausibleSpeedMS, OR when there's no prior ping to compare
// against / the gap is outside the lookback window. Errors and
// missing-row cases default to "plausible" — this is a cheating
// gate, not a hard correctness gate, so failing open is the right
// posture if the DB read flakes.
//
// The ping before the latest counts too: when the latest was itself the
// jump (a stray fix let through after a quiet spell), the phone's honest
// fixes agree with the one before it, and must not be refused until the
// stray one ages out. A spoofer gains nothing: the ping before a jump let
// through after a quiet spell is outside the window anyway, as it was
// when the jump itself was let through.
func plausibleNextPing(ctx context.Context, db *sql.DB, slotID int64, lat, lng float64) bool {
	rows, err := db.QueryContext(ctx, `
		SELECT lat, lng, server_ts FROM location_pings
		WHERE slot_id = $1
		ORDER BY server_ts DESC, id DESC
		LIMIT 2
	`, slotID)
	if err != nil {
		return true
	}
	defer rows.Close()
	seen := false
	for rows.Next() {
		var (
			lastLat, lastLng float64
			serverTS         string
		)
		if err := rows.Scan(&lastLat, &lastLng, &serverTS); err != nil {
			return true
		}
		seen = true
		prev, err := parsePlayTime(serverTS)
		if err != nil {
			return true
		}
		elapsed := time.Since(prev).Seconds()
		if elapsed < minLookbackForSpeedSec || elapsed > maxLookbackForSpeedSec {
			return true
		}
		if haversineMeters(lat, lng, lastLat, lastLng)/elapsed <= maxPlausibleSpeedMS {
			return true
		}
	}
	// No prior ping (or DB hiccup) — let it through. The first ping has
	// nothing to anchor against.
	return !seen || rows.Err() != nil
}

// PostLocation records a GPS ping for the slot's team.
func (h *Handlers) PostLocation(w http.ResponseWriter, r *http.Request) {
	var req LocationRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	if ok, retry := h.rateLimiter().AllowLocation(slot.ID); !ok {
		writeRateLimited(w, retry)
		return
	}
	status, errMsg := h.recordLocation(r.Context(), slot, req)
	if status >= 400 {
		httpx.Error(w, status, errMsg)
		return
	}
	h.publishPing(slot)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// PostLocationOff records that the player turned location sharing off:
// their earlier fixes stop placing them, so the place they were at no
// longer counts once the phone has moved on. Sharing again places them
// by the new fixes.
//
// POST /api/player/location/off
func (h *Handlers) PostLocationOff(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE player_slots SET location_off_at = now_text() WHERE id = $1`, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.publishPing(slot)
	w.WriteHeader(http.StatusNoContent)
}

// publishPing tells the team's other phones (a teammate may have brought
// the team into a zone) and the position views. Not the whole event: with
// every phone pinging, that made every player reload several times a second.
func (h *Handlers) publishPing(slot auth.Slot) {
	h.Hub.Publish(ws.TeamTopic(slot.TeamID), "changed", nil)
	h.Hub.Publish(ws.PositionsTopic(slot.EventID), "changed", nil)
}

// RenameRequest is the body of POST /api/player/me/name — lets a
// player override the slot name the arranger picked. Useful when the
// arranger pre-named slots generically ("Player 3") and the
// player wants to introduce themselves properly. Idempotent: empty
// or unchanged names short-circuit without a write.
type RenameRequest struct {
	Name string `json:"name"`
}

// RenameMyself updates the calling slot's name. Server enforces the
// same length cap the arranger flow uses. Hub-publishes so the
// arranger console + team-mates' screens pick up the new name on
// the next state push.
func (h *Handlers) RenameMyself(w http.ResponseWriter, r *http.Request) {
	var req RenameRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	if utf8.RuneCountInString(name) > 64 {
		httpx.Error(w, http.StatusBadRequest, "name too long (64 chars max)")
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	if h.refuseOnceBegun(w, r, slot.TeamID) {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE player_slots SET name = $1 WHERE id = $2`, name, slot.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "rename failed")
		return
	}
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"name": name})
}

// RenameMyTeam updates the calling slot's *team* name. Any team
// member can rename — the arranger's pre-set name ("Team 1") is
// often a placeholder and the team itself usually agrees on the
// real name as a group. There's no "first-renamer-wins" lock:
// teams that bicker over the name will figure it out at the table,
// not via the API. Same length cap as the arranger flow.
//
// Conflict handling mirrors the admin RenameTeam: the
// UNIQUE(event_id, name) constraint is the gate, and a clash
// surfaces as 409 with copy the UI can show inline.
func (h *Handlers) RenameMyTeam(w http.ResponseWriter, r *http.Request) {
	var req RenameRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	if utf8.RuneCountInString(name) > 64 {
		httpx.Error(w, http.StatusBadRequest, "name too long (64 chars max)")
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	if h.refuseOnceBegun(w, r, slot.TeamID) {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE teams SET name = $1 WHERE id = $2`, name, slot.TeamID); err != nil {
		// UNIQUE(event_id, name) is the only constraint that can
		// realistically fire here. Any other DB error reads as 409
		// from the player's perspective ("can't use that name") —
		// the alternative is a generic 500 that the UI can't
		// recover from gracefully.
		httpx.Error(w, http.StatusConflict, "name already used in this event")
		return
	}
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]string{"name": name})
}

// writeRateLimited writes a 429 with Retry-After. Mirrors the helper
// in internal/auth so the wire response is consistent across the
// rate-limited surfaces.
func writeRateLimited(w http.ResponseWriter, retry time.Duration) {
	secs := int(retry / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpx.JSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, slow down", "code": "rate_limited"})
}
