// Package public serves read-only endpoints that do not require a session.
// These power the TV-friendly live leaderboard (/live/{id}).
//
// Nothing in this package may leak information that would let a viewer
// cheat the game: prompts, accept lists and hints are never included —
// only progress, team names, puzzle titles and movement traces.
package public

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
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
	Index int    `json:"index"`
	Title string `json:"title"`
	Gated bool   `json:"gated"`
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
	Puzzles        []puzzleDTO `json:"puzzles"`
	Teams          []teamDTO   `json:"teams"`
}

// Live returns a compact snapshot suitable for a public leaderboard.
func (h *Handlers) Live(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	var (
		slug         string
		state        string
		startedAt    sql.NullString
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT adventure_slug, state, started_at FROM adventure_runs WHERE id = ?
	`, id).Scan(&slug, &state, &startedAt)
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
		puzzles[i] = puzzleDTO{Index: p.Index, Title: p.Title, Gated: p.Location.Gated()}
	}

	// Rank: completed teams first (finish order), then those still running by
	// puzzle progress (higher is better). Among teams on the same page, the
	// one who got there sooner is ahead — we approximate with team.created_at
	// plus their latest answer.right event timestamp.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, ''),
		       (SELECT MAX(created_at) FROM run_events
		        WHERE run_id = t.run_id AND team_id = t.id AND kind = 'answer.right')
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
		var (
			t         teamDTO
			lastRight sql.NullString // consumed only by the ORDER BY
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.PageIndex, &t.CompletedAt, &lastRight); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		t.PageCount = len(adv.Pages)
		teams = append(teams, t)
	}

	// Members in a second query per team — cheap for the team sizes we see.
	for i := range teams {
		mrows, err := h.DB.QueryContext(r.Context(), `
			SELECT u.username FROM team_members tm
			JOIN users u ON u.id = tm.user_id
			WHERE tm.team_id = ? ORDER BY u.username COLLATE NOCASE
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
		StartedAt:      startedAt.String,
		ServerNow:      time.Now().UTC().Format(time.RFC3339),
		PageCount:      len(adv.Pages),
		Puzzles:        puzzles,
		Teams:          teams,
	}
	// Cache-control: clients should poll, and intermediaries must not cache
	// user-visible state, so disable caching explicitly.
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, resp)
}

