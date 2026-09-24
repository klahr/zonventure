// Package effects delivers the graph effects that reach the outside
// world. A team's move queues them in graph_effect_outbox inside its own
// transaction; this worker sends them afterwards, so a network call never
// holds a transaction open and a move that rolls back sends nothing.
//
// Only webhooks are delivered here. Email effects are queued the same way
// and still wait for a sender.
package effects

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/logx"
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

// maxAttempts is when a webhook is given up on; see backoff.
const maxAttempts = 6

// backoff is the wait after the nth failed attempt: a quick retry for a
// blip, then longer for an outage. Six attempts span about a day, which
// is longer than any event a late call could still matter to.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 8 * time.Hour, 12 * time.Hour}

// lease is how long a claimed row is held before another worker may take
// it: longer than a request can take, so a slow send is not sent twice.
const lease = 2 * time.Minute

// Worker sends due webhooks from graph_effect_outbox.
type Worker struct {
	DB     *sql.DB
	Client *http.Client
	// AllowLoopback lets a webhook call localhost: for development
	// only, never where adventure authors are not the operator — it
	// would let any author make the server call services on its own
	// machine. Set by ZV_EFFECT_WEBHOOKS_ALLOW_LOOPBACK=1.
	AllowLoopback bool
	PollInterval  time.Duration
}

// Start runs a Worker until ctx is done; the channel closes when it has.
func Start(ctx context.Context, db *sql.DB) <-chan struct{} {
	done := make(chan struct{})
	w := &Worker{DB: db, AllowLoopback: os.Getenv("ZV_EFFECT_WEBHOOKS_ALLOW_LOOPBACK") == "1"}
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	return done
}

func (w *Worker) Run(ctx context.Context) {
	if w.Client == nil {
		w.Client = webhooks.NewClient()
	}
	if w.PollInterval == 0 {
		w.PollInterval = 5 * time.Second
	}
	t := time.NewTicker(w.PollInterval)
	defer t.Stop()
	for {
		for {
			sent, err := w.Once(ctx)
			if err != nil {
				logx.Warn("effects.webhook.tick_failed", "err", err)
			}
			if !sent || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once claims one due webhook and sends it, reporting whether there was
// one. A failure is recorded on the row and retried later; the error
// returned is only for the database.
func (w *Worker) Once(ctx context.Context) (bool, error) {
	var (
		id, eventID, teamID int64
		node, team          string
		doc                 []byte
		attempts            int
		queuedAt            time.Time
	)
	err := w.DB.QueryRowContext(ctx, `
		UPDATE graph_effect_outbox o SET attempts = o.attempts + 1, next_attempt_at = now() + $1::interval
		FROM teams t
		WHERE o.id = (
			SELECT id FROM graph_effect_outbox
			WHERE kind = 'webhook' AND delivered_at IS NULL AND dead_at IS NULL AND next_attempt_at <= now()
			ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
		) AND t.id = o.team_id
		RETURNING o.id, o.event_id, o.team_id, t.name, o.node, o.payload, o.attempts, o.created_at
	`, fmt.Sprintf("%d seconds", int(lease.Seconds()))).Scan(&id, &eventID, &teamID, &team, &node, &doc, &attempts, &queuedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var hook Webhook
	if err := json.Unmarshal(doc, &hook); err != nil {
		return true, w.giveUp(ctx, id, 0, "unreadable payload: "+err.Error())
	}
	status, err := w.send(ctx, hook, Body{Kind: "graph.webhook", EventID: eventID, TeamID: teamID, Team: team, Node: node, QueuedAt: queuedAt}, id)
	switch {
	case err == nil && status >= 200 && status < 300:
		_, err = w.DB.ExecContext(ctx, `UPDATE graph_effect_outbox SET delivered_at = now(), last_status = $2, last_error = NULL WHERE id = $1`, id, status)
		return true, err
	case attempts >= maxAttempts:
		return true, w.giveUp(ctx, id, status, reason(status, err))
	default:
		wait := backoff[min(attempts, len(backoff))-1]
		_, err = w.DB.ExecContext(ctx, `
			UPDATE graph_effect_outbox SET next_attempt_at = now() + $2::interval, last_status = $3, last_error = $4 WHERE id = $1
		`, id, fmt.Sprintf("%d seconds", int(wait.Seconds())), nullStatus(status), reason(status, err))
		return true, err
	}
}

func (w *Worker) giveUp(ctx context.Context, id int64, status int, why string) error {
	logx.Warn("effects.webhook.dead", "id", id, "reason", why)
	_, err := w.DB.ExecContext(ctx, `UPDATE graph_effect_outbox SET dead_at = now(), last_status = $2, last_error = $3 WHERE id = $1`, id, nullStatus(status), why)
	return err
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

func reason(status int, err error) string {
	if err != nil {
		return strings.TrimSpace(err.Error())
	}
	return fmt.Sprintf("HTTP %d", status)
}

func nullStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}
