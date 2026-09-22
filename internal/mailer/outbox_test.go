package mailer_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/mailer"
)

// recordingMailer captures every Send call so tests can assert what
// the outbox handed to the underlying mailer. mode controls the
// outcome: "ok" returns nil, "fail" returns a transient error so the
// outbox reschedules.
type recordingMailer struct {
	mu    sync.Mutex
	calls []mailer.Message
	mode  string // "ok" | "fail"
}

func (r *recordingMailer) Send(_ context.Context, m mailer.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, m)
	if r.mode == "fail" {
		return errors.New("recording mailer: forced failure")
	}
	return nil
}

func (r *recordingMailer) Calls() []mailer.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]mailer.Message, len(r.calls))
	copy(out, r.calls)
	return out
}

// TestOutbox_Enqueue_HappyPath persists a single message, runs one
// tick, and asserts the underlying Mailer saw exactly one Send.
func TestOutbox_Enqueue_HappyPath(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	mock := &recordingMailer{mode: "ok"}
	o := &mailer.Outbox{DB: db, Mailer: mock}

	msg := mailer.Message{
		To:      []string{"buyer@example.com"},
		Subject: "Your team link",
		Text:    "Open the link below to set up your team.",
	}
	if err := o.Enqueue(context.Background(), "ticket_team_link", msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Drain via the test-only ticker — we don't want to wait the
	// 5s production poll interval. Outbox.Run is a thin wrapper
	// around tick(); calling tick directly mirrors what one
	// scheduler tick would do.
	if err := mailer.ExportedTick(o, context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	calls := mock.Calls()
	if len(calls) != 1 {
		t.Fatalf("Send call count = %d, want 1", len(calls))
	}
	if calls[0].To[0] != "buyer@example.com" {
		t.Errorf("To = %v, want [buyer@example.com]", calls[0].To)
	}
	// DB row should now be marked sent.
	var status string
	if err := db.QueryRow(`SELECT status FROM mail_outbox`).Scan(&status); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if status != "sent" {
		t.Errorf("status = %q, want sent", status)
	}
}

// TestOutbox_Enqueue_RetryOnFailure exercises the failure path. After
// one failed tick the row should still be pending, attempts bumped,
// next_attempt_at scheduled in the future.
func TestOutbox_Enqueue_RetryOnFailure(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	mock := &recordingMailer{mode: "fail"}
	o := &mailer.Outbox{DB: db, Mailer: mock}

	msg := mailer.Message{
		To:      []string{"buyer@example.com"},
		Subject: "Your team link",
		Text:    "Open the link below to set up your team.",
	}
	if err := o.Enqueue(context.Background(), "ticket_team_link", msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := mailer.ExportedTick(o, context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var (
		status   string
		attempts int
		nextAt   string
		lastErr  *string
		inFlight *string
	)
	if err := db.QueryRow(
		`SELECT status, attempts, next_attempt_at, last_err, in_flight_until FROM mail_outbox`,
	).Scan(&status, &attempts, &nextAt, &lastErr, &inFlight); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending (failure should reschedule, not flip status)", status)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if lastErr == nil || *lastErr == "" {
		t.Error("last_err should be populated after a failure")
	}
	if inFlight != nil && *inFlight != "" {
		t.Errorf("in_flight_until should be cleared on outcome write, got %v", *inFlight)
	}
	// next_attempt_at should be in the future (at least the first
	// backoff slot, ~1 minute).
	t0 := time.Now()
	parsed, perr := time.Parse("2006-01-02 15:04:05", nextAt)
	if perr != nil {
		t.Fatalf("parse next_attempt_at %q: %v", nextAt, perr)
	}
	if parsed.Before(t0.Add(30 * time.Second)) {
		t.Errorf("next_attempt_at = %s; want at least ~1 minute from now", nextAt)
	}
}

// TestOutbox_Enqueue_RejectsBadInput asserts the caller-side
// validation (no recipients, empty body, attachments).
func TestOutbox_Enqueue_RejectsBadInput(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	o := &mailer.Outbox{DB: db, Mailer: &recordingMailer{mode: "ok"}}
	ctx := context.Background()

	if err := o.Enqueue(ctx, "k", mailer.Message{Subject: "x", Text: "y"}); !errors.Is(err, mailer.ErrNoRecipients) {
		t.Errorf("empty To: err = %v, want ErrNoRecipients", err)
	}
	if err := o.Enqueue(ctx, "k", mailer.Message{To: []string{"a@b"}, Subject: "x"}); !errors.Is(err, mailer.ErrEmptyBody) {
		t.Errorf("empty body: err = %v, want ErrEmptyBody", err)
	}
	att := mailer.Attachment{Filename: "f", Bytes: []byte("x")}
	err := o.Enqueue(ctx, "k", mailer.Message{To: []string{"a@b"}, Subject: "x", Text: "y", Attachments: []mailer.Attachment{att}})
	if err == nil {
		t.Error("attachments: err = nil, want refusal")
	}
}
