package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/eventfile"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
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
// at most eventfile.MaxSize. Teams' phones show it at once.
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
	h.publishMessages(eventID)
	httpx.JSON(w, http.StatusCreated, file)
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
	files, err := eventfile.List(ctx, h.DB, eventID)
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
