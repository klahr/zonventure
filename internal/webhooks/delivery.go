package webhooks

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/klahr/zonventure/internal/logx"
)

// pinnedDialIPKey is the request-context key carrying the IP we want
// the Transport.DialContext to bind. The send() path stamps this
// before calling Client.Do; the dialer below honours it for the
// outbound connect, dialing the literal address rather than re-
// resolving the hostname (which would re-open the DNS-rebinding
// window the SSRF gate exists to close).
type pinnedDialIPKey struct{}

// pinnedDialer is the package-level Dialer reused by every send. The
// timeout matches the previous default Transport budget; sharing the
// instance across requests lets Go's net stack pool resolver state.
var pinnedDialer = &net.Dialer{Timeout: 10 * time.Second}

// dialPinned is installed as Transport.DialContext on the shared
// Worker.Client. When the request context carries a pinnedDialIPKey,
// it dials that IP literal with the original port from `address`;
// otherwise it falls back to the default resolution path (used for
// loopback dev connections where pinning is overkill).
func dialPinned(ctx context.Context, network, address string) (net.Conn, error) {
	ip, ok := ctx.Value(pinnedDialIPKey{}).(string)
	if !ok || ip == "" {
		return pinnedDialer.DialContext(ctx, network, address)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	return pinnedDialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
}

// NewClient is the HTTP client every outbound webhook goes through. Use
// it with Pin: the pair is the SSRF defence.
//
// CheckRedirect refusing follows is the load-bearing piece here: without
// it, a URL that 302s to http://169.254.169.254/ (or any other internal
// host) would pivot the sender into the cloud metadata API even though
// the first host validated cleanly. ErrUseLastResponse stops the chain
// at the first 3xx and returns that response untouched — the same shape
// as "we got a non-2xx", so retry logic records it as a failure.
func NewClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:           dialPinned,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          50,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// Pin resolves host, refuses it unless every answer is public, and
// returns ctx carrying the chosen IP so a NewClient request dials exactly
// that address: a DNS answer flipped between the check and the connect
// (rebinding) cannot land on an internal service. With allowLoopback a
// localhost host is let through unpinned, for development.
func Pin(ctx context.Context, host string, allowLoopback bool) (context.Context, error) {
	if IsLoopbackHost(host) {
		if allowLoopback {
			return ctx, nil
		}
		return nil, errNonPublic
	}
	ip, err := LookupPublic(host)
	if err != nil {
		return nil, fmt.Errorf("ssrf gate: %w", err)
	}
	return context.WithValue(ctx, pinnedDialIPKey{}, ip.String()), nil
}

// MaxAttempts is the cap before a delivery is dead-lettered. With the
// backoff schedule below, 6 attempts span ~36 hours — long enough to
// ride out routine customer-side outages without retrying forever.
const MaxAttempts = 6

// backoffSchedule returns the delay applied AFTER the Nth failed
// attempt (1-indexed). Index 0 isn't used — the first send happens
// immediately after enqueue. After MaxAttempts the delivery is
// dead-lettered, so the schedule needs at most MaxAttempts entries.
//
// Stripe-shaped: short retry first (a transient blip recovers in
// minutes), then progressively longer to absorb hour- or day-scale
// outages. Anything past 24h is below the customer's "we noticed and
// fixed it" timescale.
var backoffSchedule = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	8 * time.Hour,
	24 * time.Hour,
}

// backoffDelay returns the wait before retry #attempt (1-indexed).
// Past the schedule's tail, returns the last entry — defensive in
// case attempts exceeds MaxAttempts due to a manual replay.
func backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	if attempt > len(backoffSchedule) {
		return backoffSchedule[len(backoffSchedule)-1]
	}
	return backoffSchedule[attempt-1]
}

// Worker drains webhook_deliveries. One worker per process is fine —
// the bottleneck is per-endpoint network latency, not CPU; if we
// later need parallelism it lives inside tick (fan out per row).
type Worker struct {
	DB     *sql.DB
	Client *http.Client
	// PollInterval is how often we wake up to check for due
	// deliveries. Default 5s — small enough that a customer-triggered
	// event arrives at the customer within ~5s, large enough that an
	// idle queue isn't a constant DB poll.
	PollInterval time.Duration
}

// Start launches a Worker in a goroutine and returns a done channel
// that closes when the worker exits (after ctx is cancelled). Mirrors
// scheduler.Start / retention.Start so cmd/server can wait on
// graceful shutdown the same way.
func Start(ctx context.Context, db *sql.DB) <-chan struct{} {
	done := make(chan struct{})
	w := &Worker{DB: db}
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	return done
}

// Run drives the worker until ctx is cancelled. Polls on PollInterval,
// processes whatever is due, sleeps. Errors during a tick are logged
// and swallowed — a transient DB failure shouldn't stop the worker.
func (w *Worker) Run(ctx context.Context) {
	if w.Client == nil {
		w.Client = NewClient()
	}
	if w.PollInterval == 0 {
		w.PollInterval = 5 * time.Second
	}
	logx.Info("webhooks.worker.start", "poll_interval", w.PollInterval.String())
	t := time.NewTicker(w.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logx.Info("webhooks.worker.stop")
			return
		case <-t.C:
			if err := w.tick(ctx); err != nil {
				logx.Warn("webhooks.worker.tick_failed", "err", err.Error())
			}
		}
	}
}

// leaseWindow caps how long a worker may hold a claim before another
// worker is allowed to retry the same row. The window is generous
// against the client's 30 s HTTP timeout — a worker that crashes
// mid-send leaves the row in flight for at most this long before the
// queue picks it up again. Stale leases just mean a duplicate
// delivery; consumers are required to dedupe by `X-Zv-Delivery-Id`
// (already documented in the public webhook contract).
const leaseWindow = 5 * time.Minute

// tick processes every due delivery in one pass. Each delivery moves
// through TWO transactions:
//
//	tx1: claim — stamp in_flight_until + bump attempts, commit fast
//	         (releases the row lock immediately).
//	(no DB connection held during the customer HTTP call.)
//	tx2: outcome — record success / retry / dead-letter, clear
//	         in_flight_until.
//
// Earlier the row lock was held across the entire HTTP call, which
// pinned a Postgres backend for up to 30 s per delivery and
// serialised the queue through one slow-loris customer. The lease
// pattern decouples the two so a stuck customer endpoint costs only
// a single goroutine + http connection until its timeout fires.
//
// Workers also coexist now: SKIP LOCKED + the lease guard mean a
// future per-row goroutine fan-out is a localised change in this
// loop, not a full rewrite.
func (w *Worker) tick(ctx context.Context) error {
	for {
		claimed, err := w.claimNext(ctx)
		if err != nil {
			return err
		}
		if claimed == nil {
			return nil // queue empty (or nothing eligible right now)
		}
		w.deliver(ctx, claimed)
	}
}

// claimedDelivery is what claimNext returns — the row plus the
// already-loaded endpoint config so the deliver() path doesn't need
// a second SELECT. attempts has ALREADY been incremented by claim;
// callers see the post-claim count.
type claimedDelivery struct {
	id           int64
	endpointID   int64
	deliveryUUID string
	kind         string
	payload      []byte
	attempts     int
	endpointURL  string
	secret       string
}

// claimNext reserves one due row by stamping in_flight_until and
// returning the work to do. Eligibility:
//
//  1. status='pending' AND next_attempt_at <= now (regular due)
//  2. (in_flight_until IS NULL OR in_flight_until::timestamptz < now)
//     so a stale lease from a crashed worker is reclaimable.
//
// FOR UPDATE SKIP LOCKED keeps multiple workers from racing on the
// same row inside this brief tx; once we COMMIT the lock is gone and
// any other worker will see in_flight_until in the future and skip
// the row in their own SKIP-LOCKED scan.
//
// Returns nil, nil when nothing is due.
func (w *Worker) claimNext(ctx context.Context) (*claimedDelivery, error) {
	tx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var c claimedDelivery
	var payloadStr string
	err = tx.QueryRowContext(ctx, `
		SELECT id, endpoint_id, delivery_uuid, event_kind, payload, attempts
		  FROM webhook_deliveries
		 WHERE status = 'pending'
		   AND next_attempt_at <= now_text()
		   AND (in_flight_until IS NULL OR in_flight_until::timestamptz < now())
		 ORDER BY next_attempt_at
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED
	`).Scan(&c.id, &c.endpointID, &c.deliveryUUID, &c.kind, &payloadStr, &c.attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select due: %w", err)
	}
	c.payload = []byte(payloadStr)

	var active bool
	if err := tx.QueryRowContext(ctx,
		`SELECT url, secret, active FROM webhook_endpoints WHERE id = $1`,
		c.endpointID,
	).Scan(&c.endpointURL, &c.secret, &active); err != nil {
		return nil, fmt.Errorf("select endpoint: %w", err)
	}

	// Endpoint disabled between enqueue and delivery: dead-letter
	// inline, no HTTP work to do. Same single-tx as before — we're
	// not crossing into the customer-call window for this case.
	if !active {
		if _, err := tx.ExecContext(ctx,
			`UPDATE webhook_deliveries SET status = 'dead', last_error = 'endpoint disabled', in_flight_until = NULL WHERE id = $1`,
			c.id,
		); err != nil {
			return nil, fmt.Errorf("mark dead (disabled): %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit dead-disabled: %w", err)
		}
		// Tell the caller to keep looping.
		return w.claimNext(ctx)
	}

	// Stamp the lease + bump attempts before committing. The bump
	// happens here (not on outcome write) so a crashed worker can't
	// retry forever — attempts increments toward MaxAttempts every
	// claim, with or without a successful outcome write.
	leaseExp := time.Now().UTC().Add(leaseWindow).Format("2006-01-02 15:04:05")
	c.attempts++
	if _, err := tx.ExecContext(ctx, `
		UPDATE webhook_deliveries
		   SET attempts = $1, in_flight_until = $2
		 WHERE id = $3
	`, c.attempts, leaseExp, c.id); err != nil {
		return nil, fmt.Errorf("stamp lease: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return &c, nil
}

// deliver does the actual customer HTTP call WITHOUT holding any DB
// transaction or row lock, then writes the outcome in a fresh tx.
// Errors here are logged but never bubbled to the caller — a write
// failure on the outcome path leaves the row "in flight" until the
// lease expires, and another worker will pick it up.
func (w *Worker) deliver(ctx context.Context, c *claimedDelivery) {
	status, sendErr := w.send(ctx, c.endpointURL, c.secret, c.kind, c.deliveryUUID, c.payload)

	if sendErr == nil && status >= 200 && status < 300 {
		if _, err := w.DB.ExecContext(ctx, `
			UPDATE webhook_deliveries
			   SET status = 'delivered',
			       last_status = $1,
			       last_error = NULL,
			       delivered_at = now_text(),
			       in_flight_until = NULL
			 WHERE id = $2
		`, status, c.id); err != nil {
			logx.Warn("webhooks.outcome.write_failed",
				"delivery_id", c.id, "stage", "delivered", "err", err.Error())
			return
		}
		logx.Info("webhooks.delivered",
			"delivery_id", c.id, "endpoint_id", c.endpointID,
			"kind", c.kind, "status", status, "attempts", c.attempts)
		return
	}

	// Failure path.
	errMsg := ""
	if sendErr != nil {
		errMsg = sendErr.Error()
	} else {
		errMsg = fmt.Sprintf("HTTP %d", status)
	}
	if c.attempts >= MaxAttempts {
		if _, err := w.DB.ExecContext(ctx, `
			UPDATE webhook_deliveries
			   SET status = 'dead', last_status = $1, last_error = $2, in_flight_until = NULL
			 WHERE id = $3
		`, nullableStatus(status), errMsg, c.id); err != nil {
			logx.Warn("webhooks.outcome.write_failed",
				"delivery_id", c.id, "stage", "dead", "err", err.Error())
			return
		}
		logx.Warn("webhooks.dead_letter",
			"delivery_id", c.id, "endpoint_id", c.endpointID,
			"kind", c.kind, "attempts", c.attempts, "err", errMsg)
		return
	}

	nextAt := time.Now().UTC().Add(backoffDelay(c.attempts)).Format("2006-01-02 15:04:05")
	if _, err := w.DB.ExecContext(ctx, `
		UPDATE webhook_deliveries
		   SET last_status = $1, last_error = $2, next_attempt_at = $3, in_flight_until = NULL
		 WHERE id = $4
	`, nullableStatus(status), errMsg, nextAt, c.id); err != nil {
		logx.Warn("webhooks.outcome.write_failed",
			"delivery_id", c.id, "stage", "retry", "err", err.Error())
		return
	}
	logx.Info("webhooks.retry_scheduled",
		"delivery_id", c.id, "endpoint_id", c.endpointID,
		"kind", c.kind, "attempts", c.attempts, "next_attempt_at", nextAt, "err", errMsg)
}

// nullableStatus turns 0 into a NULL for last_status when the send
// errored before producing a response code.
func nullableStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}

// maxResponseBodyBytes caps how much of the customer endpoint's
// response we read. We don't actually use the body — we only care
// about the status code — but Go's http stack keeps the connection
// open until the body is drained or closed. A misbehaving (or
// malicious) endpoint can stream gigabytes or trickle slowly,
// monopolising a worker for the full Timeout window. Capping the
// drain to a small number bounds the per-delivery cost.
const maxResponseBodyBytes = 64 << 10 // 64 KiB

// send POSTs body to url with the signature + metadata headers.
// Returns the HTTP status (or 0 on transport error) and any error.
//
// SSRF re-check: webhooks.ValidateHost runs at delivery time too,
// even though the URL was validated at registration. DNS can flip
// between then and now (rebinding attacks deliberately do this), so
// re-resolving and re-checking is the load-bearing piece.
func (w *Worker) send(ctx context.Context, target, secret, kind, deliveryUUID string, body []byte) (int, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return 0, fmt.Errorf("invalid stored url: %w", err)
	}
	// Org endpoints may be localhost: registration (validateWebhookURL)
	// allows it for development.
	if ctx, err = Pin(ctx, parsed.Hostname(), true); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Zv-Signature", sign(secret, body))
	req.Header.Set("X-Zv-Event", kind)
	req.Header.Set("X-Zv-Delivery-Id", deliveryUUID)
	req.Header.Set("User-Agent", "zv-webhooks/1")
	resp, err := w.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drain a small bounded portion of the body so the connection
	// can be reused / closed cleanly. io.Discard sinks the bytes;
	// the LimitReader caps the work even on a streaming endpoint.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBodyBytes))
	return resp.StatusCode, nil
}
