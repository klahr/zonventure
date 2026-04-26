package admin

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"git.sr.ht/~klahr/zv/internal/activity"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
)

// teamFromRequest is the canonical preamble for every team handler:
// extract the team id from the URL, authorise the caller, write the
// HTTP error on either failure. Returns (teamID, eventID, ok). When
// !ok, the response is already sent; the handler should just return.
func (h *Handlers) teamFromRequest(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return 0, 0, false
	}
	eventID, _, ok := h.authorizeTeam(w, r, teamID)
	if !ok {
		return 0, 0, false
	}
	return teamID, eventID, true
}

// authorizeTeam returns (eventID, orgID, true) when the caller may operate on
// the team, writing the appropriate error and returning false otherwise.
func (h *Handlers) authorizeTeam(w http.ResponseWriter, r *http.Request, teamID int64) (eventID, orgID int64, ok bool) {
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.event_id, ar.org_id
		FROM teams t JOIN events ar ON ar.id = t.event_id
		WHERE t.id = $1
	`, teamID).Scan(&eventID, &orgID)
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
	return eventID, orgID, true
}

type renameTeamRequest struct {
	Name string `json:"name"`
}

// RenameTeam changes a team's display name.
func (h *Handlers) RenameTeam(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
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
	// Best-effort: capture the old name for the audit row. A miss
	// here just means the audit shows old=""; not worth failing the
	// rename for.
	var oldName string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT name FROM teams WHERE id = $1`, teamID).Scan(&oldName)
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE teams SET name = $1 WHERE id = $2`, req.Name, teamID); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used in this event")
		return
	}
	me, _ := auth.FromContext(r.Context())
	activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamRenamed,
		map[string]any{"from": oldName, "to": req.Name})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteTeam removes a team and all its slots. Password-gated.
func (h *Handlers) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	// Best-effort: capture the name for the audit row before delete.
	// If the row's already gone, scan errors and we audit with name="".
	var name string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT name FROM teams WHERE id = $1`, teamID).Scan(&name)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM teams WHERE id = $1`, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	activity.Record(r.Context(), h.DB, eventID, nil, activity.UserActor(me.ID), activity.TeamDeleted,
		map[string]any{"name": name})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Slot management (players on a team) ---------------- */

type addSlotRequest struct {
	Name string `json:"name"`
}

// slotDTO is a single slot as returned to the arranger — includes the
// magic-link token so the UI can show a "copy link" affordance.
// ExpiresAt is the magic-link cutoff in our wire format
// ("YYYY-MM-DD HH:MM:SS" UTC); empty means "never expires".
type slotDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// AddSlot creates a new slot on a team with a fresh magic-link token.
func (h *Handlers) AddSlot(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
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
	expiresAt := auth.DefaultSlotExpiresAt(r.Context(), h.DB, eventID)
	var id int64
	if err := h.DB.QueryRowContext(r.Context(),
		`INSERT INTO player_slots(team_id, name, token, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		teamID, req.Name, token, expiresAt).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "name already taken on this team")
		return
	}
	me, _ := auth.FromContext(r.Context())
	activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID),
		activity.MemberAdded, map[string]any{"name": req.Name})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusCreated, slotDTO{
		ID: id, Name: req.Name, Token: token, ExpiresAt: expiresAt,
	})
}

// RemoveSlot deletes a slot.
func (h *Handlers) RemoveSlot(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
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
		`SELECT name FROM player_slots WHERE id = $1 AND team_id = $2`, slotID, teamID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "slot not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM player_slots WHERE id = $1 AND team_id = $2`, slotID, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID),
		activity.MemberRemoved, map[string]any{"name": name})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RotateSlotToken generates a fresh token for a slot — used when the
// old link has leaked or expired. The expires_at is reset to the
// default (run-anchored) so the rotation extends the window in one
// step instead of forcing a separate "set expiry" call.
func (h *Handlers) RotateSlotToken(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
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
	expiresAt := auth.DefaultSlotExpiresAt(r.Context(), h.DB, eventID)
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE player_slots SET token = $1, expires_at = $2 WHERE id = $3 AND team_id = $4`,
		token, expiresAt, slotID, teamID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "slot not found")
		return
	}
	// Invalidate any existing slot sessions so the old link stops working.
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM slot_sessions WHERE slot_id = $1`, slotID)
	httpx.JSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expiresAt})
}

// UnlockTeam advances a stuck team past their current puzzle.
func (h *Handlers) UnlockTeam(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	var (
		slug    string
		current int
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT ar.adventure_slug, t.current_page_index
		FROM teams t JOIN events ar ON ar.id = t.event_id
		WHERE t.id = $1
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
			`UPDATE teams SET current_page_index = $1, current_page_started_at = now_text(), completed_at = now_text() WHERE id = $2`,
			next, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamUnlocked,
			map[string]any{"from_page_index": current, "to_page_index": next, "by_arranger": true})
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamCompleted,
			map[string]any{"by_arranger": true})
		_, _ = activity.MaybeCompleteEvent(r.Context(), h.DB, eventID)
	} else {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE teams SET current_page_index = $1, current_page_started_at = now_text() WHERE id = $2`, next, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamUnlocked,
			map[string]any{"from_page_index": current, "to_page_index": next, "by_arranger": true})
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"page_index": next, "completed": next >= len(adv.Pages)})
}

func teamIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "teamID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

/* ---------------- Team avatar ---------------- */

// SetTeamAvatar stores a single uploaded profile picture on the team row.
// Multipart POST with a `file` field, same contract as page images. Any
// prior avatar is replaced; the DTO returns the new URL so the client
// can refresh its cache.
func (h *Handlers) SetTeamAvatar(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	if _, _, ok := h.authorizeTeam(w, r, teamID); !ok {
		return
	}
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "image/") {
		httpx.Error(w, http.StatusBadRequest, "only image uploads are accepted")
		return
	}
	shrunk, newMIME, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = shrunk, newMIME
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE teams
		   SET avatar_mime_type = $1, avatar_size_bytes = $2, avatar_content = $3
		 WHERE id = $4
	`, mime, int64(len(data)), data, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "store failed: "+err.Error())
		return
	}
	// Hub push so the event-detail view and any live-board listeners pick
	// up the new picture without a manual refresh. Best-effort: a missing
	// event_id (race with team deletion) just means we skip the broadcast.
	var eventID int64
	_ = h.DB.QueryRowContext(r.Context(), `SELECT event_id FROM teams WHERE id = $1`, teamID).Scan(&eventID)
	if eventID > 0 {
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  teamAvatarURL(teamID),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteTeamAvatar clears the stored avatar so the team falls back to
// the generated initials rendering on the client.
func (h *Handlers) DeleteTeamAvatar(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE teams
		   SET avatar_mime_type = NULL, avatar_size_bytes = NULL, avatar_content = NULL
		 WHERE id = $1
	`, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeTeamAvatar writes the stored blob to the response. No auth: the
// picture is meant to surface in the public live board and the player
// view, neither of which hold a staff session. The team id is opaque
// enough that this is an acceptable read-only leak — nothing sensitive
// lives here.
func (h *Handlers) ServeTeamAvatar(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "teamID"), 10, 64)
	if err != nil || teamID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT avatar_mime_type, avatar_size_bytes, avatar_content FROM teams WHERE id = $1
	`, teamID).Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime.String)
	w.Header().Set("Content-Length", strconv.FormatInt(size.Int64, 10))
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(content)
}

// teamAvatarURL is the canonical path for a team's profile picture.
// Returns the empty string for teams without one; callers pair that
// with a client-side initials fallback.
func teamAvatarURL(teamID int64) string {
	return fmt.Sprintf("/adventure-assets/team-avatar/%d", teamID)
}
