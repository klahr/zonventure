package public

import (
	"net/http"

	"github.com/klahr/zonventure/internal/ws"
	"github.com/go-chi/chi/v5"
)

// WSLive is the TV-leaderboard WebSocket endpoint. The event is identified
// by the same opaque /live/{token} URL segment as the HTTP endpoint, which
// keeps this connection unauthenticated but unguessable.
//
// Server→client frames:
//
//	{"kind":"snapshot","data": LiveDTO}
//	{"kind":"tracks",  "data": TracksResponse}  (only when show_map is on)
//
// The client does not need to send anything: the handler pushes on connect
// and re-pushes whenever the event-scoped hub topic fires.
func (h *Handlers) WSLive(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "bad live token", http.StatusBadRequest)
		return
	}
	// Resolve the token once up front so we can refuse bad tokens without
	// opening a socket just to close it.
	eventID, _, _, _, _, _, showMap, err := h.runByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	conn, err := ws.Accept(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	events, unsub := h.Hub.Subscribe(ws.EventTopic(eventID))
	defer unsub()

	push := func() {
		snap, _, status, _ := h.buildLive(conn.Context(), token)
		if status == http.StatusOK {
			conn.SendKind("snapshot", snap)
		}
		// show_map may flip at runtime — read the latest state from the
		// snapshot rather than the cached initial value.
		if snap.ShowMap {
			tracks, tStatus, _ := h.buildTracks(conn.Context(), eventID)
			if tStatus == http.StatusOK {
				conn.SendKind("tracks", tracks)
			}
		}
	}
	push()
	_ = showMap // retained above only for a future optimisation path.

	for {
		select {
		case <-conn.Context().Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			push()
		}
	}
}
