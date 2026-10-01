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
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/klahr/zonventure/internal/lanes"
	mailerpkg "github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/mdx"
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
		http.Redirect(w, r, "/play?link=busy", http.StatusFound)
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
		// wants the team's recap, not a sign-in error.
		if errors.Is(err, auth.ErrSlotTokenExpired) {
			if recap := h.recapForExpiredLink(r.Context(), token); recap != "" {
				http.Redirect(w, r, "/recap/"+recap, http.StatusFound)
				return true
			}
		}
		// Drop the token from the address bar and tell the /play page why
		// no one is signed in, so it can say so instead of asking the
		// player to open the very link they just opened.
		link := "invalid"
		if errors.Is(err, auth.ErrSlotTokenExpired) {
			link = "expired"
		}
		http.Redirect(w, r, "/play?link="+link, http.StatusFound)
		return true
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventMagicLink, OK: true, Username: slot.Name, IP: ip, UserAgent: ua,
		Reason: auth.ReasonSuccess,
	})
	auth.SetSlotCookie(w, session, auth.CookieSecure(r, h.SecureCookie))
	// Strip the query parameter on the way back so refreshing doesn't
	// re-trigger the token exchange (and doesn't leave the token in the
	// address bar).
	http.Redirect(w, r, "/play", http.StatusFound)
	return true
}

// recapForExpiredLink is the recap token of the team behind an expired
// magic link, when its event is over; empty otherwise.
func (h *Handlers) recapForExpiredLink(ctx context.Context, magic string) string {
	var recap string
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(t.recap_token, '')
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		JOIN events ar ON ar.id = t.event_id
		WHERE ps.token = $1 AND ar.state IN ('completed', 'cancelled')
	`, magic).Scan(&recap)
	if err != nil {
		return ""
	}
	return recap
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
	// CurrentPageIndex is preserved as a monotonic "puzzles solved"
	// counter for sorting/leaderboards and the assets-strict-mode
	// gate. With lanes, the source of truth for "which page is this
	// team on now" is CurrentPageID; CurrentPageIndex is incremented
	// once per advance regardless of which lane the team is in.
	CurrentPageIndex     int
	CurrentPageID        int64 // 0 = no active page (pre-start or completed)
	CurrentLaneID        int64 // 0 = not in a lane (pre-start or completed)
	CurrentPageStartedAt sql.NullString
	CompletedAt          sql.NullString
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
// On a successful promotion the lane initialiser runs to seed
// per-team permutations and initial cursors. Failures there are
// logged via the activity feed but don't roll back the state
// transition — the runtime fallback derives permutations on the fly
// when no row exists, so the run is still playable.
func maybePromoteScheduled(ctx context.Context, db *sql.DB, hub *ws.Hub, lib *adventure.Library, eventID int64, scheduledAt string) (bool, error) {
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
	`, eventID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if lib != nil {
		if err := lanes.Initialize(ctx, db, lib, eventID); err != nil {
			activity.Record(ctx, db, eventID, nil, activity.Actor{},
				"lanes.init.failed", map[string]any{"error": err.Error()})
		}
	}
	if _, err := play.StartIfGraph(ctx, db, eventID); err != nil {
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
	var pageID, laneID sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT t.id, t.name, t.token, ar.id, ar.org_id, ar.adventure_slug, ar.state,
		       ar.scheduled_at, ar.started_at,
		       t.current_page_index, t.current_page_id, t.current_lane_id,
		       t.current_page_started_at, t.completed_at,
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
		&tc.CurrentPageIndex, &pageID, &laneID,
		&tc.CurrentPageStartedAt, &tc.CompletedAt,
		&tc.OrgName, &hasLogo, &hasAvatar)
	tc.OrgHasLogo = hasLogo != 0
	tc.HasAvatar = hasAvatar != 0
	if pageID.Valid {
		tc.CurrentPageID = pageID.Int64
	}
	if laneID.Valid {
		tc.CurrentLaneID = laneID.Int64
	}
	return tc, err
}

// State is the response body for GET /api/player/state and the "state" WS
// push kind.
type State struct {
	Status         string `json:"status"` // waiting | running | paused | completed | cancelled | gated
	AdventureSlug  string `json:"adventure_slug,omitempty"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	TeamName       string `json:"team_name,omitempty"`
	// TeamToken is the team-share token (the public /team/{token}
	// page where each member picks their own slot). Surfaced to the
	// player so they can show a join QR to a teammate whose magic
	// link didn't reach them — the teammate scans, picks their name,
	// done. Not sensitive on its own: the team-share page reveals
	// every slot's magic link, so only share it with people who
	// already belong on this team.
	TeamToken string `json:"team_token,omitempty"`
	SlotName  string `json:"slot_name,omitempty"`
	PageIndex int    `json:"page_index,omitempty"`
	PageCount int    `json:"page_count,omitempty"`
	Title     string `json:"title,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	// Hints carries the puzzle's authored hints with their unlock
	// schedule computed against the team's arrival time on this
	// puzzle. Empty slice when the puzzle has no hints — the client
	// hides the reveal button entirely.
	Hints []hintDTO `json:"hints,omitempty"`
	// HintsRevealed is the team-shared count of hints already
	// uncovered on the current puzzle. Sourced from team_hint_reveals
	// so a device switch / storage clear / team-mate's reveal all
	// land on the same number. omitempty drops the zero so existing
	// clients on older versions ignore it cleanly.
	HintsRevealed int               `json:"hints_revealed,omitempty"`
	Images        []imageDTO        `json:"images,omitempty"`
	Audio         []audioDTO        `json:"audio,omitempty"`
	Gate          *gateDTO          `json:"gate,omitempty"`
	Colors        *adventure.Colors `json:"colors,omitempty"`
	// ScheduledAt / ServerNow let the pre-start countdown tick against
	// the server's clock. ScheduledAt is only set for scheduled events.
	ScheduledAt string `json:"scheduled_at,omitempty"`
	ServerNow   string `json:"server_now,omitempty"`
	// NoAnswer flags a puzzle whose accept list is empty — the player
	// advances by tapping a button instead of submitting an answer.
	// The client uses this to swap the submit dock for a Continue /
	// Check-location CTA.
	NoAnswer bool `json:"no_answer,omitempty"`
	// IntroText / OutroText are the author's custom copy for the
	// first-event welcome dialog and the post-completion stats screen.
	// Empty → client falls back to its built-in default copy.
	IntroText string `json:"intro_text,omitempty"`
	OutroText string `json:"outro_text,omitempty"`
	// OrgLogoURL is the public path to the operator's brand logo
	// when the owning organisation has uploaded one. Surfaced so
	// the play page can show "powered by Acme" instead of the
	// generic Zonventure mark — the small UX win that makes a
	// corporate event feel native to the customer's brand. Empty
	// string drops the surface entirely; the client falls back to
	// the bare adventure title.
	OrgLogoURL string `json:"org_logo_url,omitempty"`
	// OrgName is the human label paired with the logo (used as
	// alt-text and as a fallback when the image fails to load).
	OrgName string `json:"org_name,omitempty"`
	// Starting location surfaced on the welcome overlay so a player
	// who lands on the magic link before they've reached the meeting
	// point sees "Start at: <label>" + a map link. Both coords have
	// to be present together; an unset start lets the client fall
	// back to the first gated puzzle's location client-side.
	StartLat   *float64 `json:"start_lat,omitempty"`
	StartLng   *float64 `json:"start_lng,omitempty"`
	StartLabel string   `json:"start_label,omitempty"`
}

type imageDTO struct {
	URL string `json:"url"`
	Alt string `json:"alt,omitempty"`
}

// audioDTO is one attached audio clip's wire shape. URL points at the
// gated audio-serving endpoint (page-bound for puzzle audio,
// reveal-bound for hint audio); MIME tells the browser which decoder
// to use; description is the optional transcript fallback.
type audioDTO struct {
	URL         string `json:"url"`
	MimeType    string `json:"mime_type,omitempty"`
	Description string `json:"description,omitempty"`
}

// hintDTO is one hint shipped to the player. TextHTML is the rendered
// markdown (same pipeline as prompt). UnlocksInSeconds counts down
// from the server's "now" to the moment this hint becomes revealable;
// <=0 means the client can show it right now. Order matches the
// author's sequence — the client reveals them one at a time in that
// order via a single "Reveal hint" button.
//
// Images carries the per-hint attachments. The URLs point at the
// reveal-gated /adventure-assets/hint-image route — even if the
// client somehow tries to fetch one for an unrevealed hint, the
// server 403s. The client's own renderer hides images for hints the
// player hasn't revealed yet, so the URLs only become live once the
// hint card paints.
type hintDTO struct {
	TextHTML         string     `json:"text_html"`
	UnlocksInSeconds int        `json:"unlocks_in_seconds"`
	Images           []imageDTO `json:"images,omitempty"`
	Audio            []audioDTO `json:"audio,omitempty"`
}

type gateDTO struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Radius  float64 `json:"radius_m"`
	Reached bool    `json:"reached"`
}

// teamArrival is when the team became eligible for their current
// puzzle. For a team that's advanced at least once it's the stamped
// current_page_started_at; for a team still on page 0 we fall back
// to the event's started_at so page-0 hints unlock on the same clock
// every player shares. Zero time means "no clock yet" (run is still
// scheduled) and upstream callers treat every hint as locked.
func teamArrival(tc teamContext) time.Time {
	if tc.CurrentPageStartedAt.Valid {
		if t, err := parsePlayTime(tc.CurrentPageStartedAt.String); err == nil {
			return t
		}
	}
	if tc.StartedAt.Valid {
		if t, err := parsePlayTime(tc.StartedAt.String); err == nil {
			return t
		}
	}
	return time.Time{}
}

// buildHintDTOs renders + schedules each of a page's hints for the
// player. Output preserves author order; each entry's
// UnlocksInSeconds counts down to delay_seconds after the team's
// arrival. When arrival is the zero time (event not started yet),
// everything stays locked.
//
// hintsRevealed gates the rendered TEXT body: only hints with idx
// < hintsRevealed get TextHTML in the DTO. The unlock countdown
// still ships for every hint so the client can show "reveal in N
// seconds" without leaking the body. Without this gate, a curious
// player could read every locked hint by opening devtools.
func buildHintDTOs(hints []adventure.Hint, arrival time.Time, hintsRevealed int) []hintDTO {
	out := make([]hintDTO, 0, len(hints))
	now := time.Now().UTC()
	for i, h := range hints {
		dto := hintDTO{}
		if i < hintsRevealed {
			dto.TextHTML = mdx.Render(h.Text)
		}
		if arrival.IsZero() {
			dto.UnlocksInSeconds = h.DelaySeconds
		} else {
			unlockAt := arrival.Add(time.Duration(h.DelaySeconds) * time.Second)
			remaining := int(unlockAt.Sub(now).Round(time.Second).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			dto.UnlocksInSeconds = remaining
		}
		if len(h.Images) > 0 {
			imgs := make([]imageDTO, 0, len(h.Images))
			for _, im := range h.Images {
				imgs = append(imgs, imageDTO{
					URL: "/adventure-assets/hint-image/" + strconv.FormatInt(im.ID, 10),
					Alt: im.AltText,
				})
			}
			dto.Images = imgs
		}
		if len(h.Audio) > 0 {
			audio := make([]audioDTO, 0, len(h.Audio))
			for _, a := range h.Audio {
				audio = append(audio, audioDTO{
					URL:         "/adventure-assets/hint-audio/" + strconv.FormatInt(a.ID, 10),
					MimeType:    a.MimeType,
					Description: a.Description,
				})
			}
			dto.Audio = audio
		}
		out = append(out, dto)
	}
	return out
}

// enabledPages returns just the pages an author has kept active, in
// their original order. Disabled pages are preserved in the DB but are
// invisible to players.
func enabledPages(adv *adventure.Adventure) []adventure.Page {
	out := make([]adventure.Page, 0, len(adv.Pages))
	for _, p := range adv.Pages {
		if !p.Disabled {
			out = append(out, p)
		}
	}
	return out
}

// visibleFor returns the first enabled page at or after startIdx (where
// startIdx is a raw adventure_pages.idx value) plus its position in the
// enabled slice. The (nil, 0) result means "no more enabled pages —
// team is done". The pointer returned is borrowed from pages and must
// not be retained past the caller's use of the slice.
//
// Used for the legacy linear path that hasn't been migrated to the
// lane-aware resolver yet (e.g., the manifest's strict-assets gate
// where the cheap idx-watermark comparison is good enough). New
// progression code should call resolveCurrentPage instead.
func visibleFor(pages []adventure.Page, startIdx int) (*adventure.Page, int) {
	enabledIdx := 0
	for i := range pages {
		if pages[i].Disabled {
			continue
		}
		if pages[i].Index >= startIdx {
			return &pages[i], enabledIdx
		}
		enabledIdx++
	}
	return nil, 0
}

// resolveCurrentPage returns the page the team should be answering
// right now. Prefers the explicit current_page_id pointer (the
// post-lanes source of truth); falls back to the lane-aware first
// page resolver when the pointer is unset (a brand-new team mid-
// migration, or a team whose Initialize call hasn't run yet). Returns
// nil when the team has no active page — the caller should treat
// that as "completed" or "no current puzzle".
//
// When current_page_id points at a now-disabled page (an arranger
// disabled it mid-run), this walks forward through the team's
// permutation as if the page had been solved, so the player gets a
// playable next puzzle instead of a stuck cursor.
func resolveCurrentPage(ctx context.Context, db lanes.Execer, adv *adventure.Adventure, tc teamContext) (*adventure.Page, error) {
	if tc.CurrentPageID != 0 {
		if p := adv.PageByID(tc.CurrentPageID); p != nil && !p.Disabled {
			return p, nil
		}
		_, nextID, err := lanes.NextPageAfter(ctx, db, adv, tc.TeamID, tc.CurrentPageID)
		if err != nil {
			return nil, err
		}
		if nextID == 0 {
			return nil, nil
		}
		if p := adv.PageByID(nextID); p != nil && !p.Disabled {
			return p, nil
		}
		return nil, nil
	}
	_, firstID, err := lanes.FirstPageFor(ctx, db, adv, tc.TeamID)
	if err != nil {
		return nil, err
	}
	if firstID == 0 {
		return nil, nil
	}
	if p := adv.PageByID(firstID); p != nil && !p.Disabled {
		return p, nil
	}
	return nil, nil
}

// buildState is the shared state builder used by the HTTP and WS handlers.
func (h *Handlers) buildState(ctx context.Context, slot auth.Slot) (State, int, string) {
	tc, err := loadTeam(ctx, h.DB, slot)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, http.StatusNotFound, "team missing"
	}
	if err != nil {
		return State{}, http.StatusInternalServerError, "lookup failed"
	}
	adv := h.Lib.Get(tc.AdventureSlug)
	if adv == nil {
		return State{}, http.StatusInternalServerError, "adventure not loaded: " + tc.AdventureSlug
	}
	// Arranger-provided prose (prompt/hint/intro/outro) is stored as raw
	// markdown; the player client wants sanitised HTML so it can just
	// innerHTML it. Convert once per state build.
	resp := State{
		AdventureSlug:  adv.Slug,
		AdventureTitle: adv.Meta.Title,
		TeamName:       tc.TeamName,
		TeamToken:      tc.TeamToken,
		SlotName:       slot.Name,
		PageCount:      len(enabledPages(adv)),
		IntroText:      mdx.Render(adv.Meta.IntroText),
		OutroText:      mdx.Render(adv.Meta.OutroText),
		OrgName:        tc.OrgName,
		StartLabel:     adv.Start.Label,
	}
	if adv.Start.HasCoords() {
		resp.StartLat = adv.Start.Lat
		resp.StartLng = adv.Start.Lng
	}
	// Logo URL: only set when the owning org has actually uploaded
	// one. Players running an event for an unbranded org just see
	// the adventure title without a brand strip.
	if tc.OrgHasLogo {
		resp.OrgLogoURL = fmt.Sprintf("/org-logo/%d", tc.OrgID)
	}
	if !adv.Colors.Empty() {
		c := adv.Colors
		resp.Colors = &c
	}
	resp.ServerNow = time.Now().UTC().Format(time.RFC3339)
	if tc.CompletedAt.Valid {
		resp.Status = "completed"
		return resp, http.StatusOK, ""
	}
	// Lazy auto-start: if a scheduled event's time has arrived, promote
	// it to running right here so the player view doesn't have to wait
	// up to 15 s for the scheduler sweep. The write is idempotent via
	// the WHERE state = 'scheduled' guard — a concurrent sweep or
	// manual start loses the race cleanly.
	if tc.RunState == "scheduled" && tc.ScheduledAt.Valid {
		if started, err := maybePromoteScheduled(ctx, h.DB, h.Hub, h.Lib, tc.EventID, tc.ScheduledAt.String); err == nil && started {
			tc.RunState = "running"
			tc.StartedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
			h.publishEventStartedAuto(ctx, tc)
		}
	}
	switch tc.RunState {
	case "scheduled":
		resp.Status = "waiting"
		if tc.ScheduledAt.Valid {
			resp.ScheduledAt = tc.ScheduledAt.String
		}
		return resp, http.StatusOK, ""
	case "paused":
		resp.Status = "paused"
		return resp, http.StatusOK, ""
	case "completed":
		resp.Status = "completed"
		return resp, http.StatusOK, ""
	case "cancelled":
		// Distinguished from "completed" so the client can show an
		// honest "event cancelled" screen instead of pretending it
		// finished. Mutating endpoints already 409 because RunState
		// isn't "running"; this only changes what the GET returns.
		resp.Status = "cancelled"
		return resp, http.StatusOK, ""
	}
	page, err := resolveCurrentPage(ctx, h.DB, adv, tc)
	if err != nil {
		return State{}, http.StatusInternalServerError, "resolve page failed"
	}
	if page == nil {
		resp.Status = "completed"
		return resp, http.StatusOK, ""
	}
	resp.PageIndex = tc.CurrentPageIndex
	resp.Title = page.Title
	resp.NoAnswer = len(page.Accept) == 0
	// Gate info is attached when the puzzle has one — but we DON'T hide
	// the content behind it. Reaching the location is a prerequisite for
	// the answer (or the whole answer, on accept-less puzzles); the prompt,
	// hint, and images help the team recognise the place when they get
	// there, and teasing the content before arrival is the intended UX.
	if page.Location.Gated() {
		reached, _ := gateReached(ctx, h.DB, tc.TeamID, page.Location)
		resp.Gate = &gateDTO{
			Lat:     page.Location.Lat,
			Lng:     page.Location.Lng,
			Radius:  *page.Location.RadiusM,
			Reached: reached,
		}
	}
	resp.Status = "running"
	resp.Prompt = mdx.Render(page.Prompt)
	// Hints: render each markdown body, and compute per-hint unlock
	// countdown against the team's arrival time on this puzzle.
	// Falls back to the event's started_at when current_page_started_at
	// is NULL (team hasn't advanced since run start, so they've been
	// on page 0 since the whistle).
	//
	// HintsRevealed is read FIRST so buildHintDTOs can gate the
	// rendered hint TEXT to "already revealed" — without that, a
	// player opening devtools could read every hint body off the
	// state DTO without ever tapping reveal, defeating the whole
	// timed-hint mechanic. The unlocks_in_seconds countdown stays
	// in the DTO so the client can show the "reveal in N seconds"
	// timer; the text is what's hidden until the player taps.
	if len(page.Hints) > 0 {
		var n int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM team_hint_reveals WHERE team_id = $1 AND page_id = $2`,
			tc.TeamID, page.ID).Scan(&n); err == nil {
			resp.HintsRevealed = n
		}
	}
	resp.Hints = buildHintDTOs(page.Hints, teamArrival(tc), resp.HintsRevealed)
	if len(page.Images) > 0 {
		imgs := make([]imageDTO, 0, len(page.Images))
		for _, im := range page.Images {
			imgs = append(imgs, imageDTO{
				URL: "/adventure-assets/image/" + strconv.FormatInt(im.ID, 10),
				Alt: im.AltText,
			})
		}
		resp.Images = imgs
	}
	if len(page.Audio) > 0 {
		audio := make([]audioDTO, 0, len(page.Audio))
		for _, a := range page.Audio {
			audio = append(audio, audioDTO{
				URL:         "/adventure-assets/audio/" + strconv.FormatInt(a.ID, 10),
				MimeType:    a.MimeType,
				Description: a.Description,
			})
		}
		resp.Audio = audio
	}
	return resp, http.StatusOK, ""
}

// applyAdvance is the no-answer counterpart to applyAnswer — it pushes
// a team past a puzzle that has an empty accept list by tapping a
// button instead of typing a match. Honours the gate (403 when not
// reached) so the "Check location" button on gated no-answer puzzles
// can use the same server path.
func (h *Handlers) applyAdvance(ctx context.Context, slot auth.Slot) (AnswerResponse, int64, int, string) {
	tc, err := loadTeam(ctx, h.DB, slot)
	if err != nil {
		return AnswerResponse{}, 0, http.StatusInternalServerError, "lookup failed"
	}
	if tc.RunState == "scheduled" && tc.ScheduledAt.Valid {
		if started, _ := maybePromoteScheduled(ctx, h.DB, h.Hub, h.Lib, tc.EventID, tc.ScheduledAt.String); started {
			tc.RunState = "running"
			h.publishEventStartedAuto(ctx, tc)
		}
	}
	if tc.RunState != "running" {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "run is not active"
	}
	if tc.CompletedAt.Valid {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "team already finished"
	}
	adv := h.Lib.Get(tc.AdventureSlug)
	if adv == nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "no current page"
	}
	page, err := resolveCurrentPage(ctx, h.DB, adv, tc)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "resolve page failed"
	}
	if page == nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "no current page"
	}
	if len(page.Accept) != 0 {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "this puzzle expects an answer"
	}
	if page.Location.Gated() {
		reached, err := gateReached(ctx, h.DB, tc.TeamID, page.Location)
		if err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "gate check failed"
		}
		if !reached {
			return AnswerResponse{}, tc.EventID, http.StatusForbidden, "you must reach the location first"
		}
	}
	teamID := tc.TeamID
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "begin tx failed"
	}
	defer tx.Rollback() //nolint:errcheck

	activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
		activity.AnswerRight, map[string]any{"page_index": page.Index, "no_answer": true})
	nextLaneID, nextPageID, err := lanes.NextPageAfter(ctx, tx, adv, tc.TeamID, page.ID)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "resolve next failed"
	}
	nextIdx := tc.CurrentPageIndex + 1
	resp := AnswerResponse{Accepted: true}
	if nextPageID == 0 {
		res, err := tx.ExecContext(ctx, `
			UPDATE teams
			SET current_page_id = NULL,
			    current_lane_id = NULL,
			    current_page_index = $1,
			    current_page_started_at = now_text(),
			    completed_at = now_text()
			WHERE id = $2 AND current_page_id IS NOT DISTINCT FROM $3
		`, nextIdx, tc.TeamID, nullableID(page.ID))
		if err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return AnswerResponse{}, tc.EventID, http.StatusConflict, "team already advanced"
		}
		activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
			activity.TeamCompleted, nil)
		runCompleted, err := activity.MaybeCompleteEvent(ctx, tx, tc.EventID)
		if err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "complete-event check failed"
		}
		if err := tx.Commit(); err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "commit failed"
		}
		h.publishTeamCompleted(ctx, tc)
		if runCompleted {
			h.publishEventCompleted(ctx, tc)
		}
		resp.Completed = true
		resp.NextIndex = nextIdx
		return resp, tc.EventID, http.StatusOK, ""
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE teams
		SET current_page_id = $1,
		    current_lane_id = $2,
		    current_page_index = $3,
		    current_page_started_at = now_text()
		WHERE id = $4 AND current_page_id IS NOT DISTINCT FROM $5
	`, nextPageID, nullableID(nextLaneID), nextIdx, tc.TeamID, nullableID(page.ID))
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "team already advanced"
	}
	if err := tx.Commit(); err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "commit failed"
	}
	resp.NextIndex = nextIdx
	return resp, tc.EventID, http.StatusOK, ""
}

// GetState returns the player's current view.
func (h *Handlers) GetState(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	resp, status, errMsg := h.buildState(r.Context(), slot)
	if status != http.StatusOK {
		httpx.Error(w, status, errMsg)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// AssetManifest is the body of GET /api/player/manifest. The service
// worker on the player's device cache.addAll()s these URLs on team
// join so the assets are available offline. Hint assets ARE included
// even though the per-hint serve route is reveal-gated — the SW
// silently skips the 403s during prefetch and re-tries on cache miss
// once a hint is revealed (at which point the request succeeds and
// gets cached for the rest of the offline window).
type AssetManifest struct {
	AdventureSlug string `json:"adventure_slug"`
	// TeamID partitions the service-worker asset cache so a shared
	// device (kiosk, family iPad, sequential teams in the same
	// browser) can't serve Team A's prefetched bytes to Team B and
	// bypass the server-side canViewPageImage gate. The SW uses this
	// as the cache-name suffix and evicts any caches that don't
	// match when the active team changes.
	TeamID int64    `json:"team_id"`
	URLs   []string `json:"urls"`
}

// Manifest returns every puzzle/hint image+audio URL for the slot's
// adventure. The slot's team must be in a still-alive event;
// otherwise we don't ship URLs (the team has nothing to play).
//
// Visibility: each URL still goes through its own visibility gate at
// fetch time (canViewPageImage / canViewHintImage). The manifest
// itself doesn't bypass any check — it's just a list of URLs that
// MIGHT serve, so a misbehaving SW can't use it to peek at content
// it shouldn't see.
func (h *Handlers) Manifest(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	tc, err := loadTeam(r.Context(), h.DB, slot)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team missing")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	adv := h.Lib.Get(tc.AdventureSlug)
	if adv == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded")
		return
	}
	// Strict-assets adventures cap the manifest at pages the team has
	// already visited (solved or currently on). Hint assets stay
	// scoped per-hint by their own reveal gate regardless of this
	// flag. Visited-page detection now uses answer_attempts + the
	// team's current page id, so it works under shuffled lanes where
	// p.Index <-> progress is no longer a one-to-one mapping.
	visited := map[int64]bool{}
	if adv.StrictAssets {
		rows, err := h.DB.QueryContext(r.Context(), `
			SELECT DISTINCT p.id
			FROM answer_attempts aa
			JOIN adventure_pages p
			  ON p.adventure_id = $2 AND p.idx = aa.page_index
			WHERE aa.team_id = $1 AND aa.accepted = 1
		`, tc.TeamID, adv.ID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "manifest gate failed")
			return
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				httpx.Error(w, http.StatusInternalServerError, "manifest gate failed")
				return
			}
			visited[id] = true
		}
		rows.Close()
		if tc.CurrentPageID != 0 {
			visited[tc.CurrentPageID] = true
		}
	}
	urls := make([]string, 0, 32)
	for _, p := range adv.Pages {
		if p.Disabled {
			continue
		}
		if adv.StrictAssets && !visited[p.ID] {
			continue
		}
		for _, im := range p.Images {
			urls = append(urls, "/adventure-assets/image/"+strconv.FormatInt(im.ID, 10))
		}
		for _, au := range p.Audio {
			urls = append(urls, "/adventure-assets/audio/"+strconv.FormatInt(au.ID, 10))
		}
		for _, hint := range p.Hints {
			for _, im := range hint.Images {
				urls = append(urls, "/adventure-assets/hint-image/"+strconv.FormatInt(im.ID, 10))
			}
			for _, au := range hint.Audio {
				urls = append(urls, "/adventure-assets/hint-audio/"+strconv.FormatInt(au.ID, 10))
			}
		}
	}
	// Org logo joins the manifest when the operator has uploaded
	// one — keeps the play page's brand strip alive on offline
	// reloads. Same cache-first policy as adventure assets.
	if tc.OrgHasLogo {
		urls = append(urls, fmt.Sprintf("/org-logo/%d", tc.OrgID))
	}
	httpx.JSON(w, http.StatusOK, AssetManifest{
		AdventureSlug: tc.AdventureSlug,
		TeamID:        tc.TeamID,
		URLs:          urls,
	})
}

// gateReached checks whether the team's latest location ping places them
// inside the location's gate radius — but only credits pings that pass
// a chained-plausibility check. plausibleNextPing lets a ping land
// even when the gap to its predecessor exceeds maxLookbackForSpeedSec
// (the "free pass" for GPS dropouts), which would otherwise let a
// stationary player pause pings for two minutes and then post a
// single ping at the gate to satisfy it. Pulling the latest two pings
// here lets us reject the free-pass case for gating purposes — the
// player has to chain another plausible ping inside the window before
// the gate credits. parsePlayTime / DB errors fall through to the
// permissive branch so a flaky read doesn't lock real players out.
func gateReached(ctx context.Context, db *sql.DB, teamID int64, loc adventure.Location) (bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT lat, lng, server_ts FROM location_pings WHERE team_id = $1
		ORDER BY server_ts DESC LIMIT 2
	`, teamID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var (
		latLat, latLng            float64
		latTS, prevTS             time.Time
		gotLatest, gotPrev        bool
		latParseErr, prevParseErr bool
	)
	for rows.Next() {
		var lat, lng float64
		var serverTS string
		if err := rows.Scan(&lat, &lng, &serverTS); err != nil {
			return false, err
		}
		ts, perr := parsePlayTime(serverTS)
		if !gotLatest {
			latLat, latLng, latTS = lat, lng, ts
			gotLatest = true
			latParseErr = perr != nil
		} else {
			prevTS = ts
			gotPrev = true
			prevParseErr = perr != nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if !gotLatest {
		return false, nil
	}
	if gotPrev && !latParseErr && !prevParseErr {
		if latTS.Sub(prevTS).Seconds() > maxLookbackForSpeedSec {
			return false, nil
		}
	}
	return haversineMeters(latLat, latLng, loc.Lat, loc.Lng) <= *loc.RadiusM, nil
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

// AnswerRequest is the body of POST /api/player/answer.
//
// ClientActionID is an optional idempotency key the client generates
// per typed answer attempt. The offline replay queue retries failed
// requests with the same key — the server caches the original
// response keyed by (slot, action_id) and replays it instead of
// double-running applyAnswer. Older clients omit the field; the
// server falls back to non-idempotent behaviour, same as before.
type AnswerRequest struct {
	Answer         string `json:"answer"`
	ClientActionID string `json:"client_action_id,omitempty"`
}

// AnswerResponse is the reply.
//
// ClientActionID is echoed back when the request supplied one. The
// player's WS handler uses it to ack the matching entry in its
// at-least-once outbox so a queued submission stays on disk until
// the server has confirmed durability — we can't trust ws.send()
// returning success because that only proves the OS buffer accepted
// the frame, not that the server processed it.
type AnswerResponse struct {
	Accepted       bool   `json:"accepted"`
	Completed      bool   `json:"completed"`
	NextIndex      int    `json:"next_index,omitempty"`
	ClientActionID string `json:"client_action_id,omitempty"`
}

// AdvanceRequest is the body of the WS "advance" frame on no-answer
// puzzles. Only carries the optional idempotency key — there's no
// answer to validate, just a "tap to continue" signal.
type AdvanceRequest struct {
	ClientActionID string `json:"client_action_id,omitempty"`
}

// applyAnswer evaluates a submitted answer and persists the consequences.
// Returns (response, statusCode, errMsg). Also returns the eventID so the
// caller can broadcast to the hub.
func (h *Handlers) applyAnswer(ctx context.Context, slot auth.Slot, answer string) (AnswerResponse, int64, int, string) {
	tc, err := loadTeam(ctx, h.DB, slot)
	if err != nil {
		return AnswerResponse{}, 0, http.StatusInternalServerError, "lookup failed"
	}
	// Same lazy-promote as buildState — don't make the player wait on
	// the scheduler sweep if they click at the stroke of 0.
	if tc.RunState == "scheduled" && tc.ScheduledAt.Valid {
		if started, _ := maybePromoteScheduled(ctx, h.DB, h.Hub, h.Lib, tc.EventID, tc.ScheduledAt.String); started {
			tc.RunState = "running"
			h.publishEventStartedAuto(ctx, tc)
		}
	}
	if tc.RunState != "running" {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "run is not active"
	}
	if tc.CompletedAt.Valid {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "team already finished"
	}
	adv := h.Lib.Get(tc.AdventureSlug)
	if adv == nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "no current page"
	}
	page, err := resolveCurrentPage(ctx, h.DB, adv, tc)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "resolve page failed"
	}
	if page == nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "no current page"
	}
	if page.Location.Gated() {
		reached, err := gateReached(ctx, h.DB, tc.TeamID, page.Location)
		if err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "gate check failed"
		}
		if !reached {
			return AnswerResponse{}, tc.EventID, http.StatusForbidden, "you must reach the location first"
		}
	}
	accepted := page.CheckAnswer(answer)
	acceptedInt := 0
	if accepted {
		acceptedInt = 1
	}

	// Everything that follows mutates state, and the writes must
	// commit or roll back as a single unit — without the tx a partial
	// failure can leave (a) an answer_attempts row whose runevent never
	// landed, or (b) a team that advanced but whose AnswerRight
	// activity row is missing from the activity feed. Reads above this
	// point already finished, so the tx only spans the writes.
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "begin tx failed"
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO answer_attempts(team_id, slot_id, page_index, answer, accepted)
		VALUES ($1, $2, $3, $4, $5)
	`, tc.TeamID, slot.ID, page.Index, answer, acceptedInt); err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "record attempt failed"
	}
	teamID := tc.TeamID
	resp := AnswerResponse{Accepted: accepted}
	if !accepted {
		activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
			activity.AnswerWrong, map[string]any{"page_index": page.Index, "submitted": answer})
		if err := tx.Commit(); err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "commit failed"
		}
		return resp, tc.EventID, http.StatusOK, ""
	}
	activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
		activity.AnswerRight, map[string]any{"page_index": page.Index})
	// Advance past the page the player just solved. Lane-aware: the
	// next page comes from the team's permutation (or the next lane,
	// on lane exhaustion). Concurrency is guarded on current_page_id
	// so two slots on the same team submitting at the same instant
	// can't both land — second UPDATE finds zero rows, we roll back
	// the AnswerRight activity row, and surface a 409.
	nextLaneID, nextPageID, err := lanes.NextPageAfter(ctx, tx, adv, tc.TeamID, page.ID)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "resolve next failed"
	}
	nextIdx := tc.CurrentPageIndex + 1
	if nextPageID == 0 {
		res, err := tx.ExecContext(ctx, `
			UPDATE teams
			SET current_page_id = NULL,
			    current_lane_id = NULL,
			    current_page_index = $1,
			    current_page_started_at = now_text(),
			    completed_at = now_text()
			WHERE id = $2 AND current_page_id IS NOT DISTINCT FROM $3
		`, nextIdx, tc.TeamID, nullableID(page.ID))
		if err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return AnswerResponse{}, tc.EventID, http.StatusConflict, "team already advanced"
		}
		activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
			activity.TeamCompleted, nil)
		runCompleted, err := activity.MaybeCompleteEvent(ctx, tx, tc.EventID)
		if err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "complete-event check failed"
		}
		if err := tx.Commit(); err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "commit failed"
		}
		h.publishTeamCompleted(ctx, tc)
		if runCompleted {
			h.publishEventCompleted(ctx, tc)
		}
		resp.Completed = true
		resp.NextIndex = nextIdx
		return resp, tc.EventID, http.StatusOK, ""
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE teams
		SET current_page_id = $1,
		    current_lane_id = $2,
		    current_page_index = $3,
		    current_page_started_at = now_text()
		WHERE id = $4 AND current_page_id IS NOT DISTINCT FROM $5
	`, nextPageID, nullableID(nextLaneID), nextIdx, tc.TeamID, nullableID(page.ID))
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return AnswerResponse{}, tc.EventID, http.StatusConflict, "team already advanced"
	}
	if err := tx.Commit(); err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "commit failed"
	}
	resp.NextIndex = nextIdx
	return resp, tc.EventID, http.StatusOK, ""
}

// nullableID wraps a BIGINT into sql.NullInt64 — zero (no id) and
// negative (synthesized in-memory lane) values encode as SQL NULL,
// matching the FK column's nullability. Used by the progression
// UPDATEs so completed teams clear their cursor cleanly and so
// loader-synthesized lanes (which have no DB row) don't try to
// write a non-existent id into teams.current_lane_id.
func nullableID(id int64) any {
	if id <= 0 {
		return sql.NullInt64{}
	}
	return id
}

// SubmitAnswer evaluates a submitted answer for the slot's current page.
// Rate-limited per slot so puzzle answers — which are often short
// dictionary words — can't be brute-forced by a scripted client.
//
// When the client supplies a client_action_id, a successful (2xx)
// response is cached and replayed verbatim on retry — the offline
// queue replays actions on reconnect, and we don't want the server
// re-running applyAnswer (which would log a second answer_attempts
// row and try to advance from now-stale state). Errors are NOT
// cached so a transient "rate limited" or "run not active" doesn't
// stick across retries.
func (h *Handlers) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	var req AnswerRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	actionID := validClientActionID(req.ClientActionID)

	// Cache lookup before rate-limit + applyAnswer: a replay shouldn't
	// pay the limiter token nor re-evaluate the answer.
	if cached, found, err := lookupCachedAction(r.Context(), h.DB, slot.ID, actionID); err == nil && found {
		writeCached(w, cached)
		return
	}

	if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
		writeRateLimited(w, retry)
		return
	}
	resp, eventID, status, errMsg := h.applyAnswer(r.Context(), slot, req.Answer)
	if status != http.StatusOK {
		httpx.Error(w, status, errMsg)
		return
	}
	body, _ := json.Marshal(resp)
	recordAction(r.Context(), h.DB, slot.ID, actionID, status, body)
	if eventID != 0 {
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
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
// between two consecutive pings for the same team. 60 m/s is ~216
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
// recent ping on the same team and reject anything that implies
// a teleport above maxPlausibleSpeedMS. Rejections leave the prior
// ping in place so the next legitimate ping has a stable anchor.
func (h *Handlers) recordLocation(ctx context.Context, slot auth.Slot, req LocationRequest) (int, string) {
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		return http.StatusBadRequest, "coordinates out of range"
	}
	if !plausibleNextPing(ctx, h.DB, slot.TeamID, req.Lat, req.Lng) {
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
// reachable from the team's most recent ping at no more than
// maxPlausibleSpeedMS, OR when there's no prior ping to compare
// against / the gap is outside the lookback window. Errors and
// missing-row cases default to "plausible" — this is a cheating
// gate, not a hard correctness gate, so failing open is the right
// posture if the DB read flakes.
func plausibleNextPing(ctx context.Context, db *sql.DB, teamID int64, lat, lng float64) bool {
	var (
		lastLat, lastLng float64
		serverTS         string
	)
	err := db.QueryRowContext(ctx, `
		SELECT lat, lng, server_ts FROM location_pings
		WHERE team_id = $1
		ORDER BY server_ts DESC
		LIMIT 1
	`, teamID).Scan(&lastLat, &lastLng, &serverTS)
	if err != nil {
		// No prior ping (or DB hiccup) — let it through. The first
		// ping has nothing to anchor against.
		return true
	}
	prev, err := parsePlayTime(serverTS)
	if err != nil {
		return true
	}
	elapsed := time.Since(prev).Seconds()
	if elapsed < minLookbackForSpeedSec || elapsed > maxLookbackForSpeedSec {
		return true
	}
	d := haversineMeters(lat, lng, lastLat, lastLng)
	return d/elapsed <= maxPlausibleSpeedMS
}

// SummaryResponse is the shape returned by GET /api/player/summary.
// Populated once the team is in a meaningful "done enough" state — i.e.
// has walked and answered some — and consumed by the completion screen.
type SummaryResponse struct {
	TeamName        string  `json:"team_name"`
	AdventureTitle  string  `json:"adventure_title"`
	PageCount       int     `json:"page_count"`
	AnswersRight    int     `json:"answers_right"`
	AnswersWrong    int     `json:"answers_wrong"`
	DistanceM       float64 `json:"distance_m"`
	DurationSeconds int     `json:"duration_seconds"`
	StartedAt       string  `json:"started_at,omitempty"`
	CompletedAt     string  `json:"completed_at,omitempty"`
	// RecapToken is the public-by-token slug for /recap/{token}.
	// Only emitted once the event has reached a terminal state —
	// the recap page itself enforces the same gate, but omitting
	// the token mid-event keeps the in-app share button hidden so
	// players don't paste a link that 404s.
	RecapToken string `json:"recap_token,omitempty"`
}

// Summary returns end-of-run stats for the slot's team: right/wrong
// answers, distance walked, time taken.
func (h *Handlers) Summary(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())

	var (
		teamName      string
		adventureSlug string
		runStarted    sql.NullString
		teamDone      sql.NullString
		runState      string
		recapTok      string
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.name, ar.adventure_slug, ar.started_at, t.completed_at,
		       ar.state, COALESCE(t.recap_token, '')
		FROM teams t JOIN events ar ON ar.id = t.event_id
		WHERE t.id = $1
	`, slot.TeamID).Scan(&teamName, &adventureSlug, &runStarted, &teamDone,
		&runState, &recapTok)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	adv := h.Lib.Get(adventureSlug)
	if adv == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded: "+adventureSlug)
		return
	}
	resp := SummaryResponse{
		TeamName:       teamName,
		AdventureTitle: adv.Meta.Title,
		PageCount:      len(adv.Pages),
		StartedAt:      runStarted.String,
		CompletedAt:    teamDone.String,
	}
	// Surface the recap token only when the event has actually
	// reached a terminal state — the public /recap/{token} route
	// also enforces this, but withholding the token in the
	// summary keeps the in-app share button hidden mid-event so
	// no one pastes a link that immediately 404s.
	if recapTok != "" && (runState == "completed" || runState == "cancelled") {
		resp.RecapToken = recapTok
	}

	// Right/wrong counts.
	var right, wrong int
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT
		  COALESCE(SUM(CASE WHEN accepted = 1 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN accepted = 0 THEN 1 ELSE 0 END), 0)
		FROM answer_attempts WHERE team_id = $1
	`, slot.TeamID).Scan(&right, &wrong); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "attempts lookup failed")
		return
	}
	resp.AnswersRight = right
	resp.AnswersWrong = wrong

	// Distance: walk the team's pings in chronological order and sum
	// haversine deltas. Skipping consecutive duplicates keeps stationary
	// noise from inflating the number.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT lat, lng FROM location_pings
		WHERE team_id = $1 ORDER BY server_ts, id
	`, slot.TeamID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "pings lookup failed")
		return
	}
	defer rows.Close()
	var (
		hasPrev          bool
		prevLat, prevLng float64
		dist             float64
	)
	for rows.Next() {
		var lat, lng float64
		if err := rows.Scan(&lat, &lng); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "ping scan failed")
			return
		}
		if hasPrev {
			dist += haversineMeters(prevLat, prevLng, lat, lng)
		}
		prevLat, prevLng = lat, lng
		hasPrev = true
	}
	resp.DistanceM = dist

	// Duration.
	if resp.StartedAt != "" && resp.CompletedAt != "" {
		start, sErr := parsePlayTime(resp.StartedAt)
		end, eErr := parsePlayTime(resp.CompletedAt)
		if sErr == nil && eErr == nil {
			secs := int(end.Sub(start).Seconds())
			if secs < 0 {
				secs = 0
			}
			resp.DurationSeconds = secs
		}
	}

	httpx.JSON(w, http.StatusOK, resp)
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

// HintRequest is the body of POST /api/play/hint. The page is implicit
// (it's the team's current page); the index is the next sequential
// hint the client wants to reveal. Sequential because the UI shows
// hints in author order — skipping ahead would let a player "spend"
// a later hint without ever seeing the earlier scaffolding.
type HintRequest struct {
	HintIndex      int    `json:"hint_index"`
	ClientActionID string `json:"client_action_id,omitempty"`
}

// HintResponse mirrors the relevant slice of buildState — the client
// only needs the new revealed count to repaint the sheet without
// waiting for the WS state push to land.
type HintResponse struct {
	HintsRevealed int `json:"hints_revealed"`
	// RevealedText is the rendered HTML body of the hint that was
	// just unlocked. The state DTO doesn't ship hint text for
	// unrevealed hints (so devtools can't peek), so a fresh reveal
	// has to deliver the body via the response itself — otherwise
	// the player would see an empty hint card until the next WS
	// state push lands. Empty when the call was a no-op replay
	// (hint already revealed).
	RevealedText string `json:"revealed_text,omitempty"`
}

// RevealHint records that the team has revealed one more hint on the
// current puzzle. Idempotent on (team, page, hint) so a double-tap
// doesn't 4xx; sequential — the request must name the next hint, so a
// client can't skip past locked hints. Publishes to the event topic
// so team-mates' WS connections pull a refreshed state with the new
// HintsRevealed count.
//
// Server-side timer validation: `now - team.current_page_started_at >=
// hint.delay_seconds`. The client already gates its UI on
// UnlocksInSeconds=0, but a misclocked or malicious client could POST
// early. The 403 here is the load-bearing check.
//
// client_action_id: optional idempotency key. The (team, page, hint)
// upsert + sequential gate already make repeat calls safe, so the
// dedup is mostly a courtesy that lets retries return the cached
// success body verbatim instead of re-doing the count + insert dance.
func (h *Handlers) RevealHint(w http.ResponseWriter, r *http.Request) {
	var req HintRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	actionID := validClientActionID(req.ClientActionID)
	if cached, found, err := lookupCachedAction(r.Context(), h.DB, slot.ID, actionID); err == nil && found {
		writeCached(w, cached)
		return
	}
	tc, err := loadTeam(r.Context(), h.DB, slot)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team missing")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if tc.RunState != "running" {
		httpx.Error(w, http.StatusConflict, "event is not running")
		return
	}
	adv := h.Lib.Get(tc.AdventureSlug)
	if adv == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded")
		return
	}
	page, err := resolveCurrentPage(r.Context(), h.DB, adv, tc)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "resolve page failed")
		return
	}
	if page == nil {
		httpx.Error(w, http.StatusConflict, "no current puzzle")
		return
	}
	if req.HintIndex < 0 || req.HintIndex >= len(page.Hints) {
		httpx.Error(w, http.StatusBadRequest, "hint_index out of range")
		return
	}
	// Server-side timer gate. arrival is the moment the team reached
	// this puzzle (current_page_started_at, with a sensible page-0
	// fallback to event start). If the team has no arrival yet, the
	// run is degenerate and we refuse — the client shouldn't be able
	// to reach this point under normal flow.
	arrival := teamArrival(tc)
	if arrival.IsZero() {
		httpx.Error(w, http.StatusForbidden, "team has not started this puzzle yet")
		return
	}
	delay := time.Duration(page.Hints[req.HintIndex].DelaySeconds) * time.Second
	if elapsed := time.Since(arrival); elapsed < delay {
		// Tell the client how long they have to wait. The offline
		// replay queue uses Retry-After to back off and re-fire;
		// the same hint reveal will succeed once the timer's up.
		retry := int(math.Ceil((delay - elapsed).Seconds()))
		if retry < 1 {
			retry = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		httpx.Error(w, http.StatusForbidden, "hint not yet unlocked")
		return
	}
	// Sequential check: the next reveal must be the row after the
	// highest one already stored. This rejects out-of-order requests
	// from a stale tab without making the API order-tolerant.
	var revealed int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM team_hint_reveals WHERE team_id = $1 AND page_id = $2`,
		tc.TeamID, page.ID).Scan(&revealed); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "count failed")
		return
	}
	if req.HintIndex != revealed {
		// Already revealed (or skipping ahead). Idempotent: return the
		// authoritative count so the client can resync without an
		// error UI. Cache so a retry of the same action_id replays the
		// same body without another count query.
		body, _ := json.Marshal(HintResponse{HintsRevealed: revealed})
		recordAction(r.Context(), h.DB, slot.ID, actionID, http.StatusOK, body)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO team_hint_reveals(team_id, page_id, hint_index)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (team_id, page_id, hint_index) DO NOTHING`,
		tc.TeamID, page.ID, req.HintIndex); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "insert failed", err)
		return
	}
	// Push so other team-mates' WS state refreshes immediately with
	// the new revealed count. Same shape as SubmitAnswer.
	h.Hub.Publish(ws.EventTopic(tc.EventID), "changed", nil)
	// Ship the just-revealed hint's rendered body alongside the
	// count so the player who tapped sees the text instantly. The
	// state DTO no longer ships text for unrevealed hints, so this
	// response is the bridge from "tap" to "render" — otherwise
	// the card would flash empty until the next WS state push.
	revealedText := mdx.Render(page.Hints[req.HintIndex].Text)
	body, _ := json.Marshal(HintResponse{
		HintsRevealed: revealed + 1,
		RevealedText:  revealedText,
	})
	recordAction(r.Context(), h.DB, slot.ID, actionID, http.StatusOK, body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
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
