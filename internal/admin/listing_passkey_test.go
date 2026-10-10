package admin_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// virtualPasskey is an authenticator for tests: a P-256 key that
// registers and signs as a browser's passkey would, verifying the user.
type virtualPasskey struct {
	key    *ecdsa.PrivateKey
	credID []byte
	rpID   string
	origin string
	count  uint32
}

var rawURL = base64.RawURLEncoding

func newVirtualPasskey(t *testing.T, rpID, origin string) *virtualPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &virtualPasskey{key: key, credID: id, rpID: rpID, origin: origin}
}

// authData is the authenticator data: the RP's hash, flags, the count,
// and when registering the credential and its key.
func (p *virtualPasskey) authData(t *testing.T, flags byte, withCredential bool) []byte {
	t.Helper()
	rp := sha256.Sum256([]byte(p.rpID))
	p.count++
	out := append([]byte{}, rp[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, p.count)
	if withCredential {
		out = append(out, make([]byte, 16)...) // AAGUID
		out = binary.BigEndian.AppendUint16(out, uint16(len(p.credID)))
		out = append(out, p.credID...)
		pub, err := p.key.PublicKey.ECDH()
		if err != nil {
			t.Fatal(err)
		}
		raw := pub.Bytes() // 0x04 || X || Y
		cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: raw[1:33], -3: raw[33:65]})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cose...)
	}
	return out
}

func (p *virtualPasskey) clientData(typ, challenge string) []byte {
	raw, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": p.origin, "crossOrigin": false})
	return raw
}

// optionsChallenge reads the challenge from a begin call's options.
func optionsChallenge(t *testing.T, w *httptest.ResponseRecorder) (session, challenge string) {
	t.Helper()
	var resp struct {
		SessionID string `json:"session_id"`
		Options   struct {
			PublicKey struct {
				Challenge string `json:"challenge"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.SessionID == "" || resp.Options.PublicKey.Challenge == "" {
		t.Fatalf("begin: %v %d %s", err, w.Code, w.Body)
	}
	return resp.SessionID, resp.Options.PublicKey.Challenge
}

// register answers navigator.credentials.create.
func (p *virtualPasskey) register(t *testing.T, challenge string) json.RawMessage {
	t.Helper()
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": p.authData(t, 0x45, true)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"id": rawURL.EncodeToString(p.credID), "rawId": rawURL.EncodeToString(p.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    rawURL.EncodeToString(p.clientData("webauthn.create", challenge)),
			"attestationObject": rawURL.EncodeToString(att),
		},
		"clientExtensionResults": map[string]any{},
	})
	return raw
}

// sign answers navigator.credentials.get, verifying the user.
func (p *virtualPasskey) sign(t *testing.T, challenge string) json.RawMessage {
	t.Helper()
	auth := p.authData(t, 0x05, false)
	cd := p.clientData("webauthn.get", challenge)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, auth...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"id": rawURL.EncodeToString(p.credID), "rawId": rawURL.EncodeToString(p.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    rawURL.EncodeToString(cd),
			"authenticatorData": rawURL.EncodeToString(auth),
			"signature":         rawURL.EncodeToString(sig),
		},
		"clientExtensionResults": map[string]any{},
	})
	return raw
}

// addPasskey registers a virtual passkey for the fixture's reviewer.
func (f *listingFixture) addPasskey(t *testing.T) {
	t.Helper()
	f.passkey = newVirtualPasskey(t, "zv.example.com", "https://zv.example.com")
	sid, challenge := optionsChallenge(t, call(t, f.reviewer, "POST", "/api/admin/me/reviewer-passkeys/begin", nil))
	w := call(t, f.reviewer, "POST", "/api/admin/me/reviewer-passkeys/finish", map[string]any{
		"session_id": sid, "name": "Test key", "credential": f.passkey.register(t, challenge),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("register passkey: %d %s", w.Code, w.Body)
	}
}

// signed asks to sign a decision and signs it with the reviewer's
// passkey, returning the signature approve and reject take.
func (f *listingFixture) signed(t *testing.T, kind string, id int64, action, revision, reason string) map[string]any {
	t.Helper()
	w := call(t, f.reviewer, "POST", fmt.Sprintf("/api/admin/listing-reviews/%s/%d/sign", kind, id),
		map[string]string{"action": action, "revision": revision, "reason": reason})
	sid, challenge := optionsChallenge(t, w)
	return map[string]any{"session_id": sid, "credential": f.passkey.sign(t, challenge)}
}
