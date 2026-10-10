package player

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/eventfile"
	"github.com/klahr/zonventure/internal/httpx"
)

// GetEventFile downloads a file the organisers left for the event's
// players (PlayEvent.Files).
//
// GET /api/player/files/{fileID}
func (h *Handlers) GetEventFile(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "fileID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusNotFound, "no such file")
		return
	}
	f, content, err := eventfile.GetForPlayers(r.Context(), h.DB, slot.EventID, id)
	switch {
	case errors.Is(err, eventfile.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "no such file")
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "reading file failed", err)
		return
	}
	eventfile.Serve(w, f, content)
}
