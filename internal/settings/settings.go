// Package settings is a thin typed wrapper over the `settings` KV
// table for operator-tunable runtime knobs. Admins read + write these
// from the Settings page in /arrange; signup, login and middleware
// read them inline so a flip propagates without a process restart.
//
// The table itself is generic key/value TEXT; the helpers in this
// package own the parsing rules so callers don't repeat the same
// "1"/"true"/"on" → bool dance everywhere.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// KeySignupEnabled is the only settings key today. Surfaced as a
// constant so the migration default, the admin handler, and the auth
// handler all reference the same string.
const KeySignupEnabled = "signup_enabled"

// SignupEnabled reports whether the public /api/auth/signup endpoint
// is allowed to create new organizations. Defaults to true on any
// failure (missing row, DB hiccup) so a transient lookup error doesn't
// take a working signup flow down — the admin can still flip it off
// explicitly via the UI when they actually want it off.
func SignupEnabled(ctx context.Context, db *sql.DB) bool {
	v, err := getString(ctx, db, KeySignupEnabled)
	if err != nil {
		return true
	}
	return parseBool(v, true)
}

// SetSignupEnabled writes the new value, upserting the row so the
// caller doesn't have to know whether the key already exists.
func SetSignupEnabled(ctx context.Context, db *sql.DB, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at)
		VALUES ($1, $2, now_text())
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, KeySignupEnabled, v)
	return err
}

func getString(ctx context.Context, db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return v, err
}

// parseBool accepts the few representations callers reasonably use
// from a TEXT column: "1"/"0", "true"/"false", "on"/"off", "yes"/"no".
// Anything else falls back to def — better than panicking on a typo'd
// row hand-edited by an operator.
func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}
	return def
}
