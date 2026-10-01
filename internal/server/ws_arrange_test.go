package server_test

import (
	"bytes"
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

// The designer watches its adventure: a save of the draft — here through
// the HTTP API, as an assistant's through MCP — arrives as graph_saved,
// and someone who may not edit the adventure cannot watch it.
func TestWSArrange_WatchAdventure(t *testing.T) {
	hub := ws.NewHub()
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{Hub: hub})
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures", map[string]string{"slug": "live-watch", "title": "Live"}, nil); s != 201 {
		t.Fatalf("create adventure = %d", s)
	}

	conn := dialWS(t, ts, "/ws/arrange", boss.c)
	defer conn.Close(websocket.StatusNormalClosure, "")
	cwx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := conn.Write(cwx, websocket.MessageText, []byte(`{"kind":"watch_adventure","data":{"slug":"live-watch"}}`)); err != nil {
		cancel()
		t.Fatalf("write watch_adventure: %v", err)
	}
	cancel()
	// The watch is set up asynchronously; give it a moment before saving.
	time.Sleep(200 * time.Millisecond)

	env := map[string]any{"format": "zonventure-adventure-v2", "graph": map[string]any{
		"nodes": []map[string]any{{"slug": "start", "type": "start"}, {"slug": "end", "type": "end"}},
		"edges": []map[string]any{{"from": "start", "from_port": "out", "to": "end"}},
	}}
	var saved struct {
		SavedAt string `json:"saved_at"`
	}
	if s := boss.do("PUT", "/api/admin/adventures/live-watch/graph", env, &saved); s != 200 {
		t.Fatalf("save = %d", s)
	}
	m := recvMsg(t, conn)
	if m.Kind != "graph_saved" {
		t.Fatalf("kind = %q, want graph_saved", m.Kind)
	}
	var d struct {
		SavedAt   string `json:"saved_at"`
		By        string `json:"by"`
		Assistant bool   `json:"assistant"`
	}
	if err := json.Unmarshal(m.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.SavedAt != saved.SavedAt || d.By != "boss" || d.Assistant {
		t.Errorf("graph_saved = %+v, save said saved_at %q", d, saved.SavedAt)
	}

	// Another org's adventure is refused.
	var otherOrgID int64
	if err := db.QueryRow(`SELECT id FROM organizations WHERE name = 'OtherOrg'`).Scan(&otherOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('theirs', 't', '', '', $1)`, otherOrgID); err != nil {
		t.Fatal(err)
	}
	cwx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(cwx, websocket.MessageText, []byte(`{"kind":"watch_adventure","data":{"slug":"theirs"}}`)); err != nil {
		t.Fatal(err)
	}
	if m := recvMsg(t, conn); m.Kind != "watch_error" {
		t.Errorf("watching another org's adventure: kind = %q, want watch_error", m.Kind)
	}
}

// Two designers on one adventure see each other: who is there, which
// step each has open, where each points — and that one has left.
func TestWSArrange_AdventurePresence(t *testing.T) {
	hub := ws.NewHub()
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{Hub: hub})
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures", map[string]string{"slug": "together", "title": "T"}, nil); s != 201 {
		t.Fatalf("create adventure = %d", s)
	}
	type peer struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		Focus string `json:"focus"`
	}
	write := func(c *websocket.Conn, frame string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := c.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	// presenceUntil reads presence frames until one satisfies ok.
	presenceUntil := func(c *websocket.Conn, ok func([]peer) bool) []peer {
		t.Helper()
		for {
			m := recvKind(t, c, "presence", 3*time.Second)
			var d struct {
				Peers []peer `json:"peers"`
			}
			if err := json.Unmarshal(m.Data, &d); err != nil {
				t.Fatal(err)
			}
			if ok(d.Peers) {
				return d.Peers
			}
		}
	}
	watch := `{"kind":"watch_adventure","data":{"slug":"together"}}`

	a := dialWS(t, ts, "/ws/arrange", boss.c)
	defer a.Close(websocket.StatusNormalClosure, "")
	write(a, watch)
	self := recvKind(t, a, "presence_self", 3*time.Second)
	var aID struct {
		ID string `json:"id"`
	}
	json.Unmarshal(self.Data, &aID)

	b := dialWS(t, ts, "/ws/arrange", boss.c)
	write(b, watch)
	presenceUntil(a, func(ps []peer) bool { return len(ps) == 2 })

	write(b, `{"kind":"focus","data":{"node":"riddle"}}`)
	ps := presenceUntil(a, func(ps []peer) bool {
		for _, p := range ps {
			if p.Focus == "riddle" {
				return true
			}
		}
		return false
	})
	for _, p := range ps {
		if p.Name != "boss" || p.Kind != "human" {
			t.Errorf("peer = %+v, want boss, human", p)
		}
	}

	write(a, `{"kind":"pointer","data":{"x":10,"y":20}}`)
	m := recvKind(t, b, "pointer", 3*time.Second)
	var pt struct {
		ID string `json:"id"`
		At struct {
			X, Y float64
		} `json:"at"`
	}
	if err := json.Unmarshal(m.Data, &pt); err != nil {
		t.Fatal(err)
	}
	if pt.ID != aID.ID || pt.At.X != 10 || pt.At.Y != 20 {
		t.Errorf("pointer = %+v, want from %s at 10,20", pt, aID.ID)
	}

	b.Close(websocket.StatusNormalClosure, "")
	presenceUntil(a, func(ps []peer) bool { return len(ps) == 1 && ps[0].ID == aID.ID })
}

// An assistant's save — through an API token, as MCP makes it — shows
// the assistant on the adventure, on the steps it touched, saying what
// it said it was doing.
func TestWSArrange_AssistantPresence(t *testing.T) {
	hub := ws.NewHub()
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{Hub: hub})
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures", map[string]string{"slug": "helped", "title": "H"}, nil); s != 201 {
		t.Fatalf("create adventure = %d", s)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if s := boss.do("POST", "/api/admin/me/mcp-tokens", map[string]any{"name": "agent", "scopes": []string{"read", "write"}}, &tok); s != 201 {
		t.Fatalf("mint = %d", s)
	}

	conn := dialWS(t, ts, "/ws/arrange", boss.c)
	defer conn.Close(websocket.StatusNormalClosure, "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"kind":"watch_adventure","data":{"slug":"helped"}}`)); err != nil {
		t.Fatal(err)
	}
	recvKind(t, conn, "presence", 3*time.Second)

	body, _ := json.Marshal(map[string]any{"format": "zonventure-adventure-v2", "graph": map[string]any{
		"nodes": []map[string]any{{"slug": "start", "type": "start"}, {"slug": "end", "type": "end"}},
		"edges": []map[string]any{{"from": "start", "from_port": "out", "to": "end"}},
	}})
	req, _ := http.NewRequest("PUT", ts.URL+"/api/admin/adventures/helped/graph", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("X-Edit-Note", "laying out the route")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("save = %d", resp.StatusCode)
	}

	// The save and the presence travel apart, so either may come first.
	var sawSave, sawPeer bool
	rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer rcancel()
	for !sawSave || !sawPeer {
		_, data, err := conn.Read(rctx)
		if err != nil {
			t.Fatalf("saw save %v, assistant %v: %v", sawSave, sawPeer, err)
		}
		var m wsMsg
		json.Unmarshal(data, &m)
		switch m.Kind {
		case "graph_saved":
			var saved struct {
				Assistant bool     `json:"assistant"`
				Touched   []string `json:"touched"`
				Note      string   `json:"note"`
			}
			json.Unmarshal(m.Data, &saved)
			if !saved.Assistant || len(saved.Touched) != 2 || saved.Note != "laying out the route" {
				t.Errorf("graph_saved = %+v", saved)
			}
			sawSave = true
		case "presence":
			var p struct {
				Peers []struct {
					Kind  string `json:"kind"`
					Focus string `json:"focus"`
					Note  string `json:"note"`
				} `json:"peers"`
			}
			json.Unmarshal(m.Data, &p)
			for _, a := range p.Peers {
				if a.Kind == "assistant" {
					if a.Focus != "start" || a.Note != "laying out the route" {
						t.Errorf("assistant peer = %+v", a)
					}
					sawPeer = true
				}
			}
		}
	}
}
