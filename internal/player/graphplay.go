package player

// Player endpoints for events that run on the graph engine
// (internal/play). They sit beside the lane endpoints rather than inside
// them: an event is on one engine or the other for its whole life (see
// play.UsesGraph), and the React player app speaks only these.
//
//	GET  /api/player/play            where the team is and what it sees
//	POST /api/player/play/{action}   answer, continue, choose, skip, timeout, hint
//
// Every action carries the node the player is looking at. A teammate who
// answered first moves the team, and the second submit is refused as
// stale rather than applied to a question its sender has never seen.
// Actions also carry a client_action_id, and a successful response is
// replayed verbatim on retry, so the offline outbox can re-send safely.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// engine returns the graph engine. Handlers built directly in a test
// may leave it nil; assigning a default here would race between
// requests, so a fresh one is built instead.
func (h *Handlers) engine() *play.Engine {
	if h.Engine != nil {
		return h.Engine
	}
	return play.New(h.DB)
}

// PlayTeam is the team as the player app shows it.
type PlayTeam struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

// PlayEvent is the event as the player app shows it.
type PlayEvent struct {
	ID             int64  `json:"id"`
	State          string `json:"state"`
	ScheduledAt    string `json:"scheduled_at,omitempty"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	OrgName        string `json:"org_name,omitempty"`
	OrgLogoURL     string `json:"org_logo_url,omitempty"`
}

// PlayState is the body of GET /api/player/play.
type PlayState struct {
	// Engine is "graph", or "lanes" for an event the React player does
	// not run — the client sends those players to /play.
	Engine string    `json:"engine"`
	Team   PlayTeam  `json:"team"`
	Event  PlayEvent `json:"event"`
	// Brand is the adventure's palette, the one the lane player paints
	// with too, so an adventure styled for either looks the same.
	Brand *adventure.Colors `json:"brand,omitempty"`
	// View is set once the event has started.
	View *play.View `json:"view,omitempty"`
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
	// A player opening a due event starts it, as on the lane endpoints.
	if tc.RunState == "scheduled" && tc.ScheduledAt.Valid {
		if promoted, err := maybePromoteScheduled(ctx, h.DB, h.Hub, h.Lib, tc.EventID, tc.ScheduledAt.String); err == nil && promoted {
			h.publishEventStartedAuto(ctx, tc)
			if tc, err = loadTeam(ctx, h.DB, slot); err != nil {
				httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
				return
			}
		}
	}

	out := PlayState{
		Engine: "lanes",
		Team:   PlayTeam{ID: tc.TeamID, Name: tc.TeamName, Token: tc.TeamToken},
		Event: PlayEvent{
			ID: tc.EventID, State: tc.RunState, ScheduledAt: tc.ScheduledAt.String, OrgName: tc.OrgName,
		},
	}
	title, brand, err := adventureLook(ctx, h.DB, tc.AdventureSlug)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading adventure failed", err)
		return
	}
	out.Event.AdventureTitle = title
	if !brand.Empty() {
		out.Brand = &brand
	}
	if tc.OrgHasLogo {
		out.Event.OrgLogoURL = fmt.Sprintf("/org-logo/%d", tc.OrgID)
	}
	uses, err := play.UsesGraph(ctx, h.DB, tc.EventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading event failed", err)
		return
	}
	if !uses {
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	out.Engine = "graph"
	if tc.RunState == "scheduled" || tc.RunState == "cancelled" {
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	view, err := h.engine().Snapshot(ctx, tc.TeamID)
	if errors.Is(err, play.ErrNoGraph) && tc.RunState == "running" {
		// A team added after the start — a ticket bought for a running
		// quest — has no state yet. Placing it is StartEvent's job, and
		// StartEvent leaves teams that already have state alone.
		if _, err = play.StartIfGraph(ctx, h.DB, tc.EventID); err == nil {
			view, err = h.engine().Snapshot(ctx, tc.TeamID)
		}
	}
	if err != nil && !errors.Is(err, play.ErrNoGraph) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading game failed", err)
		return
	}
	if err == nil {
		out.View = &view
	}
	httpx.JSON(w, http.StatusOK, out)
}

// adventureLook reads what the player app shows of the adventure itself:
// its title and palette. One small query, because this runs on every
// push and poll — adventure.Library.Get would load every page, hint and
// image to answer it.
func adventureLook(ctx context.Context, db *sql.DB, slug string) (string, adventure.Colors, error) {
	var (
		title                         string
		accent, second, bg, ink, dock sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT title, accent_color, accent_secondary_color, background_color, text_color, submit_bg_color
		FROM adventures WHERE slug = $1
	`, slug).Scan(&title, &accent, &second, &bg, &ink, &dock)
	if errors.Is(err, sql.ErrNoRows) {
		return "", adventure.Colors{}, nil
	}
	return title, adventure.Colors{
		Accent: accent.String, AccentSecondary: second.String, Background: bg.String,
		Text: ink.String, SubmitBackground: dock.String,
	}, err
}

// PlayActionRequest is the body of POST /api/player/play/{action}.
type PlayActionRequest struct {
	Node           string `json:"node"`
	Guess          string `json:"guess,omitempty"`
	Port           string `json:"port,omitempty"`
	Index          int    `json:"index,omitempty"`
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
	if len(req.Guess) > maxGuessLen {
		httpx.JSON(w, http.StatusBadRequest, playRefusal{Error: "answer is too long", Code: "too_long"})
		return
	}
	slot, _ := auth.SlotFromContext(ctx)
	actionID := validClientActionID(req.ClientActionID)
	if cached, found, err := lookupCachedAction(ctx, h.DB, slot.ID, actionID); err == nil && found {
		writeCached(w, cached)
		return
	}
	if action == "answer" {
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
	case "continue":
		res, err = e.Continue(ctx, tc.TeamID, req.Node)
	case "choose":
		res, err = e.Choose(ctx, tc.TeamID, req.Node, req.Port)
	case "skip":
		res, err = e.Skip(ctx, tc.TeamID, req.Node)
	case "timeout":
		res, err = e.Timeout(ctx, tc.TeamID, req.Node)
	case "hint":
		res, err = e.RevealHint(ctx, tc.TeamID, req.Node, req.Index)
	default:
		httpx.JSON(w, http.StatusNotFound, playRefusal{Error: "unknown action", Code: "unknown_action"})
		return
	}
	if err != nil {
		h.refusePlay(w, res, err)
		return
	}

	h.recordPlay(ctx, tc, slot.ID, action, req, res)

	body, _ := json.Marshal(res)
	recordAction(ctx, h.DB, slot.ID, actionID, http.StatusOK, body)
	h.Hub.Publish(ws.EventTopic(tc.EventID), "changed", nil)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// refusePlay maps an engine refusal onto a status and a stable code.
func (h *Handlers) refusePlay(w http.ResponseWriter, res play.Result, err error) {
	if errors.Is(err, play.ErrNoGraph) {
		httpx.JSON(w, http.StatusNotFound, playRefusal{Error: "this game has not started for your team", Code: "no_game"})
		return
	}
	code := session.Code(err)
	status := map[string]int{
		"stale":       http.StatusConflict,
		"finished":    http.StatusConflict,
		"time_up":     http.StatusConflict,
		"not_here":    http.StatusForbidden,
		"not_allowed": http.StatusForbidden,
		"locked":      http.StatusTooManyRequests,
		"wrong_node":  http.StatusBadRequest,
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
	if action == "answer" {
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
