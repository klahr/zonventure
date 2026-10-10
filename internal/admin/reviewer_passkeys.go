package admin

// Passkey signing for human listing reviewers (migration 0125). A
// reviewer registers a passkey; each decision they make in the console
// is signed with it. The challenge the passkey signs is a hash of the
// decision itself (signedDecision), so the signature covers exactly what
// was decided, and the private key never leaves the reviewer's device:
// the server can't make a signed decision on its own. The signature goes
// into the hash-chained log with a copy of the public key, and checking
// the log checks it again (verifyDecisionSignature).
//
//	GET    /api/admin/me/reviewer-passkeys                    the caller's passkeys
//	POST   /api/admin/me/reviewer-passkeys/begin              start registering one
//	POST   /api/admin/me/reviewer-passkeys/finish             {session_id, name, credential}
//	DELETE /api/admin/me/reviewer-passkeys/{id}               revoke one
//	POST   /api/admin/listing-reviews/{kind}/{id}/sign        {action, revision, reason} → what to sign
//
// approve and reject then take {revision, reason, signature: {session_id,
// credential}}. A decision made through an API token (an AI assistant
// over MCP) is not signed; one made in the console must be.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

// passkeySessionTTL is how long a ceremony may take.
const passkeySessionTTL = 5 * time.Minute

// signedDecision is what a reviewer's passkey signs: its hash is the
// WebAuthn challenge. Nonce makes every challenge new.
type signedDecision struct {
	Action   string `json:"action"`
	Kind     string `json:"kind"`
	ID       int64  `json:"id"`
	Revision string `json:"revision"`
	Reason   string `json:"reason"`
	Nonce    string `json:"nonce"`
}

func (d signedDecision) challenge() []byte {
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	return sum[:]
}

// decisionSignature is what a signed decision stores, base64 (URL-safe,
// unpadded) as WebAuthn carries it.
type decisionSignature struct {
	CredentialID      string `json:"credential_id"`
	PublicKey         string `json:"public_key"`
	Nonce             string `json:"nonce"`
	AuthenticatorData string `json:"authenticator_data"`
	ClientData        string `json:"client_data"`
	Signature         string `json:"signature"`
}

var b64 = base64.RawURLEncoding

// webAuthn is the relying party: this site, from its public URL. nil
// without one, when passkeys can't work.
func (h *Handlers) webAuthn() *webauthn.WebAuthn {
	u, err := url.Parse(strings.TrimSpace(h.PublicURL))
	if err != nil || u.Hostname() == "" {
		return nil
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "Zonventure listing review",
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
	if err != nil {
		return nil
	}
	return w
}

// passkeyUser is a reviewer as WebAuthn sees them.
type passkeyUser struct {
	id    int64
	name  string
	creds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(u.id))
	sum := sha256.Sum256(append([]byte("zv-listing-reviewer:"), b...))
	return sum[:]
}
func (u passkeyUser) WebAuthnName() string                       { return u.name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (h *Handlers) passkeyUser(ctx context.Context, me auth.User) (passkeyUser, error) {
	u := passkeyUser{id: me.ID, name: me.Username}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT credential FROM reviewer_passkeys WHERE user_id = $1 AND revoked_at IS NULL ORDER BY id`, me.ID)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return u, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal(raw, &c); err != nil {
			return u, err
		}
		u.creds = append(u.creds, c)
	}
	return u, rows.Err()
}

// signer is the caller when they may manage or use reviewer passkeys:
// a listing reviewer, in a signed-in browser (not through an API token,
// which an assistant holds).
func (h *Handlers) signer(w http.ResponseWriter, r *http.Request) (auth.User, *webauthn.WebAuthn, bool) {
	me, ok := reviewer(w, r)
	if !ok {
		return me, nil, false
	}
	if auth.ViaAPIToken(r.Context()) {
		httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "passkeys are used in the browser, not through an API token", "code": "browser_only"})
		return me, nil, false
	}
	wa := h.webAuthn()
	if wa == nil {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "passkeys need the server's public URL (ZV_PUBLIC_URL)", "code": "passkeys_unavailable"})
		return me, nil, false
	}
	return me, wa, true
}

func newSessionID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

func (h *Handlers) savePasskeySession(ctx context.Context, userID int64, purpose string, s *webauthn.SessionData, decision *signedDecision) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	var dec []byte
	if decision != nil {
		if dec, err = json.Marshal(decision); err != nil {
			return "", err
		}
	}
	_, err = h.DB.ExecContext(ctx, `
		INSERT INTO reviewer_passkey_sessions (id, user_id, purpose, session, decision, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval)`,
		id, userID, purpose, raw, dec, fmt.Sprintf("%d seconds", int(passkeySessionTTL.Seconds())))
	return id, err
}

// takePasskeySession reads a ceremony and deletes it: each is used once.
func (h *Handlers) takePasskeySession(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, userID int64, purpose string) (webauthn.SessionData, *signedDecision, error) {
	var (
		s        webauthn.SessionData
		raw, dec []byte
	)
	err := q.QueryRowContext(ctx, `
		DELETE FROM reviewer_passkey_sessions
		WHERE id = $1 AND user_id = $2 AND purpose = $3 AND expires_at > now()
		RETURNING session, decision`, id, userID, purpose).Scan(&raw, &dec)
	if err != nil {
		return s, nil, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, nil, err
	}
	if dec == nil {
		return s, nil, nil
	}
	var d signedDecision
	err = json.Unmarshal(dec, &d)
	return s, &d, err
}

/* ---------------- registering ---------------- */

type reviewerPasskeyDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// ListReviewerPasskeys lists the caller's passkeys.
func (h *Handlers) ListReviewerPasskeys(w http.ResponseWriter, r *http.Request) {
	me, _, ok := h.signer(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, name, to_char(created_at AT TIME ZONE 'UTC', `+isoTime+`),
		       COALESCE(to_char(last_used_at AT TIME ZONE 'UTC', `+isoTime+`), '')
		FROM reviewer_passkeys WHERE user_id = $1 AND revoked_at IS NULL ORDER BY id`, me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkeys lookup failed", err)
		return
	}
	defer rows.Close()
	out := []reviewerPasskeyDTO{}
	for rows.Next() {
		var d reviewerPasskeyDTO
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt, &d.LastUsedAt); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "passkeys lookup failed", err)
			return
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

// BeginReviewerPasskey starts registering a passkey: the options go to
// navigator.credentials.create.
func (h *Handlers) BeginReviewerPasskey(w http.ResponseWriter, r *http.Request) {
	me, wa, ok := h.signer(w, r)
	if !ok {
		return
	}
	u, err := h.passkeyUser(r.Context(), me)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkeys lookup failed", err)
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(u.creds))
	for _, c := range u.creds {
		exclude = append(exclude, c.Descriptor())
	}
	options, session, err := wa.BeginRegistration(u,
		webauthn.WithExclusions(exclude),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired,
		}))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkey registration failed", err)
		return
	}
	id, err := h.savePasskeySession(r.Context(), me.ID, "register", session, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkey registration failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"session_id": id, "options": options})
}

type finishPasskeyRequest struct {
	SessionID  string          `json:"session_id"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

// FinishReviewerPasskey stores the passkey the browser created.
func (h *Handlers) FinishReviewerPasskey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me, wa, ok := h.signer(w, r)
	if !ok {
		return
	}
	var req finishPasskeyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	session, _, err := h.takePasskeySession(ctx, h.DB, req.SessionID, me.ID, "register")
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "that registration expired: start again", "code": "session_expired"})
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the passkey's answer could not be read", "code": "bad_credential"})
		return
	}
	u, err := h.passkeyUser(ctx, me)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkeys lookup failed", err)
		return
	}
	cred, err := wa.CreateCredential(u, session, parsed)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the passkey was not accepted: " + err.Error(), "code": "bad_credential"})
		return
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkey save failed", err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	var id int64
	if err := h.DB.QueryRowContext(ctx, `
		INSERT INTO reviewer_passkeys (user_id, credential_id, credential, name) VALUES ($1, $2, $3, $4) RETURNING id`,
		me.ID, cred.ID, raw, name).Scan(&id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkey save failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
}

// RevokeReviewerPasskey stops a passkey from signing. Decisions it
// signed stay checkable: the log keeps its public key.
func (h *Handlers) RevokeReviewerPasskey(w http.ResponseWriter, r *http.Request) {
	me, _, ok := h.signer(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such passkey")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE reviewer_passkeys SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "revoke failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "no such passkey")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- signing a decision ---------------- */

type signRequest struct {
	Action   string `json:"action"`
	Revision string `json:"revision"`
	Reason   string `json:"reason"`
}

// SignListingDecision starts signing a decision: the options go to
// navigator.credentials.get, with the decision's hash as the challenge.
//
// POST /api/admin/listing-reviews/{kind}/{id}/sign
func (h *Handlers) SignListingDecision(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me, wa, ok := h.signer(w, r)
	if !ok {
		return
	}
	kind, id, ok := listingParams(w, r)
	if !ok {
		return
	}
	var req signRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		httpx.Error(w, http.StatusBadRequest, `action must be "approve" or "reject"`)
		return
	}
	u, err := h.passkeyUser(ctx, me)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "passkeys lookup failed", err)
		return
	}
	if len(u.creds) == 0 {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "add a passkey to sign your decisions", "code": "passkey_required"})
		return
	}
	nonce, err := newSessionID()
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "sign failed", err)
		return
	}
	d := signedDecision{Action: req.Action, Kind: kind, ID: id, Revision: req.Revision, Reason: strings.TrimSpace(req.Reason), Nonce: nonce}
	options, session, err := wa.BeginLogin(u, webauthn.WithChallenge(d.challenge()),
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "sign failed", err)
		return
	}
	sid, err := h.savePasskeySession(ctx, me.ID, "sign", session, &d)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "sign failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"session_id": sid, "options": options})
}

// signatureInput is a decision's signature as approve and reject take it.
type signatureInput struct {
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential"`
}

var errBadSignature = errors.New("the decision's signature did not check out")

// checkDecisionSignature verifies a reviewer's signature over the
// decision being made, in its transaction: the ceremony is used up, and
// the passkey's sign count moves on. It returns what the log keeps.
func (h *Handlers) checkDecisionSignature(ctx context.Context, tx *sql.Tx, me auth.User, in *signatureInput, action, kind string, id int64, revision, reason string) (decisionSignature, error) {
	var out decisionSignature
	wa := h.webAuthn()
	if wa == nil || in == nil || in.SessionID == "" {
		return out, errBadSignature
	}
	session, d, err := h.takePasskeySession(ctx, tx, in.SessionID, me.ID, "sign")
	if err != nil || d == nil {
		return out, errBadSignature
	}
	// The signature must be over this very decision.
	if d.Action != action || d.Kind != kind || d.ID != id || d.Revision != revision || d.Reason != reason {
		return out, errBadSignature
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
	if err != nil {
		return out, errBadSignature
	}
	u, err := h.passkeyUser(ctx, me)
	if err != nil {
		return out, err
	}
	cred, err := wa.ValidateLogin(u, session, parsed)
	if err != nil {
		return out, fmt.Errorf("%w: %v", errBadSignature, err)
	}
	if !parsed.Response.AuthenticatorData.Flags.UserVerified() {
		return out, fmt.Errorf("%w: the passkey did not verify the person", errBadSignature)
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE reviewer_passkeys SET credential = $2, last_used_at = now() WHERE credential_id = $1`, cred.ID, raw); err != nil {
		return out, err
	}
	resp := parsed.Raw.AssertionResponse
	return decisionSignature{
		CredentialID:      b64.EncodeToString(cred.ID),
		PublicKey:         b64.EncodeToString(cred.PublicKey),
		Nonce:             d.Nonce,
		AuthenticatorData: b64.EncodeToString(resp.AuthenticatorData),
		ClientData:        b64.EncodeToString(resp.ClientDataJSON),
		Signature:         b64.EncodeToString(resp.Signature),
	}, nil
}

// verifyDecisionSignature checks a logged decision's signature again,
// from the log alone: the client data names the challenge made from the
// decision, and the public key verifies the signature over it.
func verifyDecisionSignature(e chainEntry) error {
	authData, err1 := b64.DecodeString(e.SigAuthenticatorData)
	clientData, err2 := b64.DecodeString(e.SigClientData)
	sig, err3 := b64.DecodeString(e.SigSignature)
	pub, err4 := b64.DecodeString(e.SigPublicKey)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return errors.New("its signature can't be read")
	}
	action := map[string]string{"approved": "approve", "rejected": "reject"}[e.Action]
	want := signedDecision{Action: action, Kind: e.Kind, ID: e.ItemID, Revision: e.Revision, Reason: e.Reason, Nonce: e.SigNonce}.challenge()
	var cd struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(clientData, &cd); err != nil || cd.Type != "webauthn.get" {
		return errors.New("its signature is not a passkey's")
	}
	got, err := b64.DecodeString(strings.TrimRight(cd.Challenge, "="))
	if err != nil || !bytes.Equal(got, want) {
		return errors.New("its signature is over a different decision")
	}
	if len(authData) < 37 || authData[32]&0x04 == 0 {
		return errors.New("its signature did not verify the person")
	}
	key, err := webauthncose.ParsePublicKey(pub)
	if err != nil {
		return errors.New("its public key can't be read")
	}
	sum := sha256.Sum256(clientData)
	ok, err := webauthncose.VerifySignature(key, append(append([]byte{}, authData...), sum[:]...), sig)
	if err != nil || !ok {
		return errors.New("its signature does not match")
	}
	return nil
}
