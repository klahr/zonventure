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
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
)

// authorizeTeam returns (runID, orgID, true) when the caller may operate on
// the team, writing the appropriate error and returning false otherwise.
func (h *Handlers) authorizeTeam(w http.ResponseWriter, r *http.Request, teamID int64) (runID, orgID int64, ok bool) {
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.run_id, ar.org_id
		FROM teams t JOIN adventure_runs ar ON ar.id = t.run_id
		WHERE t.id = ?
	`, teamID).Scan(&runID, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return 0, 0, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return 0, 0, false
	}
	me, _ := auth.FromContext(r.Context())
	if !sameOrg(me, orgID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return 0, 0, false
	}
	return runID, orgID, true
}

type renameTeamRequest struct {
	Name string `json:"name"`
}

// RenameTeam changes a team's display name.
func (h *Handlers) RenameTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, _, ok := h.authorizeTeam(w, r, teamID)
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
	runevents.Record(r.Context(), h.DB, runID, &teamID, runevents.UserActor(me.ID), runevents.TeamRenamed,
		map[string]any{"from": oldName, "to": req.Name})
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteTeam removes a team and all its slots. Password-gated.
func (h *Handlers) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, _, ok := h.authorizeTeam(w, r, teamID)
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
	runevents.Record(r.Context(), h.DB, runID, nil, runevents.UserActor(me.ID), runevents.TeamDeleted,
		map[string]any{"name": name})
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Slot management (players on a team) ---------------- */

type addSlotRequest struct {
	Name string `json:"name"`
}

// slotDTO is a single slot as returned to the arranger — includes the
// magic-link token so the UI can show a "copy link" affordance.
type slotDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

// AddSlot creates a new slot on a team with a fresh magic-link token.
func (h *Handlers) AddSlot(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, _, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
	var req addSlotRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	token, err := auth.NewSlotToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO player_slots(team_id, name, token) VALUES (?, ?, ?)`,
		teamID, req.Name, token)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "name already taken on this team")
		return
	}
	id, _ := res.LastInsertId()
	me, _ := auth.FromContext(r.Context())
	runevents.Record(r.Context(), h.DB, runID, &teamID, runevents.UserActor(me.ID),
		runevents.MemberAdded, map[string]any{"name": req.Name})
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
	httpx.JSON(w, http.StatusCreated, slotDTO{ID: id, Name: req.Name, Token: token})
}

// RemoveSlot deletes a slot.
func (h *Handlers) RemoveSlot(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, _, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
	slotID, err := strconv.ParseInt(chi.URLParam(r, "slotID"), 10, 64)
	if err != nil || slotID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad slot id")
		return
	}
	var name string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT name FROM player_slots WHERE id = ? AND team_id = ?`, slotID, teamID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "slot not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM player_slots WHERE id = ? AND team_id = ?`, slotID, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	runevents.Record(r.Context(), h.DB, runID, &teamID, runevents.UserActor(me.ID),
		runevents.MemberRemoved, map[string]any{"name": name})
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RotateSlotToken generates a fresh token for a slot — used when the old
// link has leaked.
func (h *Handlers) RotateSlotToken(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	if _, _, ok := h.authorizeTeam(w, r, teamID); !ok {
		return
	}
	slotID, err := strconv.ParseInt(chi.URLParam(r, "slotID"), 10, 64)
	if err != nil || slotID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad slot id")
		return
	}
	token, err := auth.NewSlotToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE player_slots SET token = ? WHERE id = ? AND team_id = ?`, token, slotID, teamID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "slot not found")
		return
	}
	// Invalidate any existing slot sessions so the old link stops working.
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM slot_sessions WHERE slot_id = ?`, slotID)
	httpx.JSON(w, http.StatusOK, map[string]any{"token": token})
}

// UnlockTeam advances a stuck team past their current puzzle.
func (h *Handlers) UnlockTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	runID, _, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return
	}
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
		runevents.Record(r.Context(), h.DB, runID, &teamID, runevents.UserActor(me.ID), runevents.TeamUnlocked,
			map[string]any{"from_page_index": current, "to_page_index": next, "by_arranger": true})
		runevents.Record(r.Context(), h.DB, runID, &teamID, runevents.UserActor(me.ID), runevents.TeamCompleted,
			map[string]any{"by_arranger": true})
	} else {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE teams SET current_page_index = ? WHERE id = ?`, next, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		runevents.Record(r.Context(), h.DB, runID, &teamID, runevents.UserActor(me.ID), runevents.TeamUnlocked,
			map[string]any{"from_page_index": current, "to_page_index": next, "by_arranger": true})
	}
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"page_index": next, "completed": next >= len(adv.Pages)})
}

func teamIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "teamID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
