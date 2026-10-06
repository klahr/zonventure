package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/klahr/zonventure/internal/httpx"
)

// TestHTTPMiddlewareWebSocketHijack pins the regression where the
// metrics middleware wrapped http.ResponseWriter without proxying
// Hijack(), causing coder/websocket.Accept to fail the http.Hijacker
// assertion and return 501 Not Implemented for every /ws/* endpoint.
func TestHTTPMiddlewareWebSocketHijack(t *testing.T) {
	mw := HTTPMiddleware(func(*http.Request) string { return "/ws/test" })

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("websocket.Accept: %v", err)
			return
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	}))

	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("ws dial through metrics middleware failed (status=%d): %v", status, err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// TestStatusRecorderImplementsHijacker is a fast structural check —
// without Hijack() on *statusRecorder the websocket upgrade above
// fails, but this assertion makes the cause obvious in test output.
func TestStatusRecorderImplementsHijacker(t *testing.T) {
	var _ http.Hijacker = (*statusRecorder)(nil)
}

// TestHTTPMiddlewareExtendDeadlines pins that a handler behind the
// metrics middleware can still lift the server's write timeout: without
// Unwrap, http.ResponseController can't reach the connection and a slow
// response is cut off at the server-wide deadline.
func TestHTTPMiddlewareExtendDeadlines(t *testing.T) {
	mw := HTTPMiddleware(func(*http.Request) string { return "/slow" })
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.ExtendDeadlines(w, 5*time.Second)
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))

	ts := httptest.NewUnstartedServer(handler)
	ts.Config.WriteTimeout = 20 * time.Millisecond
	ts.Start()
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("slow response cut off despite ExtendDeadlines: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "ok" {
		t.Fatalf("body = %q, err = %v", body, err)
	}
}
