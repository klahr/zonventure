// Package admin implements the endpoints an arranger uses to set up and run
// adventures: list the library, schedule a run, create teams with fresh
// user accounts, start/pause/resume a run, and watch positions on a map.
package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/runevents"
	"github.com/go-chi/chi/v5"
)

// Handlers wires the admin endpoints to their dependencies.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
}

type adventureDTO struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	PageCount   int    `json:"page_count"`
}

// ListAdventures returns every adventure the server has loaded. This
// endpoint is used by root (who may assign anything) and by the detail
// views; arrangers pick what to schedule from their assignments instead.
func (h *Handlers) ListAdventures(w http.ResponseWriter, r *http.Request) {
	advs := h.Lib.List()
	out := make([]adventureDTO, 0, len(advs))
	for _, a := range advs {
		out = append(out, adventureDTO{
			Slug:        a.Slug,
			Title:       a.Meta.Title,
			Description: a.Meta.Description,
			Author:      a.Meta.Author,
			PageCount:   len(a.Pages),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"adventures": out})
}

type runDTO struct {
	ID             int64  `json:"id"`
	AdventureSlug  string `json:"adventure_slug"`
	State          string `json:"state"`
	ScheduledAt    string `json:"scheduled_at,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	ArrangerUserID int64  `json:"arranger_user_id"`
	AssignmentID   int64  `json:"assignment_id"`
	MaxTeams       int    `json:"max_teams"`
}

type createRunRequest struct {
	AssignmentID int64  `json:"assignment_id"`
	ScheduledAt  string `json:"scheduled_at,omitempty"`
}

// CreateRun consumes one of the caller's unused assignments and schedules a
// run tied to it. The old semantics (pick any adventure the caller was
// "granted" unlimited times) are gone — arrangers must have an unused
// assignment and each assignment produces exactly one run.
func (h *Handlers) CreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AssignmentID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "assignment_id required")
		return
	}
	arr, _ := auth.FromContext(r.Context())

	// Atomically verify: assignment exists, belongs to the caller, and is
	// not already consumed. The unique index on adventure_runs.assignment_id
	// also enforces uniqueness at the DB level.
	var (
		slug     string
		owner    int64
		maxTeams int
		used     sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT aa.adventure_slug, aa.arranger_user_id, aa.max_teams, ar.id
		FROM adventure_assignments aa
		LEFT JOIN adventure_runs ar ON ar.assignment_id = aa.id
		WHERE aa.id = ?
	`, req.AssignmentID).Scan(&slug, &owner, &maxTeams, &used)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "assignment not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owner != arr.ID {
		httpx.Error(w, http.StatusForbidden, "that assignment is not yours")
		return
	}
	if used.Valid {
		httpx.Error(w, http.StatusConflict, "assignment already used")
		return
	}
	if h.Lib.Get(slug) == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded: "+slug)
		return
	}

	var schedAt any
	if req.ScheduledAt != "" {
		t, perr := parseISO8601(req.ScheduledAt)
		if perr != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid scheduled_at: "+perr.Error())
			return
		}
		// Store in sqlite's native format so datetime('scheduled_at') compares
		// cleanly against datetime('now').
		schedAt = t.UTC().Format("2006-01-02 15:04:05")
	}
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_runs(adventure_slug, arranger_user_id, state, scheduled_at, assignment_id)
		VALUES (?, ?, 'scheduled', ?, ?)
	`, slug, arr.ID, schedAt, req.AssignmentID)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "could not create run: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	runevents.Record(r.Context(), h.DB, id, nil, &arr.ID, runevents.RunCreated,
		map[string]any{"adventure_slug": slug, "max_teams": maxTeams})
	httpx.JSON(w, http.StatusCreated, runDTO{
		ID: id, AdventureSlug: slug, State: "scheduled",
		ScheduledAt: req.ScheduledAt, ArrangerUserID: arr.ID,
		AssignmentID: req.AssignmentID, MaxTeams: maxTeams,
	})
}

// ListRuns returns all runs owned by the authenticated arranger.
func (h *Handlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	arr, _ := auth.FromContext(r.Context())
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT ar.id, ar.adventure_slug, ar.state,
		       COALESCE(ar.scheduled_at, ''), COALESCE(ar.started_at, ''), ar.arranger_user_id,
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_teams, 0)
		FROM adventure_runs ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.arranger_user_id = ? ORDER BY ar.created_at DESC
	`, arr.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []runDTO{}
	for rows.Next() {
		var d runDTO
		if err := rows.Scan(&d.ID, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt, &d.ArrangerUserID, &d.AssignmentID, &d.MaxTeams); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"runs": out})
}

// transitionRun is the shared state-machine helper for start/pause/resume.
//
// Allowed transitions:
//
//	scheduled -> running (action = "start")
//	paused    -> running (action = "resume")
//	running   -> paused  (action = "pause")
func (h *Handlers) transitionRun(w http.ResponseWriter, r *http.Request, action string) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	arr, _ := auth.FromContext(r.Context())
	var state string
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT state FROM adventure_runs WHERE id = ? AND arranger_user_id = ?
	`, runID, arr.ID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	var (
		want    string
		update  string
	)
	switch action {
	case "start":
		want, update = "scheduled", `UPDATE adventure_runs SET state='running', started_at=datetime('now') WHERE id=?`
	case "pause":
		want, update = "running", `UPDATE adventure_runs SET state='paused', paused_at=datetime('now') WHERE id=?`
	case "resume":
		want, update = "paused", `UPDATE adventure_runs SET state='running' WHERE id=?`
	default:
		httpx.Error(w, http.StatusBadRequest, "unknown action")
		return
	}
	if state != want {
		httpx.Error(w, http.StatusConflict, "cannot "+action+" a run in state "+state)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), update, runID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	kind := map[string]string{"start": runevents.RunStarted, "pause": runevents.RunPaused, "resume": runevents.RunResumed}[action]
	runevents.Record(r.Context(), h.DB, runID, nil, &arr.ID, kind, nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// StartRun moves a scheduled run into the running state.
func (h *Handlers) StartRun(w http.ResponseWriter, r *http.Request) { h.transitionRun(w, r, "start") }

type updateRunRequest struct {
	// ScheduledAt: pointer so the client can distinguish "not provided" from
	// "explicitly cleared". Empty string clears the schedule.
	ScheduledAt *string `json:"scheduled_at,omitempty"`
}

// UpdateRun amends mutable properties of a scheduled run — currently only
// scheduled_at. The run must be in the scheduled state; once it starts, the
// field is meaningless and is rejected to avoid confusion.
func (h *Handlers) UpdateRun(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	arr, _ := auth.FromContext(r.Context())
	var (
		state string
		owner int64
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT state, arranger_user_id FROM adventure_runs WHERE id = ?`, runID).
		Scan(&state, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if arr.Role != auth.RoleRoot && owner != arr.ID {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if state != "scheduled" {
		httpx.Error(w, http.StatusConflict, "can only reschedule a run while it is 'scheduled'")
		return
	}

	var req updateRunRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.ScheduledAt == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	var schedAt any // nil clears
	if *req.ScheduledAt != "" {
		t, perr := parseISO8601(*req.ScheduledAt)
		if perr != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid scheduled_at: "+perr.Error())
			return
		}
		schedAt = t.UTC().Format("2006-01-02 15:04:05")
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventure_runs SET scheduled_at = ? WHERE id = ?`, schedAt, runID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// PauseRun pauses a running run (players see a paused screen and cannot submit).
func (h *Handlers) PauseRun(w http.ResponseWriter, r *http.Request) { h.transitionRun(w, r, "pause") }

// ResumeRun resumes a paused run.
func (h *Handlers) ResumeRun(w http.ResponseWriter, r *http.Request) { h.transitionRun(w, r, "resume") }

type createTeamRequest struct {
	Name          string  `json:"name"`
	MemberUserIDs []int64 `json:"member_user_ids"`
}

type teamDTO struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// CreateTeam creates a team on a run and attaches existing players to it.
// Players must be created separately (POST /api/admin/users) — this endpoint
// will not provision user accounts on the fly.
func (h *Handlers) CreateTeam(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	var req createTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "team name required")
		return
	}
	if len(req.MemberUserIDs) == 0 {
		httpx.Error(w, http.StatusBadRequest, "at least one member required")
		return
	}
	arr, _ := auth.FromContext(r.Context())

	// Confirm the arranger owns this run.
	var owner int64
	err := h.DB.QueryRowContext(r.Context(), `SELECT arranger_user_id FROM adventure_runs WHERE id = ?`, runID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owner != arr.ID {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()

	// Enforce the assignment's team cap. A run always has an assignment
	// (arrangers can't create a run without one); max_teams is authoritative.
	var (
		maxTeams    int
		teamsCount  int
	)
	err = tx.QueryRowContext(r.Context(), `
		SELECT COALESCE(aa.max_teams, 0),
		       (SELECT COUNT(*) FROM teams t WHERE t.run_id = ?)
		FROM adventure_runs ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = ?
	`, runID, runID).Scan(&maxTeams, &teamsCount)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "cap lookup failed")
		return
	}
	if maxTeams > 0 && teamsCount >= maxTeams {
		httpx.Error(w, http.StatusConflict,
			"this run's assignment allows up to "+strconv.Itoa(maxTeams)+" team(s); buy another assignment for more")
		return
	}

	res, err := tx.ExecContext(r.Context(), `INSERT INTO teams(run_id, name) VALUES (?, ?)`, runID, req.Name)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "team name already used for this run")
		return
	}
	teamID, _ := res.LastInsertId()

	members := make([]string, 0, len(req.MemberUserIDs))
	for _, uid := range req.MemberUserIDs {
		var (
			username, role string
			ownedBy        sql.NullInt64
		)
		err := tx.QueryRowContext(r.Context(),
			`SELECT username, role, created_by_user_id FROM users WHERE id = ?`, uid).Scan(&username, &role, &ownedBy)
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusBadRequest, "unknown user id")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "user lookup failed")
			return
		}
		if role != auth.RolePlayer {
			httpx.Error(w, http.StatusBadRequest, username+" is not a player")
			return
		}
		// Enforce ownership: arrangers may only attach their own players.
		if arr.Role != auth.RoleRoot && (!ownedBy.Valid || ownedBy.Int64 != arr.ID) {
			httpx.Error(w, http.StatusForbidden, username+" is not one of your players")
			return
		}

		// A player may only belong to one team per run. Friendly check here;
		// the trigger in migration 0002 is the authoritative enforcement.
		var existingTeam string
		err = tx.QueryRowContext(r.Context(), `
			SELECT t.name FROM team_members tm
			JOIN teams t ON t.id = tm.team_id
			WHERE tm.user_id = ? AND t.run_id = ?
		`, uid, runID).Scan(&existingTeam)
		if err == nil {
			httpx.Error(w, http.StatusConflict, username+" is already on team "+existingTeam)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusInternalServerError, "membership check failed")
			return
		}

		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO team_members(team_id, user_id) VALUES (?, ?)
		`, teamID, uid); err != nil {
			httpx.Error(w, http.StatusConflict, "could not attach "+username+": "+err.Error())
			return
		}
		members = append(members, username)
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	runevents.Record(r.Context(), h.DB, runID, &teamID, &arr.ID, runevents.TeamCreated,
		map[string]any{"name": req.Name, "members": members})
	httpx.JSON(w, http.StatusCreated, teamDTO{ID: teamID, Name: req.Name, Members: members})
}

type positionDTO struct {
	TeamID    int64   `json:"team_id"`
	TeamName  string  `json:"team_name"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	ServerTS  string  `json:"server_ts"`
	PageIndex int     `json:"page_index"`
}

// ListPositions returns the most recent location ping per team for a run,
// plus each team's current puzzle index (useful for the arranger map).
func (h *Handlers) ListPositions(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	arr, _ := auth.FromContext(r.Context())
	// Ownership check.
	var owner int64
	err := h.DB.QueryRowContext(r.Context(), `SELECT arranger_user_id FROM adventure_runs WHERE id = ?`, runID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if owner != arr.ID {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index,
		       COALESCE(p.lat, 0), COALESCE(p.lng, 0), COALESCE(p.server_ts, '')
		FROM teams t
		LEFT JOIN (
			SELECT team_id, lat, lng, server_ts
			FROM location_pings
			WHERE id IN (
				SELECT MAX(id) FROM location_pings WHERE run_id = ? GROUP BY team_id
			)
		) p ON p.team_id = t.id
		WHERE t.run_id = ?
		ORDER BY t.name
	`, runID, runID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	out := []positionDTO{}
	for rows.Next() {
		var d positionDTO
		if err := rows.Scan(&d.TeamID, &d.TeamName, &d.PageIndex, &d.Lat, &d.Lng, &d.ServerTS); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"positions": out})
}

func runIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// parseISO8601 accepts the ISO-8601 variants browsers emit (Date.toISOString
// gives fractional seconds; datetime-local inputs often don't).
func parseISO8601(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("need RFC3339 (e.g. 2026-04-22T14:00:00Z)")
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

// ListTracks returns the full ping history per team for a run — used to
// draw movement polylines on the arranger map.
func (h *Handlers) ListTracks(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	arr, _ := auth.FromContext(r.Context())
	var owner int64
	err := h.DB.QueryRowContext(r.Context(), `SELECT arranger_user_id FROM adventure_runs WHERE id = ?`, runID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if arr.Role != auth.RoleRoot && owner != arr.ID {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT p.team_id, t.name, p.lat, p.lng, p.server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id
		WHERE p.run_id = ?
		ORDER BY p.team_id, p.server_ts
	`, runID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	byTeam := map[int64]*trackDTO{}
	order := []int64{}
	for rows.Next() {
		var (
			teamID   int64
			name     string
			p        trackPoint
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
	httpx.JSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}
