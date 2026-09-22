package player

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
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
			actionID := validClientActionID(req.ClientActionID)
			// Idempotent replay of a prior successful submit. The WS
			// connection may have died mid-response and the client
			// retried with the same id; we replay the cached body as
			// an answer_result frame so the client's wiring is the
			// same as a fresh success. Errors aren't cached, so a
			// cache hit always means status=200.
			if cached, found, err := lookupCachedAction(conn.Context(), h.DB, slot.ID, actionID); err == nil && found {
				var resp AnswerResponse
				if json.Unmarshal(cached.Body, &resp) == nil {
					resp.ClientActionID = actionID
					conn.SendKind("answer_result", resp)
				}
				continue
			}
			if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
				conn.SendKind("answer_error", map[string]any{
					"error":            "too many attempts, slow down",
					"status":           http.StatusTooManyRequests,
					"retry_after_secs": int(retry / time.Second),
					"client_action_id": actionID,
				})
				continue
			}
			resp, eventID, status, errMsg := h.applyAnswer(conn.Context(), slot, req.Answer)
			if status != http.StatusOK {
				conn.SendKind("answer_error", map[string]any{
					"error":            errMsg,
					"status":           status,
					"client_action_id": actionID,
				})
				continue
			}
			body, _ := json.Marshal(resp)
			recordAction(conn.Context(), h.DB, slot.ID, actionID, status, body)
			resp.ClientActionID = actionID
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
			//
			// Same idempotency dance as "answer": the message body
			// carries an optional client_action_id, and a retry of the
			// same id replays the cached AnswerResponse.
			var req AdvanceRequest
			_ = json.Unmarshal(msg.Data, &req) // tolerate empty body
			actionID := validClientActionID(req.ClientActionID)
			if cached, found, err := lookupCachedAction(conn.Context(), h.DB, slot.ID, actionID); err == nil && found {
				var resp AnswerResponse
				if json.Unmarshal(cached.Body, &resp) == nil {
					resp.ClientActionID = actionID
					conn.SendKind("answer_result", resp)
				}
				continue
			}
			if ok, retry := h.rateLimiter().AllowAnswer(slot.ID); !ok {
				conn.SendKind("answer_error", map[string]any{
					"error":            "too many attempts, slow down",
					"status":           http.StatusTooManyRequests,
					"retry_after_secs": int(retry / time.Second),
					"client_action_id": actionID,
				})
				continue
			}
			resp, eventID, status, errMsg := h.applyAdvance(conn.Context(), slot)
			if status != http.StatusOK {
				conn.SendKind("answer_error", map[string]any{
					"error":            errMsg,
					"status":           status,
					"client_action_id": actionID,
				})
				continue
			}
			body, _ := json.Marshal(resp)
			recordAction(conn.Context(), h.DB, slot.ID, actionID, status, body)
			resp.ClientActionID = actionID
			conn.SendKind("answer_result", resp)
			if eventID != 0 {
				h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
			}
		default:
			// Unknown frame. Don't tear down the connection — a
			// future client could send a kind we don't yet
			// understand and the right behaviour is to ignore it
			// gracefully — but burn an answer-rate-limit token so
			// a misbehaving (or malicious) client can't spam junk
			// frames cost-free. Logged at debug for the operator
			// to spot pattern abuse.
			h.rateLimiter().AllowAnswer(slot.ID)
			logx.Debug("player.ws.unknown_kind", "slot", slot.ID, "kind", msg.Kind)
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
