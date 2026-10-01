package ws

import (
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Peer is someone looking at a topic's page: a person with it open, or
// an assistant that has edited it lately. Presence is only ever held in
// memory — a peer leaves when its socket does, so nothing goes stale.
type Peer struct {
	// ID is per connection ("c12"), or per user for an assistant ("a7"),
	// which has no socket to tell its visits apart.
	ID     string `json:"id"`
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	// Kind is "human" or "assistant".
	Kind string `json:"kind"`
	// Focus is what the peer has open — in the designer, a step's slug —
	// since FocusSince: whoever opened a step first holds it.
	Focus      string     `json:"focus,omitempty"`
	FocusSince *time.Time `json:"focus_since,omitempty"`
	// Touched are the steps an assistant's last edit changed.
	Touched []string `json:"touched,omitempty"`
	// Note is what an assistant said it is doing.
	Note  string    `json:"note,omitempty"`
	Since time.Time `json:"since"`
}

const (
	PeerHuman     = "human"
	PeerAssistant = "assistant"
)

var peerSeq atomic.Uint64

// NewPeerID returns an ID for a connection's peer.
func NewPeerID() string { return "c" + strconv.FormatUint(peerSeq.Add(1), 10) }

// AssistantPeerID is the one peer an assistant working as userID is.
func AssistantPeerID(userID int64) string { return "a" + strconv.FormatInt(userID, 10) }

// LiveTopic carries a topic's presence and pointers, apart from the
// topic itself: they are frequent and only ever the latest matters, so
// a slow reader dropping some never costs it a save or an event.
func LiveTopic(topic string) string { return "live:" + topic }

type presence struct {
	mu     sync.Mutex
	rooms  map[string]map[string]Peer
	expiry map[string]*time.Timer // topic + "\x00" + peer id
}

func (h *Hub) rooms() *presence {
	h.presenceOnce.Do(func() {
		h.presence = &presence{rooms: map[string]map[string]Peer{}, expiry: map[string]*time.Timer{}}
	})
	return h.presence
}

// Peers lists who is on topic, longest-present first.
func (h *Hub) Peers(topic string) []Peer {
	if h == nil {
		return nil
	}
	p := h.rooms()
	p.mu.Lock()
	out := make([]Peer, 0, len(p.rooms[topic]))
	for _, x := range p.rooms[topic] {
		out = append(out, x)
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (h *Hub) announce(topic string) {
	h.Publish(LiveTopic(topic), "presence", map[string]any{"peers": h.Peers(topic)})
}

// Join puts peer on topic until the returned leave is called.
func (h *Hub) Join(topic string, peer Peer) (leave func()) {
	if h == nil {
		return func() {}
	}
	p := h.rooms()
	peer.Since = time.Now()
	p.mu.Lock()
	if p.rooms[topic] == nil {
		p.rooms[topic] = map[string]Peer{}
	}
	p.rooms[topic][peer.ID] = peer
	p.mu.Unlock()
	h.announce(topic)
	var once sync.Once
	return func() { once.Do(func() { h.drop(topic, peer.ID) }) }
}

func (h *Hub) drop(topic, id string) {
	p := h.rooms()
	p.mu.Lock()
	_, ok := p.rooms[topic][id]
	delete(p.rooms[topic], id)
	if len(p.rooms[topic]) == 0 {
		delete(p.rooms, topic)
	}
	if t := p.expiry[topic+"\x00"+id]; t != nil {
		t.Stop()
		delete(p.expiry, topic+"\x00"+id)
	}
	p.mu.Unlock()
	if ok {
		h.announce(topic)
	}
}

// Focus sets what peer id has open on topic, announcing a change.
func (h *Hub) Focus(topic, id, focus string) {
	if h == nil {
		return
	}
	p := h.rooms()
	p.mu.Lock()
	peer, ok := p.rooms[topic][id]
	changed := ok && peer.Focus != focus
	if changed {
		peer.Focus = focus
		peer.FocusSince = nil
		if focus != "" {
			now := time.Now()
			peer.FocusSince = &now
		}
		p.rooms[topic][id] = peer
	}
	p.mu.Unlock()
	if changed {
		h.announce(topic)
	}
}

// Visit puts or refreshes a peer without a socket — an assistant — on
// topic for ttl after its latest visit.
func (h *Hub) Visit(topic string, peer Peer, ttl time.Duration) {
	if h == nil {
		return
	}
	p := h.rooms()
	key := topic + "\x00" + peer.ID
	p.mu.Lock()
	if p.rooms[topic] == nil {
		p.rooms[topic] = map[string]Peer{}
	}
	if was, ok := p.rooms[topic][peer.ID]; ok {
		peer.Since = was.Since
	} else {
		peer.Since = time.Now()
	}
	p.rooms[topic][peer.ID] = peer
	if t := p.expiry[key]; t != nil {
		t.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(ttl, func() {
		// A later visit replaced this timer: it is not this one's to end.
		p.mu.Lock()
		current := p.expiry[key] == t
		p.mu.Unlock()
		if current {
			h.drop(topic, peer.ID)
		}
	})
	p.expiry[key] = t
	p.mu.Unlock()
	h.announce(topic)
}
