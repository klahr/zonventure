package public

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/ws"
)

// WSLive is the TV-leaderboard WebSocket endpoint. The event is identified
// by the same opaque /live/{token} URL segment as the HTTP endpoint, which
// keeps this connection unauthenticated but unguessable.
//
// Server→client frames:
//
//	{"kind":"snapshot","data": LiveDTO}
//	{"kind":"tracks",  "data": TracksResponse}  (only when show_map is on)
//	{"kind":"heartbeat"}                       (every liveHeartbeat)
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

	events, unsub := h.Hub.Subscribe(ws.EventTopic(eventID), ws.PositionsTopic(eventID))
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

	// The client never sends anything, but the connection must still be
	// read: the ping in ws.Conn only sees its pong while a Read is in
	// flight, and without one every socket timed out 40 s in. A read
	// error means the peer has gone.
	go func() {
		for {
			if _, err := conn.Read(); err != nil {
				conn.Close()
				return
			}
		}
	}()

	// An application-level heartbeat as well: the board's watchdog
	// reconnects when no frame has arrived for a while, which catches
	// a socket left half-open by a TV waking from sleep.
	go func() {
		t := time.NewTicker(liveHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-conn.Context().Done():
				return
			case <-t.C:
				conn.SendKind("heartbeat", nil)
			}
		}
	}()

	ws.Coalesce(conn.Context(), events, time.Second, push)
}

// liveHeartbeat is how often WSLive sends a heartbeat frame. live.html
// treats 60 s without any frame as a dead socket.
const liveHeartbeat = 25 * time.Second
