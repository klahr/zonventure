package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/eventfile"
	"github.com/klahr/zonventure/internal/eventmsg"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// ListEventFiles lists the files the event's players can download.
//
// GET /api/admin/events/{eventID}/files
func (h *Handlers) ListEventFiles(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	files, err := eventfile.List(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading files failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"files": files, "max_files": eventfile.MaxFiles, "max_size": eventfile.MaxSize})
}

// AddEventFile uploads a file for the event's players: multipart `file`,
// at most eventfile.MaxSize. Teams' phones show it at once. With
// ?announce=0 the teams get no message for it: the organiser's page
// uploads several files that way and announces them together
// (AnnounceEventFiles).
//
// POST /api/admin/events/{eventID}/files
func (h *Handlers) AddEventFile(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, eventfile.MaxSize+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, eventfile.ErrTooLarge.Error())
			return
		}
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, eventfile.MaxSize+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	var userID int64
	if me, ok := auth.FromContext(r.Context()); ok {
		userID = me.ID
	}
	file, err := eventfile.Add(r.Context(), h.DB, eventID, userID, hdr.Filename, data)
	switch {
	case errors.Is(err, eventfile.ErrTooLarge):
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	case errors.Is(err, eventfile.ErrEmpty), errors.Is(err, eventfile.ErrUnsupported), errors.Is(err, eventfile.ErrTooMany):
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "storing file failed", err)
		return
	}
	// On an event sold in the store a new file waits for review.
	if err := h.settleEventListing(r.Context(), eventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "listing review failed", err)
		return
	}
	if r.URL.Query().Get("announce") != "0" {
		h.announceFiles(r.Context(), eventID, userID, []string{file.Name})
	}
	h.publishMessages(eventID)
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusCreated, file)
}

// announceFiles tells the teams about files added mid-game in one
// message, or they appear in the menu unnoticed. Not on a store event,
// where a new file waits for review, nor before the start.
func (h *Handlers) announceFiles(ctx context.Context, eventID, userID int64, names []string) {
	var live bool
	_ = h.DB.QueryRowContext(ctx, `SELECT state IN ('running', 'paused') AND NOT public_for_sale FROM events WHERE id = $1`, eventID).Scan(&live)
	if !live || len(names) == 0 {
		return
	}
	lang := h.eventLook(ctx, eventID).Lang
	text := emailtpl.T(lang, "New file: %s. Open the menu and look under “Files from the organisers”.", names[0])
	if len(names) > 1 {
		text = emailtpl.T(lang, "%d new files: %s. Open the menu and look under “Files from the organisers”.", len(names), strings.Join(names, ", "))
	}
	if _, err := eventmsg.Send(ctx, h.DB, eventID, userID, text, nil); err != nil {
		logx.Warn("admin.event_files.announce", "event", eventID, "err", err.Error())
	}
}

// AnnounceEventFiles sends the teams one message for files uploaded
// together with ?announce=0: {"file_ids": [...]}.
//
// POST /api/admin/events/{eventID}/files/announce
func (h *Handlers) AnnounceEventFiles(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	var req struct {
		FileIDs []int64 `json:"file_ids"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	files, err := eventfile.List(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading files failed", err)
		return
	}
	var names []string
	for _, f := range files {
		if slices.Contains(req.FileIDs, f.ID) {
			names = append(names, f.Name)
		}
	}
	if len(names) == 0 {
		httpx.Error(w, http.StatusBadRequest, "no such files")
		return
	}
	var userID int64
	if me, ok := auth.FromContext(r.Context()); ok {
		userID = me.ID
	}
	h.announceFiles(r.Context(), eventID, userID, names)
	h.publishMessages(eventID)
	w.WriteHeader(http.StatusNoContent)
}

// GetEventFile downloads one of the event's files, for the organisers
// to check what the players get.
//
// GET /api/admin/events/{eventID}/files/{fileID}
func (h *Handlers) GetEventFile(w http.ResponseWriter, r *http.Request) {
	eventID, id, ok := h.eventFileIDs(w, r)
	if !ok {
		return
	}
	f, content, err := eventfile.Get(r.Context(), h.DB, eventID, id)
	switch {
	case errors.Is(err, eventfile.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "loading file failed", err)
		return
	}
	eventfile.Serve(w, f, content)
}

// DeleteEventFile removes a file: players no longer see it.
//
// DELETE /api/admin/events/{eventID}/files/{fileID}
func (h *Handlers) DeleteEventFile(w http.ResponseWriter, r *http.Request) {
	eventID, id, ok := h.eventFileIDs(w, r)
	if !ok {
		return
	}
	switch err := eventfile.Delete(r.Context(), h.DB, eventID, id); {
	case errors.Is(err, eventfile.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "deleting file failed", err)
		return
	}
	if err := h.settleEventListing(r.Context(), eventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "listing review failed", err)
		return
	}
	h.publishMessages(eventID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) eventFileIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "fileID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad file id")
		return 0, 0, false
	}
	return eventID, id, true
}

// fileLinks are the event's files as a team's emails link them: by the
// team's share token, so they download without signing in to the game.
// A failure to read them leaves them out rather than holding up the
// email.
func (h *Handlers) fileLinks(ctx context.Context, eventID int64, publicURL, teamToken string) []emailtpl.FileLink {
	if publicURL == "" || teamToken == "" {
		return nil
	}
	files, err := eventfile.ListForPlayers(ctx, h.DB, eventID)
	if err != nil {
		logx.Warn("admin.event_files.links", "event", eventID, "err", err.Error())
		return nil
	}
	links := make([]emailtpl.FileLink, len(files))
	for i, f := range files {
		links[i] = emailtpl.FileLink{Name: f.Name, URL: publicURL + "/team/" + url.PathEscape(teamToken) + "/files/" + strconv.FormatInt(f.ID, 10)}
	}
	return links
}
