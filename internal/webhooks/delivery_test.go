package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.sr.ht/~klahr/zv/internal/dbtest"
)

// seedEndpoint inserts a minimal org + endpoint and returns the
// endpoint id and secret. The test schema requires an org row; we
// use the seed admin's "Arrangers" org if present, otherwise insert
// a stub. Either way the migrated DB has every table the worker
// touches.
func seedEndpoint(t *testing.T, db *sql.DB, url string, eventKinds []string) (endpointID int64, secret string) {
	t.Helper()
	var orgID int64
	if err := db.QueryRow(`SELECT id FROM organizations ORDER BY id LIMIT 1`).Scan(&orgID); err != nil {
		// Fresh dbtest database: insert a minimal org so the FK
		// holds. The actual org content doesn't matter for the
		// worker test — we only need a row for the endpoint to
		// reference.
		if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('test') RETURNING id`).Scan(&orgID); err != nil {
			t.Fatalf("seed org: %v", err)
		}
	}
	var s string
	{
		var err error
		s, err = newSecret()
		if err != nil {
			t.Fatalf("secret: %v", err)
		}
	}
	kindsJSON := `[`
	for i, k := range eventKinds {
		if i > 0 {
			kindsJSON += ","
		}
		kindsJSON += `"` + k + `"`
	}
	kindsJSON += `]`
	var id int64
	if err := db.QueryRow(`
		INSERT INTO webhook_endpoints(org_id, url, secret, event_kinds)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id
	`, orgID, url, s, kindsJSON).Scan(&id); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	return id, s
}

// TestWorker_DeliveredOnSuccess: a 200-returning endpoint flips a
// pending row to delivered, records last_status=200, attempts=1,
// stamps delivered_at, and clears last_error.
func TestWorker_DeliveredOnSuccess(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	type recv struct {
		body []byte
		sig  string
		kind string
		uuid string
	}
	got := make(chan recv, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- recv{
			body: body,
			sig:  r.Header.Get("X-Zv-Signature"),
			kind: r.Header.Get("X-Zv-Event"),
			uuid: r.Header.Get("X-Zv-Delivery-Id"),
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	endpointID, secret := seedEndpoint(t, db, srv.URL, []string{KindEventStarted})

	// Enqueue one delivery directly (Publish requires the slug-match
	// path; we already know the endpoint id, so insert by hand).
	if err := SendTest(context.Background(), db, endpointID); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	w := &Worker{DB: db, Client: srv.Client()}
	if err := w.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Verify the receiver saw a correctly-signed POST.
	select {
	case r := <-got:
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(r.body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if r.sig != expected {
			t.Errorf("signature mismatch:\n got %s\nwant %s", r.sig, expected)
		}
		if r.kind != KindWebhookTest {
			t.Errorf("event header = %q, want %q", r.kind, KindWebhookTest)
		}
		if r.uuid == "" {
			t.Errorf("delivery-id header empty")
		}
		// The body's "id" field is pinned to the same uuid as the
		// header (verified-once-deduped-once symmetry).
		if !strings.Contains(string(r.body), `"id":"`+r.uuid+`"`) {
			t.Errorf("body id != header delivery-id; body=%s", r.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("endpoint never received the POST")
	}

	// Verify the row is now delivered.
	var status string
	var attempts int
	var lastStatus sql.NullInt64
	var deliveredAt sql.NullString
	if err := db.QueryRow(`
		SELECT status, attempts, last_status, delivered_at FROM webhook_deliveries
		 WHERE endpoint_id = $1 ORDER BY id DESC LIMIT 1
	`, endpointID).Scan(&status, &attempts, &lastStatus, &deliveredAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status != "delivered" {
		t.Errorf("status=%q, want delivered", status)
	}
	if attempts != 1 {
		t.Errorf("attempts=%d, want 1", attempts)
	}
	if !lastStatus.Valid || lastStatus.Int64 != 200 {
		t.Errorf("last_status=%v, want 200", lastStatus)
	}
	if !deliveredAt.Valid || deliveredAt.String == "" {
		t.Errorf("delivered_at not set")
	}
}

// TestWorker_RetryOnNon2xx: a 500-returning endpoint leaves the row
// pending, increments attempts, records last_status=500, advances
// next_attempt_at by the first backoff slot (1 minute).
func TestWorker_RetryOnNon2xx(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	endpointID, _ := seedEndpoint(t, db, srv.URL, []string{KindWebhookTest})
	if err := SendTest(context.Background(), db, endpointID); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	w := &Worker{DB: db, Client: srv.Client()}
	before := time.Now().UTC()
	if err := w.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var (
		status        string
		attempts      int
		lastStatus    sql.NullInt64
		nextAttemptAt string
		lastError     sql.NullString
	)
	if err := db.QueryRow(`
		SELECT status, attempts, last_status, next_attempt_at, last_error
		FROM webhook_deliveries WHERE endpoint_id = $1 ORDER BY id DESC LIMIT 1
	`, endpointID).Scan(&status, &attempts, &lastStatus, &nextAttemptAt, &lastError); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status != "pending" {
		t.Errorf("status=%q, want pending (a 500 schedules a retry, not a dead-letter)", status)
	}
	if attempts != 1 {
		t.Errorf("attempts=%d, want 1", attempts)
	}
	if !lastStatus.Valid || lastStatus.Int64 != 500 {
		t.Errorf("last_status=%v, want 500", lastStatus)
	}
	if !lastError.Valid || !strings.Contains(lastError.String, "500") {
		t.Errorf("last_error=%v, want contains 500", lastError)
	}
	// next_attempt_at should be the first backoff (1m) ahead of when
	// we ticked. Allow a 5-second slack for clock variance + DB
	// round-trip.
	parsed, err := time.Parse("2006-01-02 15:04:05", nextAttemptAt)
	if err != nil {
		t.Fatalf("parse next_attempt_at %q: %v", nextAttemptAt, err)
	}
	delta := parsed.Sub(before)
	if delta < 55*time.Second || delta > 65*time.Second {
		t.Errorf("next_attempt_at delta=%s, want ~60s (first-failure backoff)", delta)
	}
}

// TestWorker_DeadLetterAfterMaxAttempts: a delivery already at
// MaxAttempts-1 transitions to dead on the next failed attempt and
// stops being picked up.
func TestWorker_DeadLetterAfterMaxAttempts(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	endpointID, _ := seedEndpoint(t, db, srv.URL, []string{KindWebhookTest})
	if err := SendTest(context.Background(), db, endpointID); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	// Pre-age the row to MaxAttempts-1 so a single failed tick will
	// dead-letter it. Avoids running the full backoff schedule in
	// the test.
	if _, err := db.Exec(`
		UPDATE webhook_deliveries SET attempts = $1
	`, MaxAttempts-1); err != nil {
		t.Fatalf("pre-age: %v", err)
	}

	w := &Worker{DB: db, Client: srv.Client()}
	if err := w.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var status string
	var attempts int
	if err := db.QueryRow(`
		SELECT status, attempts FROM webhook_deliveries
		 WHERE endpoint_id = $1 ORDER BY id DESC LIMIT 1
	`, endpointID).Scan(&status, &attempts); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status != "dead" {
		t.Errorf("status=%q, want dead", status)
	}
	if attempts != MaxAttempts {
		t.Errorf("attempts=%d, want %d", attempts, MaxAttempts)
	}

	// A second tick must NOT pick the dead row up again.
	if err := w.tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if err := db.QueryRow(`
		SELECT attempts FROM webhook_deliveries
		 WHERE endpoint_id = $1 ORDER BY id DESC LIMIT 1
	`, endpointID).Scan(&attempts); err != nil {
		t.Fatalf("select after second tick: %v", err)
	}
	if attempts != MaxAttempts {
		t.Errorf("dead row attempts changed to %d after re-tick; should be sticky at %d",
			attempts, MaxAttempts)
	}
}

// TestWorker_DisabledEndpointDeadletters: the worker drops a delivery
// whose endpoint went inactive between enqueue and tick. The row
// transitions to dead with last_error="endpoint disabled" and the
// customer's URL is never hit.
func TestWorker_DisabledEndpointDeadletters(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	endpointID, _ := seedEndpoint(t, db, srv.URL, []string{KindWebhookTest})
	if err := SendTest(context.Background(), db, endpointID); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	// Disable AFTER enqueue: the row is already pending; the worker
	// should observe the disabled state on pickup.
	if _, err := db.Exec(`UPDATE webhook_endpoints SET active = FALSE WHERE id = $1`, endpointID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	w := &Worker{DB: db, Client: srv.Client()}
	if err := w.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if hits != 0 {
		t.Errorf("endpoint received %d POSTs; should receive 0 when disabled", hits)
	}

	var status string
	var lastError sql.NullString
	if err := db.QueryRow(`
		SELECT status, last_error FROM webhook_deliveries
		 WHERE endpoint_id = $1 ORDER BY id DESC LIMIT 1
	`, endpointID).Scan(&status, &lastError); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status != "dead" {
		t.Errorf("status=%q, want dead", status)
	}
	if !lastError.Valid || !strings.Contains(lastError.String, "disabled") {
		t.Errorf("last_error=%v, want contains 'disabled'", lastError)
	}
}
