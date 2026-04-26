package ws_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"git.sr.ht/~klahr/zv/internal/ws"
)

// decode is a tiny helper that pulls a hub envelope apart.
func decode(t *testing.T, b []byte) (string, json.RawMessage) {
	t.Helper()
	var m struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m.Kind, m.Data
}

// recvTimeout reads from ch or fails after d. Returns the bytes.
func recvTimeout(t *testing.T, ch <-chan []byte, d time.Duration) []byte {
	t.Helper()
	select {
	case b, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed")
		}
		return b
	case <-time.After(d):
		t.Fatalf("timeout waiting for message")
		return nil
	}
}

// mustNotRecv asserts nothing arrives on ch within d. Used to verify
// topic isolation and unsubscribes.
func mustNotRecv(t *testing.T, ch <-chan []byte, d time.Duration) {
	t.Helper()
	select {
	case b := <-ch:
		t.Fatalf("unexpected message: %s", string(b))
	case <-time.After(d):
	}
}

func TestHub_SubscribePublish(t *testing.T) {
	h := ws.NewHub()
	ch, unsub := h.Subscribe("event:1")
	defer unsub()

	h.Publish("event:1", "changed", map[string]int{"x": 42})

	msg := recvTimeout(t, ch, 500*time.Millisecond)
	kind, raw := decode(t, msg)
	if kind != "changed" {
		t.Fatalf("kind = %q, want changed", kind)
	}
	var data struct{ X int }
	if err := json.Unmarshal(raw, &data); err != nil || data.X != 42 {
		t.Fatalf("data = %+v err=%v", data, err)
	}
}

func TestHub_PublishNilData(t *testing.T) {
	h := ws.NewHub()
	ch, unsub := h.Subscribe("event:1")
	defer unsub()

	h.Publish("event:1", "tick", nil)
	kind, raw := decode(t, recvTimeout(t, ch, 500*time.Millisecond))
	if kind != "tick" {
		t.Fatalf("kind = %q", kind)
	}
	if len(raw) != 0 {
		t.Fatalf("expected empty data, got %s", raw)
	}
}

func TestHub_TopicIsolation(t *testing.T) {
	h := ws.NewHub()
	a, unsubA := h.Subscribe("event:1")
	defer unsubA()
	b, unsubB := h.Subscribe("event:2")
	defer unsubB()

	h.Publish("event:1", "changed", nil)
	recvTimeout(t, a, 500*time.Millisecond)
	mustNotRecv(t, b, 50*time.Millisecond)

	h.Publish("event:2", "changed", nil)
	recvTimeout(t, b, 500*time.Millisecond)
	mustNotRecv(t, a, 50*time.Millisecond)
}

func TestHub_MultipleSubscribers(t *testing.T) {
	h := ws.NewHub()
	const n = 4
	chans := make([]<-chan []byte, n)
	for i := range chans {
		c, unsub := h.Subscribe("event:7")
		defer unsub()
		chans[i] = c
	}
	h.Publish("event:7", "changed", map[string]string{"s": "hi"})
	for i, c := range chans {
		kind, _ := decode(t, recvTimeout(t, c, 500*time.Millisecond))
		if kind != "changed" {
			t.Fatalf("subscriber %d: kind=%q", i, kind)
		}
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	h := ws.NewHub()
	ch, unsub := h.Subscribe("event:1")

	h.Publish("event:1", "a", nil)
	recvTimeout(t, ch, 500*time.Millisecond)

	unsub()
	h.Publish("event:1", "b", nil)
	mustNotRecv(t, ch, 100*time.Millisecond)
}

func TestHub_UnsubscribeIsIdempotent(t *testing.T) {
	h := ws.NewHub()
	_, unsub := h.Subscribe("event:1")
	unsub()
	unsub() // must not panic
}

func TestHub_NilReceiverSafe(t *testing.T) {
	var h *ws.Hub // nil
	// Should not panic.
	h.Publish("event:1", "x", nil)
	ch, unsub := h.Subscribe("event:1")
	unsub()
	if ch != nil {
		// A nil hub returns a nil channel so the caller can't accidentally
		// block forever waiting for a subscription that doesn't exist.
		t.Fatalf("expected nil channel from nil hub, got non-nil")
	}
}

func TestHub_SlowConsumerDrops(t *testing.T) {
	// Channel capacity is 16. Publish 64 messages to a non-reading
	// subscriber — the publisher must not block, and the subscriber
	// should see at most capacity messages.
	h := ws.NewHub()
	ch, unsub := h.Subscribe("event:1")
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 64; i++ {
			h.Publish("event:1", "x", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("publisher blocked on slow consumer")
	}

	// Drain — we must see >=1 and <=16 messages.
	got := 0
drain:
	for {
		select {
		case <-ch:
			got++
		case <-time.After(50 * time.Millisecond):
			break drain
		}
	}
	if got == 0 || got > 16 {
		t.Fatalf("delivered %d messages, want 1..16", got)
	}
}

func TestHub_ConcurrentPublishSubscribe(t *testing.T) {
	// Exercises the hub's mutex: many goroutines subscribing / publishing /
	// unsubscribing in parallel shouldn't race (run with -race).
	h := ws.NewHub()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Publishers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.Publish("event:1", "x", nil)
				}
			}
		}()
	}
	// Subscribers churning in and out.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					ch, unsub := h.Subscribe("event:1")
					// Drain one (may or may not get anything, that's fine).
					select {
					case <-ch:
					case <-time.After(5 * time.Millisecond):
					}
					unsub()
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestHub_EventTopicFormat(t *testing.T) {
	// Stable topic string so external callers don't need to care how it's
	// built. The test pins it so a refactor can't accidentally change the
	// wire-compatible key.
	cases := map[int64]string{
		0:      "event:0",
		1:      "event:1",
		42:     "event:42",
		999999: "event:999999",
	}
	for id, want := range cases {
		if got := ws.EventTopic(id); got != want {
			t.Errorf("EventTopic(%d) = %q, want %q", id, got, want)
		}
	}
}
