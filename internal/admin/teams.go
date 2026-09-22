package admin

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/lanes"
	"github.com/klahr/zonventure/internal/storage"
	"github.com/klahr/zonventure/internal/webhooks"
	"github.com/klahr/zonventure/internal/ws"
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

// updateTeamRequest accepts partial edits — any field set to a
// non-nil pointer is applied; nil pointers leave the column
// untouched. Pointer-on-string is the canonical Go shape for
// "field omitted vs explicitly empty"; an empty *Email pointer
// means "clear the email", a nil *Email means "don't touch".
//
// Renamed from renameTeamRequest now that the endpoint covers
// more than just the name. The route path stays — this is still
// PATCH /api/admin/teams/{id}.
type updateTeamRequest struct {
	Name  *string `json:"name,omitempty"`
	Email *string `json:"email,omitempty"`
}

// UpdateTeam applies a partial edit (name, email). Permission
// gates on OpRenameTeam — same operation envelope as the
// historical rename, since email is purely arranger-side
// metadata and players can't see it on the share page.
func (h *Handlers) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	var req updateTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Email == nil {
		httpx.Error(w, http.StatusBadRequest, "no fields to update")
		return
	}
	// Pick the op that matches what's being touched. Name → rename
	// gate (locked once the event hits a terminal state). Email-only
	// → the more permissive metadata gate so an arranger can fill
	// in the captain's address after the event finished and still
	// kick off the recap email. When BOTH fields are set, the
	// stricter rename gate wins — sending a name change post-
	// completion would be unexpected, so the request 409s the way
	// it always has.
	op := OpEditTeamEmail
	if req.Name != nil {
		op = OpRenameTeam
	}
	if !h.requireEventOp(w, r.Context(), eventID, op) {
		return
	}
	var (
		newName  string
		setName  bool
		newEmail string
		setEmail bool
	)
	if req.Name != nil {
		n, err := validateName(*req.Name, MaxNameLen)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		newName = n
		setName = true
	}
	if req.Email != nil {
		em, err := normaliseTeamEmail(*req.Email)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		newEmail = em
		setEmail = true
	}
	// Capture the old row best-effort so the audit entry has both
	// before and after — a miss leaves the from-fields blank rather
	// than failing the update. Same posture as the previous
	// rename-only handler.
	var oldName, oldEmail string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT name, COALESCE(email, '') FROM teams WHERE id = $1`,
		teamID,
	).Scan(&oldName, &oldEmail)

	// Build the SET clause from whichever fields were supplied.
	// Two columns at most — a tiny manual builder beats pulling in
	// a query-builder dep for a single endpoint.
	sets := []string{}
	args := []any{}
	if setName {
		sets = append(sets, "name = $"+strconv.Itoa(len(args)+1))
		args = append(args, newName)
	}
	if setEmail {
		sets = append(sets, "email = $"+strconv.Itoa(len(args)+1))
		args = append(args, newEmail)
	}
	args = append(args, teamID)
	q := "UPDATE teams SET " + strings.Join(sets, ", ") +
		" WHERE id = $" + strconv.Itoa(len(args))
	if _, err := h.DB.ExecContext(r.Context(), q, args...); err != nil {
		// UNIQUE(event_id, name) is the realistic failure mode.
		// Other DB errors read as conflict from the player flow's
		// perspective — the previous handler made the same call.
		httpx.Error(w, http.StatusConflict, "name already used in this event")
		return
	}

	me, _ := auth.FromContext(r.Context())
	if setName && newName != oldName {
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamRenamed,
			map[string]any{"from": oldName, "to": newName})
	}
	// Email-only edits don't have a dedicated activity kind today;
	// the rename-event covers the operation envelope ("the team
	// metadata changed"). A separate kind can land cleanly later
	// without re-shaping this handler.
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
	if !h.requireEventOp(w, r.Context(), eventID, OpDeleteTeam) {
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
	if !h.requireEventOp(w, r.Context(), eventID, OpAddSlot) {
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
	if !h.requireEventOp(w, r.Context(), eventID, OpRemoveSlot) {
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
	if !h.requireEventOp(w, r.Context(), eventID, OpRotateSlotToken) {
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
	if !h.requireEventOp(w, r.Context(), eventID, OpUnlockTeam) {
		return
	}
	var (
		slug          string
		currentIdx    int
		currentPageID sql.NullInt64
		completedAt   sql.NullString
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT ar.adventure_slug, t.current_page_index, t.current_page_id, t.completed_at
		FROM teams t JOIN events ar ON ar.id = t.event_id
		WHERE t.id = $1
	`, teamID).Scan(&slug, &currentIdx, &currentPageID, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if completedAt.Valid {
		httpx.Error(w, http.StatusConflict, "team already finished")
		return
	}
	adv := h.Lib.Get(slug)
	if adv == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded")
		return
	}
	// Resolve the team's current page id when not yet set (a team
	// that hasn't moved since the lanes migration backfilled it).
	curID := currentPageID.Int64
	if !currentPageID.Valid {
		_, firstID, ferr := lanes.FirstPageFor(r.Context(), h.DB, adv, teamID)
		if ferr != nil {
			httpx.Error(w, http.StatusInternalServerError, "resolve current failed")
			return
		}
		curID = firstID
	}
	if curID == 0 {
		httpx.Error(w, http.StatusConflict, "team already finished")
		return
	}
	nextLaneID, nextPageID, err := lanes.NextPageAfter(r.Context(), h.DB, adv, teamID, curID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "resolve next failed")
		return
	}
	nextIdx := currentIdx + 1
	me, _ := auth.FromContext(r.Context())
	if nextPageID == 0 {
		if _, err := h.DB.ExecContext(r.Context(), `
			UPDATE teams
			SET current_page_id = NULL,
			    current_lane_id = NULL,
			    current_page_index = $1,
			    current_page_started_at = now_text(),
			    completed_at = now_text()
			WHERE id = $2
		`, nextIdx, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamUnlocked,
			map[string]any{"from_page_index": currentIdx, "to_page_index": nextIdx, "by_arranger": true})
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamCompleted,
			map[string]any{"by_arranger": true})
		_, _ = activity.MaybeCompleteEvent(r.Context(), h.DB, eventID)
	} else {
		var laneArg any
		if nextLaneID != 0 {
			laneArg = nextLaneID
		} else {
			laneArg = sql.NullInt64{}
		}
		if _, err := h.DB.ExecContext(r.Context(), `
			UPDATE teams
			SET current_page_id = $1,
			    current_lane_id = $2,
			    current_page_index = $3,
			    current_page_started_at = now_text()
			WHERE id = $4
		`, nextPageID, laneArg, nextIdx, teamID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamUnlocked,
			map[string]any{"from_page_index": currentIdx, "to_page_index": nextIdx, "by_arranger": true})
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"page_index": nextIdx, "completed": nextPageID == 0})
}

// WithdrawTeam soft-flags a team as withdrawn (stamps withdrawn_at).
// Used when a team gives up mid-event but should still appear in the
// recap as "didn't finish — withdrew." Idempotent: a second call on a
// team already withdrawn is a no-op (still 200, no second activity
// row).
func (h *Handlers) WithdrawTeam(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	if !h.requireEventOp(w, r.Context(), eventID, OpWithdrawTeam) {
		return
	}
	var alreadyWithdrawn sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT withdrawn_at FROM teams WHERE id = $1`, teamID).Scan(&alreadyWithdrawn); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "team not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if alreadyWithdrawn.Valid && alreadyWithdrawn.String != "" {
		httpx.JSON(w, http.StatusOK, map[string]any{"withdrawn_at": alreadyWithdrawn.String})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE teams SET withdrawn_at = now_text() WHERE id = $1`, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	var withdrawnAt string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT withdrawn_at FROM teams WHERE id = $1`, teamID).Scan(&withdrawnAt)
	me, _ := auth.FromContext(r.Context())
	activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamWithdrawn,
		map[string]any{"by_arranger": true})
	orgID, _ := h.eventOrg(r.Context(), eventID)
	webhooks.TryPublish(r.Context(), h.DB, orgID, webhooks.KindTeamWithdrawn, map[string]any{
		"event_id":      eventID,
		"team_id":       teamID,
		"withdrawn_at":  withdrawnAt,
	})
	// Withdrawing the last unfinished team should flip the event to
	// completed — same shape as an Unlock that takes the final team
	// past its last puzzle.
	_, _ = activity.MaybeCompleteEvent(r.Context(), h.DB, eventID)
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"withdrawn_at": withdrawnAt})
}

// UnwithdrawTeam clears the withdrawn_at flag — used to undo an
// accidental click or to bring a team back if they returned. Records a
// distinct activity kind ("team.returned") so the timeline reads
// honestly. Permission gating mirrors WithdrawTeam.
func (h *Handlers) UnwithdrawTeam(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	if !h.requireEventOp(w, r.Context(), eventID, OpWithdrawTeam) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE teams SET withdrawn_at = NULL WHERE id = $1 AND withdrawn_at IS NOT NULL`, teamID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		me, _ := auth.FromContext(r.Context())
		activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamReturned,
			map[string]any{"by_arranger": true})
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"withdrawn_at": ""})
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
	if !h.teamEventOp(w, r.Context(), teamID, OpSetTeamAvatar) {
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
		// Generic public message — the imageutil error text can carry
		// decoder internals (offsets, codec names) that don't help the
		// uploader and might leak format-detection state.
		httpx.ServerErr(w, http.StatusBadRequest, "image could not be processed", err)
		return
	}
	data, mime = shrunk, newMIME
	if err := h.Storage.PutTeamAvatar(r.Context(), teamID, data, mime); err != nil {
		// Pre-flight authorizeTeam already proved the team exists, so
		// ErrNotFound here means a delete-team race; surface as 404.
		if errors.Is(err, storage.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "team not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "store failed", err)
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
	if !h.requireEventOp(w, r.Context(), eventID, OpSetTeamAvatar) {
		return
	}
	if err := h.Storage.DeleteTeamAvatar(r.Context(), teamID); err != nil {
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
	content, mime, err := h.Storage.GetTeamAvatar(r.Context(), teamID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(content)
}

// teamAvatarURL is the canonical path for a team's profile picture.
// Returns the empty string for teams without one; callers pair that
// with a client-side initials fallback.
func teamAvatarURL(teamID int64) string {
	return fmt.Sprintf("/adventure-assets/team-avatar/%d", teamID)
}
