package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// TestHashSecret_RoundTripAndSalted covers the fresh-hash contract in
// one go (each Argon2id hash is deliberately expensive): PHC format,
// per-call random salt, the right password verifies without a rehash,
// and a wrong password is a plain mismatch rather than an error.
func TestHashSecret_RoundTripAndSalted(t *testing.T) {
	t.Parallel()
	const pw = "correct horse battery staple"
	a, err := hashSecret(pw)
	if err != nil {
		t.Fatalf("hashSecret: %v", err)
	}
	if !strings.HasPrefix(a, "$argon2id$v=19$") {
		t.Fatalf("hash does not look like argon2id PHC: %q", a)
	}
	// Two hashes of the same plaintext must differ — confirms the
	// salt is actually being randomised per call rather than baked
	// into the format.
	b, err := hashSecret(pw)
	if err != nil {
		t.Fatalf("hashSecret: %v", err)
	}
	if a == b {
		t.Fatalf("two hashes of the same plaintext are identical — salt is not random")
	}

	ok, needsRehash, err := verifySecret(a, pw)
	if err != nil || !ok {
		t.Fatalf("verifySecret on fresh hash: ok=%v err=%v", ok, err)
	}
	if needsRehash {
		t.Errorf("fresh hash with current params should not need a rehash")
	}

	ok, needsRehash, err = verifySecret(a, "wrong")
	if err != nil {
		t.Fatalf("verifySecret returned error on plain mismatch: %v", err)
	}
	if ok {
		t.Errorf("verifySecret accepted the wrong password")
	}
	if needsRehash {
		t.Errorf("wrong password signalled rehash — no UPDATE on a bad credential, ever")
	}
}

func TestVerifySecret_LegacyBcrypt(t *testing.T) {
	t.Parallel()
	// Existing rows in the wild use bcrypt — they must still verify,
	// and the verifier must signal a rehash so the next login
	// migrates them to Argon2id.
	legacy, err := bcrypt.GenerateFromPassword([]byte("legacy-pw"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	ok, needsRehash, err := verifySecret(string(legacy), "legacy-pw")
	if err != nil {
		t.Fatalf("verifySecret on bcrypt: %v", err)
	}
	if !ok {
		t.Fatalf("verifySecret rejected a valid bcrypt hash")
	}
	if !needsRehash {
		t.Errorf("verifySecret did not signal rehash for legacy bcrypt — every successful bcrypt verify should upgrade")
	}

	// Wrong-password path must not signal rehash (no UPDATE on a
	// bad credential, ever) and must not surface the bcrypt
	// mismatch error to the caller.
	ok, needsRehash, err = verifySecret(string(legacy), "nope")
	if err != nil {
		t.Errorf("bcrypt mismatch leaked through verifySecret: %v", err)
	}
	if ok || needsRehash {
		t.Errorf("bcrypt mismatch returned ok=%v needsRehash=%v, want false/false", ok, needsRehash)
	}
}

func TestVerifySecret_BadFormat(t *testing.T) {
	cases := []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=1,t=1,p=1$YWFhYQ$YWFhYQ", // wrong variant
		"$argon2id$v=19$m=BAD,t=2,p=1$YWFhYQ$YWFhYQ",
		"$argon2id$v=99$m=1024,t=1,p=1$YWFhYQ$YWFhYQ",
	}
	for _, c := range cases {
		ok, _, err := verifySecret(c, "anything")
		if ok {
			t.Errorf("verifySecret accepted malformed hash %q", c)
		}
		if err == nil {
			t.Errorf("verifySecret returned no error for malformed hash %q", c)
		}
	}
}

func TestVerifySecret_RehashOnWeakerParams(t *testing.T) {
	// Hand-roll a hash with parameters below the current target —
	// simulates an existing row written before we bumped m/t/p.
	// Should verify cleanly and signal rehash so the lazy-upgrade
	// path replaces it.
	weak := mustHashWithParams(t, "tune-me", argonMemoryKiB/2, argonTime, argonThreads)
	ok, needsRehash, err := verifySecret(weak, "tune-me")
	if err != nil || !ok {
		t.Fatalf("verifySecret on weak-params hash: ok=%v err=%v", ok, err)
	}
	if !needsRehash {
		t.Errorf("weaker stored params did not trigger needsRehash")
	}
}

// mustHashWithParams builds an Argon2id PHC string with explicit
// params, sidestepping hashSecret so the test can simulate a row
// written under earlier (weaker) defaults without touching package
// state.
func mustHashWithParams(t *testing.T, plain string, m, time uint32, threads uint8) string {
	t.Helper()
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("rand: %v", err)
	}
	key := argon2.IDKey([]byte(plain), salt, time, m, threads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, m, time, threads,
		enc.EncodeToString(salt), enc.EncodeToString(key))
}
