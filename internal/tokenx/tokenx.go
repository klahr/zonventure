// Package tokenx generates random, URL-safe tokens. Pulled out
// because six packages were each rolling their own
//
//	var b [N]byte
//	rand.Read(b[:])
//	base64.RawURLEncoding.EncodeToString(b[:])
//
// at slightly different sizes — slot tokens at 18 bytes (144 bits),
// CSRF + verification at 24 bytes (192 bits), session tokens at
// 32 bytes (256 bits), live-share tokens at 16 bytes (128 bits).
//
// Centralising lets us pick a single encoding and audit entropy
// choices in one place. RawURLEncoding is the right call: no padding
// (so tokens slot into URLs without escaping), URL-safe alphabet
// (so they slot into Path/Query without escaping either).
package tokenx

import (
	"crypto/rand"
	"encoding/base64"
)

// New returns a new random token of n bytes of entropy, base64-URL
// encoded without padding. n is the byte count, NOT the encoded
// length: `New(18)` yields 24 ASCII characters; `New(32)` yields 43.
//
// Returns an error only if crypto/rand fails — vanishingly rare in
// practice but propagated rather than panicked so the caller can
// surface a clean 500 instead of crashing the process.
func New(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
