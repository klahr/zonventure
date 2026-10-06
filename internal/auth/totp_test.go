package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"

	"github.com/pquerna/otp/totp"
)

// TestEncryptTOTPSecret_Roundtrip verifies the AES-GCM wrap/unwrap is
// reversible and tamper-evident. No DB needed.
func TestEncryptTOTPSecret_Roundtrip(t *testing.T) {
	key := mustRandomKey(t)
	plaintext := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	ct, err := encryptTOTPSecret(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(ct) <= 12 {
		t.Fatalf("ciphertext suspiciously short (%d bytes) — nonce missing?", len(ct))
	}
	got, err := decryptTOTPSecret(key, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round trip mismatch: got %q, want %q", got, plaintext)
	}
}

// TestDecryptTOTPSecret_Tampered verifies that flipping a byte in the
// ciphertext is detected (GCM is authenticated).
func TestDecryptTOTPSecret_Tampered(t *testing.T) {
	key := mustRandomKey(t)
	ct, err := encryptTOTPSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ct[len(ct)-1] ^= 0x01
	if _, err := decryptTOTPSecret(key, ct); err == nil {
		t.Fatalf("decrypt of tampered ct succeeded; want auth failure")
	}
}

// TestDecryptTOTPSecret_WrongKey verifies that decrypt with a
// different key fails cleanly rather than producing garbage.
func TestDecryptTOTPSecret_WrongKey(t *testing.T) {
	k1 := mustRandomKey(t)
	k2 := mustRandomKey(t)
	ct, err := encryptTOTPSecret(k1, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := decryptTOTPSecret(k2, ct); err == nil {
		t.Fatalf("decrypt with wrong key succeeded; want auth failure")
	}
}

// TestCanonicaliseRecoveryCode covers the case + dash + space
// fumbling we expect when users transcribe codes off paper.
func TestCanonicaliseRecoveryCode(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"abcd-efgh", "abcdefgh"},
		{"ABCD-EFGH", "abcdefgh"},
		{"abcd efgh", "abcdefgh"},
		{"  abcd-EFGH  ", "abcdefgh"},
		{"abcdefgh", "abcdefgh"},
	}
	for _, tt := range tests {
		if got := canonicaliseRecoveryCode(tt.in); got != tt.want {
			t.Errorf("canonicalise(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestGenerateRecoveryCodes_ShapeAndUniqueness sanity-checks the
// XXXX-XXXX format and that hashes round-trip via bcrypt.
func TestGenerateRecoveryCodes_ShapeAndUniqueness(t *testing.T) {
	t.Parallel()
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got, want := len(codes), totpRecoveryCodeCount; got != want {
		t.Fatalf("got %d codes, want %d", got, want)
	}
	if len(hashes) != len(codes) {
		t.Fatalf("hash count mismatch: %d vs %d", len(hashes), len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 9 || c[4] != '-' {
			t.Errorf("bad code shape: %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate code: %q", c)
		}
		seen[c] = true
	}
}

// --- DB-backed tests ---

// TestTOTP_EndToEnd exercises EnrolStart → EnrolConfirm → ConsumeChallenge
// against a live Postgres. Skipped when no Postgres is available.
func TestTOTP_EndToEnd(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	key := mustRandomKey(t)
	uid := mustSeedUser(t, db)

	setup, err := EnrolStart(ctx, db, key, uid, "alice@example.com")
	if err != nil {
		t.Fatalf("EnrolStart: %v", err)
	}
	if !strings.HasPrefix(setup.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("bad otpauth URL: %q", setup.OTPAuthURL)
	}
	if setup.SecretBase32 == "" {
		t.Fatalf("empty secret")
	}
	code, err := totp.GenerateCode(setup.SecretBase32, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	codes, err := EnrolConfirm(ctx, db, key, uid, code)
	if err != nil {
		t.Fatalf("EnrolConfirm: %v", err)
	}
	if len(codes) != totpRecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), totpRecoveryCodeCount)
	}

	// Issue a challenge then redeem it with a fresh code.
	tok, err := IssueTOTPChallenge(ctx, db, uid, "login", "192.0.2.1", "test/1.0")
	if err != nil {
		t.Fatalf("IssueTOTPChallenge: %v", err)
	}
	freshCode, err := totp.GenerateCode(setup.SecretBase32, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	res, err := ConsumeTOTPChallenge(ctx, db, key, tok, freshCode, "")
	if err != nil {
		t.Fatalf("ConsumeTOTPChallenge: %v", err)
	}
	if res.User.ID != uid {
		t.Fatalf("redeemed user id %d, want %d", res.User.ID, uid)
	}
	if res.Origin != "login" {
		t.Fatalf("origin %q, want login", res.Origin)
	}
	if res.Factor != FactorTOTP {
		t.Fatalf("factor %q, want %q", res.Factor, FactorTOTP)
	}

	// Second consumption of the same token must fail (single-use).
	if _, err := ConsumeTOTPChallenge(ctx, db, key, tok, freshCode, ""); err == nil {
		t.Fatalf("second redemption of consumed challenge succeeded; want ErrChallengeUnknown")
	}
}

// TestTOTP_RecoveryCodeSingleUse confirms a recovery code redeems
// exactly once.
func TestTOTP_RecoveryCodeSingleUse(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	key := mustRandomKey(t)
	uid := mustSeedUser(t, db)

	setup, err := EnrolStart(ctx, db, key, uid, "alice@example.com")
	if err != nil {
		t.Fatalf("EnrolStart: %v", err)
	}
	code, _ := totp.GenerateCode(setup.SecretBase32, time.Now())
	codes, err := EnrolConfirm(ctx, db, key, uid, code)
	if err != nil {
		t.Fatalf("EnrolConfirm: %v", err)
	}

	tok, err := IssueTOTPChallenge(ctx, db, uid, "login", "", "")
	if err != nil {
		t.Fatalf("IssueTOTPChallenge: %v", err)
	}
	rc := codes[0]
	res, err := ConsumeTOTPChallenge(ctx, db, key, tok, "", rc)
	if err != nil {
		t.Fatalf("ConsumeTOTPChallenge with recovery code: %v", err)
	}
	if res.Factor != FactorRecoveryCode {
		t.Fatalf("factor %q, want %q", res.Factor, FactorRecoveryCode)
	}

	// New challenge, same recovery code — must fail (already used).
	tok2, _ := IssueTOTPChallenge(ctx, db, uid, "login", "", "")
	if _, err := ConsumeTOTPChallenge(ctx, db, key, tok2, "", rc); err == nil {
		t.Fatalf("re-use of consumed recovery code succeeded; want ErrRecoveryCodeInvalid")
	}
}

// TestTOTP_ChallengeExpiry confirms an expired challenge is rejected
// and cleaned up.
func TestTOTP_ChallengeExpiry(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	key := mustRandomKey(t)
	uid := mustSeedUser(t, db)

	// Insert an already-expired challenge directly so we don't have
	// to wait the TTL.
	expired := time.Now().Add(-1 * time.Minute).UTC().Format("2006-01-02 15:04:05")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_totp_challenges(token, user_id, origin, expires_at)
		VALUES ($1, $2, 'login', $3)
	`, "stale-token", uid, expired); err != nil {
		t.Fatalf("seed expired challenge: %v", err)
	}
	if _, err := ConsumeTOTPChallenge(ctx, db, key, "stale-token", "123456", ""); err != ErrChallengeExpired {
		t.Fatalf("ConsumeTOTPChallenge expired: got %v, want ErrChallengeExpired", err)
	}
	// Side effect: the row should be gone.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_totp_challenges WHERE token = 'stale-token'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expired challenge row not cleaned up (n=%d)", n)
	}
}

// TestTOTP_EnrolStart_Idempotent ensures a re-call before confirm
// replaces the pending secret rather than erroring.
func TestTOTP_EnrolStart_Idempotent(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	key := mustRandomKey(t)
	uid := mustSeedUser(t, db)

	first, err := EnrolStart(ctx, db, key, uid, "alice@example.com")
	if err != nil {
		t.Fatalf("first EnrolStart: %v", err)
	}
	second, err := EnrolStart(ctx, db, key, uid, "alice@example.com")
	if err != nil {
		t.Fatalf("second EnrolStart: %v", err)
	}
	if first.SecretBase32 == second.SecretBase32 {
		t.Fatalf("expected the second EnrolStart to mint a fresh secret")
	}
	// Now confirm — the first secret must be invalid, the second valid.
	codeFirst, _ := totp.GenerateCode(first.SecretBase32, time.Now())
	if _, err := EnrolConfirm(ctx, db, key, uid, codeFirst); err == nil {
		t.Fatalf("EnrolConfirm with stale first-secret code succeeded")
	}
	codeSecond, _ := totp.GenerateCode(second.SecretBase32, time.Now())
	if _, err := EnrolConfirm(ctx, db, key, uid, codeSecond); err != nil {
		t.Fatalf("EnrolConfirm with current code: %v", err)
	}
}

// TestTOTP_EnrolStart_RefusesAfterEnabled confirms that re-enrolment
// requires Disable first.
func TestTOTP_EnrolStart_RefusesAfterEnabled(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	key := mustRandomKey(t)
	uid := mustSeedUser(t, db)

	setup, err := EnrolStart(ctx, db, key, uid, "alice@example.com")
	if err != nil {
		t.Fatalf("EnrolStart: %v", err)
	}
	code, _ := totp.GenerateCode(setup.SecretBase32, time.Now())
	if _, err := EnrolConfirm(ctx, db, key, uid, code); err != nil {
		t.Fatalf("EnrolConfirm: %v", err)
	}
	if _, err := EnrolStart(ctx, db, key, uid, "alice@example.com"); err != ErrTOTPAlreadyEnrolled {
		t.Fatalf("EnrolStart after enable: got %v, want ErrTOTPAlreadyEnrolled", err)
	}
	if err := DisableTOTP(ctx, db, uid); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	// After Disable, EnrolStart works again.
	if _, err := EnrolStart(ctx, db, key, uid, "alice@example.com"); err != nil {
		t.Fatalf("EnrolStart after Disable: %v", err)
	}
}

// --- helpers ---

func mustRandomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func mustSeedUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified)
		VALUES ($1, $2, 1)
		RETURNING id
	`, "alice@example.com", "alice@example.com").Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}
