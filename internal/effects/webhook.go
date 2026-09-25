package effects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/webhooks"
)

// Webhook is a queued webhook call: the request as the step described it
// when the team passed.
type Webhook struct {
	URL     string         `json:"url"`
	Method  string         `json:"method,omitempty"`
	Headers []graph.Header `json:"headers,omitempty"`
}

// Body is what a webhook sends: which team passed which step, and when.
// GET and DELETE send the same fields as query parameters.
type Body struct {
	Kind     string    `json:"kind"`
	EventID  int64     `json:"event_id"`
	TeamID   int64     `json:"team_id"`
	Team     string    `json:"team"`
	Node     string    `json:"node"`
	QueuedAt time.Time `json:"queued_at"`
}

// send makes the request. The host is resolved and pinned at send time,
// so a DNS answer changed since the adventure was saved cannot reach an
// internal address.
func (w *Worker) send(ctx context.Context, hook Webhook, body Body, id int64) (int, error) {
	u, err := url.Parse(hook.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return 0, fmt.Errorf("bad url %q", hook.URL)
	}
	method := hook.Method
	if method == "" {
		method = http.MethodPost
	}
	var payload io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		q := u.Query()
		q.Set("kind", body.Kind)
		q.Set("event_id", strconv.FormatInt(body.EventID, 10))
		q.Set("team_id", strconv.FormatInt(body.TeamID, 10))
		q.Set("team", body.Team)
		q.Set("node", body.Node)
		q.Set("queued_at", body.QueuedAt.UTC().Format(time.RFC3339))
		u.RawQuery = q.Encode()
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		payload = bytes.NewReader(b)
	}
	ctx, err = webhooks.Pin(ctx, u.Hostname(), w.AllowLoopback)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "zv-effects/1")
	// The same id on every retry, so a receiver can drop a repeat.
	req.Header.Set("X-Zv-Delivery-Id", "effect-"+strconv.FormatInt(id, 10))
	for _, h := range hook.Headers {
		req.Header.Set(h.Name, h.Value)
	}
	resp, err := w.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, nil
}
