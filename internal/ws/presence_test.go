package ws_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/ws"
)

func peersOf(t *testing.T, b []byte) []ws.Peer {
	t.Helper()
	kind, data := decode(t, b)
	if kind != "presence" {
		t.Fatalf("kind = %q, want presence", kind)
	}
	var p struct {
		Peers []ws.Peer `json:"peers"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return p.Peers
}

func TestPresenceJoinFocusLeave(t *testing.T) {
	h := ws.NewHub()
	ch, unsub := h.Subscribe(ws.LiveTopic("adventure:1"))
	defer unsub()

	leave := h.Join("adventure:1", ws.Peer{ID: "c1", UserID: 7, Name: "anna", Kind: ws.PeerHuman})
	if ps := peersOf(t, recvTimeout(t, ch, time.Second)); len(ps) != 1 || ps[0].Name != "anna" {
		t.Fatalf("after join: %+v", ps)
	}
	h.Focus("adventure:1", "c1", "intro")
	if ps := peersOf(t, recvTimeout(t, ch, time.Second)); ps[0].Focus != "intro" {
		t.Fatalf("after focus: %+v", ps)
	}
	h.Focus("adventure:1", "c1", "intro") // unchanged: no announcement
	leave()
	leave()
	if ps := peersOf(t, recvTimeout(t, ch, time.Second)); len(ps) != 0 {
		t.Fatalf("after leave: %+v", ps)
	}
	select {
	case b := <-ch:
		t.Fatalf("unexpected frame %s", b)
	case <-time.After(50 * time.Millisecond):
	}
}

// A page back from going offline joins again as a new peer while its old
// socket lingers. Opening the step again it keeps its place, rather than
// coming after someone who opened it meanwhile — whom the old socket
// still holds it against.
func TestPresenceRejoinKeepsFocusPlace(t *testing.T) {
	h := ws.NewHub()
	ch, unsub := h.Subscribe(ws.LiveTopic("adventure:1"))
	defer unsub()
	last := func() []ws.Peer {
		t.Helper()
		var ps []ws.Peer
		for {
			select {
			case b := <-ch:
				ps = peersOf(t, b)
			case <-time.After(50 * time.Millisecond):
				return ps
			}
		}
	}

	h.Join("adventure:1", ws.Peer{ID: "c1", UserID: 7, Name: "anna", Kind: ws.PeerHuman})
	h.Focus("adventure:1", "c1", "intro")
	time.Sleep(5 * time.Millisecond)
	h.Join("adventure:1", ws.Peer{ID: "c2", UserID: 8, Name: "bo", Kind: ws.PeerHuman})
	h.Focus("adventure:1", "c2", "intro")
	time.Sleep(5 * time.Millisecond)
	h.Join("adventure:1", ws.Peer{ID: "c3", UserID: 7, Name: "anna", Kind: ws.PeerHuman})
	h.Focus("adventure:1", "c3", "intro")

	at := map[string]time.Time{}
	for _, p := range last() {
		at[p.ID] = *p.FocusSince
	}
	if !at["c3"].Equal(at["c1"]) || !at["c3"].Before(at["c2"]) {
		t.Fatalf("anna's new page opened intro at %v, her old at %v, bo at %v", at["c3"], at["c1"], at["c2"])
	}
}

func TestPresenceVisitExpires(t *testing.T) {
	h := ws.NewHub()
	ch, unsub := h.Subscribe(ws.LiveTopic("adventure:2"))
	defer unsub()
	p := ws.Peer{ID: ws.AssistantPeerID(7), UserID: 7, Kind: ws.PeerAssistant, Touched: []string{"a"}}
	h.Visit("adventure:2", p, 50*time.Millisecond)
	recvTimeout(t, ch, time.Second)
	// A second visit restarts the clock rather than racing the first.
	// Margins are generous on both sides (refresh well inside the first
	// ttl, check well inside the second) so a slow scheduler under
	// -race can't flip the outcome.
	h.Visit("adventure:2", p, 250*time.Millisecond)
	recvTimeout(t, ch, time.Second)
	time.Sleep(100 * time.Millisecond)
	if len(h.Peers("adventure:2")) != 1 {
		t.Fatal("assistant left before its refreshed visit ran out")
	}
	if ps := peersOf(t, recvTimeout(t, ch, time.Second)); len(ps) != 0 {
		t.Fatalf("after expiry: %+v", ps)
	}
}
