package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/server"
	"github.com/klahr/zonventure/internal/ws"

	"github.com/coder/websocket"
)

// seedEventInOrg inserts an assignment + event for orgID and returns
// the event id. Used by the /ws/arrange tests so the watch path has a
// real row to authorise against without going through the HTTP API.
func seedEventInOrg(t *testing.T, db *sql.DB, orgID int64) int64 {
	t.Helper()
	var assID int64
	if err := db.QueryRow(
		`INSERT INTO adventure_assignments(org_id, adventure_slug, max_players) VALUES ($1, 'demo', 20) RETURNING id`,
		orgID,
	).Scan(&assID); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
	var eventID int64
	if err := db.QueryRow(
		`INSERT INTO events(org_id, adventure_slug, assignment_id, name, state, live_token)
		 VALUES ($1, 'demo', $2, 'WS test event', 'scheduled', 'tok-test') RETURNING id`,
		orgID, assID,
	).Scan(&eventID); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return eventID
}

func TestWSArrange_RequiresAuth(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	// Anonymous (no session cookie) — middleware rejects with 401
	// before the upgrade completes.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/arrange"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatalf("expected dial failure, got success")
	}
	if resp == nil {
		t.Fatalf("expected HTTP response on failed upgrade, got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWSArrange_WatchOwnEventDelivers(t *testing.T) {
	hub := ws.NewHub()
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{Hub: hub})
	defer cleanup()
	eventID := seedEventInOrg(t, db, orgID)

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	conn := dialWS(t, ts, "/ws/arrange", boss.c)
	defer conn.Close(websocket.StatusNormalClosure, "")

	cwx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := conn.Write(cwx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":`+itoa(eventID)+`}}`)); err != nil {
		cancel()
		t.Fatalf("write watch_event: %v", err)
	}
	cancel()

	// The handler emits four kinds on the initial push burst. Order is
	// not guaranteed, so collect them.
	got := map[string]bool{}
	for i := 0; i < 4; i++ {
		m := recvMsg(t, conn)
		got[m.Kind] = true
	}
	for _, want := range []string{"positions", "tracks", "activity", "event_detail"} {
		if !got[want] {
			t.Errorf("missing initial push kind %q (got %+v)", want, got)
		}
	}

	// Subsequent Hub.Publish on this event's topic triggers another
	// push burst — assert at least one expected kind arrives.
	hub.Publish(ws.EventTopic(eventID), "noop", map[string]any{})
	m := recvMsg(t, conn)
	switch m.Kind {
	case "positions", "tracks", "activity", "event_detail":
		// expected
	default:
		t.Fatalf("unexpected post-publish kind %q", m.Kind)
	}
}

func TestWSArrange_WatchCrossOrgRefused(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	var otherOrgID int64
	if err := db.QueryRow(`SELECT id FROM organizations WHERE name = 'OtherOrg'`).Scan(&otherOrgID); err != nil {
		t.Fatalf("lookup other org: %v", err)
	}
	otherEventID := seedEventInOrg(t, db, otherOrgID)

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	conn := dialWS(t, ts, "/ws/arrange", boss.c)
	defer conn.Close(websocket.StatusNormalClosure, "")

	cwx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := conn.Write(cwx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":`+itoa(otherEventID)+`}}`)); err != nil {
		cancel()
		t.Fatalf("write: %v", err)
	}
	cancel()

	m := recvMsg(t, conn)
	if m.Kind != "watch_error" {
		t.Fatalf("kind = %q, want watch_error", m.Kind)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(m.Data, &errBody)
	if errBody.Error != "forbidden" {
		t.Fatalf("error = %q, want forbidden", errBody.Error)
	}
}

func TestWSArrange_WatchUnknownEventReportsError(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	conn := dialWS(t, ts, "/ws/arrange", boss.c)
	defer conn.Close(websocket.StatusNormalClosure, "")

	cwx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := conn.Write(cwx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":999999}}`)); err != nil {
		cancel()
		t.Fatalf("write: %v", err)
	}
	cancel()

	m := recvMsg(t, conn)
	if m.Kind != "watch_error" {
		t.Fatalf("kind = %q, want watch_error", m.Kind)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(m.Data, &errBody)
	if errBody.Error != "event not found" {
		t.Fatalf("error = %q, want 'event not found'", errBody.Error)
	}
}

func TestWSArrange_UnwatchStopsDelivery(t *testing.T) {
	hub := ws.NewHub()
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{Hub: hub})
	defer cleanup()
	eventID := seedEventInOrg(t, db, orgID)

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	conn := dialWS(t, ts, "/ws/arrange", boss.c)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Watch, drain the four initial frames.
	cwx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := conn.Write(cwx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":`+itoa(eventID)+`}}`)); err != nil {
		cancel()
		t.Fatalf("write watch: %v", err)
	}
	cancel()
	for i := 0; i < 4; i++ {
		recvMsg(t, conn)
	}

	// Unwatch.
	cwx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	if err := conn.Write(cwx2, websocket.MessageText, []byte(`{"kind":"unwatch_event"}`)); err != nil {
		cancel2()
		t.Fatalf("write unwatch: %v", err)
	}
	cancel2()

	// Give the server a beat to tear down the subscription, then publish
	// on the topic and assert nothing arrives.
	time.Sleep(100 * time.Millisecond)
	hub.Publish(ws.EventTopic(eventID), "noop", map[string]any{})
	mustNotRecv(t, conn, 500*time.Millisecond)
}

// Access is checked when a watch starts, but a watch can run for hours:
// once the session behind the socket ends, the next push must report
// forbidden and stop, not keep streaming the event.
func TestWSArrange_WatchStopsWhenSessionEnds(t *testing.T) {
	defer admin.SetWatchAccessRecheck(admin.SetWatchAccessRecheck(0))
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	arr, _, _, eventID := setupRun(t, ts, assignmentID)
	// A second session for the same arranger keeps driving the event.
	other := newClient(t, ts)
	if s := other.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("second login = %d", s)
	}

	c := dialWS(t, ts, "/ws/arrange", arr.c)
	defer c.Close(websocket.StatusNormalClosure, "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText,
		[]byte(`{"kind":"watch_event","data":{"event_id":`+itoa(eventID)+`}}`)); err != nil {
		t.Fatalf("send watch: %v", err)
	}
	recvKind(t, c, "event_detail", 2*time.Second)

	if s := arr.do("POST", "/api/auth/logout", nil, nil); s != 204 {
		t.Fatalf("logout = %d", s)
	}
	if s := other.do("POST", "/api/admin/events/"+itoa(eventID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}
	m := recvKind(t, c, "watch_error", 3*time.Second)
	var e struct {
		Error string `json:"error"`
	}
	json.Unmarshal(m.Data, &e)
	if e.Error != "forbidden" {
		t.Fatalf("watch_error = %q, want forbidden", e.Error)
	}

	// The watch is gone: later changes push nothing.
	if s := other.do("POST", "/api/admin/events/"+itoa(eventID)+"/pause", map[string]string{}, nil); s != 204 {
		t.Fatalf("pause event = %d", s)
	}
	mustNotRecv(t, c, 1500*time.Millisecond)
}
