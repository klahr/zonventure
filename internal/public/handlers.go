// Package public serves read-only endpoints that do not require a session.
// These power the TV-friendly live leaderboard (/live/{token}).
//
// Nothing in this package may leak information that would let a viewer
// cheat the game: prompts, accept lists and hints are never included —
// only progress, team names, puzzle titles and (optionally) movement.
package public

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
)

// Handlers is the dependency bag for public endpoints.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
	Hub *ws.Hub
}

type puzzleDTO struct {
	Index int     `json:"index"`
	Title string  `json:"title"`
	Gated bool    `json:"gated"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
}

type memberDTO struct {
	Username string `json:"username"`
}

type teamDTO struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	PageIndex   int         `json:"page_index"`
	PageCount   int         `json:"page_count"`
	CompletedAt string      `json:"completed_at,omitempty"`
	Rank        int         `json:"rank"`
	AvatarURL   string      `json:"avatar_url,omitempty"`
	Members     []memberDTO `json:"members"`
}

// LiveDTO is the snapshot shape served both by /api/public/live/{token}
// and pushed as a "snapshot" WS message.
type LiveDTO struct {
	RunID          int64             `json:"run_id"`
	AdventureTitle string            `json:"adventure_title"`
	AdventureSlug  string            `json:"adventure_slug"`
	State          string            `json:"state"`
	StartedAt      string            `json:"started_at,omitempty"`
	ScheduledAt    string            `json:"scheduled_at,omitempty"`
	ServerNow      string            `json:"server_now"`
	PageCount      int               `json:"page_count"`
	ShowMap        bool              `json:"show_map"`
	Puzzles        []puzzleDTO       `json:"puzzles"`
	Teams          []teamDTO         `json:"teams"`
	Colors         *adventure.Colors `json:"colors,omitempty"`
}

// runByToken resolves a URL token to a run id, returning sql.ErrNoRows if
// the token is unknown.
func (h *Handlers) runByToken(ctx context.Context, token string) (runID int64, slug, state, startedAt, scheduledAt string, showMap int, err error) {
	var started, scheduled sql.NullString
	err = h.DB.QueryRowContext(ctx, `
		SELECT id, adventure_slug, state, started_at, scheduled_at, show_map
		FROM adventure_runs WHERE live_token = ?
	`, token).Scan(&runID, &slug, &state, &started, &scheduled, &showMap)
	if started.Valid {
		startedAt = started.String
	}
	if scheduled.Valid {
		scheduledAt = scheduled.String
	}
	return
}

// buildLive assembles the full snapshot. Shared between the HTTP handler
// and the WS push path.
func (h *Handlers) buildLive(ctx context.Context, token string) (LiveDTO, int64, int, string) {
	id, slug, state, startedAt, scheduledAt, showMap, err := h.runByToken(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveDTO{}, 0, http.StatusNotFound, "run not found"
	}
	if err != nil {
		return LiveDTO{}, 0, http.StatusInternalServerError, "lookup failed"
	}
	adv := h.Lib.Get(slug)
	if adv == nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "adventure not loaded"
	}
	// The TV board only shows pages a player will actually encounter, so
	// disabled pages are hidden entirely. enabledPages preserves the raw
	// idx order so the index emitted here stays stable across runs.
	enabled := make([]adventure.Page, 0, len(adv.Pages))
	for _, p := range adv.Pages {
		if !p.Disabled {
			enabled = append(enabled, p)
		}
	}
	puzzles := make([]puzzleDTO, len(enabled))
	for i, p := range enabled {
		puzzles[i] = puzzleDTO{
			Index: i, Title: p.Title, Gated: p.Location.Gated(),
			Lat: p.Location.Lat, Lng: p.Location.Lng,
		}
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, ''),
		       t.avatar_mime_type IS NOT NULL AS has_avatar
		FROM teams t
		WHERE t.run_id = ?
		ORDER BY
		  (CASE WHEN t.completed_at IS NULL THEN 1 ELSE 0 END),
		  t.completed_at ASC,
		  t.current_page_index DESC,
		  COALESCE(
		    (SELECT MAX(created_at) FROM run_events
		     WHERE run_id = t.run_id AND team_id = t.id AND kind = 'answer.right'),
		    t.created_at
		  ) ASC,
		  t.created_at ASC
	`, id)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "query failed"
	}
	defer rows.Close()
	teams := []teamDTO{}
	for rows.Next() {
		var (
			t         teamDTO
			rawCurr   int
			hasAvatar int
		)
		if err := rows.Scan(&t.ID, &t.Name, &rawCurr, &t.CompletedAt, &hasAvatar); err != nil {
			return LiveDTO{}, id, http.StatusInternalServerError, "scan failed"
		}
		if hasAvatar != 0 {
			t.AvatarURL = fmt.Sprintf("/adventure-assets/team-avatar/%d", t.ID)
		}
		// Convert the stored cursor (raw adventure_pages.idx of the next
		// page to consider) into a count of enabled pages already solved.
		for _, p := range enabled {
			if p.Index < rawCurr {
				t.PageIndex++
			}
		}
		if t.PageIndex > len(enabled) {
			t.PageIndex = len(enabled)
		}
		t.PageCount = len(enabled)
		teams = append(teams, t)
	}
	for i := range teams {
		mrows, err := h.DB.QueryContext(ctx, `
			SELECT name FROM player_slots
			WHERE team_id = ? ORDER BY name COLLATE NOCASE
		`, teams[i].ID)
		if err != nil {
			return LiveDTO{}, id, http.StatusInternalServerError, "members failed"
		}
		members := []memberDTO{}
		for mrows.Next() {
			var m memberDTO
			if err := mrows.Scan(&m.Username); err != nil {
				mrows.Close()
				return LiveDTO{}, id, http.StatusInternalServerError, "member scan failed"
			}
			members = append(members, m)
		}
		mrows.Close()
		teams[i].Members = members
		teams[i].Rank = i + 1
	}
	dto := LiveDTO{
		RunID:          id,
		AdventureTitle: adv.Meta.Title,
		AdventureSlug:  slug,
		State:          state,
		StartedAt:      startedAt,
		ScheduledAt:    scheduledAt,
		ServerNow:      time.Now().UTC().Format(time.RFC3339),
		PageCount:      len(enabled),
		ShowMap:        showMap != 0,
		Puzzles:        puzzles,
		Teams:          teams,
	}
	if !adv.Colors.Empty() {
		c := adv.Colors
		dto.Colors = &c
	}
	return dto, id, http.StatusOK, ""
}

// Live returns the compact snapshot for the public leaderboard.
func (h *Handlers) Live(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad live token")
		return
	}
	resp, _, status, errMsg := h.buildLive(r.Context(), token)
	if status != http.StatusOK {
		httpx.Error(w, status, errMsg)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, resp)
}

type trackPoint struct {
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	ServerTS string  `json:"server_ts"`
}

type trackDTO struct {
	TeamID    int64        `json:"team_id"`
	TeamName  string       `json:"team_name"`
	AvatarURL string       `json:"avatar_url,omitempty"`
	Points    []trackPoint `json:"points"`
}

// TracksResponse is the payload for the tracks endpoint and for the
// "tracks" WS push. Named so the WS handler can reuse it.
type TracksResponse struct {
	Tracks []trackDTO `json:"tracks"`
}

// buildTracks fetches per-team ping history for a run. Returns
// (resp, status, errMsg). Caller is responsible for authorising by token.
func (h *Handlers) buildTracks(ctx context.Context, runID int64) (TracksResponse, int, string) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT p.team_id, t.name, t.avatar_mime_type IS NOT NULL AS has_avatar,
		       p.lat, p.lng, p.server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id
		WHERE p.run_id = ?
		ORDER BY p.team_id, p.server_ts
	`, runID)
	if err != nil {
		return TracksResponse{}, http.StatusInternalServerError, "query failed"
	}
	defer rows.Close()
	byTeam := map[int64]*trackDTO{}
	order := []int64{}
	for rows.Next() {
		var (
			teamID    int64
			name      string
			hasAvatar int
			p         trackPoint
		)
		if err := rows.Scan(&teamID, &name, &hasAvatar, &p.Lat, &p.Lng, &p.ServerTS); err != nil {
			return TracksResponse{}, http.StatusInternalServerError, "scan failed"
		}
		t, ok := byTeam[teamID]
		if !ok {
			t = &trackDTO{TeamID: teamID, TeamName: name}
			if hasAvatar != 0 {
				t.AvatarURL = fmt.Sprintf("/adventure-assets/team-avatar/%d", teamID)
			}
			byTeam[teamID] = t
			order = append(order, teamID)
		}
		t.Points = append(t.Points, p)
	}
	tracks := make([]trackDTO, 0, len(order))
	for _, id := range order {
		tracks = append(tracks, *byTeam[id])
	}
	return TracksResponse{Tracks: tracks}, http.StatusOK, ""
}

// Tracks returns per-team ping history for the live map. Refuses when the
// arranger has not enabled show_map on the run.
func (h *Handlers) Tracks(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad live token")
		return
	}
	id, _, _, _, _, showMap, err := h.runByToken(r.Context(), token)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if showMap == 0 {
		httpx.Error(w, http.StatusForbidden, "map not enabled")
		return
	}
	resp, status, errMsg := h.buildTracks(r.Context(), id)
	if status != http.StatusOK {
		httpx.Error(w, status, errMsg)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, resp)
}
