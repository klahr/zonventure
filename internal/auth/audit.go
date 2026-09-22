package auth

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/klahr/zonventure/internal/logx"
)

// AuthEvent kinds. Strings are the values stored in auth_events.event;
// keep stable across releases so old rows remain readable.
const (
	EventLogin       = "login"
	EventSetPassword = "set_password"
	EventLogout      = "logout"
	EventMagicLink   = "magic_link"
	// EventEmailVerify covers both self-serve verification (via the
	// emailed token) and admin-initiated manual verification. The
	// reason field disambiguates: ReasonSuccess for token redemption,
	// ReasonAdminManual for the admin override.
	EventEmailVerify = "email_verify"
	// TOTP 2FA lifecycle events. Setup is the first leg of enrolment
	// (a secret is generated, totp_enabled stays false); Confirm flips
	// totp_enabled=true after the user verifies the first code.
	// Disable / AdminReset clear the secret; Verify is one successful
	// post-password TOTP step; RecoveryCodeUse is one successful
	// recovery-code redemption (also single-use).
	EventTOTPSetup           = "totp_setup"
	EventTOTPConfirm         = "totp_confirm"
	EventTOTPDisable         = "totp_disable"
	EventTOTPVerify          = "totp_verify"
	EventTOTPRecoveryCodeUse = "totp_recovery_code_use"
	EventTOTPAdminReset      = "totp_admin_reset"
)

// Common reason codes. Free-form strings — these are the canonical
// values the admin UI knows how to label, but anything goes.
const (
	ReasonSuccess            = "success"
	ReasonInvalidCreds       = "invalid_credentials"
	ReasonRateLimited        = "rate_limited"
	ReasonPasswordNotSet     = "password_not_set"
	ReasonPasswordAlreadySet = "password_already_set"
	ReasonExpiredToken       = "expired_token"
	ReasonUnknownToken       = "unknown_token"
	ReasonAdminManual        = "admin_manual"
	// TOTP-specific reasons. Required is set on a login attempt that
	// short-circuits with a totp_required response (password was OK,
	// second factor still needs entering). Invalid covers both bad
	// codes and bad recovery codes; ChallengeExpired is for the
	// 5-minute TTL elapsing between the password and the code.
	ReasonTOTPRequired         = "totp_required"
	ReasonTOTPInvalid          = "totp_invalid"
	ReasonTOTPChallengeExpired = "totp_challenge_expired"
	ReasonRecoveryCodeInvalid  = "recovery_code_invalid"
	ReasonTOTPAdminReset       = "totp_admin_reset"
)

// AuditEvent is the payload of one auth_events row. UserID is optional;
// pass nil for failed logins where the username didn't resolve to a
// real user — username + ip + ts are the parts an investigator needs
// in that case.
type AuditEvent struct {
	Event     string
	OK        bool
	UserID    *int64
	Username  string
	IP        string
	UserAgent string
	Reason    string
}

// Record writes one auth_events row. Best-effort — a write failure is
// logged via logx but never returned, because nothing user-facing
// should fail because the audit table is unreachable.
func Record(ctx context.Context, db *sql.DB, e AuditEvent) {
	if db == nil {
		return
	}
	ok := 0
	if e.OK {
		ok = 1
	}
	var userID any
	if e.UserID != nil {
		userID = *e.UserID
	}
	// Cap user-agent at 512 bytes — long enough to keep the useful
	// fingerprint, short enough that a malicious client can't bloat the
	// row to break pagination.
	ua := e.UserAgent
	if len(ua) > 512 {
		ua = ua[:512]
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_events(event, ok, user_id, username, ip, user_agent, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, e.Event, ok, userID, e.Username, e.IP, ua, e.Reason); err != nil {
		logx.Warn("auth.audit.write_failed", "event", e.Event, "username", e.Username, "err", err.Error())
	}
}

// UserAgent extracts the request's User-Agent header, trimmed of the
// surrounding context the audit row doesn't need.
func UserAgent(r *http.Request) string {
	return r.Header.Get("User-Agent")
}
