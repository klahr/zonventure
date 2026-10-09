package public

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
	"github.com/klahr/zonventure/internal/play"
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
type recapResponse struct {
	TeamName       string       `json:"team_name"`
	AdventureTitle string       `json:"adventure_title"`
	AdventureSlug  string       `json:"adventure_slug,omitempty"`
	OutroText      string       `json:"outro_text,omitempty"`
	Colors         *recapColors `json:"colors,omitempty"`
	// Look is the adventure's cover picture and title typeface.
	Look            *PageLook    `json:"look,omitempty"`
	StartedAt       string       `json:"started_at,omitempty"`
	CompletedAt     string       `json:"completed_at,omitempty"`
	DurationSeconds int          `json:"duration_seconds"`
	DistanceM       float64      `json:"distance_m"`
	AnswersRight    int          `json:"answers_right"`
	AnswersWrong    int          `json:"answers_wrong"`
	PageCount       int          `json:"page_count"`
	State           string       `json:"state"`
	Track           []recapTrack `json:"track"`

	Points    *float64    `json:"points,omitempty"`
	Rank      int         `json:"rank,omitempty"`
	TeamCount int         `json:"team_count,omitempty"`
	Outcome   string      `json:"outcome,omitempty"`
	Steps     []recapStep `json:"steps,omitempty"`
	Zones     []recapZone `json:"zones,omitempty"`
	Quiz      *recapQuiz  `json:"quiz,omitempty"`
	// Photos are the team's approved pictures for photo blocks, until
	// retention prunes them; each is served at
	// /api/public/recap/{token}/photos/{id}.
	Photos []recapPhoto `json:"photos,omitempty"`
}

// recapPhoto is one approved team picture: what it was of, and where.
type recapPhoto struct {
	ID     int64  `json:"id"`
	Prompt string `json:"prompt,omitempty"`
	Step   string `json:"step,omitempty"`
}

// recapStep is one puzzle or story a graph team reached, in graph order.
type recapStep struct {
	Node      string `json:"node"`
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	ReachedAt string `json:"reached_at,omitempty"`
	SolvedAt  string `json:"solved_at,omitempty"`
	Solved    bool   `json:"solved,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`
	Zone      string `json:"zone,omitempty"`
}

// recapZone is a zone one of the reached steps is gated on: the graph
// recap's map pins. Solved is whether a step there was completed — a
// solved puzzle, or a story the team reached.
type recapZone struct {
	Slug    string       `json:"slug"`
	Name    string       `json:"name,omitempty"`
	Title   string       `json:"title,omitempty"`
	Lat     float64      `json:"lat"`
	Lng     float64      `json:"lng"`
	RadiusM float64      `json:"radius_m,omitempty"`
	Polygon [][2]float64 `json:"polygon,omitempty"`
	Solved  bool         `json:"solved,omitempty"`
}

type recapQuiz struct {
	Right int `json:"right"`
	Total int `json:"total"`
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
		eventID       int64
		teamID        int64
		teamName      string
		adventureSlug string
		startedAt     sql.NullString
		completedAt   sql.NullString
		teamCompleted sql.NullString
		state         string
		teamDone      bool
	)
	// A team is done when it finished the adventure, or when its game of
	// a hosted event was finished: its recap opens then, without waiting
	// for an event that may run for days (hosted) or months (self-guided).
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT ar.id, t.id, t.name, ar.adventure_slug, ar.started_at,
		       ar.completed_at, t.completed_at, ar.state,
		       COALESCE((SELECT s.completed_at IS NOT NULL FROM team_graph_state s WHERE s.team_id = t.id), FALSE)
		       OR EXISTS (SELECT 1 FROM event_rounds er
		                  WHERE er.event_id = t.event_id AND er.round = t.round AND er.finished_at IS NOT NULL)
		FROM teams t
		JOIN events ar ON ar.id = t.event_id
		WHERE t.recap_token = $1
	`, token).Scan(&eventID, &teamID, &teamName, &adventureSlug, &startedAt,
		&completedAt, &teamCompleted, &state, &teamDone)
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
	if state != "completed" && state != "cancelled" && !teamDone {
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

	look, err := LoadPageLook(r.Context(), h.DB, h.Looks, adv.Slug)
	if err != nil {
		http.Error(w, "look lookup failed", http.StatusInternalServerError)
		return
	}
	resp.Look = look

	// Counts, score, steps and pins come from the team's graph state.
	if err := h.recapGraph(r.Context(), eventID, teamID, &resp); err != nil {
		http.Error(w, "graph recap failed", http.StatusInternalServerError)
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

// recapTimeLayout is the recap's wire format for timestamps, the one
// recap.html reads.
const recapTimeLayout = "2006-01-02 15:04:05"

// recapGraph fills a graph team's recap from its graph state: points,
// rank and outcome, its own start and finish, the puzzles and stories
// it reached, the zones they are gated on, and its quiz walk. A team
// with no graph state never started, and keeps the event-level fields.
func (h *Handlers) recapGraph(ctx context.Context, eventID, teamID int64, resp *recapResponse) error {
	var graphID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `SELECT graph_id FROM events WHERE id = $1`, eventID).Scan(&graphID); err != nil {
		return err
	}
	g := &graph.Graph{}
	if graphID.Valid {
		s, err := graphstore.ByID(ctx, h.DB, graphID.Int64)
		if err != nil {
			return err
		}
		g = s.Graph
	}
	resp.PageCount = 0
	for _, n := range g.Nodes {
		if n.Type == graph.Puzzle {
			resp.PageCount++
		}
	}
	if err := recapPhotos(ctx, h.DB, g, teamID, resp); err != nil {
		return err
	}

	var (
		points    float64
		startedAt time.Time
		completed sql.NullTime
		outcome   sql.NullString
		lang      string
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT score - penalty_score, started_at, completed_at, outcome, lang
		FROM team_graph_state WHERE team_id = $1
	`, teamID).Scan(&points, &startedAt, &completed, &outcome, &lang)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	resp.Points = &points
	resp.Outcome = outcome.String
	resp.StartedAt = startedAt.UTC().Format(recapTimeLayout)
	if completed.Valid {
		resp.CompletedAt = completed.Time.UTC().Format(recapTimeLayout)
	}
	if resp.Outcome == "fail" {
		// The adventure's outro is written for a team that made it.
		resp.OutroText = ""
	}

	e := h.Engine
	if e == nil {
		e = play.New(h.DB)
	}
	ranked, err := e.EventRanking(ctx, eventID)
	if err != nil {
		return err
	}
	for _, me := range ranked {
		if me.TeamID != teamID {
			continue
		}
		resp.Rank, resp.TeamCount = 1, len(ranked)
		for _, o := range ranked {
			if play.Ahead(o, me) {
				resp.Rank++
			}
		}
	}

	type nodeState struct {
		firstSeen time.Time
		solved    sql.NullTime
		attempts  int
		answer    string
	}
	visited := map[string]nodeState{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT node, first_seen_at, solved_at, attempts, answer
		FROM team_node_state WHERE team_id = $1
	`, teamID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var slug string
		var ns nodeState
		if err := rows.Scan(&slug, &ns.firstSeen, &ns.solved, &ns.attempts, &ns.answer); err != nil {
			rows.Close()
			return err
		}
		visited[slug] = ns
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	picks := map[[2]string]string{}
	pRows, err := h.DB.QueryContext(ctx, `
		SELECT node, block_id, choice FROM team_block_picks WHERE team_id = $1
	`, teamID)
	if err != nil {
		return err
	}
	for pRows.Next() {
		var node, block, choice string
		if err := pRows.Scan(&node, &block, &choice); err != nil {
			pRows.Close()
			return err
		}
		picks[[2]string{node, block}] = choice
	}
	pRows.Close()
	if err := pRows.Err(); err != nil {
		return err
	}

	zones := map[string]graph.Zone{}
	for _, z := range g.Zones {
		zones[z.Slug] = z
	}
	zoneAt := map[string]int{}
	var quiz recapQuiz
	resp.AnswersRight, resp.AnswersWrong = 0, 0
	for _, n := range g.Nodes {
		ns, ok := visited[n.Slug]
		if !ok {
			continue
		}
		if _, ok := g.QuizAnswer(n); ok {
			quiz.Total++
			if ns.answer != "" {
				quiz.Right++
			}
		}
		for _, b := range n.Blocks() {
			if b.Kind != graph.BlockQuiz {
				continue
			}
			quiz.Total++
			if p := picks[[2]string{n.Slug, b.ID}]; p != "" && p == b.Answer {
				quiz.Right++
			}
		}
		step := recapStep{
			Node:      n.Slug,
			Kind:      string(n.Type),
			Title:     g.Tr(lang, graph.NodeKey(n.Slug, "title"), n.Title),
			ReachedAt: ns.firstSeen.UTC().Format(recapTimeLayout),
		}
		done := true
		switch {
		case n.PuzzleCfg != nil:
			step.Zone = n.PuzzleCfg.Zone
			step.Attempts = ns.attempts
			step.Solved = ns.solved.Valid
			if step.Solved {
				step.SolvedAt = ns.solved.Time.UTC().Format(recapTimeLayout)
				resp.AnswersRight++
			}
			resp.AnswersWrong += ns.attempts
			done = step.Solved
		case n.StoryCfg != nil:
			step.Zone = n.StoryCfg.Zone
		default:
			continue
		}
		resp.Steps = append(resp.Steps, step)
		z, ok := zones[step.Zone]
		if !ok {
			continue
		}
		if i, seen := zoneAt[z.Slug]; seen {
			resp.Zones[i].Solved = resp.Zones[i].Solved || done
			continue
		}
		zoneAt[z.Slug] = len(resp.Zones)
		resp.Zones = append(resp.Zones, recapZone{
			Slug:    z.Slug,
			Name:    g.Tr(lang, graph.ZoneKey(z.Slug), z.Name),
			Title:   step.Title,
			Lat:     z.Lat,
			Lng:     z.Lng,
			RadiusM: z.RadiusM,
			Polygon: z.Polygon,
			Solved:  done,
		})
	}
	if quiz.Total > 0 {
		resp.Quiz = &quiz
	}
	return nil
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

// recapPhotos lists the team's approved pictures, in the order taken.
func recapPhotos(ctx context.Context, db *sql.DB, g *graph.Graph, teamID int64, resp *recapResponse) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, node, block_id FROM team_photos WHERE team_id = $1 AND status = 'approved' ORDER BY id`, teamID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p recapPhoto
		var node, block string
		if err := rows.Scan(&p.ID, &node, &block); err != nil {
			return err
		}
		if n, ok := g.Node(node); ok {
			p.Step = n.Title
			for _, b := range n.AllBlocks() {
				if b.ID == block {
					p.Prompt = b.Text
				}
			}
		}
		resp.Photos = append(resp.Photos, p)
	}
	return rows.Err()
}

// RecapPhoto serves one of a team's approved pictures, behind the same
// gate as the recap: the team's recap token, once the event is over.
//
// GET /api/public/recap/{token}/photos/{photoID}
func (h *Handlers) RecapPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var content []byte
	var mime string
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.content, p.mime
		FROM team_photos p
		JOIN teams t ON t.id = p.team_id
		JOIN events ar ON ar.id = t.event_id
		WHERE t.recap_token = $1 AND p.id = $2 AND p.status = 'approved'
		  AND (ar.state IN ('completed', 'cancelled')
		       OR EXISTS (SELECT 1 FROM team_graph_state s WHERE s.team_id = t.id AND s.completed_at IS NOT NULL)
		       OR EXISTS (SELECT 1 FROM event_rounds er
		                  WHERE er.event_id = t.event_id AND er.round = t.round AND er.finished_at IS NOT NULL))`,
		chi.URLParam(r, "token"), id).Scan(&content, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}
