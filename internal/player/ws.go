package player

import (
	"encoding/json"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// WSPlay is the player-side WebSocket endpoint. It replaces the 5s state
// poll and 15s location POST: the server pushes a fresh "state" frame
// whenever anything in the run changes, and the client pipes its GPS fixes
// and answer submissions through the same socket.
//
// Requires a slot session — the HTTP upgrade is gated by RequireSlot, same
// as the REST endpoints.
func (h *Handlers) WSPlay(w http.ResponseWriter, r *http.Request) {
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := ws.Accept(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	events, unsub := h.Hub.Subscribe(ws.RunTopic(slot.RunID))
	defer unsub()

	// Push the initial state so the client has something to render while it
	// waits for its first broadcast.
	h.pushState(conn, slot)

	// Push-on-change: whenever anything in this run changes, re-read and
	// send the slot's current view. Coalescing is implicit — the hub's
	// 1-deep buffered channel drops duplicate pokes that arrive back to back.
	go func() {
		for {
			select {
			case <-conn.Context().Done():
				return
			case _, ok := <-events:
				if !ok {
					return
				}
				h.pushState(conn, slot)
			}
		}
	}()

	// Incoming: location pings and answer submissions. Read until peer goes
	// away or the writer cancels our context (eg. a send queue overflow).
	for {
		msg, err := conn.Read()
		if err != nil {
			return
		}
		switch msg.Kind {
		case "location":
			var req LocationRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				continue
			}
			status, _ := h.recordLocation(conn.Context(), slot, req)
			if status >= 400 {
				continue
			}
			h.Hub.Publish(ws.RunTopic(slot.RunID), "changed", nil)
		case "answer":
			var req AnswerRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				continue
			}
			resp, runID, status, errMsg := h.applyAnswer(conn.Context(), slot, req.Answer)
			if status != http.StatusOK {
				conn.SendKind("answer_error", map[string]any{"error": errMsg, "status": status})
				continue
			}
			conn.SendKind("answer_result", resp)
			if runID != 0 {
				h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
			}
		}
	}
}

func (h *Handlers) pushState(conn *ws.Conn, slot auth.Slot) {
	resp, status, _ := h.buildState(conn.Context(), slot)
	if status != http.StatusOK {
		return
	}
	conn.SendKind("state", resp)
}
