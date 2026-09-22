package public

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
)

/* ---------------- Post-event recap (public-by-token) ----------------
 *
 * The recap is the team's shareable post-event keepsake — track
 * replay + stats + outro — addressed by teams.recap_token. Distinct
 * token from teams.token (the team-share-join URL) so leaking the
 * recap link does NOT grant join/play access; the two have very
 * different blast radii.
 *
 * Visibility rules:
 *
 *   - 404 on unknown token (no enumerable existence-vs-not signal)
 *   - 404 when event.state is not in (completed, cancelled). A live
 *     event's track would expose teams' real-time positions to
 *     anyone who happens onto the URL, which neither the player
 *     nor the arranger consented to. The same shape as "unknown
 *     token" is deliberate: nothing about the page-being-soon-to-
 *     exist is leaked.
 *   - On a wiped slot (player called /forget) the track will be
 *     missing; we still return the team-level stats. Acceptable
 *     graceful-degradation rather than an empty 404.
 */

// recapResponse is the JSON shape the recap page renders against.
// Mirrors player.SummaryResponse plus the track + per-puzzle data
// the post-event view needs.
type recapResponse struct {
	TeamName        string        `json:"team_name"`
	AdventureTitle  string        `json:"adventure_title"`
	AdventureSlug   string        `json:"adventure_slug,omitempty"`
	OutroText       string        `json:"outro_text,omitempty"`
	Colors          *recapColors  `json:"colors,omitempty"`
	StartedAt       string        `json:"started_at,omitempty"`
	CompletedAt     string        `json:"completed_at,omitempty"`
	DurationSeconds int           `json:"duration_seconds"`
	DistanceM       float64       `json:"distance_m"`
	AnswersRight    int           `json:"answers_right"`
	AnswersWrong    int           `json:"answers_wrong"`
	PageCount       int           `json:"page_count"`
	State           string        `json:"state"`
	Track           []recapTrack  `json:"track"`
	Puzzles         []recapPuzzle `json:"puzzles"`
}

type recapColors struct {
	Accent          string `json:"accent,omitempty"`
	AccentSecondary string `json:"accent_secondary,omitempty"`
	Background      string `json:"background,omitempty"`
	Text            string `json:"text,omitempty"`
}

type recapTrack struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
	TS  string  `json:"ts"`
}

type recapPuzzle struct {
	Index    int     `json:"index"`
	Title    string  `json:"title,omitempty"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	SolvedAt string  `json:"solved_at,omitempty"`
}

// Recap serves the JSON for /api/public/recap/{token}. The HTML
// page at /recap/{token} fetches this on load and renders it
// client-side; both endpoints share the same visibility gate.
func (h *Handlers) Recap(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var (
		teamID        int64
		teamName      string
		adventureSlug string
		startedAt     sql.NullString
		completedAt   sql.NullString
		teamCompleted sql.NullString
		state         string
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.id, t.name, ar.adventure_slug, ar.started_at,
		       ar.completed_at, t.completed_at, ar.state
		FROM teams t
		JOIN events ar ON ar.id = t.event_id
		WHERE t.recap_token = $1
	`, token).Scan(&teamID, &teamName, &adventureSlug, &startedAt,
		&completedAt, &teamCompleted, &state)
	if errors.Is(err, sql.ErrNoRows) {
		// Same shape as "not yet visible" below — never leak that
		// the token exists during the live phase, only that the
		// recap isn't accessible right now.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if state != "completed" && state != "cancelled" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	adv := h.Lib.Get(adventureSlug)
	if adv == nil {
		http.Error(w, "adventure missing", http.StatusInternalServerError)
		return
	}

	resp := recapResponse{
		TeamName:       teamName,
		AdventureTitle: adv.Meta.Title,
		AdventureSlug:  adv.Slug,
		// Pre-render to HTML so the recap page can drop it into
		// innerHTML and the author's markdown actually renders. Same
		// goldmark pipeline puzzle prompts and the player intro/outro
		// already use.
		OutroText: mdx.Render(adv.Meta.OutroText),
		StartedAt: startedAt.String,
		PageCount: len(adv.Pages),
		State:     state,
	}
	// Team's own completion stamp wins over the event's — multiple
	// teams in one event can finish at different times, and the
	// recap is per-team.
	if teamCompleted.Valid && teamCompleted.String != "" {
		resp.CompletedAt = teamCompleted.String
	} else if completedAt.Valid {
		resp.CompletedAt = completedAt.String
	}
	if !adv.Colors.Empty() {
		resp.Colors = &recapColors{
			Accent:          adv.Colors.Accent,
			AccentSecondary: adv.Colors.AccentSecondary,
			Background:      adv.Colors.Background,
			Text:            adv.Colors.Text,
		}
	}

	// Right / wrong counts from answer_attempts. Same shape as
	// player.Summary; duplicating the SQL beats coupling the two
	// packages (the second caller is a public read-only render —
	// the formulas should stay parallel, not shared by reference).
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT
		  COALESCE(SUM(CASE WHEN accepted = 1 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN accepted = 0 THEN 1 ELSE 0 END), 0)
		FROM answer_attempts WHERE team_id = $1
	`, teamID).Scan(&resp.AnswersRight, &resp.AnswersWrong); err != nil {
		http.Error(w, "stats lookup failed", http.StatusInternalServerError)
		return
	}

	// Track + accumulated distance in one pass. Track is exposed
	// to the client for the Leaflet replay; distance is the
	// haversine-summed total. Both come from location_pings in
	// chronological order, so a /forget that wiped pings naturally
	// leaves both empty without special-casing.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT lat, lng, server_ts
		FROM location_pings
		WHERE team_id = $1
		ORDER BY server_ts, id
	`, teamID)
	if err != nil {
		http.Error(w, "track lookup failed", http.StatusInternalServerError)
		return
	}
	var (
		hasPrev          bool
		prevLat, prevLng float64
		dist             float64
	)
	for rows.Next() {
		var p recapTrack
		if err := rows.Scan(&p.Lat, &p.Lng, &p.TS); err != nil {
			rows.Close()
			http.Error(w, "track scan failed", http.StatusInternalServerError)
			return
		}
		if hasPrev {
			dist += haversineMeters(prevLat, prevLng, p.Lat, p.Lng)
		}
		prevLat, prevLng = p.Lat, p.Lng
		hasPrev = true
		resp.Track = append(resp.Track, p)
	}
	rows.Close()
	resp.DistanceM = dist

	// Per-puzzle solve timestamps. We pick the *first* accepted
	// attempt per page_index so a re-submitted answer (the team
	// re-typed after coming back from a hint) doesn't shift the
	// completion mark to the later submission. ROW_NUMBER would
	// be cleaner; the GROUP BY MIN works on every Postgres we
	// support.
	pRows, err := h.DB.QueryContext(r.Context(), `
		SELECT page_index, MIN(submitted_at)
		FROM answer_attempts
		WHERE team_id = $1 AND accepted = 1
		GROUP BY page_index
		ORDER BY page_index
	`, teamID)
	if err != nil {
		http.Error(w, "puzzles lookup failed", http.StatusInternalServerError)
		return
	}
	solveAt := map[int]string{}
	for pRows.Next() {
		var idx int
		var at string
		if err := pRows.Scan(&idx, &at); err != nil {
			pRows.Close()
			http.Error(w, "puzzles scan failed", http.StatusInternalServerError)
			return
		}
		solveAt[idx] = at
	}
	pRows.Close()
	for _, p := range adv.Pages {
		if p.Disabled {
			continue
		}
		entry := recapPuzzle{
			Index: p.Index,
			Title: p.Title,
			Lat:   p.Location.Lat,
			Lng:   p.Location.Lng,
		}
		if at, ok := solveAt[p.Index]; ok {
			entry.SolvedAt = at
		}
		resp.Puzzles = append(resp.Puzzles, entry)
	}

	// Duration: prefer team's own completed_at — different teams in
	// one event can finish at different times. Falls back to
	// event.completed_at when the team row is missing the stamp
	// (cancelled events frequently take this branch).
	if resp.StartedAt != "" && resp.CompletedAt != "" {
		if start, sErr := parseRecapTime(resp.StartedAt); sErr == nil {
			if end, eErr := parseRecapTime(resp.CompletedAt); eErr == nil {
				secs := int(end.Sub(start).Seconds())
				if secs < 0 {
					secs = 0
				}
				resp.DurationSeconds = secs
			}
		}
	}

	// Cache hint: 5 minutes is short enough that a recap viewed
	// before the player's /forget call refreshes promptly,
	// long enough to absorb a paste-and-discuss burst on a chat app.
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	httpx.JSON(w, http.StatusOK, resp)
}

// parseRecapTime accepts both wire formats the play-side stores —
// "YYYY-MM-DD HH:MM:SS" UTC and RFC3339 — so a recap written by
// either path renders without per-call branching.
func parseRecapTime(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// haversineMeters lives in handlers.go — reuse it rather than
// shadowing.
