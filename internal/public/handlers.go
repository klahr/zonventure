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
	"math"
	"net/http"
	"sort"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/storage"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
)

// Handlers is the dependency bag for public endpoints.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
	Hub *ws.Hub
	// Storage backs the token-authed team-avatar upload/delete on the
	// share page. Always non-nil: server.New defaults to dbblob when
	// nothing else is wired.
	Storage storage.Blobs
	// PublicURL is the externally-reachable origin used when
	// building absolute URLs for printed/shared assets (the team
	// QR code, principally). Forwarded-host headers are NOT
	// consulted — see buildResetLink for the security rationale.
	PublicURL string
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
	EventID          int64             `json:"event_id"`
	AdventureTitle string            `json:"adventure_title"`
	AdventureSlug  string            `json:"adventure_slug"`
	State          string            `json:"state"`
	StartedAt      string            `json:"started_at,omitempty"`
	ScheduledAt    string            `json:"scheduled_at,omitempty"`
	CompletedAt    string            `json:"completed_at,omitempty"`
	ServerNow      string            `json:"server_now"`
	PageCount      int               `json:"page_count"`
	ShowMap        bool              `json:"show_map"`
	Puzzles        []puzzleDTO       `json:"puzzles"`
	Teams          []teamDTO         `json:"teams"`
	Colors         *adventure.Colors `json:"colors,omitempty"`
}

// runByToken resolves a URL token to an event id, returning sql.ErrNoRows if
// the token is unknown.
func (h *Handlers) runByToken(ctx context.Context, token string) (eventID int64, slug, state, startedAt, scheduledAt, completedAt string, showMap int, err error) {
	var started, scheduled, completed sql.NullString
	err = h.DB.QueryRowContext(ctx, `
		SELECT id, adventure_slug, state, started_at, scheduled_at, completed_at, show_map
		FROM events WHERE live_token = $1
	`, token).Scan(&eventID, &slug, &state, &started, &scheduled, &completed, &showMap)
	if started.Valid {
		startedAt = started.String
	}
	if scheduled.Valid {
		scheduledAt = scheduled.String
	}
	if completed.Valid {
		completedAt = completed.String
	}
	return
}

// buildLive assembles the full snapshot. Shared between the HTTP handler
// and the WS push path.
func (h *Handlers) buildLive(ctx context.Context, token string) (LiveDTO, int64, int, string) {
	id, slug, state, startedAt, scheduledAt, completedAt, showMap, err := h.runByToken(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveDTO{}, 0, http.StatusNotFound, "event not found"
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
	// Fetch teams without sorting here — the tiebreaker logic (distance
	// to next puzzle) is cleaner in Go than in SQL. The ordering that
	// was previously baked into ORDER BY lives in sortTeams below.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, ''),
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       t.created_at,
		       COALESCE(
		         (SELECT MAX(created_at) FROM event_activity
		          WHERE event_id = t.event_id AND team_id = t.id AND kind = 'answer.right'),
		         t.created_at
		       ) AS last_progress_at
		FROM teams t
		WHERE t.event_id = $1
	`, id)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "query failed"
	}
	defer rows.Close()
	rowMeta := map[int64]sortRowForSort{}
	teams := []teamDTO{}
	for rows.Next() {
		var (
			t              teamDTO
			rawCurr        int
			hasAvatar      int
			createdAt      string
			lastProgressAt string
		)
		if err := rows.Scan(&t.ID, &t.Name, &rawCurr, &t.CompletedAt, &hasAvatar, &createdAt, &lastProgressAt); err != nil {
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
		rowMeta[t.ID] = sortRowForSort{raw: rawCurr, createdAt: createdAt, lastProgressAt: lastProgressAt}
		teams = append(teams, t)
	}
	for i := range teams {
		mrows, err := h.DB.QueryContext(ctx, `
			SELECT name FROM player_slots
			WHERE team_id = $1 ORDER BY LOWER(name)
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
	}
	// Latest GPS ping per team drives the distance tiebreaker — teams
	// tied on puzzles solved rank by who's physically closer to their
	// next puzzle. Missing pings fall back to the temporal tiebreak.
	latest, err := latestPingsByTeam(ctx, h.DB, id)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "ping lookup failed"
	}
	sortTeams(teams, rowMeta, latest, enabled)
	for i := range teams {
		teams[i].Rank = i + 1
	}
	dto := LiveDTO{
		EventID:          id,
		AdventureTitle: adv.Meta.Title,
		AdventureSlug:  slug,
		State:          state,
		StartedAt:      startedAt,
		ScheduledAt:    scheduledAt,
		CompletedAt:    completedAt,
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

// buildTracks fetches per-team ping history for an event. Returns
// (resp, status, errMsg). Caller is responsible for authorising by token.
func (h *Handlers) buildTracks(ctx context.Context, eventID int64) (TracksResponse, int, string) {
	// Smooth the per-team line by collapsing pings into 10s buckets and
	// emitting one centroid per bucket. Bucketing is done by truncating
	// server_ts at the tens-of-seconds digit (server_ts is a fixed-width
	// 'YYYY-MM-DD HH24:MI:SS' string from now_text()). The centroid is an
	// inverse-variance weighted mean: pings with smaller `accuracy` (in
	// metres) dominate, with a 1m floor to bound the weight and a 50m
	// fallback when accuracy is null.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT p.team_id, t.name, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       SUM(p.lat / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) /
		         SUM(1.0 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) AS lat,
		       SUM(p.lng / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) /
		         SUM(1.0 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) AS lng,
		       substring(p.server_ts, 1, 18) || '0' AS server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id
		WHERE p.event_id = $1
		GROUP BY p.team_id, t.name, (t.avatar_mime_type IS NOT NULL),
		         substring(p.server_ts, 1, 18)
		ORDER BY p.team_id, server_ts
	`, eventID)
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
// arranger has not enabled show_map on the event.
func (h *Handlers) Tracks(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad live token")
		return
	}
	id, _, _, _, _, _, showMap, err := h.runByToken(r.Context(), token)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "event not found")
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

// latestPingsByTeam returns each team's most recent GPS ping for an event.
// Teams without any ping are absent from the result — callers handle
// that as "distance unknown" and fall back to the temporal tiebreaker.
func latestPingsByTeam(ctx context.Context, db *sql.DB, eventID int64) (map[int64]trackPoint, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.team_id, p.lat, p.lng, p.server_ts
		FROM location_pings p
		JOIN (
		  SELECT team_id, MAX(server_ts) AS ts
		  FROM location_pings WHERE event_id = $1
		  GROUP BY team_id
		) latest ON latest.team_id = p.team_id AND latest.ts = p.server_ts
		WHERE p.event_id = $2
	`, eventID, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]trackPoint{}
	for rows.Next() {
		var (
			tid int64
			p   trackPoint
		)
		if err := rows.Scan(&tid, &p.Lat, &p.Lng, &p.ServerTS); err != nil {
			return nil, err
		}
		out[tid] = p
	}
	return out, nil
}

// sortRowForSort is the subset of per-team metadata the sort needs.
// Named separately so buildLive can pass a typed map without leaking
// the shape all over the package.
type sortRowForSort struct {
	raw            int
	createdAt      string
	lastProgressAt string
}

// sortTeams applies the leaderboard ordering rules. Extracted from
// buildLive because the distance tiebreaker is too awkward for SQL.
//
// Sort keys, in order:
//  1. Finishers first (completed_at NOT NULL), earliest completion wins.
//  2. Among non-finishers, more puzzles solved wins.
//  3. Tied on puzzles: closer to the next puzzle wins (Haversine metres).
//     Teams without a GPS ping are treated as "unknown" and rank below
//     pinged teams — being on the map is a prerequisite for the tiebreak.
//  4. Tied on distance (or both unknown): earlier last-right-answer wins
//     (the old temporal rule is a stable fallback).
//  5. Final fallback: earlier created_at.
func sortTeams(teams []teamDTO, rowMeta map[int64]sortRowForSort, pings map[int64]trackPoint, enabled []adventure.Page) {
	// nextPuzzleIdx returns the index into `enabled` that the team is
	// currently working on. raw is adventure_pages.idx of the stored
	// cursor (next page to consider).
	nextPuzzleIdx := func(raw int) int {
		for i, p := range enabled {
			if p.Index >= raw {
				return i
			}
		}
		return len(enabled)
	}
	distanceMeters := func(id int64) (float64, bool) {
		ping, ok := pings[id]
		if !ok {
			return 0, false
		}
		meta, ok := rowMeta[id]
		if !ok {
			return 0, false
		}
		idx := nextPuzzleIdx(meta.raw)
		if idx >= len(enabled) {
			return 0, false
		}
		p := enabled[idx]
		return haversineMeters(ping.Lat, ping.Lng, p.Location.Lat, p.Location.Lng), true
	}
	sort.SliceStable(teams, func(i, j int) bool {
		a, b := teams[i], teams[j]
		// 1. Finished before unfinished.
		if (a.CompletedAt != "") != (b.CompletedAt != "") {
			return a.CompletedAt != ""
		}
		// Both finished — earliest completion wins.
		if a.CompletedAt != "" && b.CompletedAt != "" && a.CompletedAt != b.CompletedAt {
			return a.CompletedAt < b.CompletedAt
		}
		// 2. More puzzles solved wins.
		if a.PageIndex != b.PageIndex {
			return a.PageIndex > b.PageIndex
		}
		// 3. Distance tiebreak — shorter is better; known beats unknown.
		da, aok := distanceMeters(a.ID)
		db, bok := distanceMeters(b.ID)
		if aok != bok {
			return aok
		}
		if aok && bok && da != db {
			return da < db
		}
		// 4. Temporal tiebreak — earlier last-right-answer wins.
		ma, mb := rowMeta[a.ID], rowMeta[b.ID]
		if ma.lastProgressAt != mb.lastProgressAt {
			return ma.lastProgressAt < mb.lastProgressAt
		}
		// 5. Creation-order final fallback for full determinism.
		return ma.createdAt < mb.createdAt
	})
}

// haversineMeters returns the great-circle distance between two
// lat/lng pairs in metres. Accuracy is fine for the sub-kilometre ranges
// leaderboard tiebreaks care about.
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0 // mean Earth radius in metres
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Min(1, math.Sqrt(a)))
}
