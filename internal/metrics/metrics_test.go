package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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
