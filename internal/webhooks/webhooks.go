// Package webhooks delivers run-lifecycle and billing events to
// per-org HTTP endpoints that arrangers configure on themselves.
//
// The shape mirrors the Stripe contract on purpose: signed POST,
// HMAC-SHA256 of the raw body, retry with exponential backoff,
// per-event idempotency token. Customers who've integrated with
// Stripe already know the verification dance.
//
// The package splits responsibility:
//
//   - Publish enqueues a delivery row per matching endpoint within
//     the caller's transaction. Cheap; no network.
//   - Worker (delivery.go) drains the queue out-of-band, signs, posts,
//     records the outcome, schedules the next attempt or dead-letters.
//
// Two tables back this: webhook_endpoints (config) and
// webhook_deliveries (queue). See migration 0008.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"git.sr.ht/~klahr/zv/internal/logx"
)

// Event kind constants — the v1 catalog. Adding a new kind is a
// schema-free change, but customers can only subscribe to kinds they
// know exist; keep this list authoritative and document additions.
const (
	KindEventScheduled    = "event.scheduled"
	KindEventStarted      = "event.started"
	KindEventCompleted    = "event.completed"
	KindTeamCreated     = "team.created"
	KindTeamCompleted   = "team.completed"
	KindPurchasePaid    = "purchase.paid"
	KindPurchaseRefunded = "purchase.refunded"

	// KindWebhookTest is reserved for the manual "Send test" button.
	// Deliberately NOT in AllKinds — customers can't subscribe to it
	// (it would just produce unsolicited test deliveries), but the
	// worker happily delivers it because matching happens at enqueue
	// and the test-send path bypasses subscription filtering.
	KindWebhookTest = "webhook.test"
)

// AllKinds is the set advertised in the admin UI / API as
// subscribable. Tests assert this matches the constants above.
var AllKinds = []string{
	KindEventScheduled,
	KindEventStarted,
	KindEventCompleted,
	KindTeamCreated,
	KindTeamCompleted,
	KindPurchasePaid,
	KindPurchaseRefunded,
}

// KnownKind reports whether kind is in AllKinds. Used by the admin
// CRUD handler to reject typoed subscriptions before they reach the
// table.
func KnownKind(kind string) bool {
	for _, k := range AllKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Execer matches *sql.DB and *sql.Tx so callers that already hold a
// transaction can include the delivery enqueue in their atomic unit
// (eg. team.completed should commit together with the team UPDATE
// that triggered it). Same shape as activity.Execer.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Publish enqueues one webhook_deliveries row per active endpoint in
// orgID that subscribes to kind. Returns nil if there are no matching
// endpoints — that's the common case (most orgs don't configure
// webhooks). Errors here are propagated to the caller, but most
// call sites should log and proceed: a failed enqueue isn't worth
// failing the user-visible action that triggered it.
func Publish(ctx context.Context, db Execer, orgID int64, kind string, data any) error {
	if !KnownKind(kind) {
		return fmt.Errorf("webhooks: unknown event kind %q", kind)
	}
	// `event_kinds ? $2` uses the JSONB key-exists operator: for a
	// JSON array it returns true when $2 appears as a string element.
	// Postgres-specific but readable; the alternative TEXT[] scheme
	// doesn't round-trip cleanly through database/sql + pgx stdlib.
	rows, err := db.QueryContext(ctx, `
		SELECT id FROM webhook_endpoints
		 WHERE org_id = $1 AND active = TRUE AND event_kinds ? $2
	`, orgID, kind)
	if err != nil {
		return fmt.Errorf("webhooks: scan endpoints: %w", err)
	}
	var endpointIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("webhooks: scan endpoint id: %w", err)
		}
		endpointIDs = append(endpointIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("webhooks: rows err: %w", err)
	}
	for _, id := range endpointIDs {
		uuid, err := newDeliveryUUID()
		if err != nil {
			return fmt.Errorf("webhooks: gen uuid: %w", err)
		}
		// Each delivery gets its own body — the payload's `id` is
		// pinned to the row's delivery_uuid, so two endpoints
		// receiving the "same" event still see distinct ids.
		body, err := buildPayload(uuid, kind, data)
		if err != nil {
			return fmt.Errorf("webhooks: marshal payload: %w", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO webhook_deliveries
			    (endpoint_id, delivery_uuid, event_kind, payload, status)
			VALUES ($1, $2, $3, $4, 'pending')
		`, id, uuid, kind, string(body)); err != nil {
			return fmt.Errorf("webhooks: enqueue delivery: %w", err)
		}
	}
	return nil
}

// payload is the wire shape we POST to subscribers. type + occurred_at
// at the top level lets customers branch on type without parsing data.
type payload struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurred_at"`
	Data       json.RawMessage `json:"data,omitempty"`
}

// buildPayload returns the bytes that will be POSTed (and signed),
// using the supplied uuid as both the payload's `id` field and the
// stored delivery_uuid (so the JSON body's `id` matches the
// X-Zv-Delivery-Id header — verified-once-deduped-once symmetry).
// We marshal at enqueue time, not at send time, so retries reuse the
// exact bytes the original signature was computed over.
func buildPayload(uuid, kind string, data any) ([]byte, error) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(payload{
		ID:         uuid,
		Type:       kind,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Data:       raw,
	})
}

// newDeliveryUUID returns a 32-char hex random string used both as the
// per-delivery dedupe id and as the payload's "id" field. Crypto-rand
// so customers can't guess delivery ids.
func newDeliveryUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// sign returns the X-Zv-Signature value for body under secret.
// "sha256=<hex>" matches the GitHub / Stripe convention; consumers
// strip the prefix and compare in constant time.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// newSecret returns 32 random bytes hex-encoded — the value we hand
// the customer once, on endpoint creation, for them to configure on
// their verification side.
func newSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// NewSecret is exported for the admin handler that mints endpoints.
func NewSecret() (string, error) { return newSecret() }

// SendTest enqueues a single KindWebhookTest delivery against the
// given endpoint, bypassing subscription matching so the customer
// can verify their handler regardless of which kinds they've
// subscribed to. The worker will pick it up on its next tick.
//
// The payload's `note` field lets a customer who logs bodies
// immediately tell this isn't a real lifecycle event without having
// to branch on the event kind.
func SendTest(ctx context.Context, db Execer, endpointID int64) error {
	uuid, err := newDeliveryUUID()
	if err != nil {
		return fmt.Errorf("webhooks: gen uuid: %w", err)
	}
	body, err := buildPayload(uuid, KindWebhookTest, map[string]any{
		"note": "This is a manual test from the zv arrange UI. " +
			"No real lifecycle event happened. " +
			"Verify your signature handler against this delivery.",
	})
	if err != nil {
		return fmt.Errorf("webhooks: marshal test payload: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries
		    (endpoint_id, delivery_uuid, event_kind, payload, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`, endpointID, uuid, KindWebhookTest, string(body)); err != nil {
		return fmt.Errorf("webhooks: enqueue test delivery: %w", err)
	}
	return nil
}

// TryPublish is the fire-and-forget variant for hot paths. A failed
// enqueue logs and is dropped — the user-visible action that
// triggered the event has already happened, and we'd rather lose
// telemetry to the customer than 500 the user. Use this at the
// call sites that don't have a meaningful way to recover.
func TryPublish(ctx context.Context, db Execer, orgID int64, kind string, data any) {
	if err := Publish(ctx, db, orgID, kind, data); err != nil {
		logx.Warn("webhooks.publish_failed",
			"org_id", orgID, "kind", kind, "err", err.Error())
	}
}
