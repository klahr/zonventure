package player

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// maxClientActionIDLen caps the size of an idempotency key. The
// client generates UUIDs (36 chars) so 64 is comfortable headroom
// without making accidental abuse cheap.
const maxClientActionIDLen = 64

// validClientActionID returns the trimmed, validated key (or "" if
// the caller didn't supply one or supplied something we won't store).
// Whitespace-only and over-length values are quietly dropped — the
// dedup is best-effort, never load-bearing for correctness.
func validClientActionID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxClientActionIDLen {
		return ""
	}
	return s
}

// cachedAction is what we replay on a retry: the HTTP status and the
// JSON body bytes recorded the first time the action ran. Empty body
// is fine — some endpoints return 204 No Content.
type cachedAction struct {
	Status int
	Body   []byte
}

// lookupCachedAction reads any prior response for (slot, clientActionID).
// Returns (cached, true) on hit; (zero, false) on miss (including a
// blank clientActionID — callers gate on that and skip the lookup
// when there's nothing to dedup against).
func lookupCachedAction(ctx context.Context, db *sql.DB, slotID int64, clientActionID string) (cachedAction, bool, error) {
	if clientActionID == "" {
		return cachedAction{}, false, nil
	}
	var (
		status int
		body   string
	)
	err := db.QueryRowContext(ctx, `
		SELECT response_status, response_body FROM player_action_dedup
		WHERE slot_id = $1 AND client_action_id = $2
	`, slotID, clientActionID).Scan(&status, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return cachedAction{}, false, nil
	}
	if err != nil {
		return cachedAction{}, false, err
	}
	return cachedAction{Status: status, Body: []byte(body)}, true, nil
}

// recordAction stores (status, body) under (slot, clientActionID) so
// a future retry returns the same response. ON CONFLICT DO NOTHING
// because two simultaneous retries that both miss the lookup will
// both attempt to write — first one wins, second is discarded
// silently. Returning an error here would mask the actual handler
// outcome and isn't useful to the caller.
func recordAction(ctx context.Context, db *sql.DB, slotID int64, clientActionID string, status int, body []byte) {
	if clientActionID == "" {
		return
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO player_action_dedup(slot_id, client_action_id, response_status, response_body)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, slotID, clientActionID, status, string(body))
}
