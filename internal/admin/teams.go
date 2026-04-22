package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/runevents"
	"github.com/go-chi/chi/v5"
)

// teamOwnership returns (runID, arrangerUserID) for a team, or sql.ErrNoRows
// if the team does not exist.
func (h *Handlers) teamOwnership(r *http.Request, teamID int64) (int64, int64, error) {
	var runID, owner int64
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.run_id, ar.arranger_user_id
		FROM teams t JOIN adventure_runs ar ON ar.id = t.run_id
		WHERE t.id = ?
	`, teamID).Scan(&runID, &owner)
	return runID, owner, err
}

// authorizeTeam ensures the caller owns the run that team belongs to (root
// also passes — it has full admin powers). Writes the appropriate HTTP
// error on failure and returns false.
func (h *Handlers) authorizeTeam(w http.ResponseWriter, r *http.Request, teamID int64) (int64, bool) {
	runID, owner, err := h.teamOwnership(r, teamID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return 0, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return 0, false
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleRoot && me.ID != owner {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return 0, false
	}
	return runID, true
}

type renameTeamRequest struct {
	Name string `json:"name"`
}

// RenameTeam changes a team's display name. The (run_id, name) UNIQUE index
// means duplicate names in the same run come back as a 409.
func (h *Handlers) RenameTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
	var req renameTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	var oldName string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT name FROM teams WHERE id = ?`, teamID).Scan(&oldName)
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE teams SET name = ? WHERE id = ?`, req.Name, teamID); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used in this run")
		return
	}
	me, _ := auth.FromContext(r.Context())
	runevents.Record(r.Context(), h.DB, runID, &teamID, &me.ID, runevents.TeamRenamed,
		map[string]any{"from": oldName, "to": req.Name})
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteTeam removes a team and its members. Progress for the team (pings,
// attempts) cascades via the schema. Requires the arranger's password in
// X-Confirm-Password.
func (h *Handlers) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
	var name string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT name FROM teams WHERE id = ?`, teamID).Scan(&name)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM teams WHERE id = ?`, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	runevents.Record(r.Context(), h.DB, runID, nil, &me.ID, runevents.TeamDeleted,
		map[string]any{"name": name})
	httpx.JSON(w, http.StatusNoContent, nil)
}

type addMemberRequest struct {
	UserID int64 `json:"user_id"`
}

// AddMember attaches a player to a team. Enforces that the user is a player
// and isn't already on another team in this run.
func (h *Handlers) AddMember(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
	var req addMemberRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	var (
		username, role string
		ownedBy        sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT username, role, created_by_user_id FROM users WHERE id = ?`, req.UserID).Scan(&username, &role, &ownedBy)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusBadRequest, "unknown user")
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
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleRoot && (!ownedBy.Valid || ownedBy.Int64 != me.ID) {
		httpx.Error(w, http.StatusForbidden, username+" is not one of your players")
		return
	}

	var existingTeam string
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT t.name FROM team_members tm JOIN teams t ON t.id = tm.team_id
		WHERE tm.user_id = ? AND t.run_id = ?
	`, req.UserID, runID).Scan(&existingTeam)
	if err == nil {
		httpx.Error(w, http.StatusConflict, username+" is already on team "+existingTeam)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusInternalServerError, "membership check failed")
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO team_members(team_id, user_id) VALUES (?, ?)`, teamID, req.UserID); err != nil {
		httpx.Error(w, http.StatusConflict, "could not attach: "+err.Error())
		return
	}
	runevents.Record(r.Context(), h.DB, runID, &teamID, &me.ID, runevents.MemberAdded,
		map[string]any{"username": username})
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RemoveMember detaches a player from a team.
func (h *Handlers) RemoveMember(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
	userID, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var username string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id = ?`, userID).Scan(&username)
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "remove failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "not a member")
		return
	}
	me, _ := auth.FromContext(r.Context())
	runevents.Record(r.Context(), h.DB, runID, &teamID, &me.ID, runevents.MemberRemoved,
		map[string]any{"username": username})
	httpx.JSON(w, http.StatusNoContent, nil)
}

// UnlockTeam nudges the team past their current puzzle — intended for
// arrangers helping a stuck team. Advances current_page_index by 1 and, if
// that was the last page, also marks the team completed. Idempotent only in
// the sense that completed teams can't be advanced further.
func (h *Handlers) UnlockTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}

	// Need the adventure slug so we know how many pages there are.
	var (
		slug    string
		current int
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT ar.adventure_slug, t.current_page_index
		FROM teams t JOIN adventure_runs ar ON ar.id = t.run_id
		WHERE t.id = ?
	`, teamID).Scan(&slug, &current)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
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
	if current >= len(adv.Pages) {
		httpx.Error(w, http.StatusConflict, "team already finished")
		return
	}

	next := current + 1
	me, _ := auth.FromContext(r.Context())
	if next >= len(adv.Pages) {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE teams SET current_page_index = ?, completed_at = datetime('now') WHERE id = ?`,
			next, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		runevents.Record(r.Context(), h.DB, runID, &teamID, &me.ID, runevents.TeamUnlocked,
			map[string]any{"from_page_index": current, "to_page_index": next, "by_arranger": true})
		runevents.Record(r.Context(), h.DB, runID, &teamID, &me.ID, runevents.TeamCompleted,
			map[string]any{"by_arranger": true})
	} else {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE teams SET current_page_index = ? WHERE id = ?`, next, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		runevents.Record(r.Context(), h.DB, runID, &teamID, &me.ID, runevents.TeamUnlocked,
			map[string]any{"from_page_index": current, "to_page_index": next, "by_arranger": true})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"page_index": next, "completed": next >= len(adv.Pages)})
}

func teamIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "teamID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
