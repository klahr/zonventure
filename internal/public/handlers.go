// Package public serves read-only endpoints that do not require a session.
// These power the TV-friendly live leaderboard (/live/{token}).
//
// Nothing in this package may leak information that would let a viewer
// cheat the game: prompts, accept lists and hints are never included —
// only progress, team names, puzzle titles and (optionally) movement.
package public

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// Handlers is the dependency bag for public endpoints.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
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
	Members     []memberDTO `json:"members"`
}

type liveDTO struct {
	RunID          int64       `json:"run_id"`
	AdventureTitle string      `json:"adventure_title"`
	AdventureSlug  string      `json:"adventure_slug"`
	State          string      `json:"state"`
	StartedAt      string      `json:"started_at,omitempty"`
	ServerNow      string      `json:"server_now"`
	PageCount      int         `json:"page_count"`
	ShowMap        bool        `json:"show_map"`
	Puzzles        []puzzleDTO `json:"puzzles"`
	Teams          []teamDTO   `json:"teams"`
}

// runByToken resolves a URL token to a run id, returning sql.ErrNoRows if
// the token is unknown.
func (h *Handlers) runByToken(r *http.Request, token string) (runID int64, slug, state, startedAt string, showMap int, err error) {
	var started sql.NullString
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT id, adventure_slug, state, started_at, show_map
		FROM adventure_runs WHERE live_token = ?
	`, token).Scan(&runID, &slug, &state, &started, &showMap)
	if started.Valid {
		startedAt = started.String
	}
	return
}

// Live returns the compact snapshot for the public leaderboard.
func (h *Handlers) Live(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad live token")
		return
	}
	id, slug, state, startedAt, showMap, err := h.runByToken(r, token)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	adv := h.Lib.Get(slug)
	if adv == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded")
		return
	}

	puzzles := make([]puzzleDTO, len(adv.Pages))
	for i, p := range adv.Pages {
		puzzles[i] = puzzleDTO{
			Index: p.Index, Title: p.Title, Gated: p.Location.Gated(),
			Lat: p.Location.Lat, Lng: p.Location.Lng,
		}
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, '')
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
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	teams := []teamDTO{}
	for rows.Next() {
		var t teamDTO
		if err := rows.Scan(&t.ID, &t.Name, &t.PageIndex, &t.CompletedAt); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		t.PageCount = len(adv.Pages)
		teams = append(teams, t)
	}

	for i := range teams {
		mrows, err := h.DB.QueryContext(r.Context(), `
			SELECT name FROM player_slots
			WHERE team_id = ? ORDER BY name COLLATE NOCASE
		`, teams[i].ID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "members failed")
			return
		}
		members := []memberDTO{}
		for mrows.Next() {
			var m memberDTO
			if err := mrows.Scan(&m.Username); err != nil {
				mrows.Close()
				httpx.Error(w, http.StatusInternalServerError, "member scan failed")
				return
			}
			members = append(members, m)
		}
		mrows.Close()
		teams[i].Members = members
		teams[i].Rank = i + 1
	}

	resp := liveDTO{
		RunID:          id,
		AdventureTitle: adv.Meta.Title,
		AdventureSlug:  slug,
		State:          state,
		StartedAt:      startedAt,
		ServerNow:      time.Now().UTC().Format(time.RFC3339),
		PageCount:      len(adv.Pages),
		ShowMap:        showMap != 0,
		Puzzles:        puzzles,
		Teams:          teams,
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
	TeamID   int64        `json:"team_id"`
	TeamName string       `json:"team_name"`
	Points   []trackPoint `json:"points"`
}

// Tracks returns per-team ping history for the live map. Refuses when the
// arranger has not enabled show_map on the run.
func (h *Handlers) Tracks(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad live token")
		return
	}
	id, _, _, _, showMap, err := h.runByToken(r, token)
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

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT p.team_id, t.name, p.lat, p.lng, p.server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id
		WHERE p.run_id = ?
		ORDER BY p.team_id, p.server_ts
	`, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	byTeam := map[int64]*trackDTO{}
	order := []int64{}
	for rows.Next() {
		var (
			teamID int64
			name   string
			p      trackPoint
		)
		if err := rows.Scan(&teamID, &name, &p.Lat, &p.Lng, &p.ServerTS); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		t, ok := byTeam[teamID]
		if !ok {
			t = &trackDTO{TeamID: teamID, TeamName: name}
			byTeam[teamID] = t
			order = append(order, teamID)
		}
		t.Points = append(t.Points, p)
	}
	tracks := make([]trackDTO, 0, len(order))
	for _, id := range order {
		tracks = append(tracks, *byTeam[id])
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}
