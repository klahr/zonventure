package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

/* ---------- test helpers ---------- */

// wsMsg mirrors the server's Msg envelope.
type wsMsg struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// dialWS upgrades to the given path. httpClient may be nil (no cookies).
func dialWS(t *testing.T, ts *httptest.Server, path string, httpClient *http.Client) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + path
	opts := &websocket.DialOptions{}
	if httpClient != nil {
		opts.HTTPClient = httpClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return c
}

// recvMsg reads one envelope with a timeout. Fails the test on error.
func recvMsg(t *testing.T, c *websocket.Conn) wsMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var m wsMsg
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("ws decode: %v", err)
		}
		if !isPresence(m.Kind) {
			return m
		}
	}
}

// isPresence tells the arranger stream's presence frames — who else is
// watching, where they point — from the data the tests are about.
func isPresence(kind string) bool {
	return kind == "presence" || kind == "presence_self" || kind == "pointer"
}

// recvKind reads until a frame with the given kind arrives or the test
// deadline fires. Useful because broadcasts often emit multiple frames
// (snapshot + tracks) in quick succession — the caller may only care
// about one kind.
func recvKind(t *testing.T, c *websocket.Conn, kind string, timeout time.Duration) wsMsg {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("ws read waiting for %q: %v", kind, err)
		}
		var m wsMsg
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("ws decode: %v", err)
		}
		if m.Kind == kind {
			return m
		}
	}
	t.Fatalf("no %q frame within %s", kind, timeout)
	return wsMsg{}
}

// mustNotRecv asserts no frame arrives within d — used to verify that an
// unwatch / close stops pushes.
func mustNotRecv(t *testing.T, c *websocket.Conn, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m wsMsg
		if json.Unmarshal(data, &m) == nil && isPresence(m.Kind) {
			continue
		}
		t.Fatalf("unexpected frame: %s", data)
	}
}

// setupRun seeds an event + one team + one player slot and returns the
// arranger-authenticated client, the live token, the slot magic token,
// and the event id. Shared by every WS test that needs a real run.
func setupRun(t *testing.T, ts *httptest.Server, assignmentID int64) (arranger *client, liveToken, slotToken string, eventID int64) {
	t.Helper()
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}

	var run struct {
		ID        int64  `json:"id"`
		LiveToken string `json:"live_token"`
	}
	if s := arr.do("POST", "/api/admin/events",
		map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}

	var team struct {
		ID      int64 `json:"id"`
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams",
		map[string]any{"name": "Team Alpha", "member_names": []string{"alice"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if len(team.Members) == 0 {
		t.Fatalf("no slot token")
	}
	return arr, run.LiveToken, team.Members[0].Token, run.ID
}

// loginPlayer walks the magic-link exchange so the returned client's jar
// holds a valid zv_slot cookie.
func loginPlayer(t *testing.T, ts *httptest.Server, magic string) *client {
	t.Helper()
	c := newClient(t, ts)
	if s := c.do("GET", "/play?t="+magic, nil, nil); s != 200 {
		t.Fatalf("magic-link login = %d", s)
	}
	return c
}

/* ---------- /ws/play ---------- */

// Every socket refuses a caller without credentials before the upgrade:
// no slot cookie on /ws/play, an unknown token on /ws/live, no staff
// session on /ws/arrange.
func TestWS_RefusesWithoutCredentials(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	for _, path := range []string{"/ws/play", "/ws/live/nope", "/ws/arrange"} {
		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + path
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, resp, err := websocket.Dial(ctx, wsURL, nil)
		cancel()
		if err == nil {
			t.Fatalf("%s: expected dial failure", path)
		}
		// The arranger middleware answers 401 rather than just failing
		// the handshake.
		if path == "/ws/arrange" && (resp == nil || resp.StatusCode != http.StatusUnauthorized) {
			t.Fatalf("%s: response = %+v, want 401", path, resp)
		}
	}
}

func TestWSPlay_LocationFrame(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	alice := loginPlayer(t, ts, slotToken)

	// Before the start the socket already pushes. It only says "look
	// again"; the view is read over HTTP.
	early := dialWS(t, ts, "/ws/play", alice.c)
	if m := recvMsg(t, early); m.Kind != "changed" {
		t.Fatalf("before start: kind=%q, want changed", m.Kind)
	}
	early.Close(websocket.StatusNormalClosure, "")

	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}
	c := dialWS(t, ts, "/ws/play", alice.c)
	defer c.Close(websocket.StatusNormalClosure, "")
	recvKind(t, c, "changed", 2*time.Second) // initial push

	// Push a location frame. Server should broadcast, which triggers
	// another push.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText,
		[]byte(`{"kind":"location","data":{"lat":59.3308,"lng":18.0731,"accuracy":10}}`)); err != nil {
		t.Fatalf("send location: %v", err)
	}
	recvKind(t, c, "changed", 2*time.Second)

	// Assert the ping is visible to the arranger — proves the WS write
	// reached the DB.
	var pos struct {
		Positions []struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"positions"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(eventID)+"/positions", nil, &pos); s != 200 {
		t.Fatalf("positions = %d", s)
	}
	if len(pos.Positions) != 1 || pos.Positions[0].Lat == 0 {
		t.Fatalf("ping not recorded: %+v", pos.Positions)
	}
}

/* ---------- /ws/live/{token} ---------- */

func TestWSLive_PushedOnMutation(t *testing.T) {
	// An admin starting the event must propagate to the live subscriber as
	// a fresh snapshot within a very short window.
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, liveToken, _, eventID := setupRun(t, ts, assignmentID)

	c := dialWS(t, ts, "/ws/live/"+liveToken, nil)
	defer c.Close(websocket.StatusNormalClosure, "")
	initial := recvKind(t, c, "snapshot", 2*time.Second)
	var s struct {
		State   string `json:"state"`
		ShowMap bool   `json:"show_map"`
	}
	json.Unmarshal(initial.Data, &s)
	if s.State != "scheduled" {
		t.Fatalf("initial state=%q, want scheduled", s.State)
	}
	// tracks follows a snapshot only when show_map is on.
	if s.ShowMap {
		recvKind(t, c, "tracks", 2*time.Second)
	}

	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}

	// With show_map off (the demo default) no tracks frame went out after
	// the initial snapshot, so the next frame is the fresh snapshot.
	snap := recvMsg(t, c)
	if snap.Kind != "snapshot" {
		t.Fatalf("kind=%q after start, want snapshot", snap.Kind)
	}
	json.Unmarshal(snap.Data, &s)
	if s.State != "running" {
		t.Fatalf("state=%q, want running", s.State)
	}
}

/* ---------- /ws/arrange ---------- */

func TestWSArrange_PushedOnMutation(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, _, eventID := setupRun(t, ts, assignmentID)

	c := dialWS(t, ts, "/ws/arrange", arr.c)
	defer c.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := c.Write(ctx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":`+itoa(eventID)+`}}`)); err != nil {
		cancel()
		t.Fatalf("send watch: %v", err)
	}
	cancel()
	// The watch pushes these four kinds at once, in no fixed order.
	got := map[string]bool{}
	for i := 0; i < 4; i++ {
		got[recvMsg(t, c).Kind] = true
	}
	for _, want := range []string{"positions", "tracks", "activity", "event_detail"} {
		if !got[want] {
			t.Errorf("missing initial push kind %q (got %+v)", want, got)
		}
	}

	// Start the event — the broadcast should re-push event_detail (and the
	// other kinds) to the watcher.
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}
	m := recvKind(t, c, "event_detail", 2*time.Second)
	var rd struct {
		Event struct {
			State string `json:"state"`
		} `json:"event"`
	}
	json.Unmarshal(m.Data, &rd)
	if rd.Event.State != "running" {
		t.Fatalf("event_detail.state=%q, want running", rd.Event.State)
	}
}

func TestWSArrange_MalformedFramesIgnored(t *testing.T) {
	// Garbage frames must not disconnect the socket — the handler skips
	// them and keeps serving. Proves we don't hand the client a foot-gun
	// where one bad message kills the whole subscription.
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, _, eventID := setupRun(t, ts, assignmentID)

	c := dialWS(t, ts, "/ws/arrange", arr.c)
	defer c.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Not JSON at all.
	if err := c.Write(ctx, websocket.MessageText, []byte(`not json`)); err != nil {
		t.Fatalf("send garbage: %v", err)
	}
	// Unknown kind.
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"kind":"bogus"}`)); err != nil {
		t.Fatalf("send unknown kind: %v", err)
	}
	// Legit watch request — must still work.
	if err := c.Write(ctx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":`+itoa(eventID)+`}}`)); err != nil {
		t.Fatalf("send watch: %v", err)
	}
	recvKind(t, c, "event_detail", 2*time.Second)
}

/* ---------- broadcast propagation (cross-client) ---------- */

func TestBroadcast_PlayAnswerReachesLive(t *testing.T) {
	// A player submitting a correct answer must cause the public live
	// subscriber to receive a fresh snapshot.
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, liveToken, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}

	live := dialWS(t, ts, "/ws/live/"+liveToken, nil)
	defer live.Close(websocket.StatusNormalClosure, "")
	recvKind(t, live, "snapshot", 2*time.Second) // initial

	alice := loginPlayer(t, ts, slotToken)
	if s := alice.doAny("POST", "/api/player/play/answer", map[string]string{"node": "welcome", "guess": "hello"}, nil); s != 200 {
		t.Fatalf("answer = %d", s)
	}

	snap := recvKind(t, live, "snapshot", 2*time.Second)
	var s struct {
		Teams []struct {
			Score *float64 `json:"score"`
		} `json:"teams"`
	}
	json.Unmarshal(snap.Data, &s)
	if len(s.Teams) != 1 || s.Teams[0].Score == nil {
		t.Fatalf("leaderboard did not update: %+v", s.Teams)
	}
}
