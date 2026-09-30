package auth

// Password hashing — Argon2id with lazy migration from legacy bcrypt.
//
// Stored hashes use the standard Argon2 PHC string format
//
//   $argon2id$v=19$m=19456,t=2,p=1$<b64-salt>$<b64-hash>
//
// which is self-describing: algorithm, version and parameters all
// live in the column, so the verifier can authenticate any historical
// hash and we don't need a separate password_hash_version column.
//
// Verify dispatches on prefix:
//
//   $argon2id$…  → argon2.IDKey, constant-time compare. Triggers a
//                  rehash only when the embedded params are weaker
//                  than the current targets below.
//   $2a/2b/2y$…  → legacy bcrypt path. Successful verification always
//                  signals a rehash so the user transitions to
//                  Argon2id on next login.
//
// Lazy upgrade is the *only* migration path — we don't have plaintexts
// to do a batch rehash. Users who never log in keep their bcrypt hash
// indefinitely; that is acceptable because bcrypt with cost 10 is
// still well above brute-force-feasible for an offline attacker on
// any single account, and untouched accounts are also the lowest-
// value targets in a leak.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id parameters. OWASP "first option" defaults as of the 2024
// Password Storage cheat sheet — m=19 MiB, t=2, p=1 — which keep a
// per-attempt cost in the ~50 ms range on commodity hardware while
// forcing each guess to occupy ~19 MiB of RAM (the property bcrypt
// lacks). saltLen and keyLen follow the same recommendation.
//
// Bumping any of these is safe: the lazy-upgrade in verifySecret
// rehashes any user whose stored params are weaker than the current
// target on their next successful login.
const (
	argonMemoryKiB uint32 = 19 * 1024
	argonTime      uint32 = 2
	argonThreads   uint8  = 1
	argonSaltLen          = 16
	argonKeyLen    uint32 = 32
)

// errUnknownHashFormat is returned for stored values that match
// neither the bcrypt nor the Argon2id prefix. Should never happen on
// a healthy DB; treated as an internal error so the caller logs and
// rejects the login (rather than silently returning "bad password").
var errUnknownHashFormat = errors.New("unknown password-hash format")

// decoyHash is a one-shot Argon2id PHC string we run verifySecret
// against on the user-not-found branch of VerifyPassword so the
// response time of "unknown identifier" matches "known identifier,
// wrong password". Without it, a network observer can mine valid
// usernames / emails by timing the difference between the no-DB-row
// path (~1 ms) and the Argon2id path (~50 ms), despite the per-
// (ip, user) rate limiter. The plaintext used to mint the decoy is
// fixed and meaningless — the verify dispatch only cares that the
// PHC parses with the current params so the cost matches a real
// login.
var (
	decoyHashMu  sync.Mutex
	decoyHashStr string
)

// primeDecoyHash builds the dummy Argon2id PHC string lazily on the
// first call. PrimeDecoyHash exposes the same trigger from outside
// the package so cmd/server/main.go can amortise the cost at server
// boot — without that, the very first user-not-found request after
// process start pays Argon2 mint (~50ms) PLUS the verify (~50ms),
// distinguishable from subsequent ~50ms calls and re-opening the
// timing oracle this whole mechanism exists to close.
//
// Retries on each call when the prior attempt failed (e.g. crypto/rand
// hiccup at boot): a one-shot sync.Once would freeze decoyHashStr at
// "" forever and silently turn SpendVerifyTime into a no-op,
// re-opening the timing oracle. The mutex is uncontended in the warm
// path because we early-out on `decoyHashStr != ""` before grabbing
// it.
func primeDecoyHash() {
	if decoyHashStr != "" {
		return
	}
	decoyHashMu.Lock()
	defer decoyHashMu.Unlock()
	if decoyHashStr != "" {
		return
	}
	if h, err := hashSecret("decoy-not-a-real-password"); err == nil {
		decoyHashStr = h
	}
}

// PrimeDecoyHash is the public entry point for the lazy init above.
// Idempotent — sync.Once guards re-entry; calling more than once is
// a no-op. Safe to invoke from main.go before the HTTP listener
// starts.
func PrimeDecoyHash() { primeDecoyHash() }

// SpendVerifyTime runs the full Argon2id verify against the decoy
// hash and discards the result. Callers (VerifyPassword on the
// user-not-found branch) use this to equalise login response time
// against the user-found, wrong-password branch.
func SpendVerifyTime(plain string) {
	primeDecoyHash()
	if decoyHashStr == "" {
		return
	}
	_, _, _ = verifySecret(decoyHashStr, plain)
}

// CheckSecret reports whether plain matches a hash from HashPassword.
// An empty stored hash never matches, after the same work as a real
// check, so the reply's timing doesn't tell whether one is set.
func CheckSecret(stored, plain string) bool {
	if stored == "" {
		SpendVerifyTime(plain)
		return false
	}
	ok, _, err := verifySecret(stored, plain)
	return ok && err == nil
}

// hashSecret produces a fresh Argon2id PHC string for plain. Used
// for both passwords (HashPassword) and TOTP recovery codes.
func hashSecret(plain string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// verifySecret authenticates plain against stored. ok reports whether
// the credential matched; needsRehash signals that the caller should
// rehash plain with hashSecret and persist the new value (legacy
// bcrypt → Argon2id, or Argon2id with weaker params than current
// targets). err is reserved for unrecoverable issues like a
// malformed PHC string — treat it as "reject this login and log".
func verifySecret(stored, plain string) (ok bool, needsRehash bool, err error) {
	switch {
	case strings.HasPrefix(stored, "$argon2id$"):
		return verifyArgon2id(stored, plain)
	case strings.HasPrefix(stored, "$2a$"),
		strings.HasPrefix(stored, "$2b$"),
		strings.HasPrefix(stored, "$2y$"):
		if cmpErr := bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)); cmpErr != nil {
			if errors.Is(cmpErr, bcrypt.ErrMismatchedHashAndPassword) {
				return false, false, nil
			}
			return false, false, cmpErr
		}
		// Always upgrade legacy bcrypt rows on next successful login.
		return true, true, nil
	default:
		return false, false, errUnknownHashFormat
	}
}

func verifyArgon2id(stored, plain string) (ok bool, needsRehash bool, err error) {
	// PHC layout: ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false, fmt.Errorf("argon2id: malformed PHC string")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, false, fmt.Errorf("argon2id: bad version field: %w", err)
	}
	if version != argon2.Version {
		// Argon2 has only ever shipped v=0x10 and v=0x13, and we only
		// emit v=0x13. Anything else is data we didn't write.
		return false, false, fmt.Errorf("argon2id: unsupported version %d", version)
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, false, fmt.Errorf("argon2id: bad params field: %w", err)
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, false, fmt.Errorf("argon2id: bad salt: %w", err)
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false, false, fmt.Errorf("argon2id: bad hash: %w", err)
	}
	got := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	// Rehash if any stored param is weaker than the current target.
	// We deliberately never rehash *down* — an operator who tunes
	// memory below the previous floor (eg. on a tiny self-host VM)
	// shouldn't have every login regenerate a weaker hash than the
	// one already on disk.
	needsRehash = m < argonMemoryKiB || t < argonTime || p < argonThreads
	return true, needsRehash, nil
}
