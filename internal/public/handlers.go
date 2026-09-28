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

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/storage"
	"github.com/klahr/zonventure/internal/ws"
)

// Handlers is the dependency bag for public endpoints.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
	Hub *ws.Hub
	// Engine ranks a graph event's teams. The server shares the player's,
	// so decoded graphs are cached once; nil makes one per call.
	Engine *play.Engine
	// Storage backs the token-authed team-avatar upload/delete on the
	// share page. Always non-nil: server.New defaults to dbblob when
	// nothing else is wired.
	Storage storage.Blobs
	// PublicURL is the externally-reachable origin used when
	// building absolute URLs for printed/shared assets (the team
	// QR code, principally). Forwarded-host headers are NOT
	// consulted — see buildResetLink for the security rationale.
	PublicURL string
	// tracks caches each event's tracks for tracksTTL; see buildTracks.
	tracks tracksCache
	// PlatformFee gates the marketplace-aware /store filter: when
	// the operator has configured a platform fee, only quests whose
	// org has completed Stripe Connect onboarding are listed
	// (because un-onboarded quests can't actually take a payment).
	// Zero-value = no marketplace = no extra filter, which is the
	// right posture for self-host.
	PlatformFee payments.PlatformFee
}

type puzzleDTO struct {
	Index int    `json:"index"`
	Title string `json:"title"`
	Gated bool   `json:"gated"`
	// Lat/Lng stay empty until the event is over: the board is public
	// and a pin per puzzle would hand every player the route.
	Lat float64 `json:"lat,omitempty"`
	Lng float64 `json:"lng,omitempty"`
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
	// Score is a graph event's points; lane events rank by pages instead.
	Score *float64 `json:"score,omitempty"`
	// TiebreakGuess is the team's answer to the tie-breaker, shown once
	// the event is over: before then it would help the teams still out.
	TiebreakGuess *float64 `json:"tiebreak_guess,omitempty"`
	// Withdrawn teams stay on the board, ranked after everyone still
	// playing.
	Withdrawn bool `json:"withdrawn,omitempty"`
}

// LiveDTO is the snapshot shape served both by /api/public/live/{token}
// and pushed as a "snapshot" WS message.
type LiveDTO struct {
	EventID        int64  `json:"event_id"`
	AdventureTitle string `json:"adventure_title"`
	AdventureSlug  string `json:"adventure_slug"`
	State          string `json:"state"`
	StartedAt      string `json:"started_at,omitempty"`
	ScheduledAt    string `json:"scheduled_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	// PausedSeconds totals the event's closed pauses; PausedAt is when
	// the open one began, set only while paused. Elapsed time is the
	// span from started_at to PausedAt, completed_at or now, less
	// PausedSeconds.
	PausedSeconds int    `json:"paused_seconds"`
	PausedAt      string `json:"paused_at,omitempty"`
	ServerNow     string `json:"server_now"`
	PageCount     int    `json:"page_count"`
	// Engine is "graph" for an event ranked by points (see play.Ahead),
	// "lanes" for one ranked by pages solved.
	Engine  string      `json:"engine"`
	ShowMap bool        `json:"show_map"`
	Puzzles []puzzleDTO `json:"puzzles"`
	// Bounds frames the map before any team has pinged, as
	// [south, west, north, east] around every puzzle. It shows the play
	// area without pointing at any one puzzle.
	Bounds *[4]float64       `json:"bounds,omitempty"`
	Teams  []teamDTO         `json:"teams"`
	Colors *adventure.Colors `json:"colors,omitempty"`
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
	over := state == "completed" || state == "cancelled"
	puzzles := make([]puzzleDTO, len(enabled))
	var bounds *[4]float64
	for i, p := range enabled {
		puzzles[i] = puzzleDTO{Index: i, Title: p.Title, Gated: p.Location.Gated()}
		lat, lng := p.Location.Lat, p.Location.Lng
		if lat == 0 && lng == 0 {
			continue
		}
		if over {
			puzzles[i].Lat, puzzles[i].Lng = lat, lng
		}
		if bounds == nil {
			bounds = &[4]float64{lat, lng, lat, lng}
		}
		bounds[0], bounds[1] = min(bounds[0], lat), min(bounds[1], lng)
		bounds[2], bounds[3] = max(bounds[2], lat), max(bounds[3], lng)
	}
	// Fetch teams without sorting here — the tiebreaker logic (distance
	// to next puzzle) is cleaner in Go than in SQL. The ordering that
	// was previously baked into ORDER BY lives in sortTeams below.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, ''),
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       (t.withdrawn_at IS NOT NULL)::int AS withdrawn,
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
			withdrawn      int
			createdAt      string
			lastProgressAt string
		)
		if err := rows.Scan(&t.ID, &t.Name, &rawCurr, &t.CompletedAt, &hasAvatar, &withdrawn, &createdAt, &lastProgressAt); err != nil {
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
		t.Withdrawn = withdrawn != 0
		rowMeta[t.ID] = sortRowForSort{raw: rawCurr, createdAt: createdAt, lastProgressAt: lastProgressAt}
		teams = append(teams, t)
	}
	// Every team's members in one query: this runs for each viewer on
	// every push.
	teamIdx := make(map[int64]int, len(teams))
	for i := range teams {
		teams[i].Members = []memberDTO{}
		teamIdx[teams[i].ID] = i
	}
	mrows, err := h.DB.QueryContext(ctx, `
		SELECT ps.team_id, ps.name FROM player_slots ps JOIN teams t ON t.id = ps.team_id
		WHERE t.event_id = $1 ORDER BY ps.team_id, LOWER(ps.name)
	`, id)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "members failed"
	}
	for mrows.Next() {
		var (
			teamID int64
			m      memberDTO
		)
		if err := mrows.Scan(&teamID, &m.Username); err != nil {
			mrows.Close()
			return LiveDTO{}, id, http.StatusInternalServerError, "member scan failed"
		}
		if i, ok := teamIdx[teamID]; ok {
			teams[i].Members = append(teams[i].Members, m)
		}
	}
	mrows.Close()
	engine := "lanes"
	usesGraph, err := play.UsesGraph(ctx, h.DB, id)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "event lookup failed"
	}
	if usesGraph {
		engine = "graph"
		if err := h.rankGraph(ctx, id, state, teams); err != nil {
			return LiveDTO{}, id, http.StatusInternalServerError, "ranking failed"
		}
	} else {
		// Latest GPS ping per team drives the distance tiebreaker — teams
		// tied on puzzles solved rank by who's physically closer to their
		// next puzzle. Missing pings fall back to the temporal tiebreak.
		latest, err := latestPingsByTeam(ctx, h.DB, id)
		if err != nil {
			return LiveDTO{}, id, http.StatusInternalServerError, "ping lookup failed"
		}
		sortTeams(teams, rowMeta, latest, enabled)
		// As on a graph board, withdrawn teams follow everyone still
		// playing, keeping their order among themselves.
		sort.SliceStable(teams, func(i, j int) bool {
			return !teams[i].Withdrawn && teams[j].Withdrawn
		})
	}
	for i := range teams {
		teams[i].Rank = i + 1
	}
	var (
		pausedSeconds int
		pausedAt      sql.NullString
	)
	if err := h.DB.QueryRowContext(ctx,
		`SELECT paused_seconds, paused_at FROM events WHERE id = $1`, id).Scan(&pausedSeconds, &pausedAt); err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "event lookup failed"
	}
	dto := LiveDTO{
		EventID:        id,
		AdventureTitle: adv.Meta.Title,
		AdventureSlug:  slug,
		State:          state,
		StartedAt:      startedAt,
		ScheduledAt:    scheduledAt,
		CompletedAt:    completedAt,
		PausedSeconds:  pausedSeconds,
		PausedAt:       pausedAt.String,
		ServerNow:      time.Now().UTC().Format(time.RFC3339),
		PageCount:      len(enabled),
		Engine:         engine,
		ShowMap:        showMap != 0,
		Puzzles:        puzzles,
		Bounds:         bounds,
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

// queryTracks fetches per-team ping history for an event. Returns
// (resp, status, errMsg). Caller is responsible for authorising by token.
// Callers go through buildTracks, which caches the result.
func (h *Handlers) queryTracks(ctx context.Context, eventID int64) (TracksResponse, int, string) {
	// Smooth the per-team line by collapsing pings into time buckets and
	// emitting one centroid per bucket. Buckets are 10s wide, widened in
	// 10s steps once the event's ping history spans more than 20 minutes
	// so a track never exceeds ~120 points: this is resent on every push,
	// and full 10s resolution over a multi-hour event made it megabytes.
	// server_ts is a UTC 'YYYY-MM-DD HH24:MI:SS' string from now_text(),
	// and the bucket start goes back out in that format. The centroid is an
	// inverse-variance weighted mean: pings with smaller `accuracy` (in
	// metres) dominate, with a 1m floor to bound the weight and a 50m
	// fallback when accuracy is null.
	rows, err := h.DB.QueryContext(ctx, `
		WITH span AS (
		  SELECT GREATEST(10, CEIL(EXTRACT(EPOCH FROM
		           MAX(server_ts::timestamp) - MIN(server_ts::timestamp)) / 1200) * 10) AS w
		  FROM location_pings WHERE event_id = $1
		)
		SELECT p.team_id, t.name, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       SUM(p.lat / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) /
		         SUM(1.0 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) AS lat,
		       SUM(p.lng / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) /
		         SUM(1.0 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) AS lng,
		       to_char(to_timestamp(FLOOR(EXTRACT(EPOCH FROM p.server_ts::timestamp) / s.w) * s.w)
		         AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS') AS server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id CROSS JOIN span s
		WHERE p.event_id = $1
		GROUP BY p.team_id, t.name, (t.avatar_mime_type IS NOT NULL),
		         FLOOR(EXTRACT(EPOCH FROM p.server_ts::timestamp) / s.w), s.w
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

// rankGraph orders a graph event's teams by the shared ranking and gives
// each its points. Teams not yet started, or withdrawn, follow the
// ranked ones in the order they had.
func (h *Handlers) rankGraph(ctx context.Context, eventID int64, state string, teams []teamDTO) error {
	e := h.Engine
	if e == nil {
		e = play.New(h.DB)
	}
	ranked, err := e.EventRanking(ctx, eventID)
	if err != nil {
		return err
	}
	pos := make(map[int64]int, len(ranked))
	for i, r := range ranked {
		pos[r.TeamID] = i
	}
	for i := range teams {
		p, ok := pos[teams[i].ID]
		if !ok {
			continue
		}
		r := ranked[p]
		score := r.Score
		teams[i].Score = &score
		if state == "completed" && r.Guess != nil {
			g := *r.Guess
			teams[i].TiebreakGuess = &g
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		pi, iok := pos[teams[i].ID]
		pj, jok := pos[teams[j].ID]
		if iok != jok {
			return iok
		}
		return iok && pi < pj
	})
	return nil
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
