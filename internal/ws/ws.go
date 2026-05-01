package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Accept upgrades the request and returns a Conn plus a context that is
// cancelled when the peer closes the connection.
//
// SECURITY: passing nil AcceptOptions invokes the coder/websocket
// default Origin check, which rejects upgrades whose Origin host
// doesn't match the request host. That's the load-bearing CSRF
// equivalent for WebSocket — a same-site XSS bypass is the only
// remaining vector. DO NOT pass `OriginPatterns: []string{"*"}` or
// `InsecureSkipVerify: true` here; both /ws/play, /ws/arrange,
// /ws/live, /ws/logs are served from the same origin as the app
// they're upgrading from, so there's no cross-origin use case to
// allow. A future test should pin this — see TestWSPlay_ tests
// for the existing coverage shape.
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(1 << 16)
	ctx, cancel := context.WithCancel(r.Context())
	conn := &Conn{
		c:      c,
		ctx:    ctx,
		cancel: cancel,
		out:    make(chan []byte, 32),
	}
	go conn.writer()
	return conn, nil
}

// Conn wraps coder/websocket.Conn with a single outbound queue, since
// concurrent writes on the underlying connection are not safe. All senders
// push to the out channel; a dedicated writer goroutine drains it.
type Conn struct {
	c      *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	out    chan []byte
}

// Context returns a context that is cancelled when the connection closes.
func (c *Conn) Context() context.Context { return c.ctx }

// Send queues a pre-encoded Msg for delivery. Drops silently if the queue
// is full — better than blocking a publisher behind one lagging client.
func (c *Conn) Send(b []byte) {
	select {
	case c.out <- b:
	case <-c.ctx.Done():
	default:
		// Queue full: the peer is too slow. Tear the connection down so the
		// hub can stop publishing to this dead end.
		c.cancel()
	}
}

// SendKind encodes kind+data as a Msg and queues it.
func (c *Conn) SendKind(kind string, data any) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return
		}
		raw = b
	}
	b, err := json.Marshal(Msg{Kind: kind, Data: raw})
	if err != nil {
		return
	}
	c.Send(b)
}

// Read reads the next text frame as a Msg. A returned error means the
// underlying connection failed (peer gone, context cancelled) and the
// handler should exit its loop. A frame that fails JSON decoding is NOT
// an error — it returns an empty Msg (Kind == ""), which handlers ignore
// via the switch's default case. This keeps one malformed message from
// killing an otherwise healthy subscription.
func (c *Conn) Read() (Msg, error) {
	_, data, err := c.c.Read(c.ctx)
	if err != nil {
		return Msg{}, err
	}
	var m Msg
	if err := json.Unmarshal(data, &m); err != nil {
		return Msg{}, nil
	}
	return m, nil
}

// Close terminates the connection.
func (c *Conn) Close() {
	c.cancel()
	_ = c.c.Close(websocket.StatusNormalClosure, "")
}

// CloseWithReason is like Close but reports a status + short reason.
func (c *Conn) CloseWithReason(status websocket.StatusCode, reason string) {
	c.cancel()
	_ = c.c.Close(status, reason)
}

// writer drains out and writes to the underlying conn. Also emits a ping
// every 30s so NAT timeouts can't silently eat an idle connection.
func (c *Conn) writer() {
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case b, ok := <-c.out:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			err := c.c.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			err := c.c.Ping(pctx)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}
