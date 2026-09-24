package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/eventmsg"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/ws"
)

// ListEventMessages lists what the organisers have told the event's
// teams, newest first.
//
// GET /api/admin/events/{eventID}/messages
func (h *Handlers) ListEventMessages(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	msgs, err := eventmsg.List(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading messages failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// SendEventMessage sends a message to every team of the event, or with
// team_ids only to those. Teams' phones show it at once.
//
// POST /api/admin/events/{eventID}/messages
func (h *Handlers) SendEventMessage(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	var req struct {
		Text    string  `json:"text"`
		TeamIDs []int64 `json:"team_ids"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	var userID int64
	if me, ok := auth.FromContext(r.Context()); ok {
		userID = me.ID
	}
	m, err := eventmsg.Send(r.Context(), h.DB, eventID, userID, req.Text, req.TeamIDs)
	switch {
	case errors.Is(err, eventmsg.ErrEmpty), errors.Is(err, eventmsg.ErrTooLong), errors.Is(err, eventmsg.ErrTeam):
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "sending message failed", err)
		return
	}
	h.publishMessages(eventID)
	httpx.JSON(w, http.StatusCreated, m)
}

// DeleteEventMessage takes a message back: teams no longer see it.
//
// DELETE /api/admin/events/{eventID}/messages/{messageID}
func (h *Handlers) DeleteEventMessage(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "messageID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad message id")
		return
	}
	switch err := eventmsg.Delete(r.Context(), h.DB, eventID, id); {
	case errors.Is(err, eventmsg.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "deleting message failed", err)
		return
	}
	h.publishMessages(eventID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) messagesEvent(w http.ResponseWriter, r *http.Request) (int64, bool) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return 0, false
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return 0, false
	}
	return eventID, true
}

// publishMessages tells the event's phones to read their state again,
// which carries the messages.
func (h *Handlers) publishMessages(eventID int64) {
	if h.Hub != nil {
		h.Hub.Publish(ws.EventTopic(eventID), "messages", nil)
	}
}
