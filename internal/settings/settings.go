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
	"sync/atomic"
)

// Setting keys. Surfaced as constants so the migration defaults, the
// admin handler, and the read sites in auth/server all reference the
// same strings.
const (
	KeySignupEnabled   = "signup_enabled"
	KeyLandingEnabled  = "landing_enabled"
	KeyRedirectTarget  = "redirect_target"
	KeyMaintenanceMode = "maintenance_mode"
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

// redirectTarget is the in-process cache the redirect middleware reads
// on every request — atomic so the middleware never blocks on a DB
// query, never panics on a torn read, and works after the operator's
// DB has gone away. Seeded from LoadRedirectTarget at startup;
// SetRedirectTarget keeps it in sync with the DB row.
var redirectTarget atomic.Pointer[string]

// RedirectTarget returns the URL the redirect-mode middleware should
// 301 every non-allowlisted path to, or "" when redirect mode is off.
// Read from the in-memory atomic pointer that LoadRedirectTarget
// seeded at startup, so callers don't hit the DB on the hot path.
func RedirectTarget() string {
	if p := redirectTarget.Load(); p != nil {
		return *p
	}
	return ""
}

// RedirectPinnedByEnv reports whether ZV_REDIRECT_TARGET pinned the
// value at startup. The admin Settings UI uses this to disable the
// "Disable redirect mode" affordance and explain why flipping it has
// no effect.
func RedirectPinnedByEnv() bool {
	return strings.TrimSpace(os.Getenv("ZV_REDIRECT_TARGET")) != ""
}

// LoadRedirectTarget seeds the in-memory atomic pointer at startup.
// Env var wins (so an operator can always pin the value via .env);
// otherwise the DB row is read. Called once from server.New before
// the middleware chain is built. Tolerates a nil db so tests that
// drive the middleware can pin the value via env without standing
// up Postgres — production always passes a real handle.
func LoadRedirectTarget(ctx context.Context, db *sql.DB) {
	if v := strings.TrimSpace(os.Getenv("ZV_REDIRECT_TARGET")); v != "" {
		setRedirectInMemory(v)
		return
	}
	if db == nil {
		setRedirectInMemory("")
		return
	}
	v, err := getString(ctx, db, KeyRedirectTarget)
	if err != nil {
		setRedirectInMemory("")
		return
	}
	setRedirectInMemory(strings.TrimSpace(v))
}

// SetRedirectTarget persists the redirect-mode target and updates the
// in-memory atomic pointer in one step. Empty string clears the row
// (and disables redirect mode). Returns an error when the env var has
// pinned the value — the admin Settings UI surfaces RedirectPinnedByEnv
// before the user even tries, but the API guard catches a stale tab.
func SetRedirectTarget(ctx context.Context, db *sql.DB, target string) error {
	if RedirectPinnedByEnv() {
		return errors.New("settings: ZV_REDIRECT_TARGET pinned by env; unset it to manage from the admin UI")
	}
	target = strings.TrimSpace(target)
	if err := setString(ctx, db, KeyRedirectTarget, target); err != nil {
		return err
	}
	setRedirectInMemory(target)
	return nil
}

func setRedirectInMemory(v string) {
	cp := v
	redirectTarget.Store(&cp)
}

// maintenanceMode is the in-process flag the maintenance middleware
// reads on every mutation request. When true, every non-allowlisted
// write 503s so an operator running an org-bundle export can be sure
// the snapshot they're capturing isn't being mutated underneath them.
// Atomic so the middleware path is lock-free.
var maintenanceMode atomic.Bool

// MaintenanceMode reports whether the instance is in operator-
// initiated maintenance mode. Reads are constant-time and DB-free.
func MaintenanceMode() bool { return maintenanceMode.Load() }

// MaintenancePinnedByEnv reports whether ZV_MAINTENANCE_MODE pinned
// the value at startup. The admin Settings UI uses this to disable
// the toggle and explain why flipping it has no effect.
func MaintenancePinnedByEnv() bool {
	_, ok := envBool("ZV_MAINTENANCE_MODE")
	return ok
}

// LoadMaintenanceMode seeds the in-memory flag at startup. Env
// pin wins; otherwise the DB row is read. Tolerates a nil db so
// tests can drive the middleware via env without a Postgres handle.
func LoadMaintenanceMode(ctx context.Context, db *sql.DB) {
	if v, ok := envBool("ZV_MAINTENANCE_MODE"); ok {
		maintenanceMode.Store(v)
		return
	}
	if db == nil {
		maintenanceMode.Store(false)
		return
	}
	v, err := getString(ctx, db, KeyMaintenanceMode)
	if err != nil {
		maintenanceMode.Store(false)
		return
	}
	maintenanceMode.Store(parseBool(v, false))
}

// SetMaintenanceMode persists the flag and updates the in-memory
// atomic in one step. Refuses when the env var has pinned the
// value — the GUI surfaces this via MaintenancePinnedByEnv before
// the user even tries.
func SetMaintenanceMode(ctx context.Context, db *sql.DB, on bool) error {
	if MaintenancePinnedByEnv() {
		return errors.New("settings: ZV_MAINTENANCE_MODE pinned by env; unset it to manage from the admin UI")
	}
	if err := setBool(ctx, db, KeyMaintenanceMode, on); err != nil {
		return err
	}
	maintenanceMode.Store(on)
	return nil
}

func setBool(ctx context.Context, db *sql.DB, key string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return setString(ctx, db, key, v)
}

func setString(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at)
		VALUES ($1, $2, now_text())
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value)
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
