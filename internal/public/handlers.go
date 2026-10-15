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
	// SecureCookie mirrors the operator's force-secure override so the
	// slot cookie set by the scan-to-join flow matches the auth/player
	// handlers' Secure attribute.
	SecureCookie bool
	// Looks resolves look covers and typefaces for the store, recap and
	// live board (LoadPageLook).
	Looks LookAssets
	// Payments is whether a payment provider is wired. Without one no
	// ticket can be bought, so the store lists nothing.
	Payments bool
	// Marketplace reports whether organisations sell into Stripe
	// accounts of their own (admin.Handlers.Marketplace): then only
	// those whose account can take charges are listed.
	Marketplace func() bool
	// tracks caches each event's tracks for tracksTTL; see buildTracks.
	tracks tracksCache
}

type memberDTO struct {
	Username string `json:"username"`
}

type teamDTO struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	CompletedAt string      `json:"completed_at,omitempty"`
	Rank        int         `json:"rank"`
	AvatarURL   string      `json:"avatar_url,omitempty"`
	Members     []memberDTO `json:"members"`
	// Score is the team's points.
	Score *float64 `json:"score,omitempty"`
	// TiebreakGuess is the team's answer to the tie-breaker, shown once
	// the event is over: before then it would help the teams still out.
	TiebreakGuess *float64 `json:"tiebreak_guess,omitempty"`
	// Withdrawn teams stay on the board, ranked after everyone still
	// playing.
	Withdrawn bool `json:"withdrawn,omitempty"`
	// Joined is set once any of the team's players has signed in.
	Joined bool `json:"joined,omitempty"`
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
	ShowMap       bool   `json:"show_map"`
	// Teams are ranked by points (see play.Ahead). ResultsHeld is set
	// while a party game keeps its results for the host's reveal: its
	// teams come first, in the order they joined, with no points or rank.
	Teams       []teamDTO `json:"teams"`
	ResultsHeld bool      `json:"results_held,omitempty"`
	// Round is the party game the board shows, alone: its teams, its
	// clock, and completed once the host finishes it. 0 for an event
	// that is not a party.
	Round  int               `json:"round,omitempty"`
	Colors *adventure.Colors `json:"colors,omitempty"`
	// Look is the adventure's cover picture and title typeface.
	Look *PageLook `json:"look,omitempty"`
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
	// A party plays game after game: the board shows the latest alone.
	var (
		round                   int
		held                    bool
		roundStart, roundEnd    sql.NullTime
		pausedFrom, roundPaused sql.NullInt64
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT CASE WHEN e.party THEN COALESCE(e.current_round, 0) ELSE 0 END,
		       e.party AND e.party_hold_results AND r.round IS NOT NULL AND r.finished_at IS NULL,
		       r.started_at, r.finished_at, r.paused_from, r.paused_seconds
		FROM events e LEFT JOIN event_rounds r ON e.party AND r.event_id = e.id AND r.round = e.current_round
		WHERE e.id = $1
	`, id).Scan(&round, &held, &roundStart, &roundEnd, &pausedFrom, &roundPaused); err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "event lookup failed"
	}
	if round > 0 && roundStart.Valid {
		startedAt = roundStart.Time.UTC().Format(time.DateTime)
		if roundEnd.Valid && state != "cancelled" {
			state, completedAt = "completed", roundEnd.Time.UTC().Format(time.DateTime)
		}
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(t.completed_at, ''),
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       (t.withdrawn_at IS NOT NULL)::int AS withdrawn,
		       EXISTS (SELECT 1 FROM player_slots ps WHERE ps.team_id = t.id AND ps.last_login_at IS NOT NULL) AS joined
		FROM teams t
		WHERE t.event_id = $1 AND ($2 = 0 OR t.round = $2)
	`, id, round)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "query failed"
	}
	defer rows.Close()
	teams := []teamDTO{}
	for rows.Next() {
		var (
			t         teamDTO
			hasAvatar int
			withdrawn int
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.CompletedAt, &hasAvatar, &withdrawn, &t.Joined); err != nil {
			return LiveDTO{}, id, http.StatusInternalServerError, "scan failed"
		}
		if hasAvatar != 0 {
			t.AvatarURL = fmt.Sprintf("/adventure-assets/team-avatar/%d", t.ID)
		}
		t.Withdrawn = withdrawn != 0
		teams = append(teams, t)
	}
	// Every team's members in one query: this runs for each viewer on
	// every push. A player who erased their data is left out.
	teamIdx := make(map[int64]int, len(teams))
	for i := range teams {
		teams[i].Members = []memberDTO{}
		teamIdx[teams[i].ID] = i
	}
	mrows, err := h.DB.QueryContext(ctx, `
		SELECT ps.team_id, ps.name FROM player_slots ps JOIN teams t ON t.id = ps.team_id
		WHERE t.event_id = $1 AND ps.name <> '(forgotten)' ORDER BY ps.team_id, LOWER(ps.name)
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
	if err := h.rankGraph(ctx, id, state, teams); err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "ranking failed"
	}
	// Withdrawn teams follow everyone still playing, keeping their
	// order among themselves — also before any team has a score.
	sort.SliceStable(teams, func(i, j int) bool {
		return !teams[i].Withdrawn && teams[j].Withdrawn
	})
	playing := func(t teamDTO) bool { return held && !t.Withdrawn }
	if held {
		sort.SliceStable(teams, func(i, j int) bool {
			pi, pj := playing(teams[i]), playing(teams[j])
			if pi != pj {
				return pi
			}
			return pi && teams[i].ID < teams[j].ID
		})
	}
	for i := range teams {
		if playing(teams[i]) {
			teams[i].Score, teams[i].TiebreakGuess = nil, nil
			continue
		}
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
	// A game's clock counts its own pauses: a finished one's as it
	// ended, an open one's since it opened. A game opened before they
	// were kept counts none.
	if round > 0 && roundStart.Valid {
		switch {
		case roundEnd.Valid:
			pausedSeconds, pausedAt = int(roundPaused.Int64), sql.NullString{}
		case pausedFrom.Valid:
			pausedSeconds = max(0, pausedSeconds-int(pausedFrom.Int64))
		default:
			pausedSeconds = 0
		}
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
		ShowMap:        showMap != 0,
		Teams:          teams,
		ResultsHeld:    held,
		Round:          round,
	}
	if !adv.Colors.Empty() {
		c := adv.Colors
		dto.Colors = &c
	}
	look, err := LoadPageLook(ctx, h.DB, h.Looks, slug)
	if err != nil {
		return LiveDTO{}, id, http.StatusInternalServerError, "look lookup failed"
	}
	dto.Look = look
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
