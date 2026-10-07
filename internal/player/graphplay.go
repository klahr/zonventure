package player

// Player endpoints for the graph engine (internal/play), which the React
// player app speaks.
//
//	GET  /api/player/play            where the team is and what it sees
//	POST /api/player/play/{action}   begin, answer, continue, choose, skip, leave, timeout, hint, pick, vote
//	POST /api/player/play/photo      a picture for a photo block (blocks.go)
//	GET  /api/player/play/photo/{id} a picture the team took
//
// Every action carries the node the player is looking at. A teammate who
// answered first moves the team, and the second submit is refused as
// stale rather than applied to a question its sender has never seen.
// Actions also carry a client_action_id, and a successful response is
// replayed verbatim on retry, so the offline outbox can re-send safely.

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/eventmsg"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// engine returns the graph engine. Handlers built directly in a test
// may leave it nil; assigning a default here would race between
// requests, so a fresh one is built instead.
// ForPhone is middleware marking a slot's requests as its phone's, so
// the views and actions they run are built for that player: a block
// shown only "here" asks where this phone is (play.ForPhone).
func ForPhone(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slot, ok := auth.SlotFromContext(r.Context()); ok {
			r = r.WithContext(play.ForPhone(r.Context(), slot.ID))
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) engine() *play.Engine {
	if h.Engine != nil {
		return h.Engine
	}
	return play.New(h.DB)
}

// PlayTeam is the team as the player app shows it. Locked says the team
// has begun (play.TeamBegun), so its players can no longer change their
// names or its picture; organisers still can.
type PlayTeam struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Locked    bool   `json:"locked,omitempty"`
}

// PlayEvent is the event as the player app shows it.
type PlayEvent struct {
	ID             int64  `json:"id"`
	State          string `json:"state"`
	ScheduledAt    string `json:"scheduled_at,omitempty"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	OrgName        string `json:"org_name,omitempty"`
	OrgLogoURL     string `json:"org_logo_url,omitempty"`
	// AdventureIconURL is the adventure's own picture, when it has one:
	// the phone shows it beside the menu, the organisation's logo failing it.
	AdventureIconURL string `json:"adventure_icon_url,omitempty"`
	// IntroText and OutroText are the author's welcome and farewell,
	// Markdown. The client renders them with the same safe renderer as
	// puzzle text.
	IntroText string `json:"intro_text,omitempty"`
	OutroText string `json:"outro_text,omitempty"`
	// Start is where the team should meet, when the author set it.
	Start *PlayStart `json:"start,omitempty"`
	// ServerNow lets the lobby count down to ScheduledAt against the
	// server's clock rather than the phone's.
	ServerNow string `json:"server_now,omitempty"`
	// Cheat says the adventure has a cheat password, so the phone offers
	// its hidden way to enter one.
	Cheat bool `json:"cheat,omitempty"`
	// Theme is the starter theme a party adventure's look was taken from
	// (adventures.trail_theme), for the phone to show its cover picture.
	Theme string `json:"theme,omitempty"`
	// Cover is the adventure's own cover picture (a media id the team
	// may fetch), shown in place of the starter theme's.
	Cover int64 `json:"cover,omitempty"`
}

// PlayStart is the adventure's meeting point. RadiusM is set when teams
// can only begin within that many metres of it.
type PlayStart struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Label   string  `json:"label,omitempty"`
	RadiusM float64 `json:"radius_m,omitempty"`
}

// PlayMe is the signed-in player: one slot of the team.
type PlayMe struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
}

// PlayFinish is what the end screen tells a finished team.
type PlayFinish struct {
	// Rank is the team's place among the event's started teams, of Teams.
	// It can still change while others play.
	Rank  int `json:"rank,omitempty"`
	Teams int `json:"teams,omitempty"`
	// Seconds is how long the team took, start to finish.
	Seconds int `json:"seconds,omitempty"`
	// RecapURL is the team's recap page. It opens once the event ends.
	RecapURL string `json:"recap_url,omitempty"`
}

// PlayState is the body of GET /api/player/play.
type PlayState struct {
	Team  PlayTeam  `json:"team"`
	Me    PlayMe    `json:"me"`
	Event PlayEvent `json:"event"`
	// Finish is set once the team has finished.
	Finish *PlayFinish `json:"finish,omitempty"`
	// Brand is the adventure's palette.
	Brand *adventure.Colors `json:"brand,omitempty"`
	// OrgBrand is the running organisation's colours, for the app's own
	// chrome (bar, menu, welcome) and for steps of an adventure that has
	// no palette of its own.
	OrgBrand *adventure.Colors `json:"org_brand,omitempty"`
	// View is set once the event has started.
	View *play.View `json:"view,omitempty"`
	// Prefetch is every media id the team may fetch now. The player app
	// downloads them ahead of need, so pictures show in a dead spot.
	Prefetch []int64 `json:"prefetch,omitempty"`
	// Messages are what the organisers have told this team, newest
	// first: information beside the game, shown in the app's inbox.
	Messages []eventmsg.Message `json:"messages"`
	// Dev is set for a dev ticket, playing the draft as it is edited.
	Dev *PlayDev `json:"dev,omitempty"`
}

// GetPlay returns the team's current state.
func (h *Handlers) GetPlay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slot, _ := auth.SlotFromContext(ctx)
	tc, err := loadTeam(ctx, h.DB, slot)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
		return
	}
	// A player opening a due event starts it.
	if tc.RunState == "scheduled" && tc.ScheduledAt.Valid {
		if promoted, err := maybePromoteScheduled(ctx, h.DB, h.Hub, tc.EventID, tc.ScheduledAt.String); err == nil && promoted {
			h.publishEventStartedAuto(ctx, tc)
			if tc, err = loadTeam(ctx, h.DB, slot); err != nil {
				httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
				return
			}
		}
	}

	out := PlayState{
		Team: PlayTeam{ID: tc.TeamID, Name: tc.TeamName, Token: tc.TeamToken, AvatarURL: teamAvatarURL(tc)},
		Me:   PlayMe{ID: slot.ID, Name: slot.Name},
		Event: PlayEvent{
			ID: tc.EventID, State: tc.RunState, ScheduledAt: tc.ScheduledAt.String, OrgName: tc.OrgName,
			ServerNow: time.Now().UTC().Format(time.RFC3339),
		},
	}
	look, err := adventureLook(ctx, h.DB, tc.AdventureSlug)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading adventure failed", err)
		return
	}
	out.Event.AdventureTitle, out.Event.IntroText, out.Event.OutroText = look.title, look.intro, look.outro
	out.Event.Start = look.start
	out.Event.Cheat = look.cheat
	out.Event.Theme = look.theme
	if s := look.brand.Style; s != nil {
		out.Event.Cover = s.Cover
	}
	if !look.brand.Empty() {
		out.Brand = &look.brand
	}
	if tc.OrgHasLogo {
		out.Event.OrgLogoURL = fmt.Sprintf("/org-logo/%d", tc.OrgID)
	}
	if look.icon {
		out.Event.AdventureIconURL = "/adventure-assets/avatar/" + url.PathEscape(tc.AdventureSlug)
	}
	if ob, err := orgBrand(ctx, h.DB, tc.OrgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading organisation failed", err)
		return
	} else if !ob.Empty() {
		out.OrgBrand = &ob
	}
	if out.Messages, err = eventmsg.ForTeam(ctx, h.DB, tc.EventID, tc.TeamID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading messages failed", err)
		return
	}
	if out.Dev, err = devInfo(ctx, h.DB, tc.EventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the dev steps failed", err)
		return
	}
	if out.Team.Locked, err = play.TeamBegun(ctx, h.DB, tc.TeamID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
		return
	}
	if tc.RunState == "scheduled" || tc.RunState == "cancelled" {
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	view, err := h.engine().Snapshot(ctx, tc.TeamID)
	if errors.Is(err, play.ErrNoGraph) && tc.RunState == "running" {
		// A team added after the start — a ticket bought for a running
		// quest — has no state yet. Placing it is StartEvent's job, and
		// StartEvent leaves teams that already have state alone.
		if err = play.Start(ctx, h.DB, tc.EventID); err == nil {
			view, err = h.engine().Snapshot(ctx, tc.TeamID)
		}
	}
	if err != nil && !errors.Is(err, play.ErrNoGraph) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading game failed", err)
		return
	}
	if err == nil {
		if err = decorateView(ctx, h.DB, tc.TeamID, slot.ID, &view); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading game failed", err)
			return
		}
		out.View = &view
		// The adventure's own words, in the team's language where translated.
		if s := view.Meta["title"]; s != "" {
			out.Event.AdventureTitle = s
		}
		if s := view.Meta["intro"]; s != "" {
			out.Event.IntroText = s
		}
		if s := view.Meta["outro"]; s != "" {
			out.Event.OutroText = s
		}
		if out.Prefetch, err = h.engine().Media(ctx, tc.TeamID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading media failed", err)
			return
		}
		if view.Finished {
			if out.Finish, err = h.finishOf(ctx, tc.EventID, tc.TeamID); err != nil {
				httpx.ServerErr(w, http.StatusInternalServerError, "loading result failed", err)
				return
			}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// finishOf reads a finished team's place, time and recap link.
func (h *Handlers) finishOf(ctx context.Context, eventID, teamID int64) (*PlayFinish, error) {
	var (
		f         PlayFinish
		recap     string
		started   time.Time
		completed sql.NullTime
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(t.recap_token, ''), s.started_at, s.completed_at
		FROM teams t JOIN team_graph_state s ON s.team_id = t.id
		WHERE t.id = $1`, teamID).Scan(&recap, &started, &completed)
	if err != nil {
		return nil, err
	}
	if recap != "" {
		f.RecapURL = "/recap/" + recap
	}
	if completed.Valid && completed.Time.After(started) {
		f.Seconds = int(completed.Time.Sub(started).Seconds())
	}
	ranked, err := h.engine().EventRanking(ctx, eventID)
	if err != nil {
		return nil, err
	}
	for i, r := range ranked {
		if r.TeamID == teamID {
			f.Rank, f.Teams = i+1, len(ranked)
		}
	}
	return &f, nil
}

// adventureLook reads what the player app shows of the adventure itself.
// One small query, because this runs on every push and poll —
// adventure.Library.Get would load every page, hint and image to answer
// it.
type look struct {
	title, intro, outro string
	theme               string
	brand               adventure.Colors
	start               *PlayStart
	cheat               bool
	icon                bool
}

func adventureLook(ctx context.Context, db *sql.DB, slug string) (look, error) {
	var (
		l                             look
		accent, second, bg, ink, dock sql.NullString
		lat, lng, radius              sql.NullFloat64
		label                         sql.NullString
		backdrop, style               []byte
	)
	err := db.QueryRowContext(ctx, `
		SELECT title, intro_text, outro_text,
		       accent_color, accent_secondary_color, background_color, text_color, submit_bg_color, font,
		       heading_font, corners, cards, card_outline, backdrop, look_style,
		       start_lat, start_lng, start_label, start_radius_m, (cheat_password IS NOT NULL), (avatar_mime_type IS NOT NULL),
		       trail_theme
		FROM adventures WHERE slug = $1
	`, slug).Scan(&l.title, &l.intro, &l.outro, &accent, &second, &bg, &ink, &dock, &l.brand.Font,
		&l.brand.HeadingFont, &l.brand.Corners, &l.brand.Cards, &l.brand.CardOutline, &backdrop, &style, &lat, &lng, &label, &radius, &l.cheat, &l.icon,
		&l.theme)
	if errors.Is(err, sql.ErrNoRows) {
		return look{}, nil
	}
	l.brand = adventure.Colors{
		Accent: accent.String, AccentSecondary: second.String, Background: bg.String,
		Text: ink.String, SubmitBackground: dock.String, Font: l.brand.Font,
		HeadingFont: l.brand.HeadingFont, Corners: l.brand.Corners, Cards: l.brand.Cards, CardOutline: l.brand.CardOutline,
	}
	if backdrop != nil {
		var b adventure.Backdrop
		if json.Unmarshal(backdrop, &b) == nil {
			l.brand.Backdrop = &b
		}
	}
	if style != nil {
		var s adventure.Style
		if json.Unmarshal(style, &s) == nil {
			l.brand.Style = &s
		}
	}
	if lat.Valid && lng.Valid {
		l.start = &PlayStart{Lat: lat.Float64, Lng: lng.Float64, Label: label.String, RadiusM: radius.Float64}
	}
	return l, err
}

// PlayActionRequest is the body of POST /api/player/play/{action}.
type PlayActionRequest struct {
	Node  string `json:"node"`
	Guess string `json:"guess,omitempty"`
	Port  string `json:"port,omitempty"`
	Index int    `json:"index,omitempty"`
	// Lang is the language for the "language" action.
	Lang string `json:"lang,omitempty"`
	// Password is the adventure's cheat password, for "cheat".
	Password string `json:"password,omitempty"`
	// Block and Choice are the "pick" and "vote" actions': a quiz
	// question, the tie-breaker, a suspect board, a draw or a vote, and
	// the answer.
	Block          string `json:"block,omitempty"`
	Choice         string `json:"choice,omitempty"`
	ClientActionID string `json:"client_action_id,omitempty"`
}

// playRefusal is the body of a refused action. It carries the result, and
// with it the team's current view, so the client can redraw without a
// second request — after a stale submit, that view is the question the
// teammate moved the team on to.
type playRefusal struct {
	Error  string       `json:"error"`
	Code   string       `json:"code"`
	Result *play.Result `json:"result,omitempty"`
}

// maxGuessLen bounds an answer. Far above any real one; it only stops a
// client from storing megabytes in the activity feed.
const maxGuessLen = 500

// PostPlayAction runs one player action.
func (h *Handlers) PostPlayAction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	action := chi.URLParam(r, "action")
	var req PlayActionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Guess) > maxGuessLen || len(req.Choice) > maxGuessLen || len(req.Password) > maxGuessLen {
		httpx.JSON(w, http.StatusBadRequest, playRefusal{Error: "answer is too long", Code: "too_long"})
		return
	}
	slot, _ := auth.SlotFromContext(ctx)
	actionID := validClientActionID(req.ClientActionID)
	if cached, found, err := lookupCachedAction(ctx, h.DB, slot.ID, actionID); err == nil && found {
		writeCached(w, cached)
		return
	}
	// A cheat checks a password, and a station a card's code, so they
	// share the answers' budget: that is what stops guessing them.
	if action == "answer" || action == "cheat" || action == "station" {
		if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
			writeRateLimited(w, retry)
			return
		}
	}

	tc, err := loadTeam(ctx, h.DB, slot)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
		return
	}
	if tc.RunState != "running" {
		code := "not_running"
		if tc.RunState == "paused" {
			code = "paused"
		}
		httpx.JSON(w, http.StatusConflict, playRefusal{Error: "the event is not running", Code: code})
		return
	}

	e := h.engine()
	var res play.Result
	switch action {
	case "answer":
		res, err = e.Answer(ctx, tc.TeamID, req.Node, req.Guess)
	case "begin":
		res, err = e.ChooseStart(ctx, tc.TeamID, req.Port, req.Guess)
	case "continue":
		res, err = e.Continue(ctx, tc.TeamID, req.Node)
	case "back":
		res, err = e.Back(ctx, tc.TeamID, req.Node)
	case "timeup":
		res, err = e.TimeUp(ctx, tc.TeamID)
	case "language":
		res, err = e.SetLanguage(ctx, tc.TeamID, req.Lang)
	case "choose":
		res, err = e.Choose(ctx, tc.TeamID, req.Node, req.Port)
	case "skip":
		res, err = e.Skip(ctx, tc.TeamID, req.Node)
	case "cheat":
		ok, cerr := h.cheatOK(ctx, tc.AdventureSlug, req.Password)
		if cerr != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "checking password failed", cerr)
			return
		}
		if !ok {
			httpx.JSON(w, http.StatusForbidden, playRefusal{Error: "wrong cheat password", Code: "bad_password"})
			return
		}
		res, err = e.Cheat(ctx, tc.TeamID, req.Node)
	case "turn":
		// A two-key launch: this player's key.
		res, err = e.Turn(ctx, tc.TeamID, req.Node)
	case "sabotage":
		// The saboteur's button. Not logged where teammates could see it.
		res, err = e.Sabotage(ctx, tc.TeamID, req.Node, req.Block)
	case "leave":
		res, err = e.Leave(ctx, tc.TeamID, req.Node)
	case "timeout":
		res, err = e.Timeout(ctx, tc.TeamID, req.Node)
	case "hint":
		res, err = e.RevealHint(ctx, tc.TeamID, req.Node, req.Index)
	case "pick":
		res, err = e.Pick(ctx, tc.TeamID, req.Node, req.Block, req.Choice)
	case "station":
		// A party's station card, scanned or typed: the team is there now.
		res, err = e.Station(ctx, tc.TeamID, stationCode(req.Guess))
		if errors.Is(err, play.ErrUnknownStation) {
			if res.View, err = e.Snapshot(ctx, tc.TeamID); err == nil {
				_ = decorateView(ctx, h.DB, tc.TeamID, slot.ID, &res.View)
				httpx.JSON(w, http.StatusNotFound, playRefusal{Error: "that is not one of this party's cards", Code: "unknown_station", Result: &res})
				return
			}
		}
	case "vote":
		// A vote is the player's, not the team's, so the engine has no
		// part in it; the reply carries the team's view like any action's.
		if err = h.vote(ctx, tc, slot.ID, req); err == nil || errors.Is(err, session.ErrStale) || errors.Is(err, session.ErrWrongNode) {
			var verr error
			if res.View, verr = e.Snapshot(ctx, tc.TeamID); verr != nil && err == nil {
				err = verr
			}
		}
	default:
		httpx.JSON(w, http.StatusNotFound, playRefusal{Error: "unknown action", Code: "unknown_action"})
		return
	}
	// Every view a player gets is theirs: their part of a split secret,
	// their vote. A refusal carries one too.
	if derr := decorateView(ctx, h.DB, tc.TeamID, slot.ID, &res.View); derr != nil && err == nil {
		err = derr
	}
	if err != nil {
		h.refusePlay(w, res, err)
		return
	}

	// The move is committed: what follows must not die with the request
	// when the phone drops off, or the event may never auto-complete.
	post := context.WithoutCancel(ctx)
	h.recordPlay(post, tc, slot.ID, action, req, res)

	body, _ := json.Marshal(res)
	recordAction(post, h.DB, slot.ID, actionID, http.StatusOK, body)
	if res.Shared {
		h.Hub.Publish(ws.EventTopic(tc.EventID), "changed", nil)
	} else {
		h.Hub.Publish(ws.EventTopic(tc.EventID), ws.KindProgress, ws.Progress{Team: tc.TeamID, Scored: res.Scored})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// PostPlayUnlock checks a cheat password before the phone turns cheating
// on. The phone keeps the password and sends it with every cheat, which
// checks it again; this only tells it whether the password is right.
func (h *Handlers) PostPlayUnlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(ctx)
	if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
		writeRateLimited(w, retry)
		return
	}
	tc, err := loadTeam(ctx, h.DB, slot)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
		return
	}
	ok, err := h.cheatOK(ctx, tc.AdventureSlug, req.Password)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "checking password failed", err)
		return
	}
	if !ok {
		httpx.JSON(w, http.StatusForbidden, playRefusal{Error: "wrong cheat password", Code: "bad_password"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cheatOK reports whether password is the adventure's cheat password.
// An adventure without one accepts nothing.
func (h *Handlers) cheatOK(ctx context.Context, slug, password string) (bool, error) {
	if password == "" || len(password) > maxGuessLen {
		return false, nil
	}
	var want sql.NullString
	err := h.DB.QueryRowContext(ctx, `SELECT cheat_password FROM adventures WHERE slug = $1`, slug).Scan(&want)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return want.Valid && subtle.ConstantTimeCompare([]byte(want.String), []byte(password)) == 1, nil
}

// refusePlay maps an engine refusal onto a status and a stable code.
func (h *Handlers) refusePlay(w http.ResponseWriter, res play.Result, err error) {
	if errors.Is(err, play.ErrNoGraph) {
		httpx.JSON(w, http.StatusNotFound, playRefusal{Error: "this game has not started for your team", Code: "no_game"})
		return
	}
	code := session.Code(err)
	status := map[string]int{
		"stale":          http.StatusConflict,
		"finished":       http.StatusConflict,
		"time_up":        http.StatusConflict,
		"bad_start_code": http.StatusForbidden,
		"not_here":       http.StatusForbidden,
		"no_fix":         http.StatusForbidden,
		"not_allowed":    http.StatusForbidden,
		"locked":         http.StatusTooManyRequests,
		"wrong_node":     http.StatusBadRequest,
		"unanswered":     http.StatusForbidden,
		"not_yours":      http.StatusForbidden,
	}[code]
	if status == 0 {
		httpx.ServerErr(w, http.StatusInternalServerError, "action failed", err)
		return
	}
	if code == "locked" && res.BlockedUntil != nil {
		secs := int(time.Until(*res.BlockedUntil).Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
	}
	httpx.JSON(w, status, playRefusal{Error: err.Error(), Code: code, Result: &res})
}

// recordPlay writes the activity rows the live view and the recap read,
// and fires completion hooks when the team has just finished. Best
// effort, after the engine's transaction: the move is already durable,
// and a lost feed row must not turn it into an error for the player.
func (h *Handlers) recordPlay(ctx context.Context, tc teamContext, slotID int64, action string, req PlayActionRequest, res play.Result) {
	teamID := tc.TeamID
	actor := activity.SlotActor(slotID)
	if action == "cheat" {
		activity.Record(ctx, h.DB, tc.EventID, &teamID, actor, activity.AnswerRight,
			map[string]any{"node": req.Node, "port": res.Port, "points": 0, "cheat": true})
	}
	if action == "turn" && res.Accepted {
		activity.Record(ctx, h.DB, tc.EventID, &teamID, actor, activity.AnswerRight,
			map[string]any{"node": req.Node, "port": res.Port, "points": res.Points})
	}
	// A right answer waiting for teammates to agree is neither yet.
	if action == "answer" && res.Waiting == nil {
		if res.Accepted {
			activity.Record(ctx, h.DB, tc.EventID, &teamID, actor, activity.AnswerRight,
				map[string]any{"node": req.Node, "port": res.Port, "points": res.Points})
		} else {
			activity.Record(ctx, h.DB, tc.EventID, &teamID, actor, activity.AnswerWrong,
				map[string]any{"node": req.Node, "submitted": req.Guess})
		}
	}
	if !res.View.Finished || tc.CompletedAt.Valid {
		return
	}
	activity.Record(ctx, h.DB, tc.EventID, &teamID, actor, activity.TeamCompleted,
		map[string]any{"outcome": res.View.Outcome, "score": res.View.Score - res.View.PenaltyScore})
	h.publishTeamCompleted(ctx, tc)
	if done, err := activity.MaybeCompleteEvent(ctx, h.DB, tc.EventID); err == nil && done {
		h.publishEventCompleted(ctx, tc)
	}
}

// orgBrand reads the organisation's colours, the same four an adventure
// palette has. Missing ones stay empty and fall back to the defaults.
func orgBrand(ctx context.Context, db *sql.DB, orgID int64) (adventure.Colors, error) {
	var accent, second, bg, ink sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT accent_color, accent_secondary_color, background_color, text_color
		FROM organizations WHERE id = $1
	`, orgID).Scan(&accent, &second, &bg, &ink)
	if errors.Is(err, sql.ErrNoRows) {
		return adventure.Colors{}, nil
	}
	return adventure.Colors{Accent: longHex(accent.String), AccentSecondary: longHex(second.String), Background: longHex(bg.String), Text: longHex(ink.String)}, err
}

// longHex spells an organisation's "#rgb" as "#rrggbb": the org settings
// accept both, the player only the long form.
func longHex(c string) string {
	if len(c) == 4 && c[0] == '#' {
		return "#" + string([]byte{c[1], c[1], c[2], c[2], c[3], c[3]})
	}
	return c
}

// stationCode is the code a station card's scan or typing gives: the
// card's QR holds the play page's URL with ?station=CODE, so a phone's
// own camera opens the game at the card; the game's scanner hands the
// URL over whole.
func stationCode(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "station="); i >= 0 {
		s = s[i+len("station="):]
		if j := strings.IndexAny(s, "&#"); j >= 0 {
			s = s[:j]
		}
	}
	return s
}
