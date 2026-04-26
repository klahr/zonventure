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
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/mdx"
	"git.sr.ht/~klahr/zv/internal/activity"
	"git.sr.ht/~klahr/zv/internal/webhooks"
	"git.sr.ht/~klahr/zv/internal/ws"
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
		// Best-effort: let the /play page render without a session so the UI
		// can say "open your magic link again" rather than a blank error.
		return false
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
		httpx.Error(w, http.StatusInternalServerError, "wipe pings: "+err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM answer_attempts WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "wipe answers: "+err.Error())
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
		httpx.Error(w, http.StatusInternalServerError, "wipe slot: "+err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM slot_sessions WHERE slot_id = $1`, slot.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "wipe sessions: "+err.Error())
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
	TeamID               int64
	TeamName             string
	EventID                int64
	OrgID                int64
	AdventureSlug        string
	RunState             string
	ScheduledAt          sql.NullString
	StartedAt            sql.NullString
	CurrentPageIndex     int
	CurrentPageStartedAt sql.NullString
	CompletedAt          sql.NullString
}

// publishEventStartedAuto emits event.started for an event that was just
// auto-promoted (player arrived at the scheduled time). Distinct
// from the manual-start webhook in admin/handlers.go via the "manual"
// field on the payload.
func (h *Handlers) publishEventStartedAuto(ctx context.Context, tc teamContext) {
	advTitle := ""
	if adv := h.Lib.Get(tc.AdventureSlug); adv != nil {
		advTitle = adv.Meta.Title
	}
	webhooks.TryPublish(ctx, h.DB, tc.OrgID, webhooks.KindEventStarted, map[string]any{
		"event_id":          tc.EventID,
		"adventure_slug":  tc.AdventureSlug,
		"adventure_title": advTitle,
		"manual":          false,
	})
}

// publishTeamCompleted emits the team.completed webhook after the
// team's last-page UPDATE has committed. Best-effort: a failed
// enqueue logs and is dropped (the player has already seen "you're
// done!").
func (h *Handlers) publishTeamCompleted(ctx context.Context, tc teamContext) {
	advTitle := ""
	if adv := h.Lib.Get(tc.AdventureSlug); adv != nil {
		advTitle = adv.Meta.Title
	}
	webhooks.TryPublish(ctx, h.DB, tc.OrgID, webhooks.KindTeamCompleted, map[string]any{
		"event_id":          tc.EventID,
		"team_id":         tc.TeamID,
		"team_name":       tc.TeamName,
		"adventure_slug":  tc.AdventureSlug,
		"adventure_title": advTitle,
	})
}

// publishEventCompleted emits event.completed when MaybeCompleteEvent
// transitioned the event. Same best-effort discipline.
func (h *Handlers) publishEventCompleted(ctx context.Context, tc teamContext) {
	advTitle := ""
	if adv := h.Lib.Get(tc.AdventureSlug); adv != nil {
		advTitle = adv.Meta.Title
	}
	webhooks.TryPublish(ctx, h.DB, tc.OrgID, webhooks.KindEventCompleted, map[string]any{
		"event_id":          tc.EventID,
		"adventure_slug":  tc.AdventureSlug,
		"adventure_title": advTitle,
	})
}

// maybePromoteScheduled atomically flips a scheduled event to running
// when its scheduled_at has arrived. Returns (true, nil) on a
// successful promotion so the caller can carry on as if the event were
// already running; a lost race (manual start, concurrent sweep) is
// returned as (false, nil). The intent is to remove the "player
// arrived at 00:00 but still waits 15 s for the scheduler" UX hiccup.
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
	`, eventID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
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
	err := db.QueryRowContext(ctx, `
		SELECT t.id, t.name, ar.id, ar.org_id, ar.adventure_slug, ar.state,
		       ar.scheduled_at, ar.started_at,
		       t.current_page_index, t.current_page_started_at, t.completed_at
		FROM teams t JOIN events ar ON ar.id = t.event_id
		WHERE t.id = $1
	`, slot.TeamID).Scan(&tc.TeamID, &tc.TeamName, &tc.EventID, &tc.OrgID, &tc.AdventureSlug,
		&tc.RunState, &tc.ScheduledAt, &tc.StartedAt,
		&tc.CurrentPageIndex, &tc.CurrentPageStartedAt, &tc.CompletedAt)
	return tc, err
}

// State is the response body for GET /api/player/state and the "state" WS
// push kind.
type State struct {
	Status         string            `json:"status"` // waiting | running | paused | completed | gated
	AdventureSlug  string            `json:"adventure_slug,omitempty"`
	AdventureTitle string            `json:"adventure_title,omitempty"`
	TeamName       string            `json:"team_name,omitempty"`
	SlotName       string            `json:"slot_name,omitempty"`
	PageIndex      int               `json:"page_index,omitempty"`
	PageCount      int               `json:"page_count,omitempty"`
	Title          string            `json:"title,omitempty"`
	Prompt         string            `json:"prompt,omitempty"`
	// Hints carries the puzzle's authored hints with their unlock
	// schedule computed against the team's arrival time on this
	// puzzle. Empty slice when the puzzle has no hints — the client
	// hides the reveal button entirely.
	Hints  []hintDTO         `json:"hints,omitempty"`
	Images []imageDTO        `json:"images,omitempty"`
	Gate   *gateDTO          `json:"gate,omitempty"`
	Colors *adventure.Colors `json:"colors,omitempty"`
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
}

type imageDTO struct {
	URL string `json:"url"`
}

// hintDTO is one hint shipped to the player. TextHTML is the rendered
// markdown (same pipeline as prompt). UnlocksInSeconds counts down
// from the server's "now" to the moment this hint becomes revealable;
// <=0 means the client can show it right now. Order matches the
// author's sequence — the client reveals them one at a time in that
// order via a single "Reveal hint" button.
type hintDTO struct {
	TextHTML         string `json:"text_html"`
	UnlocksInSeconds int    `json:"unlocks_in_seconds"`
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
func buildHintDTOs(hints []adventure.Hint, arrival time.Time) []hintDTO {
	out := make([]hintDTO, 0, len(hints))
	now := time.Now().UTC()
	for _, h := range hints {
		dto := hintDTO{TextHTML: mdx.Render(h.Text)}
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
		SlotName:       slot.Name,
		PageCount:      len(enabledPages(adv)),
		IntroText:      mdx.Render(adv.Meta.IntroText),
		OutroText:      mdx.Render(adv.Meta.OutroText),
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
		if started, err := maybePromoteScheduled(ctx, h.DB, h.Hub, tc.EventID, tc.ScheduledAt.String); err == nil && started {
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
	}
	page, enabledIdx := visibleFor(adv.Pages, tc.CurrentPageIndex)
	if page == nil {
		resp.Status = "completed"
		return resp, http.StatusOK, ""
	}
	resp.PageIndex = enabledIdx
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
	resp.Hints = buildHintDTOs(page.Hints, teamArrival(tc))
	if len(page.Images) > 0 {
		imgs := make([]imageDTO, 0, len(page.Images))
		for _, im := range page.Images {
			imgs = append(imgs, imageDTO{URL: "/adventure-assets/image/" + strconv.FormatInt(im.ID, 10)})
		}
		resp.Images = imgs
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
		if started, _ := maybePromoteScheduled(ctx, h.DB, h.Hub, tc.EventID, tc.ScheduledAt.String); started {
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
	page, _ := visibleFor(adv.Pages, tc.CurrentPageIndex)
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
	// Same atomicity argument as applyAnswer: the AnswerRight runevent
	// and the team UPDATE must commit together, or the activity feed
	// disagrees with the team's actual page index.
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "begin tx failed"
	}
	defer tx.Rollback() //nolint:errcheck

	activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
		activity.AnswerRight, map[string]any{"page_index": page.Index, "no_answer": true})
	nextIdx := page.Index + 1
	resp := AnswerResponse{Accepted: true}
	if next, _ := visibleFor(adv.Pages, nextIdx); next == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE teams SET current_page_index = $1, current_page_started_at = now_text(), completed_at = now_text() WHERE id = $2
		`, nextIdx, tc.TeamID); err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
		}
		activity.Record(ctx, tx, tc.EventID, &teamID, activity.SlotActor(slot.ID),
			activity.TeamCompleted, nil)
		// If this team was the last one to finish, the event itself
		// transitions to 'completed' too. Caller pushes the broadcast
		// after we return (it already does on any answer).
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
	if _, err := tx.ExecContext(ctx, `
		UPDATE teams SET current_page_index = $1, current_page_started_at = now_text() WHERE id = $2
	`, nextIdx, tc.TeamID); err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
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

// gateReached checks whether the team's latest location ping places them
// inside the location's gate radius.
func gateReached(ctx context.Context, db *sql.DB, teamID int64, loc adventure.Location) (bool, error) {
	var lat, lng float64
	err := db.QueryRowContext(ctx, `
		SELECT lat, lng FROM location_pings WHERE team_id = $1
		ORDER BY server_ts DESC LIMIT 1
	`, teamID).Scan(&lat, &lng)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return haversineMeters(lat, lng, loc.Lat, loc.Lng) <= *loc.RadiusM, nil
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
type AnswerRequest struct {
	Answer string `json:"answer"`
}

// AnswerResponse is the reply.
type AnswerResponse struct {
	Accepted  bool `json:"accepted"`
	Completed bool `json:"completed"`
	NextIndex int  `json:"next_index,omitempty"`
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
		if started, _ := maybePromoteScheduled(ctx, h.DB, h.Hub, tc.EventID, tc.ScheduledAt.String); started {
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
	// The player is answering the first enabled page at or after their
	// stored cursor — NOT necessarily adv.Pages[tc.CurrentPageIndex],
	// because authors may have disabled one or more pages in between.
	page, _ := visibleFor(adv.Pages, tc.CurrentPageIndex)
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
	// Advance past the page the player just solved. The NEXT call to
	// buildState will re-resolve the new visible page (which will skip
	// any disabled pages that follow).
	nextIdx := page.Index + 1
	if next, _ := visibleFor(adv.Pages, nextIdx); next == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE teams SET current_page_index = $1, current_page_started_at = now_text(), completed_at = now_text() WHERE id = $2
		`, nextIdx, tc.TeamID); err != nil {
			return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
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
	if _, err := tx.ExecContext(ctx, `
		UPDATE teams SET current_page_index = $1, current_page_started_at = now_text() WHERE id = $2
	`, nextIdx, tc.TeamID); err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "save progress failed"
	}
	if err := tx.Commit(); err != nil {
		return AnswerResponse{}, tc.EventID, http.StatusInternalServerError, "commit failed"
	}
	resp.NextIndex = nextIdx
	return resp, tc.EventID, http.StatusOK, ""
}

// SubmitAnswer evaluates a submitted answer for the slot's current page.
// Rate-limited per slot so puzzle answers — which are often short
// dictionary words — can't be brute-forced by a scripted client.
func (h *Handlers) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	var req AnswerRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
		writeRateLimited(w, retry)
		return
	}
	resp, eventID, status, errMsg := h.applyAnswer(r.Context(), slot, req.Answer)
	if status != http.StatusOK {
		httpx.Error(w, status, errMsg)
		return
	}
	if eventID != 0 {
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	httpx.JSON(w, http.StatusOK, resp)
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

// recordLocation inserts a ping row. Returns (statusCode, errMsg).
func (h *Handlers) recordLocation(ctx context.Context, slot auth.Slot, req LocationRequest) (int, string) {
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		return http.StatusBadRequest, "coordinates out of range"
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
}

// Summary returns end-of-run stats for the slot's team: right/wrong
// answers, distance walked, time taken.
func (h *Handlers) Summary(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())

	var (
		teamName     string
		adventureSlug string
		runStarted   sql.NullString
		teamDone     sql.NullString
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.name, ar.adventure_slug, ar.started_at, t.completed_at
		FROM teams t JOIN events ar ON ar.id = t.event_id
		WHERE t.id = $1
	`, slot.TeamID).Scan(&teamName, &adventureSlug, &runStarted, &teamDone)
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
		hasPrev        bool
		prevLat, prevLng float64
		dist           float64
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
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
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
	httpx.Error(w, http.StatusTooManyRequests, "too many attempts, slow down")
}
