// Package player implements the endpoints a player (slot) uses to play
// through an adventure.
//
// A slot authenticates via a magic-link exchange (see /play?t=<token>)
// which sets the zv_slot cookie. All endpoints in this package require a
// slot context populated by auth.SlotMiddleware + auth.RequireSlot.
package player

import (
	"database/sql"
	"errors"
	"math"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/runevents"
)

// Handlers bundles the dependencies the player endpoints need.
type Handlers struct {
	DB           *sql.DB
	Lib          *adventure.Library
	SecureCookie bool
}

// MagicLinkLogin exchanges a player_slots.token (delivered to the player
// via their own message) for a slot session cookie and redirects the
// browser to /play. Handled at GET /play if the ?t= query parameter is
// present; if the token is bad we fall through to the ordinary /play
// page which will tell the player to open their link again.
func (h *Handlers) MagicLinkLogin(w http.ResponseWriter, r *http.Request) bool {
	token := r.URL.Query().Get("t")
	if token == "" {
		return false
	}
	_, session, err := auth.LoginSlotByToken(r.Context(), h.DB, token)
	if err != nil {
		// Best-effort: let the /play page render without a session so the UI
		// can say "open your magic link again" rather than a blank error.
		return false
	}
	auth.SetSlotCookie(w, session, h.SecureCookie)
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
	auth.ClearSlotCookie(w, h.SecureCookie)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// teamContext is the slot's team/run snapshot used by all the handlers.
type teamContext struct {
	TeamID           int64
	TeamName         string
	RunID            int64
	AdventureSlug    string
	RunState         string
	CurrentPageIndex int
	CompletedAt      sql.NullString
}

func loadTeam(r *http.Request, db *sql.DB, slot auth.Slot) (teamContext, error) {
	var tc teamContext
	err := db.QueryRowContext(r.Context(), `
		SELECT t.id, t.name, ar.id, ar.adventure_slug, ar.state,
		       t.current_page_index, t.completed_at
		FROM teams t JOIN adventure_runs ar ON ar.id = t.run_id
		WHERE t.id = ?
	`, slot.TeamID).Scan(&tc.TeamID, &tc.TeamName, &tc.RunID, &tc.AdventureSlug,
		&tc.RunState, &tc.CurrentPageIndex, &tc.CompletedAt)
	return tc, err
}

// State is the response body for GET /api/player/state.
type State struct {
	Status         string   `json:"status"` // waiting | running | paused | completed | gated
	AdventureSlug  string   `json:"adventure_slug,omitempty"`
	AdventureTitle string   `json:"adventure_title,omitempty"`
	TeamName       string   `json:"team_name,omitempty"`
	SlotName       string   `json:"slot_name,omitempty"`
	PageIndex      int      `json:"page_index,omitempty"`
	PageCount      int      `json:"page_count,omitempty"`
	Title          string   `json:"title,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	Hint           string   `json:"hint,omitempty"`
	Gate           *gateDTO `json:"gate,omitempty"`
}

type gateDTO struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Radius  float64 `json:"radius_m"`
	Reached bool    `json:"reached"`
}

// GetState returns the player's current view.
func (h *Handlers) GetState(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	tc, err := loadTeam(r, h.DB, slot)
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
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded: "+tc.AdventureSlug)
		return
	}

	resp := State{
		AdventureSlug:  adv.Slug,
		AdventureTitle: adv.Meta.Title,
		TeamName:       tc.TeamName,
		SlotName:       slot.Name,
		PageCount:      len(adv.Pages),
	}

	if tc.CompletedAt.Valid {
		resp.Status = "completed"
		httpx.JSON(w, http.StatusOK, resp)
		return
	}
	switch tc.RunState {
	case "scheduled":
		resp.Status = "waiting"
		httpx.JSON(w, http.StatusOK, resp)
		return
	case "paused":
		resp.Status = "paused"
		httpx.JSON(w, http.StatusOK, resp)
		return
	case "completed":
		resp.Status = "completed"
		httpx.JSON(w, http.StatusOK, resp)
		return
	}

	if tc.CurrentPageIndex >= len(adv.Pages) {
		resp.Status = "completed"
		httpx.JSON(w, http.StatusOK, resp)
		return
	}

	page := adv.Pages[tc.CurrentPageIndex]
	resp.PageIndex = page.Index
	resp.Title = page.Title

	if page.Location.Gated() {
		reached, _ := gateReached(r, h.DB, tc.TeamID, page.Location)
		resp.Gate = &gateDTO{
			Lat:     page.Location.Lat,
			Lng:     page.Location.Lng,
			Radius:  *page.Location.RadiusM,
			Reached: reached,
		}
		if !reached {
			resp.Status = "gated"
			httpx.JSON(w, http.StatusOK, resp)
			return
		}
	}

	resp.Status = "running"
	resp.Prompt = page.Prompt
	resp.Hint = page.Hint
	httpx.JSON(w, http.StatusOK, resp)
}

// gateReached checks whether the team's latest location ping places them
// inside the location's gate radius.
func gateReached(r *http.Request, db *sql.DB, teamID int64, loc adventure.Location) (bool, error) {
	var lat, lng float64
	err := db.QueryRowContext(r.Context(), `
		SELECT lat, lng FROM location_pings WHERE team_id = ?
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

// SubmitAnswer evaluates a submitted answer for the slot's current page.
func (h *Handlers) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	var req AnswerRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	tc, err := loadTeam(r, h.DB, slot)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if tc.RunState != "running" {
		httpx.Error(w, http.StatusConflict, "run is not active")
		return
	}
	if tc.CompletedAt.Valid {
		httpx.Error(w, http.StatusConflict, "team already finished")
		return
	}

	adv := h.Lib.Get(tc.AdventureSlug)
	if adv == nil || tc.CurrentPageIndex >= len(adv.Pages) {
		httpx.Error(w, http.StatusInternalServerError, "no current page")
		return
	}
	page := adv.Pages[tc.CurrentPageIndex]

	if page.Location.Gated() {
		reached, err := gateReached(r, h.DB, tc.TeamID, page.Location)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "gate check failed")
			return
		}
		if !reached {
			httpx.Error(w, http.StatusForbidden, "you must reach the location first")
			return
		}
	}

	accepted := page.CheckAnswer(req.Answer)
	acceptedInt := 0
	if accepted {
		acceptedInt = 1
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO answer_attempts(team_id, slot_id, page_index, answer, accepted)
		VALUES (?, ?, ?, ?, ?)
	`, tc.TeamID, slot.ID, tc.CurrentPageIndex, req.Answer, acceptedInt); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "record attempt failed")
		return
	}

	teamID := tc.TeamID
	resp := AnswerResponse{Accepted: accepted}
	if !accepted {
		runevents.Record(r.Context(), h.DB, tc.RunID, &teamID, runevents.SlotActor(slot.ID),
			runevents.AnswerWrong, map[string]any{"page_index": tc.CurrentPageIndex, "submitted": req.Answer})
		httpx.JSON(w, http.StatusOK, resp)
		return
	}

	runevents.Record(r.Context(), h.DB, tc.RunID, &teamID, runevents.SlotActor(slot.ID),
		runevents.AnswerRight, map[string]any{"page_index": tc.CurrentPageIndex})

	nextIdx := tc.CurrentPageIndex + 1
	if nextIdx >= len(adv.Pages) {
		if _, err := h.DB.ExecContext(r.Context(), `
			UPDATE teams SET current_page_index = ?, completed_at = datetime('now') WHERE id = ?
		`, nextIdx, tc.TeamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "save progress failed")
			return
		}
		runevents.Record(r.Context(), h.DB, tc.RunID, &teamID, runevents.SlotActor(slot.ID),
			runevents.TeamCompleted, nil)
		resp.Completed = true
		resp.NextIndex = nextIdx
		httpx.JSON(w, http.StatusOK, resp)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE teams SET current_page_index = ? WHERE id = ?
	`, nextIdx, tc.TeamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "save progress failed")
		return
	}
	resp.NextIndex = nextIdx
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

// PostLocation records a GPS ping for the slot's team.
func (h *Handlers) PostLocation(w http.ResponseWriter, r *http.Request) {
	var req LocationRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		httpx.Error(w, http.StatusBadRequest, "coordinates out of range")
		return
	}
	slot, _ := auth.SlotFromContext(r.Context())
	var (
		acc, speed, heading, cts any
	)
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
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO location_pings(slot_id, team_id, run_id, lat, lng, accuracy, speed, heading, client_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, slot.ID, slot.TeamID, slot.RunID, req.Lat, req.Lng, acc, speed, heading, cts); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "save ping failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}
