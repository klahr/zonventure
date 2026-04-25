// Package ws carries the WebSocket endpoints and the in-memory pub/sub hub
// they use. The hub is topic-keyed so broadcasts are scoped to a single run
// (one topic per run id) and fan out only to the subscribers who asked for
// that topic.
//
// Every other zv package only needs Publish — the WS handlers take care of
// subscribe/unsubscribe themselves.
package ws

import (
	"encoding/json"
	"sync"
)

// Msg is what gets sent over the wire after JSON-encoding. Kind lets clients
// branch; Data is the payload shape for that kind.
type Msg struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Hub fans out messages from Publish to all subscribers whose topic matches.
// Subscribers register a 1-deep buffered channel; a slow consumer silently
// drops older pokes rather than blocking publishers.
type Hub struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[string]map[uint64]chan []byte // topic → id → chan
}

// NewHub returns a fresh empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[string]map[uint64]chan []byte{}}
}

// Subscribe registers ch for messages on topic. The caller reads from ch.
// The returned cancel function removes the subscription.
//
// CONTRACT: callers must invoke the returned function — typically via
// `defer unsub()` immediately after the Subscribe call — for every
// successful Subscribe. As of this writing the only call sites are
// internal/player/ws.go, internal/public/ws.go, and internal/admin/ws.go,
// all of which defer the cancel. Audited and clean: a panic mid-handler
// still triggers the defer and removes the entry. Any new caller MUST
// follow the same pattern.
func (h *Hub) Subscribe(topic string) (<-chan []byte, func()) {
	if h == nil {
		return nil, func() {}
	}
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	m, ok := h.subs[topic]
	if !ok {
		m = map[uint64]chan []byte{}
		h.subs[topic] = m
	}
	m[id] = ch
	h.mu.Unlock()
	return ch, func() {
		// The channel is removed from the map but intentionally NOT closed:
		// a concurrent Publish may have captured the channel reference
		// before we ran, and closing would race with its send. The
		// subscriber's own goroutine exits via its owning context, so the
		// channel is orphaned and the GC collects it.
		h.mu.Lock()
		defer h.mu.Unlock()
		if m, ok := h.subs[topic]; ok {
			if _, ok := m[id]; ok {
				delete(m, id)
				if len(m) == 0 {
					delete(h.subs, topic)
				}
			}
		}
	}
}

// Publish encodes kind+data as a Msg and delivers it to every subscriber on
// topic. Delivery is best-effort: if a subscriber's channel is full we
// drop the OLDEST queued message and re-try the send. This keeps the
// stream "freshest wins" — a slow consumer sees recent state rather
// than a stale snapshot — and avoids the alternative of dropping the
// new message and leaving the old one to keep aging.
//
// Safe on a nil receiver so callers needn't guard.
func (h *Hub) Publish(topic, kind string, data any) {
	if h == nil {
		return
	}
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return
		}
		raw = b
	}
	payload, err := json.Marshal(Msg{Kind: kind, Data: raw})
	if err != nil {
		return
	}
	h.mu.Lock()
	chans := make([]chan []byte, 0, len(h.subs[topic]))
	for _, c := range h.subs[topic] {
		chans = append(chans, c)
	}
	h.mu.Unlock()
	for _, c := range chans {
		// Try the cheap send first.
		select {
		case c <- payload:
			continue
		default:
		}
		// Channel is full. Drop the oldest queued message (non-blocking
		// receive) then re-try the send. Worst case we drop and re-try
		// in tight loop until either room appears or another publisher
		// fills it again — but we cap the retry at one attempt so
		// pathological contention can't pin a publisher.
		select {
		case <-c:
		default:
		}
		select {
		case c <- payload:
		default:
			// Still full after dropping one — another publisher beat
			// us to the slot. Give up on this delivery; the next
			// message we publish will repeat the same drop-oldest.
		}
	}
}

// RunTopic is the topic string used for run-scoped broadcasts.
func RunTopic(runID int64) string {
	return "run:" + itoa(runID)
}

func itoa(v int64) string {
	// Small helper so callers don't pull strconv for one integer.
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
