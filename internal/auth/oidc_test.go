package auth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// ----- pure-unit tests (no DB / no network) -------------------------

func TestOIDCConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  auth.OIDCConfig
		want bool // true = expects error
	}{
		{"disabled is always ok", auth.OIDCConfig{Enabled: false}, false},
		{"enabled with no fields fails", auth.OIDCConfig{Enabled: true}, true},
		{"enabled with all fields ok", auth.OIDCConfig{
			Enabled:      true,
			Issuer:       "https://accounts.google.com",
			ClientID:     "abc",
			ClientSecret: "xyz",
			HostedDomain: "example.com",
			RedirectURL:  "https://zv.example.com/auth/oidc/callback",
		}, false},
		{"missing hosted_domain rejected", auth.OIDCConfig{
			Enabled:      true,
			Issuer:       "https://accounts.google.com",
			ClientID:     "abc",
			ClientSecret: "xyz",
			RedirectURL:  "https://zv.example.com/auth/oidc/callback",
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.want && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !c.want && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestOIDCMatchesHostedDomain(t *testing.T) {
	o, err := auth.NewOIDC(context.Background(), auth.OIDCConfig{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if o.MatchesHostedDomain("alice@example.com") {
		t.Errorf("disabled SSO should never match")
	}
	// Build a live OIDC client without doing real discovery — we set
	// the cfg fields and rely on MatchesHostedDomain being a pure
	// string check that doesn't touch the provider/verifier.
	live := liveOIDCWithDomain(t, "example.com")
	for _, c := range []struct {
		email string
		want  bool
	}{
		{"alice@example.com", true},
		{"alice@EXAMPLE.com", true},  // case-insensitive
		{"alice@example.org", false}, // wrong domain
		{"alice@subdomain.example.com", false},
		{"no-at-sign", false},
		{"", false},
		{"bob@", false},
	} {
		if got := live.MatchesHostedDomain(c.email); got != c.want {
			t.Errorf("MatchesHostedDomain(%q) = %v, want %v", c.email, got, c.want)
		}
	}
}

// ----- integration tests (DB + fake issuer) -------------------------

func TestOIDCCallback_HappyPath(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	fi := newFakeIssuer(t, "test-client")
	defer fi.Close()

	h := newOIDCHandlers(t, db, fi, "example.com", "Test Org")

	// 1. /auth/oidc/login → captures state, nonce, cookie.
	state, nonce, cookie := startLogin(t, h)

	// 2. fake issuer queues the ID-token claims for this code.
	code := "code-happy-" + randomToken(t)
	fi.queue(code, idClaims{
		Sub:           "google-user-1",
		Email:         "alice@example.com",
		EmailVerified: true,
		HD:            "example.com",
		Nonce:         nonce,
	})

	// 3. /auth/oidc/callback should mint the session.
	resp := callCallback(t, h, code, state, cookie)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("got status %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/arrange") {
		t.Fatalf("redirect %q does not lead to /arrange", loc)
	}
	if strings.Contains(loc, "sso-error") {
		t.Fatalf("happy path landed on error: %s", loc)
	}

	// 4. session cookie should be present.
	if !hasCookie(resp, auth.CookieName) {
		t.Fatalf("session cookie not set on happy path")
	}

	// 5. user row should exist with the OIDC binding.
	var sub string
	if err := db.QueryRow(`
		SELECT oidc_subject FROM users WHERE email = $1
	`, "alice@example.com").Scan(&sub); err != nil {
		t.Fatalf("user not provisioned: %v", err)
	}
	if sub != "google-user-1" {
		t.Errorf("oidc_subject = %q, want %q", sub, "google-user-1")
	}

	// 6. org_memberships row should exist for the JIT-provisioned org.
	var role string
	if err := db.QueryRow(`
		SELECT m.role
		FROM org_memberships m
		JOIN users u ON u.id = m.user_id
		WHERE u.email = $1
	`, "alice@example.com").Scan(&role); err != nil {
		t.Fatalf("membership not created: %v", err)
	}
	if role != auth.RoleOrgAdmin {
		t.Errorf("first SSO user should be org_admin, got %q", role)
	}
}

func TestOIDCCallback_WrongHostedDomain(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	fi := newFakeIssuer(t, "test-client")
	defer fi.Close()
	h := newOIDCHandlers(t, db, fi, "example.com", "")

	state, nonce, cookie := startLogin(t, h)
	code := "code-wronghd-" + randomToken(t)
	fi.queue(code, idClaims{
		Sub:           "google-user-evil",
		Email:         "evil@other.com",
		EmailVerified: true,
		HD:            "other.com", // wrong workspace
		Nonce:         nonce,
	})

	resp := callCallback(t, h, code, state, cookie)
	if !strings.Contains(resp.Header.Get("Location"), "sso-error=domain_not_allowed") {
		t.Fatalf("wrong hd should redirect to domain_not_allowed, got %q", resp.Header.Get("Location"))
	}
	if hasCookie(resp, auth.CookieName) {
		t.Fatalf("session cookie should NOT be set on hd mismatch")
	}
	// And no user should have been provisioned.
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = $1`, "evil@other.com").Scan(&n)
	if n != 0 {
		t.Errorf("hd mismatch should not provision user; got %d rows", n)
	}
}

func TestOIDCCallback_NonceMismatch(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	fi := newFakeIssuer(t, "test-client")
	defer fi.Close()
	h := newOIDCHandlers(t, db, fi, "example.com", "")

	state, _, cookie := startLogin(t, h)
	code := "code-nonce-" + randomToken(t)
	// Sign a token whose nonce differs from what the StartLogin call
	// stamped into the state cookie. A replay attempt would land
	// here — different browser session, different nonce.
	fi.queue(code, idClaims{
		Sub:           "google-user-1",
		Email:         "alice@example.com",
		EmailVerified: true,
		HD:            "example.com",
		Nonce:         "wrong-nonce",
	})

	resp := callCallback(t, h, code, state, cookie)
	if !strings.Contains(resp.Header.Get("Location"), "sso-error=nonce_mismatch") {
		t.Fatalf("wrong nonce should redirect to nonce_mismatch, got %q", resp.Header.Get("Location"))
	}
	if hasCookie(resp, auth.CookieName) {
		t.Fatalf("session cookie should NOT be set on nonce mismatch")
	}
}

func TestOIDCCallback_StateMismatch(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	fi := newFakeIssuer(t, "test-client")
	defer fi.Close()
	h := newOIDCHandlers(t, db, fi, "example.com", "")

	_, _, cookie := startLogin(t, h)
	// Use a state value that doesn't match the one in the cookie.
	resp := callCallback(t, h, "anycode", "tampered-state", cookie)
	if !strings.Contains(resp.Header.Get("Location"), "sso-error=state_mismatch") {
		t.Fatalf("state mismatch should redirect to state_mismatch, got %q", resp.Header.Get("Location"))
	}
}

func TestOIDCCallback_EmailUnverified(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	fi := newFakeIssuer(t, "test-client")
	defer fi.Close()
	h := newOIDCHandlers(t, db, fi, "example.com", "")

	state, nonce, cookie := startLogin(t, h)
	code := "code-unverified-" + randomToken(t)
	fi.queue(code, idClaims{
		Sub:           "google-user-1",
		Email:         "alice@example.com",
		EmailVerified: false, // should be rejected even with a valid sig
		HD:            "example.com",
		Nonce:         nonce,
	})
	resp := callCallback(t, h, code, state, cookie)
	if !strings.Contains(resp.Header.Get("Location"), "sso-error=email_unverified") {
		t.Fatalf("unverified email should redirect to email_unverified, got %q", resp.Header.Get("Location"))
	}
}

// TestOIDCCallback_RefusesLinkingExistingPasswordAccount pins the
// security posture in oidc.go's findOrCreateOIDCUser: when an
// existing local-password user shares the email of an incoming SSO
// claim, the link is REFUSED. The doc-comment in oidc.go explains
// why — without an "explicit link my SSO from inside an existing
// session" step (which the codebase doesn't yet implement), an IdP
// account on the same email could take over a legacy local user's
// row and bypass their TOTP + password. The test was originally
// authored for the opposite behaviour (auto-link); when the
// security gate landed the test should have flipped along with it
// but didn't, so it's been silently failing under DB-backed runs.
func TestOIDCCallback_RefusesLinkingExistingPasswordAccount(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	// Pre-create an org + user with a password. The SSO sign-in
	// must NOT link to this row; production refuses on purpose.
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Test Org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, password_hash, email_verified)
		VALUES ($1, $2, 'fake-hash', 0)
		RETURNING id
	`, "alice@example.com", "alice@example.com").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')
	`, userID, orgID); err != nil {
		t.Fatal(err)
	}

	fi := newFakeIssuer(t, "test-client")
	defer fi.Close()
	h := newOIDCHandlers(t, db, fi, "example.com", "Test Org")

	state, nonce, cookie := startLogin(t, h)
	code := "code-link-" + randomToken(t)
	fi.queue(code, idClaims{
		Sub:           "google-user-1",
		Email:         "alice@example.com",
		EmailVerified: true,
		HD:            "example.com",
		Nonce:         nonce,
	})

	resp := callCallback(t, h, code, state, cookie)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("link should redirect, got status=%d", resp.StatusCode)
	}
	if !strings.Contains(loc, "sso-error=link_refused") {
		t.Fatalf("link should be refused, got location=%q", loc)
	}

	// The pre-existing user row must be untouched: no oidc_subject
	// stamped, password_hash still present, email_verified still 0.
	// Any of those flipping would mean the gate is leaking through.
	var (
		oidcSub       sql.NullString
		passwordHash  sql.NullString
		emailVerified int
	)
	if err := db.QueryRow(`
		SELECT oidc_subject, password_hash, email_verified
		FROM users WHERE id = $1
	`, userID).Scan(&oidcSub, &passwordHash, &emailVerified); err != nil {
		t.Fatal(err)
	}
	if oidcSub.Valid && oidcSub.String != "" {
		t.Errorf("oidc_subject populated despite refusal: %q", oidcSub.String)
	}
	if !passwordHash.Valid || passwordHash.String == "" {
		t.Errorf("password_hash cleared despite refusal — local creds must remain authoritative")
	}
	if emailVerified != 0 {
		t.Errorf("email_verified flipped to %d despite refusal", emailVerified)
	}
}

// ----- shared helpers ----------------------------------------------

// liveOIDCWithDomain builds a live OIDC client without doing real
// discovery, by pointing at a stand-in fake issuer just for the
// purpose of returning a non-nil `*OIDC` whose cfg is filled in. Used
// by the pure-string MatchesHostedDomain test.
func liveOIDCWithDomain(t *testing.T, domain string) *auth.OIDC {
	t.Helper()
	fi := newFakeIssuer(t, "irrelevant")
	t.Cleanup(fi.Close)
	o, err := auth.NewOIDC(context.Background(), auth.OIDCConfig{
		Enabled:      true,
		Issuer:       fi.URL,
		ClientID:     "irrelevant",
		ClientSecret: "irrelevant",
		HostedDomain: domain,
		RedirectURL:  "http://server/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	return o
}

// newOIDCHandlers builds a Handlers wired to the fake issuer + given DB.
func newOIDCHandlers(t *testing.T, db *sql.DB, fi *fakeIssuer, hostedDomain, orgName string) *auth.Handlers {
	t.Helper()
	o, err := auth.NewOIDC(context.Background(), auth.OIDCConfig{
		Enabled:      true,
		Issuer:       fi.URL,
		ClientID:     fi.clientID,
		ClientSecret: "test-secret",
		HostedDomain: hostedDomain,
		RedirectURL:  "http://server/auth/oidc/callback",
		OrgName:      orgName,
	})
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	return &auth.Handlers{
		DB:           db,
		SecureCookie: false,
		Limiter:      auth.NewLoginLimiter(),
		OIDC:         o,
	}
}

// startLogin fires GET /auth/oidc/login and returns state, nonce,
// and the resulting state cookie that the IdP-callback flow needs.
func startLogin(t *testing.T, h *auth.Handlers) (state, nonce string, cookie *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest("GET", "/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	h.OIDCLogin(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("OIDCLogin status = %d, want 302", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	q := loc.Query()
	state = q.Get("state")
	nonce = q.Get("nonce")
	if state == "" || nonce == "" {
		t.Fatalf("missing state/nonce on auth URL: %s", loc.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "zv_oidc_state" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("state cookie not set by OIDCLogin")
	}
	return state, nonce, cookie
}

// callCallback wires up GET /auth/oidc/callback?code=...&state=...
// with the given cookie, and returns the recorder result.
func callCallback(t *testing.T, h *auth.Handlers, code, state string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest("GET", "/auth/oidc/callback?code="+code+"&state="+url.QueryEscape(state), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.OIDCCallback(w, req)
	return w.Result()
}

func hasCookie(resp *http.Response, name string) bool {
	for _, c := range resp.Cookies() {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

func randomToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ----- minimal fake OIDC issuer ------------------------------------

// fakeIssuer is an in-process OpenID Connect provider just rich
// enough to drive the auth.OIDCCallback handler. It serves discovery,
// JWKS, and a /token endpoint that returns ID tokens we sign locally
// with an RSA key generated once per test binary (fakeIssuerKey). The
// /auth endpoint is intentionally a no-op — tests bypass the IdP-side
// redirect and call /callback directly with a code we've pre-queued
// via fi.queue().
type fakeIssuer struct {
	URL      string
	clientID string

	server  *httptest.Server
	keyID   string
	privKey *rsa.PrivateKey

	mu      sync.Mutex
	pending map[string]idClaims // code → claims
}

type idClaims struct {
	Sub           string
	Email         string
	EmailVerified bool
	HD            string
	Nonce         string
	Aud           string
	Iss           string
	Exp           int64
	Iat           int64
}

// fakeIssuerKey is generated once per test binary: RSA-2048 keygen
// is the slowest part of newFakeIssuer (worse under -race), and no
// test depends on issuers having distinct keys — each fakeIssuer has
// its own server URL / issuer, which is what the verifier pins.
var fakeIssuerKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

func newFakeIssuer(t *testing.T, clientID string) *fakeIssuer {
	t.Helper()
	priv, err := fakeIssuerKey()
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	fi := &fakeIssuer{
		clientID: clientID,
		keyID:    "test-key-1",
		privKey:  priv,
		pending:  map[string]idClaims{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", fi.discovery)
	mux.HandleFunc("/jwks", fi.jwks)
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		// Tests don't drive the auth-URL leg — they call /callback
		// directly with a code they've queued. A real IdP would
		// redirect here. No-op for parity.
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", fi.token)
	fi.server = httptest.NewServer(mux)
	fi.URL = fi.server.URL
	return fi
}

func (fi *fakeIssuer) Close() {
	if fi != nil && fi.server != nil {
		fi.server.Close()
	}
}

func (fi *fakeIssuer) queue(code string, claims idClaims) {
	fi.mu.Lock()
	defer fi.mu.Unlock()
	fi.pending[code] = claims
}

func (fi *fakeIssuer) discovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                fi.URL,
		"authorization_endpoint":                fi.URL + "/auth",
		"token_endpoint":                        fi.URL + "/token",
		"jwks_uri":                              fi.URL + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
	})
}

func (fi *fakeIssuer) jwks(w http.ResponseWriter, r *http.Request) {
	pub := fi.privKey.Public().(*rsa.PublicKey)
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": fi.keyID,
				"n":   n,
				"e":   e,
			},
		},
	})
}

func (fi *fakeIssuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := r.FormValue("code")
	fi.mu.Lock()
	claims, ok := fi.pending[code]
	delete(fi.pending, code)
	fi.mu.Unlock()
	if !ok {
		http.Error(w, "unknown code", http.StatusBadRequest)
		return
	}
	idToken, err := fi.signIDToken(claims)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func (fi *fakeIssuer) signIDToken(c idClaims) (string, error) {
	if c.Iss == "" {
		c.Iss = fi.URL
	}
	if c.Aud == "" {
		c.Aud = fi.clientID
	}
	if c.Iat == 0 {
		c.Iat = time.Now().Unix()
	}
	if c.Exp == 0 {
		c.Exp = time.Now().Add(time.Hour).Unix()
	}
	header := map[string]any{
		"alg": "RS256",
		"typ": "JWT",
		"kid": fi.keyID,
	}
	payload := map[string]any{
		"iss":            c.Iss,
		"aud":            c.Aud,
		"sub":            c.Sub,
		"email":          c.Email,
		"email_verified": c.EmailVerified,
		"hd":             c.HD,
		"nonce":          c.Nonce,
		"exp":            c.Exp,
		"iat":            c.Iat,
	}
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(payload)
	signing := base64.RawURLEncoding.EncodeToString(hb) + "." +
		base64.RawURLEncoding.EncodeToString(pb)
	hash := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, fi.privKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
