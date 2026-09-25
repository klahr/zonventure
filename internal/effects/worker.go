// Package effects delivers the graph effects that reach the outside
// world: "Call a webhook" and "Send an email" steps. A team's move
// queues them in graph_effect_outbox inside its own transaction; this
// worker sends them afterwards, so a network call never holds a
// transaction open and a move that rolls back sends nothing.
package effects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/webhooks"
)

// maxAttempts is when an effect is given up on; see backoff.
const maxAttempts = 6

// backoff is the wait after the nth failed attempt: a quick retry for a
// blip, then longer for an outage. Six attempts span about a day, which
// is longer than any event a late call could still matter to.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 8 * time.Hour, 12 * time.Hour}

// lease is how long a claimed row is held before another worker may take
// it: longer than a request can take, so a slow send is not sent twice.
const lease = 2 * time.Minute

// permanent marks a failure no retry can fix, such as a team without an
// email address: the row is given up on at once.
type permanent struct{ error }

// Worker sends due effects from graph_effect_outbox.
type Worker struct {
	DB     *sql.DB
	Client *http.Client
	Mailer mailer.Mailer
	// AllowLoopback lets a webhook call localhost: for development
	// only, never where adventure authors are not the operator — it
	// would let any author make the server call services on its own
	// machine. Set by ZV_EFFECT_WEBHOOKS_ALLOW_LOOPBACK=1.
	AllowLoopback bool
	PollInterval  time.Duration
}

// Start runs a Worker until ctx is done; the channel closes when it has.
func Start(ctx context.Context, db *sql.DB, mail mailer.Mailer) <-chan struct{} {
	done := make(chan struct{})
	w := &Worker{DB: db, Mailer: mail, AllowLoopback: os.Getenv("ZV_EFFECT_WEBHOOKS_ALLOW_LOOPBACK") == "1"}
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
				logx.Warn("effects.tick_failed", "err", err)
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

// queued is a claimed outbox row with what its senders need to know.
type queued struct {
	id, eventID, teamID int64
	kind, node, team    string
	teamEmail, event    string
	payload             []byte
	attempts            int
	queuedAt            time.Time
}

// Once claims one due effect and sends it, reporting whether there was
// one. A failure is recorded on the row and retried later; the error
// returned is only for the database.
func (w *Worker) Once(ctx context.Context) (bool, error) {
	var q queued
	err := w.DB.QueryRowContext(ctx, `
		UPDATE graph_effect_outbox o SET attempts = o.attempts + 1, next_attempt_at = now() + $1::interval
		FROM teams t, events e
		WHERE o.id = (
			SELECT id FROM graph_effect_outbox
			WHERE delivered_at IS NULL AND dead_at IS NULL AND next_attempt_at <= now()
			ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
		) AND t.id = o.team_id AND e.id = o.event_id
		RETURNING o.id, o.event_id, o.team_id, o.kind, o.node, t.name, t.email, e.name, o.payload, o.attempts, o.created_at
	`, fmt.Sprintf("%d seconds", int(lease.Seconds()))).Scan(
		&q.id, &q.eventID, &q.teamID, &q.kind, &q.node, &q.team, &q.teamEmail, &q.event, &q.payload, &q.attempts, &q.queuedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	status, err := w.deliver(ctx, q)
	var perm permanent
	switch {
	case err == nil:
		_, err = w.DB.ExecContext(ctx, `UPDATE graph_effect_outbox SET delivered_at = now(), last_status = $2, last_error = NULL WHERE id = $1`, q.id, nullStatus(status))
		return true, err
	case errors.As(err, &perm) || q.attempts >= maxAttempts:
		return true, w.giveUp(ctx, q, status, reason(err))
	default:
		wait := backoff[min(q.attempts, len(backoff))-1]
		_, err = w.DB.ExecContext(ctx, `
			UPDATE graph_effect_outbox SET next_attempt_at = now() + $2::interval, last_status = $3, last_error = $4 WHERE id = $1
		`, q.id, fmt.Sprintf("%d seconds", int(wait.Seconds())), nullStatus(status), reason(err))
		return true, err
	}
}

// deliver sends one effect. status is the webhook's HTTP status, 0 for
// an email or when no response came back.
func (w *Worker) deliver(ctx context.Context, q queued) (int, error) {
	switch q.kind {
	case "webhook":
		var hook Webhook
		if err := json.Unmarshal(q.payload, &hook); err != nil {
			return 0, permanent{fmt.Errorf("unreadable payload: %w", err)}
		}
		status, err := w.send(ctx, hook, Body{Kind: "graph.webhook", EventID: q.eventID, TeamID: q.teamID, Team: q.team, Node: q.node, QueuedAt: q.queuedAt}, q.id)
		if err == nil && (status < 200 || status >= 300) {
			err = fmt.Errorf("HTTP %d", status)
		}
		return status, err
	case "email":
		var mail Email
		if err := json.Unmarshal(q.payload, &mail); err != nil {
			return 0, permanent{fmt.Errorf("unreadable payload: %w", err)}
		}
		return 0, w.sendEmail(ctx, mail, q)
	default:
		return 0, permanent{fmt.Errorf("unknown effect kind %q", q.kind)}
	}
}

func (w *Worker) giveUp(ctx context.Context, q queued, status int, why string) error {
	logx.Warn("effects.dead", "id", q.id, "kind", q.kind, "reason", why)
	_, err := w.DB.ExecContext(ctx, `UPDATE graph_effect_outbox SET dead_at = now(), last_status = $2, last_error = $3 WHERE id = $1`, q.id, nullStatus(status), why)
	return err
}

func reason(err error) string {
	return strings.TrimSpace(err.Error())
}

func nullStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}
