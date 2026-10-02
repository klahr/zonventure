package player

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// WSPlay is the player-side WebSocket endpoint: the server pushes a
// "changed" frame whenever anything in the event changes, and the client
// may pipe its GPS fixes through the same socket.
//
// Requires a slot session — the HTTP upgrade is gated by RequireSlot, same
// as the REST endpoints.
// playerPushInterval is the most often a phone's view is rebuilt.
const playerPushInterval = time.Second

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

	events, unsub := h.Hub.Subscribe(ws.EventTopic(slot.EventID), ws.TeamTopic(slot.TeamID))
	defer unsub()

	// Push once at the start so the client reads its view straight away.
	h.pushState(conn, slot)

	// Push-on-change: whenever anything in this event changes, tell the
	// phone. Every teammate's ping publishes on the team topic, so a team
	// of K phones would re-read K times per ping; Coalesce pushes the
	// first change at once — an answer still shows straight away — and
	// folds the rest of a burst into one push a second at most.
	go ws.Coalesce(conn.Context(), h.forPhone(conn.Context(), slot, events), playerPushInterval, func() { h.pushState(conn, slot) })

	// Incoming: location pings. Read until peer goes away or the writer
	// cancels our context (eg. a send queue overflow).
	//
	// Pings consult the same rate limiter PostLocation uses, so a WS
	// client cannot sidestep it.
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
			h.publishPing(slot)
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

// pushState tells the phone something changed; it re-reads its view
// over HTTP (GET /api/player/play).
func (h *Handlers) pushState(conn *ws.Conn, slot auth.Slot) {
	conn.SendKind("changed", nil)
}

// forPhone passes on the wake-ups this phone needs. Another team's move
// is published as progress so the console and live screen still see it;
// a phone reloads for it only once its own team has finished, when the
// move may shift the team's place.
func (h *Handlers) forPhone(ctx context.Context, slot auth.Slot, events <-chan []byte) <-chan []byte {
	out := make(chan []byte, 1)
	go func() {
		defer logx.Guard("player.ws.filter")
		defer close(out)
		finished := false
		for {
			var raw []byte
			var ok bool
			select {
			case <-ctx.Done():
				return
			case raw, ok = <-events:
				if !ok {
					return
				}
			}
			var m ws.Msg
			if json.Unmarshal(raw, &m) == nil && m.Kind == ws.KindProgress {
				var p ws.Progress
				_ = json.Unmarshal(m.Data, &p)
				if p.Team != slot.TeamID {
					if !p.Scored {
						continue
					}
					if !finished {
						_ = h.DB.QueryRowContext(ctx, `SELECT completed_at IS NOT NULL FROM teams WHERE id = $1`, slot.TeamID).Scan(&finished)
					}
					if !finished {
						continue
					}
				}
			}
			// A wake-up already queued covers this one.
			select {
			case out <- raw:
			default:
			}
		}
	}()
	return out
}
