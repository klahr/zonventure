package player

import (
	"encoding/json"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// WSPlay is the player-side WebSocket endpoint. It replaces the 5s state
// poll and 15s location POST: the server pushes a fresh "state" frame
// whenever anything in the event changes, and the client pipes its GPS fixes
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

	events, unsub := h.Hub.Subscribe(ws.EventTopic(slot.EventID))
	defer unsub()

	// Push the initial state so the client has something to render while it
	// waits for its first broadcast.
	h.pushState(conn, slot)

	// Push-on-change: whenever anything in this event changes, re-read and
	// send the slot's current view. Coalescing is implicit — the hub's
	// drop-oldest send means a burst of pokes collapses to "render once".
	// The events channel is never closed by the hub (see hub.go), so we
	// rely on the connection context to break the loop on disconnect.
	go func() {
		for {
			select {
			case <-conn.Context().Done():
				return
			case <-events:
				h.pushState(conn, slot)
			}
		}
	}()

	// Incoming: location pings and answer submissions. Read until peer goes
	// away or the writer cancels our context (eg. a send queue overflow).
	//
	// Each mutating arm consults the same rate limiter the HTTP wrappers
	// use — without this gate, a WS client could brute-force short
	// answers or spam location pings, sidestepping the protection that
	// SubmitAnswer / PostLocation enforce on the REST surface.
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
			if ok, _ := h.rateLimiter().AllowLocation(slot.ID); !ok {
				continue
			}
			status, _ := h.recordLocation(conn.Context(), slot, req)
			if status >= 400 {
				continue
			}
			h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
		case "answer":
			var req AnswerRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				continue
			}
			if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
				conn.SendKind("answer_error", map[string]any{
					"error":            "too many attempts, slow down",
					"status":           http.StatusTooManyRequests,
					"retry_after_secs": int(retry / time.Second),
				})
				continue
			}
			resp, eventID, status, errMsg := h.applyAnswer(conn.Context(), slot, req.Answer)
			if status != http.StatusOK {
				conn.SendKind("answer_error", map[string]any{"error": errMsg, "status": status})
				continue
			}
			conn.SendKind("answer_result", resp)
			if eventID != 0 {
				h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
			}
		case "advance":
			// No-answer advance: the puzzle has an empty accept list so
			// the client sends this instead of "answer". Server responds
			// on the same `answer_result` / `answer_error` channels so
			// the client's existing wiring keeps working. Same answer
			// bucket — advance is a submission too and we don't want
			// to leave a free-spam path on no-answer puzzles.
			if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
				conn.SendKind("answer_error", map[string]any{
					"error":            "too many attempts, slow down",
					"status":           http.StatusTooManyRequests,
					"retry_after_secs": int(retry / time.Second),
				})
				continue
			}
			resp, eventID, status, errMsg := h.applyAdvance(conn.Context(), slot)
			if status != http.StatusOK {
				conn.SendKind("answer_error", map[string]any{"error": errMsg, "status": status})
				continue
			}
			conn.SendKind("answer_result", resp)
			if eventID != 0 {
				h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
			}
		}
	}
}

func (h *Handlers) pushState(conn *ws.Conn, slot auth.Slot) {
	resp, status, msg := h.buildState(conn.Context(), slot)
	if status == http.StatusNotFound {
		// Team gone (arranger deleted it mid-event). Tell the client
		// explicitly so it can render the "your team was removed"
		// card instead of falling through to the generic "open your
		// magic link" empty state.
		conn.SendKind("removed", map[string]any{"reason": msg})
		return
	}
	if status != http.StatusOK {
		return
	}
	conn.SendKind("state", resp)
}
