package public

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// tracksTTL is how long one event's tracks are reused. Building them
// aggregates the event's whole ping history, and every viewer of the
// board asks for them on every push; the trail is bucketed at 10 s or
// coarser, so a few seconds of staleness doesn't show.
const tracksTTL = 5 * time.Second

// tracksCache holds the latest tracks per event, shared by every viewer.
// The zero value is ready to use.
type tracksCache struct {
	mu    sync.Mutex
	slots map[int64]*tracksSlot
}

// tracksSlot serialises the rebuild for one event, so viewers asking at
// the same moment wait for one query rather than each running their own.
type tracksSlot struct {
	mu   sync.Mutex
	resp TracksResponse
	at   time.Time
}

// buildTracks returns the event's tracks, built at most once per
// tracksTTL. The response is shared between callers and must not be
// modified.
func (h *Handlers) buildTracks(ctx context.Context, eventID int64) (TracksResponse, int, string) {
	c := &h.tracks
	c.mu.Lock()
	if c.slots == nil {
		c.slots = map[int64]*tracksSlot{}
	}
	slot := c.slots[eventID]
	if slot == nil {
		// Drop idle events' slots here, where the map grows, so ended
		// events don't linger. A slot mid-rebuild is left alone.
		for id, s := range c.slots {
			if s.mu.TryLock() {
				if time.Since(s.at) > time.Minute {
					delete(c.slots, id)
				}
				s.mu.Unlock()
			}
		}
		slot = &tracksSlot{}
		c.slots[eventID] = slot
	}
	c.mu.Unlock()

	slot.mu.Lock()
	defer slot.mu.Unlock()
	if !slot.at.IsZero() && time.Since(slot.at) < tracksTTL {
		return slot.resp, http.StatusOK, ""
	}
	resp, status, msg := h.queryTracks(ctx, eventID)
	if status == http.StatusOK {
		slot.resp, slot.at = resp, time.Now()
	}
	return resp, status, msg
}
