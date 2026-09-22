package mailer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/klahr/zonventure/internal/logx"
)

// Outbox is a durable retry layer in front of a Mailer. Callers that
// must not lose a message (today: the post-Stripe ticket-fulfilment
// magic link) call Enqueue inside whatever transaction commits the
// load-bearing rows; a background worker pulls due rows and calls
// the wrapped Mailer.Send with retry + exponential backoff.
//
// The shape mirrors webhooks/delivery.go: a claim+lease tx, a send
// without a held DB connection, an outcome tx. A worker that
// crashes mid-send leaves the row in flight for at most LeaseWindow
// before another worker can pick it up.
//
// What this is NOT: a general-purpose mail queue. Attachments aren't
// modelled, and best-effort sites (signup verification, password
// reset, hint reveals — flows where the user has a UI to retry)
// keep their direct Mailer.Send calls. Adding the indirection
// everywhere would just bury the failure mode.
type Outbox struct {
	DB     *sql.DB
	Mailer Mailer

	// PollInterval is how often the worker wakes to look for due
	// rows. Default 5 s — small enough that a paid buyer sees their
	// magic link within seconds, large enough that an idle queue
	// isn't a tight DB poll.
	PollInterval time.Duration
}

// MaxAttempts caps retries before a row is dead-lettered. With the
// schedule below, 6 attempts span ~36 h — enough to ride out a
// half-day SMTP outage without the queue retrying forever.
const MaxAttempts = 6

// LeaseWindow caps how long a worker may hold a claim. Stale-lease
// recovery: a worker that crashed mid-send leaves the row in flight
// for at most this long before being eligible for re-claim. Stale
// claims do mean a duplicate send is possible — recipients dedupe
// by message content + the rare "you got two copies" is much better
// than "you got zero copies."
const LeaseWindow = 5 * time.Minute

// backoffSchedule mirrors the webhook delivery shape — short retry
// first (transient blip), then progressively longer.
var backoffSchedule = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	8 * time.Hour,
	24 * time.Hour,
}

func backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	if attempt > len(backoffSchedule) {
		return backoffSchedule[len(backoffSchedule)-1]
	}
	return backoffSchedule[attempt-1]
}

// Enqueue persists `m` for a worker to deliver. Returns once the
// row is committed. `kind` is a free-form label for telemetry —
// today only "ticket_team_link" is in use; future callers add their
// own.
//
// Caller MUST validate at least one of Text / HTML is non-empty;
// the outbox refuses an empty body to avoid burning retries on a
// definitely-broken row.
func (o *Outbox) Enqueue(ctx context.Context, kind string, m Message) error {
	if o == nil || o.DB == nil {
		return errors.New("outbox: not configured")
	}
	if len(m.To) == 0 {
		return ErrNoRecipients
	}
	if m.Text == "" && m.HTML == "" {
		return ErrEmptyBody
	}
	if len(m.Attachments) > 0 {
		// Attachments aren't modelled in the outbox schema. Refuse
		// loudly rather than silently drop — callers with attachments
		// either route through Mailer.Send directly (interactive
		// flows) or extend the schema.
		return errors.New("outbox: attachments not supported; use Mailer.Send directly")
	}
	// One row per recipient: keeps retries scoped (one bad address
	// doesn't gate the others) and matches the typical use-case
	// (single-buyer transactional sends). Bulk fan-out lives at a
	// different layer.
	for _, addr := range m.To {
		if _, err := o.DB.ExecContext(ctx, `
			INSERT INTO mail_outbox (kind, to_addr, subject, body_text, body_html, reply_to)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		`, kind, addr, m.Subject, m.Text, m.HTML, m.ReplyTo); err != nil {
			return fmt.Errorf("outbox enqueue: %w", err)
		}
	}
	return nil
}

// Run drives the worker until ctx is cancelled. Polls on PollInterval,
// processes whatever is due, sleeps. Errors during a tick are logged
// and swallowed so a transient DB blip doesn't kill the worker.
func (o *Outbox) Run(ctx context.Context) {
	if o.PollInterval == 0 {
		o.PollInterval = 5 * time.Second
	}
	logx.Info("mail.outbox.start", "poll_interval", o.PollInterval.String())
	t := time.NewTicker(o.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logx.Info("mail.outbox.stop")
			return
		case <-t.C:
			if err := o.tick(ctx); err != nil {
				logx.Warn("mail.outbox.tick_failed", "err", err.Error())
			}
		}
	}
}

// Start launches Outbox.Run in a goroutine and returns a done
// channel that closes when the worker exits. Mirrors the shape of
// webhooks.Start / scheduler.Start so cmd/server can wait on it
// during graceful shutdown.
func Start(ctx context.Context, db *sql.DB, m Mailer) <-chan struct{} {
	done := make(chan struct{})
	o := &Outbox{DB: db, Mailer: m}
	go func() {
		defer close(done)
		o.Run(ctx)
	}()
	return done
}

// claimedMail is one row reserved for delivery. attempts has already
// been incremented by claim.
type claimedMail struct {
	id       int64
	kind     string
	toAddr   string
	subject  string
	bodyText string
	bodyHTML string
	replyTo  string
	attempts int
}

// tick claims one due row at a time, sends it, and records the
// outcome. Loops until claimNext finds nothing.
func (o *Outbox) tick(ctx context.Context) error {
	for {
		c, err := o.claimNext(ctx)
		if err != nil {
			return err
		}
		if c == nil {
			return nil
		}
		o.deliver(ctx, c)
	}
}

// claimNext is the lease step — same shape as the webhook worker.
// FOR UPDATE SKIP LOCKED guards the brief tx; the lease itself
// keeps other workers from reclaiming the row until the window
// expires.
func (o *Outbox) claimNext(ctx context.Context) (*claimedMail, error) {
	tx, err := o.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		c       claimedMail
		replyTo sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, kind, to_addr, subject, body_text, body_html, reply_to, attempts
		  FROM mail_outbox
		 WHERE status = 'pending'
		   AND next_attempt_at <= (now()::text)
		   AND (in_flight_until IS NULL OR in_flight_until::timestamptz < now())
		 ORDER BY next_attempt_at
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED
	`).Scan(&c.id, &c.kind, &c.toAddr, &c.subject, &c.bodyText, &c.bodyHTML, &replyTo, &c.attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select due: %w", err)
	}
	c.replyTo = replyTo.String

	// Bump attempts + stamp lease before commit. Same rationale as
	// webhooks: a crashed worker can't retry forever because every
	// claim moves the counter toward MaxAttempts.
	leaseExp := time.Now().UTC().Add(LeaseWindow).Format("2006-01-02 15:04:05")
	c.attempts++
	if _, err := tx.ExecContext(ctx, `
		UPDATE mail_outbox SET attempts = $1, in_flight_until = $2 WHERE id = $3
	`, c.attempts, leaseExp, c.id); err != nil {
		return nil, fmt.Errorf("stamp lease: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return &c, nil
}

// deliver does the actual SMTP/log send WITHOUT holding any DB
// transaction or row lock. Outcome writes are best-effort: a failed
// outcome write leaves the row in flight until the lease expires
// and another worker picks it up.
func (o *Outbox) deliver(ctx context.Context, c *claimedMail) {
	msg := Message{
		To:      []string{c.toAddr},
		Subject: c.subject,
		Text:    c.bodyText,
		HTML:    c.bodyHTML,
		ReplyTo: c.replyTo,
	}
	sendErr := o.Mailer.Send(ctx, msg)
	if sendErr == nil {
		if _, err := o.DB.ExecContext(ctx, `
			UPDATE mail_outbox
			   SET status = 'sent',
			       last_err = NULL,
			       sent_at = (now()::text),
			       in_flight_until = NULL
			 WHERE id = $1
		`, c.id); err != nil {
			logx.Warn("mail.outbox.outcome.write_failed",
				"id", c.id, "stage", "sent", "err", err.Error())
			return
		}
		logx.Info("mail.outbox.sent",
			"id", c.id, "kind", c.kind, "to", c.toAddr,
			"attempts", c.attempts)
		return
	}

	errMsg := sendErr.Error()
	if c.attempts >= MaxAttempts {
		if _, err := o.DB.ExecContext(ctx, `
			UPDATE mail_outbox
			   SET status = 'dead', last_err = $1, in_flight_until = NULL
			 WHERE id = $2
		`, errMsg, c.id); err != nil {
			logx.Warn("mail.outbox.outcome.write_failed",
				"id", c.id, "stage", "dead", "err", err.Error())
			return
		}
		logx.Warn("mail.outbox.dead_letter",
			"id", c.id, "kind", c.kind, "to", c.toAddr,
			"attempts", c.attempts, "err", errMsg)
		return
	}

	nextAt := time.Now().UTC().Add(backoffDelay(c.attempts)).Format("2006-01-02 15:04:05")
	if _, err := o.DB.ExecContext(ctx, `
		UPDATE mail_outbox
		   SET last_err = $1, next_attempt_at = $2, in_flight_until = NULL
		 WHERE id = $3
	`, errMsg, nextAt, c.id); err != nil {
		logx.Warn("mail.outbox.outcome.write_failed",
			"id", c.id, "stage", "retry", "err", err.Error())
		return
	}
	logx.Info("mail.outbox.retry_scheduled",
		"id", c.id, "kind", c.kind, "to", c.toAddr,
		"attempts", c.attempts, "next_attempt_at", nextAt, "err", errMsg)
}
