package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/tokenx"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// TOTP 2FA implementation. The user-visible flow is:
//
//   enrol:    SetupTOTP → ConfirmTOTP (returns recovery codes once)
//   login:    VerifyPassword → IssueChallenge → ConsumeChallenge
//   disable:  DisableTOTP / AdminReset
//
// The secret is encrypted at rest with AES-GCM using a key from the
// ZV_TOTP_KEY env var. Losing the key forces every enrolled user to
// re-enrol — operators should treat it like a database credential.
//
// TOTPChallengeTTL is how long the bridge token between the password
// step and the TOTP step stays valid. Five minutes is generous enough
// for a fumble with the authenticator app, tight enough that a leaked
// challenge token doesn't sit on the wire for long.
const TOTPChallengeTTL = 5 * time.Minute

// TOTPIssuer is the label that appears in the user's authenticator
// app next to the account row. Matches the product name; not
// configurable today since most authenticator apps merge entries by
// (issuer, account) anyway.
const TOTPIssuer = "Zonventure"

// totpRecoveryCodeCount is how many single-use recovery codes are
// minted per enrolment. Ten balances "enough for a few lockouts" with
// "few enough that operators don't have to sweep a long table".
const totpRecoveryCodeCount = 10

// totpRecoveryCodeAlphabet is the alphabet used for the XXXX-XXXX
// codes. Lowercase a-z + 2-9 — drops 0, 1, o, l to dodge the
// hand-transcription confusables.
const totpRecoveryCodeAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// Errors returned by the TOTP layer. Handlers map these to specific
// audit reasons + HTTP status codes.
var (
	ErrTOTPKeyMissing       = errors.New("ZV_TOTP_KEY is not configured")
	ErrTOTPKeyMalformed     = errors.New("ZV_TOTP_KEY is malformed (need 32 bytes base64)")
	ErrTOTPNotEnrolled      = errors.New("totp not enrolled")
	ErrTOTPAlreadyEnrolled  = errors.New("totp already enrolled")
	ErrTOTPInvalid          = errors.New("invalid totp code")
	ErrChallengeUnknown     = errors.New("totp challenge unknown")
	ErrChallengeExpired     = errors.New("totp challenge expired")
	ErrRecoveryCodeInvalid  = errors.New("recovery code invalid")
	ErrTOTPSecretCorrupt    = errors.New("totp secret could not be decrypted")
)

// LoadTOTPKey decodes ZV_TOTP_KEY into a 32-byte AES key. Called
// once at server boot — any misconfiguration fails fast with a
// clear error rather than ambushing the first user trying to enrol.
func LoadTOTPKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("ZV_TOTP_KEY"))
	if raw == "" {
		return nil, ErrTOTPKeyMissing
	}
	// Accept both standard and URL base64, with or without padding —
	// any of `openssl rand -base64 32`, `openssl rand -hex 32 |
	// xxd -r -p | base64`, or whatever the operator's key store
	// produced will round-trip.
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(raw)
	}
	if err != nil {
		b, err = base64.RawURLEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTOTPKeyMalformed, err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("%w: got %d bytes, want 32", ErrTOTPKeyMalformed, len(b))
	}
	return b, nil
}

// encryptTOTPSecret wraps the base32 secret with AES-256-GCM. The
// 12-byte random nonce is prepended to the ciphertext so the BYTEA
// column is self-contained — decryptTOTPSecret slices it back off.
func encryptTOTPSecret(key []byte, plaintextBase32 string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintextBase32), nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func decryptTOTPSecret(key, blob []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(blob) < gcm.NonceSize() {
		return "", ErrTOTPSecretCorrupt
	}
	nonce := blob[:gcm.NonceSize()]
	ct := blob[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTOTPSecretCorrupt, err)
	}
	return string(pt), nil
}

// TOTPSetup is what EnrolStart returns: the data the client renders
// as a QR code (otpauth://...) and the manual base32 secret a user
// can type in if their authenticator app can't scan.
type TOTPSetup struct {
	OTPAuthURL   string
	SecretBase32 string
}

// EnrolStart generates a fresh TOTP secret for the given user,
// stores it encrypted with totp_enabled=false, and returns the
// provisioning URL + manual secret. Idempotent: a second call
// replaces any prior pending secret. If the user already has
// totp_enabled=true, returns ErrTOTPAlreadyEnrolled — they must
// disable first to re-enrol.
func EnrolStart(ctx context.Context, db *sql.DB, key []byte, userID int64, accountLabel string) (TOTPSetup, error) {
	var enabled bool
	if err := db.QueryRowContext(ctx,
		`SELECT totp_enabled FROM users WHERE id = $1`, userID).Scan(&enabled); err != nil {
		return TOTPSetup{}, err
	}
	if enabled {
		return TOTPSetup{}, ErrTOTPAlreadyEnrolled
	}
	k, err := totp.Generate(totp.GenerateOpts{
		Issuer:      TOTPIssuer,
		AccountName: accountLabel,
	})
	if err != nil {
		return TOTPSetup{}, err
	}
	enc, err := encryptTOTPSecret(key, k.Secret())
	if err != nil {
		return TOTPSetup{}, err
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE users SET totp_secret_encrypted = $1, totp_enabled = FALSE, totp_enrolled_at = NULL WHERE id = $2`,
		enc, userID); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{OTPAuthURL: k.URL(), SecretBase32: k.Secret()}, nil
}

// EnrolConfirm verifies the first TOTP code against the pending
// secret stored by EnrolStart, flips totp_enabled=true, and mints a
// set of single-use recovery codes. The plaintext codes are returned
// once for the caller to display — only their bcrypt hashes are
// retained.
func EnrolConfirm(ctx context.Context, db *sql.DB, key []byte, userID int64, code string) ([]string, error) {
	secret, enabled, err := loadTOTPSecret(ctx, db, key, userID)
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, ErrTOTPAlreadyEnrolled
	}
	if !totp.Validate(strings.TrimSpace(code), secret) {
		return nil, ErrTOTPInvalid
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_enabled = TRUE, totp_enrolled_at = now_text() WHERE id = $1`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_recovery_codes(user_id, code_hash) VALUES ($1, $2)`,
			userID, h); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP clears the secret + enabled flag + recovery codes for
// the given user. Caller is responsible for any reauth gate (the
// /me/totp/disable handler uses requirePasswordConfirm).
func DisableTOTP(ctx context.Context, db *sql.DB, userID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET totp_secret_encrypted = NULL, totp_enabled = FALSE, totp_enrolled_at = NULL WHERE id = $1
	`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// RegenerateRecoveryCodes drops every existing recovery code for
// the user and mints a fresh set. Returns the plaintext codes for
// one-time display. Refuses if the user isn't enrolled — there's no
// useful "regenerate while disabled" path.
func RegenerateRecoveryCodes(ctx context.Context, db *sql.DB, userID int64) ([]string, error) {
	var enabled bool
	if err := db.QueryRowContext(ctx,
		`SELECT totp_enabled FROM users WHERE id = $1`, userID).Scan(&enabled); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrTOTPNotEnrolled
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_recovery_codes(user_id, code_hash) VALUES ($1, $2)`,
			userID, h); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// IssueTOTPChallenge stores a fresh challenge token bridging a
// successful first-factor step (password or password-reset) to the
// follow-up TOTP step. origin is "login" or "reset"; ConsumeChallenge
// returns it so the verify handler can branch on what to do after
// a successful TOTP code.
func IssueTOTPChallenge(ctx context.Context, db *sql.DB, userID int64, origin, ip, ua string) (string, error) {
	tok, err := tokenx.New(24)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(TOTPChallengeTTL).UTC().Format("2006-01-02 15:04:05")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_totp_challenges(token, user_id, origin, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), NULLIF($6,''))
	`, tok, userID, origin, expires, ip, ua); err != nil {
		return "", err
	}
	return tok, nil
}

// TOTPChallengeResult is what ConsumeTOTPChallenge returns: the user
// whose challenge was redeemed, the challenge's origin so the
// caller can branch on login vs reset, and the factor used so audit
// can distinguish a TOTP code from a recovery code.
type TOTPChallengeResult struct {
	User   User
	Origin string
	Factor Factor
}

// ConsumeTOTPChallenge looks up the challenge token, verifies it
// hasn't expired, validates either the supplied TOTP code or the
// recovery code, deletes the challenge row on success (single-use),
// and returns the redeemed user.
//
// Pass exactly one of code / recoveryCode (the other empty). The
// caller is responsible for rate-limiting by challenge token — a
// brute-force pass would otherwise enumerate the 10^6 TOTP space
// trivially.
func ConsumeTOTPChallenge(ctx context.Context, db *sql.DB, key []byte, token, code, recoveryCode string) (TOTPChallengeResult, error) {
	if token == "" {
		return TOTPChallengeResult{}, ErrChallengeUnknown
	}
	var (
		userID    int64
		origin    string
		expiresAt string
	)
	err := db.QueryRowContext(ctx, `
		SELECT user_id, origin, expires_at FROM auth_totp_challenges WHERE token = $1
	`, token).Scan(&userID, &origin, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPChallengeResult{}, ErrChallengeUnknown
	}
	if err != nil {
		return TOTPChallengeResult{}, err
	}
	if exp, perr := parseSlotTimestamp(expiresAt); perr != nil || time.Now().After(exp) {
		_, _ = db.ExecContext(ctx, `DELETE FROM auth_totp_challenges WHERE token = $1`, token)
		return TOTPChallengeResult{}, ErrChallengeExpired
	}
	var factor Factor
	switch {
	case strings.TrimSpace(code) != "":
		secret, enabled, err := loadTOTPSecret(ctx, db, key, userID)
		if err != nil {
			return TOTPChallengeResult{}, err
		}
		if !enabled {
			return TOTPChallengeResult{}, ErrTOTPNotEnrolled
		}
		if !totp.Validate(strings.TrimSpace(code), secret) {
			return TOTPChallengeResult{}, ErrTOTPInvalid
		}
		factor = FactorTOTP
	case strings.TrimSpace(recoveryCode) != "":
		if err := consumeRecoveryCode(ctx, db, userID, strings.TrimSpace(recoveryCode)); err != nil {
			return TOTPChallengeResult{}, err
		}
		factor = FactorRecoveryCode
	default:
		return TOTPChallengeResult{}, ErrTOTPInvalid
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM auth_totp_challenges WHERE token = $1`, token); err != nil {
		return TOTPChallengeResult{}, err
	}
	u, err := loadUserByID(ctx, db, userID)
	if err != nil {
		return TOTPChallengeResult{}, err
	}
	return TOTPChallengeResult{User: u, Origin: origin, Factor: factor}, nil
}

// loadTOTPSecret fetches and decrypts the user's TOTP secret. Used
// by both the enrol-confirm path (where enabled=false is the expected
// state) and the login-verify path (where enabled=true is required —
// the caller checks this).
func loadTOTPSecret(ctx context.Context, db *sql.DB, key []byte, userID int64) (string, bool, error) {
	var (
		blob    []byte
		enabled bool
	)
	if err := db.QueryRowContext(ctx,
		`SELECT totp_secret_encrypted, totp_enabled FROM users WHERE id = $1`, userID,
	).Scan(&blob, &enabled); err != nil {
		return "", false, err
	}
	if len(blob) == 0 {
		return "", enabled, ErrTOTPNotEnrolled
	}
	secret, err := decryptTOTPSecret(key, blob)
	if err != nil {
		return "", enabled, err
	}
	return secret, enabled, nil
}

// consumeRecoveryCode finds an unused row whose bcrypt hash matches
// the supplied plaintext code, marks it used_at=now, and returns
// nil. Returns ErrRecoveryCodeInvalid for any miss.
//
// We have to bcrypt-compare every unused row in the worst case —
// there are ten of them, so this is acceptable. The alternative
// would be a fast hash, which weakens the DB-leak posture for no
// real performance gain at this scale.
func consumeRecoveryCode(ctx context.Context, db *sql.DB, userID int64, plaintext string) error {
	plaintext = canonicaliseRecoveryCode(plaintext)
	rows, err := db.QueryContext(ctx,
		`SELECT id, code_hash FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   int64
			hash string
		)
		if err := rows.Scan(&id, &hash); err != nil {
			return err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil {
			rows.Close()
			if _, err := db.ExecContext(ctx,
				`UPDATE user_recovery_codes SET used_at = now_text() WHERE id = $1 AND used_at IS NULL`, id); err != nil {
				return err
			}
			return nil
		}
	}
	return ErrRecoveryCodeInvalid
}

// generateRecoveryCodes mints totpRecoveryCodeCount codes formatted
// as XXXX-XXXX (lowercase, 8 alphanumeric chars + a dash) and their
// bcrypt hashes. Returns plaintext + hashes in matching order.
func generateRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, totpRecoveryCodeCount)
	hashes := make([]string, totpRecoveryCodeCount)
	for i := range codes {
		raw, err := randomRecoveryCodeChars(8)
		if err != nil {
			return nil, nil, err
		}
		codes[i] = raw[:4] + "-" + raw[4:]
		// Hash the canonical form so the input case at redemption
		// time doesn't matter — operators reading codes off paper
		// often fumble a character's case.
		h, err := bcrypt.GenerateFromPassword([]byte(canonicaliseRecoveryCode(codes[i])), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		hashes[i] = string(h)
	}
	return codes, hashes, nil
}

func randomRecoveryCodeChars(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	alpha := totpRecoveryCodeAlphabet
	for i, x := range b {
		out[i] = alpha[int(x)%len(alpha)]
	}
	return string(out), nil
}

// canonicaliseRecoveryCode normalises user input before bcrypt
// comparison: lowercase + strip dashes/spaces. The displayed form
// includes the dash for readability; an authenticator-app-fumbled
// "ABCD EFGH" still matches the stored hash for "abcdefgh".
func canonicaliseRecoveryCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// loadUserByID is the post-TOTP user reload — same shape as Lookup
// but keyed on user_id since at this point we don't have a session
// row yet (CompleteLogin is the next step). Mirrors the role/orgID
// resolution VerifyPassword does.
func loadUserByID(ctx context.Context, db *sql.DB, userID int64) (User, error) {
	var u User
	if err := db.QueryRowContext(ctx, `
		SELECT id, username, is_admin, COALESCE(email,''), (email_verified <> 0), totp_enabled
		FROM users WHERE id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Email, &u.EmailVerified, &u.TOTPEnabled); err != nil {
		return User{}, err
	}
	active, role, err := resolveInitialActiveOrg(ctx, db, u.ID)
	if err != nil {
		return User{}, err
	}
	if active.Valid {
		v := active.Int64
		u.OrgID = &v
	}
	if u.IsAdmin {
		u.Role = RoleAdmin
	} else {
		u.Role = role
	}
	return u, nil
}

// TOTPStatus describes whether a user has TOTP enabled and, if so,
// when they enrolled. Surfaced via the /me/totp endpoint so the
// settings UI can render the right state without a separate query.
type TOTPStatus struct {
	Enabled    bool
	EnrolledAt string
}

// LoadTOTPStatus returns the current TOTP state for a user. Used by
// the /me/totp GET handler.
func LoadTOTPStatus(ctx context.Context, db *sql.DB, userID int64) (TOTPStatus, error) {
	var (
		enabled bool
		at      sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT totp_enabled, totp_enrolled_at FROM users WHERE id = $1`, userID,
	).Scan(&enabled, &at); err != nil {
		return TOTPStatus{}, err
	}
	return TOTPStatus{Enabled: enabled, EnrolledAt: at.String}, nil
}

// validateBase32Secret is here to keep imports tidy when callers want
// to sanity-check a manually-typed secret before treating it as
// authoritative. Keeps the single base32 dependency in one file.
func validateBase32Secret(s string) error {
	_, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(s)))
	return err
}

var _ = validateBase32Secret // reserved for future "import a secret" admin hook
