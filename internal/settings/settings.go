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
	"os"
	"strings"
)

// Setting keys. Surfaced as constants so the migration defaults, the
// admin handler, and the read sites in auth/server all reference the
// same strings.
const (
	KeySignupEnabled  = "signup_enabled"
	KeyLandingEnabled = "landing_enabled"
)

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
	return setBool(ctx, db, KeySignupEnabled, on)
}

// LandingEnabled reports whether the public marketing surfaces
// (`/` landing page, `/pricing`) are exposed. When off, an
// unauthenticated visitor lands directly on /arrange and /pricing
// 404s. Logged-in arrangers and players are unaffected (they're
// redirected away from `/` either way).
//
// Precedence:
//  1. ZV_LANDING_ENABLED env var (when set, wins) — lets self-host
//     operators pin the value via .env without touching the DB.
//  2. The `landing_enabled` row in the settings table (admin UI).
//
// Defaults to false on lookup error: the canonical deployment is
// self-host, where there is no marketing site to expose, so a
// transient DB hiccup mustn't accidentally surface one. Hosted ops
// who flipped it on via the admin Settings page see the row hit
// during a normal request and so don't trip the fallback path.
func LandingEnabled(ctx context.Context, db *sql.DB) bool {
	if v, ok := envBool("ZV_LANDING_ENABLED"); ok {
		return v
	}
	v, err := getString(ctx, db, KeyLandingEnabled)
	if err != nil {
		return false
	}
	return parseBool(v, false)
}

// LandingPinnedByEnv reports whether the env var is currently
// overriding the DB. The admin Settings UI uses this to disable the
// toggle and explain why flipping it has no effect, so a self-host
// operator who pinned it in .env doesn't waste time wondering why the
// UI is lying to them.
func LandingPinnedByEnv() bool {
	_, ok := envBool("ZV_LANDING_ENABLED")
	return ok
}

// SetLandingEnabled persists the marketing-surface toggle.
func SetLandingEnabled(ctx context.Context, db *sql.DB, on bool) error {
	return setBool(ctx, db, KeyLandingEnabled, on)
}

func setBool(ctx context.Context, db *sql.DB, key string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at)
		VALUES ($1, $2, now_text())
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, v)
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

// envBool reads a boolean env var. Returns (value, true) when the var
// is set to a recognisable boolean string, (false, false) otherwise —
// so an unset or garbled var falls through to the next precedence
// layer instead of silently meaning "false".
func envBool(name string) (bool, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	}
	return false, false
}
