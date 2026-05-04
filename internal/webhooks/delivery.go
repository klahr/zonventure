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

	"git.sr.ht/~klahr/zv/internal/logx"
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
		// CheckRedirect refusing follows is the load-bearing piece
		// here: without it, a customer-supplied URL that 302s to
		// http://169.254.169.254/ (or any other internal host) would
		// silently pivot the worker into the cloud metadata API
		// even though the registered host validated cleanly. Returning
		// ErrUseLastResponse stops the redirect chain at the first
		// 3xx and returns that response untouched — same shape as
		// "we got a non-2xx", so the existing retry/backoff logic
		// just records it as a delivery failure.
		w.Client = &http.Client{
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

// tick processes every due delivery in one pass. Each delivery is
// fetched, attempted, and recorded individually so a single bad row
// doesn't poison the batch. Order: oldest next_attempt_at first, so
// retried-and-due rows precede freshly-enqueued ones (fairness).
func (w *Worker) tick(ctx context.Context) error {
	for {
		// Pull one due row at a time and SKIP LOCKED so multiple
		// workers (today: one) wouldn't fight for the same row. The
		// FOR UPDATE arms the row lock; we update status/attempts
		// inside the same tx and commit so the lock releases.
		tx, err := w.DB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin: %w", err)
		}
		var (
			id           int64
			endpointID   int64
			deliveryUUID string
			kind         string
			payloadStr   string
			attempts     int
		)
		err = tx.QueryRowContext(ctx, `
			SELECT id, endpoint_id, delivery_uuid, event_kind, payload, attempts
			  FROM webhook_deliveries
			 WHERE status = 'pending' AND next_attempt_at <= now_text()
			 ORDER BY next_attempt_at
			 LIMIT 1
			 FOR UPDATE SKIP LOCKED
		`).Scan(&id, &endpointID, &deliveryUUID, &kind, &payloadStr, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return nil
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("select due: %w", err)
		}
		var (
			endpointURL string
			secret      string
			active      bool
		)
		if err := tx.QueryRowContext(ctx,
			`SELECT url, secret, active FROM webhook_endpoints WHERE id = $1`,
			endpointID,
		).Scan(&endpointURL, &secret, &active); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("select endpoint: %w", err)
		}
		// Endpoint disabled between enqueue and delivery: drop the
		// delivery quietly. Re-enabling won't replay — that matches
		// "active=false means stop sending", which is what the
		// customer asked for.
		if !active {
			if _, err := tx.ExecContext(ctx,
				`UPDATE webhook_deliveries SET status = 'dead', last_error = 'endpoint disabled' WHERE id = $1`,
				id,
			); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("mark dead (disabled): %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit dead-disabled: %w", err)
			}
			continue
		}

		// Send happens INSIDE the tx-with-row-lock so the row stays
		// locked for the duration. That's intentional: it prevents
		// a future second worker from picking up the same row mid-
		// flight. The 30s client timeout caps how long we hold it.
		status, sendErr := w.send(ctx, endpointURL, secret, kind, deliveryUUID, []byte(payloadStr))

		newAttempts := attempts + 1
		if sendErr == nil && status >= 200 && status < 300 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE webhook_deliveries
				   SET status = 'delivered',
				       attempts = $1,
				       last_status = $2,
				       last_error = NULL,
				       delivered_at = now_text()
				 WHERE id = $3
			`, newAttempts, status, id); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("mark delivered: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit delivered: %w", err)
			}
			logx.Info("webhooks.delivered",
				"delivery_id", id, "endpoint_id", endpointID,
				"kind", kind, "status", status, "attempts", newAttempts)
			continue
		}

		// Failure path: record + reschedule, or dead-letter if we've
		// exhausted attempts.
		errMsg := ""
		if sendErr != nil {
			errMsg = sendErr.Error()
		} else {
			errMsg = fmt.Sprintf("HTTP %d", status)
		}
		if newAttempts >= MaxAttempts {
			if _, err := tx.ExecContext(ctx, `
				UPDATE webhook_deliveries
				   SET status = 'dead', attempts = $1, last_status = $2, last_error = $3
				 WHERE id = $4
			`, newAttempts, nullableStatus(status), errMsg, id); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("mark dead: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit dead: %w", err)
			}
			logx.Warn("webhooks.dead_letter",
				"delivery_id", id, "endpoint_id", endpointID,
				"kind", kind, "attempts", newAttempts, "err", errMsg)
			continue
		}
		nextAt := time.Now().UTC().Add(backoffDelay(newAttempts)).Format("2006-01-02 15:04:05")
		if _, err := tx.ExecContext(ctx, `
			UPDATE webhook_deliveries
			   SET attempts = $1, last_status = $2, last_error = $3, next_attempt_at = $4
			 WHERE id = $5
		`, newAttempts, nullableStatus(status), errMsg, nextAt, id); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("reschedule: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit reschedule: %w", err)
		}
		logx.Info("webhooks.retry_scheduled",
			"delivery_id", id, "endpoint_id", endpointID,
			"kind", kind, "attempts", newAttempts, "next_attempt_at", nextAt, "err", errMsg)
	}
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
	host := parsed.Hostname()
	if !IsLoopbackHost(host) {
		// Resolve once and pin the literal IP onto the request
		// context. The Transport's DialContext (dialPinned) reads
		// the value and connects to that exact address — without it,
		// the default resolver would re-resolve the hostname at dial
		// time and a TTL-flip / DNS-rebinding flip between this
		// LookupPublic call and the connect could land on an
		// internal IP.
		ip, err := LookupPublic(host)
		if err != nil {
			return 0, fmt.Errorf("ssrf gate: %w", err)
		}
		ctx = context.WithValue(ctx, pinnedDialIPKey{}, ip.String())
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
