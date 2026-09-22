package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/metrics"
	"github.com/klahr/zonventure/internal/orgslug"
	"github.com/klahr/zonventure/internal/tokenx"
)

// isASCIIPrintable reports whether s contains only printable ASCII
// (0x20..0x7E). Non-printable, non-ASCII (Unicode confusables,
// homoglyphs, RTL marks), and control bytes all fail. Used to reject
// oddly-shaped IdP claims before they become usernames.
func isASCIIPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return true
}

// OIDCConfig is the operator-supplied OpenID Connect configuration.
// SSO is gated by Enabled — when false, all SSO endpoints 404 and the
// login UI hides the SSO button. Required fields are validated at
// boot via Validate so a typo doesn't manifest as a half-broken flow
// in production.
//
// HostedDomain (Google's `hd` claim) is mandatory for two reasons:
// (1) it locks the deployment to a single Google Workspace tenant, so
// a misconfigured client_id can't accept logins from arbitrary Gmail
// users; (2) it lets us safely link by email match on first sign-in,
// since Google sets email_verified=true for managed-domain users.
//
// OrgName names the organisation that JIT-provisioned users join. If
// empty, the hosted domain is used. The first SSO user becomes the
// org_admin; subsequent users join as org_member.
type OIDCConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	HostedDomain string
	RedirectURL  string
	OrgName      string
	// ButtonLabel overrides the default "Sign in with Google" copy on
	// the login page. Optional.
	ButtonLabel string
	// ProviderName is what we record in users.oidc_provider. Defaults
	// to "google". Distinct from HostedDomain so a future Microsoft
	// Entra integration can coexist on the same column with a
	// different value.
	ProviderName string
}

// Validate returns an error describing the missing / malformed fields
// when SSO is enabled. Operators see this at boot, not at first login.
func (c OIDCConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	var missing []string
	if strings.TrimSpace(c.Issuer) == "" {
		missing = append(missing, "ZV_OIDC_ISSUER")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		missing = append(missing, "ZV_OIDC_CLIENT_ID")
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		missing = append(missing, "ZV_OIDC_CLIENT_SECRET")
	}
	if strings.TrimSpace(c.HostedDomain) == "" {
		missing = append(missing, "ZV_OIDC_HOSTED_DOMAIN")
	}
	if strings.TrimSpace(c.RedirectURL) == "" {
		missing = append(missing, "ZV_OIDC_REDIRECT_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("oidc: ZV_OIDC_ENABLED=1 but missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// OIDC is the live, validated SSO client. nil pointer or Enabled()
// returning false means "SSO not configured" — handlers and UI code
// branch on Enabled() before doing anything else.
type OIDC struct {
	cfg      OIDCConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// NewOIDC performs OpenID Connect discovery against the issuer's
// /.well-known/openid-configuration and builds an ID-token verifier
// bound to the configured client_id. Returns (nil, nil) when SSO is
// disabled — callers should treat that as "no SSO" rather than an
// error so a self-host without SSO continues to boot cleanly.
func NewOIDC(ctx context.Context, cfg OIDCConfig) (*OIDC, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery against %s failed: %w", cfg.Issuer, err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	if cfg.ProviderName == "" {
		cfg.ProviderName = "google"
	}
	if cfg.ButtonLabel == "" {
		cfg.ButtonLabel = "Sign in with Google"
	}
	if cfg.OrgName == "" {
		cfg.OrgName = cfg.HostedDomain
	}
	return &OIDC{
		cfg:      cfg,
		provider: provider,
		verifier: verifier,
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
	}, nil
}

// Enabled tells the rest of the binary whether to surface SSO. Safe
// to call on a nil receiver.
func (o *OIDC) Enabled() bool { return o != nil && o.cfg.Enabled }

// ButtonLabel is what the login UI renders inside the SSO button.
func (o *OIDC) ButtonLabel() string {
	if o == nil {
		return ""
	}
	return o.cfg.ButtonLabel
}

// HostedDomain is the Google Workspace `hd` value used both as the
// auth-URL hint and as the post-callback enforcement check.
func (o *OIDC) HostedDomain() string {
	if o == nil {
		return ""
	}
	return o.cfg.HostedDomain
}

// ProviderName is the human / logging-friendly identifier for the
// IdP this client talks to ("google", "microsoft", …). Surfaced on
// the public signup-config so the login screen can pick provider-
// specific button styling (Google's official Material design vs.
// a generic SSO button). Defaults to "google".
func (o *OIDC) ProviderName() string {
	if o == nil {
		return ""
	}
	if o.cfg.ProviderName != "" {
		return o.cfg.ProviderName
	}
	return "google"
}

// matchesHostedDomain returns true if the given email's domain
// matches the configured hosted domain (case-insensitive). Used by
// the password-login path to redirect SSO-bound users away from
// password fallback.
func (o *OIDC) MatchesHostedDomain(email string) bool {
	if !o.Enabled() {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	return strings.EqualFold(email[at+1:], o.cfg.HostedDomain)
}

// ---- HTTP handlers --------------------------------------------------

const (
	oidcStateCookieName = "zv_oidc_state"
	oidcStateCookieTTL  = 10 * time.Minute
	// oidcStateSep separates the state and nonce values inside the
	// cookie payload. ":" is unambiguous because both halves are
	// URL-safe base64 (no ":") by construction.
	oidcStateSep = ":"
)

// ErrOIDCLinkRefused signals that findOrCreateOIDCUser declined to
// link the IdP identity to a local-password account at the same
// email. The callback surfaces it as a "link_refused" redirect so
// the operator can see the audit trail without leaking which email
// was matched.
var ErrOIDCLinkRefused = errors.New("oidc: refusing to link existing local-password account")

// OIDCLogin handles GET /auth/oidc/login. Generates a fresh state and
// nonce, stores both in a short-lived HttpOnly cookie, and redirects
// the user to the IdP's authorization endpoint. The state pins the
// redirect to this browser; the nonce pins the eventual ID token to
// this auth attempt (replay defence).
func (h *Handlers) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	if !h.OIDC.Enabled() {
		http.NotFound(w, r)
		return
	}
	state, err := randomURLSafeToken(24)
	if err != nil {
		http.Error(w, "state error", http.StatusInternalServerError)
		return
	}
	nonce, err := randomURLSafeToken(24)
	if err != nil {
		http.Error(w, "nonce error", http.StatusInternalServerError)
		return
	}
	setOIDCStateCookie(w, r, state+oidcStateSep+nonce, h.SecureCookie)
	authURL := h.OIDC.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce),
		// `hd` parameter on the auth URL is a hint to Google's account
		// chooser; the post-callback `hd` claim check is what actually
		// enforces the domain restriction.
		oauth2.SetAuthURLParam("hd", h.OIDC.cfg.HostedDomain),
		// Always show the account picker so a user with multiple
		// Google accounts in the browser doesn't get auto-signed-in
		// to the wrong one.
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// OIDCCallback handles GET /auth/oidc/callback?code=...&state=....
// Validates state + nonce, exchanges the authorization code for an
// ID token, validates the token (signature, iss, aud, exp, nonce,
// hd, email_verified), JIT-provisions or links the user, mints the
// session, and redirects to /arrange.
func (h *Handlers) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !h.OIDC.Enabled() {
		http.NotFound(w, r)
		return
	}
	ip := ClientIP(r)
	ua := UserAgent(r)
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		// IdP-side denial (user clicked cancel, consent rejected,
		// admin policy block). Log it but show a generic message —
		// the IdP often includes a description we don't want to
		// echo verbatim.
		logx.Warn("auth.oidc.idp_error", "error", e, "desc", q.Get("error_description"), "ip", ip)
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_idp_error",
		})
		oidcRedirectErr(w, r, "idp_error")
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		oidcRedirectErr(w, r, "bad_request")
		return
	}
	cookieVal, err := readOIDCStateCookie(r)
	clearOIDCStateCookie(w, r, h.SecureCookie)
	if err != nil {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_state_missing",
		})
		oidcRedirectErr(w, r, "session_expired")
		return
	}
	parts := strings.SplitN(cookieVal, oidcStateSep, 2)
	if len(parts) != 2 || !subtleEqualString(parts[0], state) {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_state_mismatch",
		})
		oidcRedirectErr(w, r, "state_mismatch")
		return
	}
	expectedNonce := parts[1]

	tok, err := h.OIDC.oauth.Exchange(r.Context(), code)
	if err != nil {
		logx.Warn("auth.oidc.exchange_failed", "err", err.Error(), "ip", ip)
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_exchange_failed",
		})
		oidcRedirectErr(w, r, "exchange_failed")
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_no_id_token",
		})
		oidcRedirectErr(w, r, "no_id_token")
		return
	}
	idTok, err := h.OIDC.verifier.Verify(r.Context(), rawID)
	if err != nil {
		logx.Warn("auth.oidc.verify_failed", "err", err.Error(), "ip", ip)
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_verify_failed",
		})
		oidcRedirectErr(w, r, "verify_failed")
		return
	}
	var claims oidcClaims
	if err := idTok.Claims(&claims); err != nil {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, IP: ip, UserAgent: ua,
			Reason: "oidc_claims_failed",
		})
		oidcRedirectErr(w, r, "claims_failed")
		return
	}
	if !subtleEqualString(idTok.Nonce, expectedNonce) {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_nonce_mismatch",
		})
		oidcRedirectErr(w, r, "nonce_mismatch")
		return
	}
	if !strings.EqualFold(claims.HD, h.OIDC.cfg.HostedDomain) {
		// Defence in depth: the auth-URL `hd` parameter is only a hint;
		// the post-callback claim is the enforcement point. A user
		// outside the domain who somehow reaches this code path with a
		// valid Google ID token gets rejected here.
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_hd_mismatch",
		})
		oidcRedirectErr(w, r, "domain_not_allowed")
		return
	}
	if !claims.EmailVerified || claims.Email == "" || claims.Sub == "" {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_email_unverified",
		})
		oidcRedirectErr(w, r, "email_unverified")
		return
	}
	// Normalise the IdP-provided email before it becomes the username.
	// Trim surrounding whitespace, reject any non-printable byte, and
	// require a plausible local@domain shape (mail.ParseAddress). An
	// IdP returning oddly-shaped values (whitespace, unicode confusables,
	// CRLF) would otherwise mint an account whose username collides
	// visually with a legitimate user — combined with the auto-org-admin
	// grant on the first user, that's a privilege-escalation seam.
	claims.Email = strings.TrimSpace(claims.Email)
	if !isASCIIPrintable(claims.Email) {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_email_unprintable",
		})
		oidcRedirectErr(w, r, "email_unverified")
		return
	}
	if _, err := mail.ParseAddress(claims.Email); err != nil {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_email_unparseable",
		})
		oidcRedirectErr(w, r, "email_unverified")
		return
	}
	if claims.Aud != "" && claims.Aud != h.OIDC.cfg.ClientID {
		// go-oidc verifies aud, but defence in depth: also reject when
		// a future change widens the verifier and our claim doesn't
		// match the configured client_id.
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_aud_mismatch",
		})
		oidcRedirectErr(w, r, "aud_mismatch")
		return
	}

	// JIT provision / link by email / load existing.
	u, err := findOrCreateOIDCUser(r.Context(), h.DB, h.OIDC.cfg, claims)
	if err != nil {
		// Refused-link is its own audit class so the operator can
		// distinguish "IdP fine, we wouldn't bind" from "IdP/DB
		// failure". The user-facing message stays generic so the
		// page doesn't leak which email matched.
		if errors.Is(err, ErrOIDCLinkRefused) {
			logx.Warn("auth.oidc.link_refused", "email", claims.Email)
			Record(r.Context(), h.DB, AuditEvent{
				Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
				Reason: "oidc_link_refused",
			})
			oidcRedirectErr(w, r, "link_refused")
			return
		}
		logx.Warn("auth.oidc.provision_failed", "err", err.Error(), "email", claims.Email)
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: claims.Email, IP: ip, UserAgent: ua,
			Reason: "oidc_provision_failed",
		})
		oidcRedirectErr(w, r, "provision_failed")
		return
	}

	// SSO bypasses the local TOTP step. The IdP already enforces
	// 2FA at the workspace level for the vast majority of corporate
	// deployments, so re-prompting locally is friction without a
	// security gain. FactorIdPPrimary on the session row records
	// "this session was minted via OIDC" for audit + future policy.
	factors := []Factor{FactorIdPPrimary}
	if amrIncludesMFA(claims.AMR) {
		factors = append(factors, FactorIdPMFA)
	}
	var active sql.NullInt64
	if u.OrgID != nil {
		active = sql.NullInt64{Int64: *u.OrgID, Valid: true}
	}
	sessionTok, err := CompleteLogin(r.Context(), h.DB, u, factors, ua, ip, active)
	if err != nil {
		logx.Warn("auth.oidc.session_create_failed", "err", err.Error())
		oidcRedirectErr(w, r, "session_failed")
		return
	}
	metrics.AuthLogins.WithLabelValues("success").Inc()
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventLogin, OK: true, UserID: &u.ID, Username: u.Username, IP: ip, UserAgent: ua,
		Reason: "oidc_success",
	})
	SetCookie(w, sessionTok, CookieSecure(r, h.SecureCookie))
	http.Redirect(w, r, "/arrange", http.StatusFound)
}

// ---- claims, provisioning, helpers ---------------------------------

// oidcClaims captures the subset of the ID token we read. Other claims
// (name, picture, locale) are deliberately ignored — the user record
// derives those from elsewhere if at all, and keeping the surface
// minimal limits the blast radius of a future claims-injection bug.
type oidcClaims struct {
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	HD            string   `json:"hd"`
	Aud           string   `json:"aud"`
	AMR           []string `json:"amr"`
}

// findOrCreateOIDCUser looks up the user by (provider, sub) first;
// failing that, by email match (safe because Google sets
// email_verified=true for managed-domain accounts and we've already
// asserted hd matches); failing that, JIT-provisions a new user and
// org membership.
//
// All within one transaction so a partial provisioning failure
// doesn't leave a half-created user.
func findOrCreateOIDCUser(ctx context.Context, db *sql.DB, cfg OIDCConfig, c oidcClaims) (User, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback() //nolint:errcheck — Commit takes over on success

	// 1) (provider, sub) — the IdP's stable identifier, immune to
	//    email renames in Workspace.
	var userID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE oidc_provider = $1 AND oidc_subject = $2
	`, cfg.ProviderName, c.Sub).Scan(&userID)
	if err == nil {
		return loadUserAfterOIDC(ctx, tx, userID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("oidc: lookup by sub: %w", err)
	}

	// 2) Email match — link an existing password-less account (e.g. a
	//    user previously SSO-bound whose oidc_subject was wiped, or
	//    an admin-seeded placeholder) to this OIDC identity.
	//
	//    SECURITY: refuse the implicit link when the existing user
	//    has a local password set. Otherwise an attacker who controls
	//    a Workspace account at the same email as a legacy local user
	//    (someone who left the org years ago and never bound to SSO)
	//    can take over the account, bypassing whatever local TOTP +
	//    password the legitimate owner had. The hd claim guarantees
	//    the IdP-side domain match; it does NOT guarantee the IdP
	//    user is the same human as the local user. An explicit
	//    "link my SSO" step from inside the existing session is the
	//    safe path; we don't yet implement that, so the operator's
	//    short-term remedy is to clear the local password (forcing
	//    the user through SSO) before SSO is enabled.
	var passwordHash sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id, password_hash FROM users WHERE LOWER(email) = LOWER($1)
	`, c.Email).Scan(&userID, &passwordHash)
	if err == nil {
		if passwordHash.Valid && passwordHash.String != "" {
			return User{}, ErrOIDCLinkRefused
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET oidc_provider = $1,
			    oidc_subject  = $2,
			    email_verified = 1
			WHERE id = $3
		`, cfg.ProviderName, c.Sub, userID); err != nil {
			return User{}, fmt.Errorf("oidc: link existing: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return User{}, err
		}
		return loadUserPostCommit(ctx, db, userID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("oidc: lookup by email: %w", err)
	}

	// 3) JIT-provision: create the user, ensure the SSO org exists,
	//    grant org_admin to the first user / org_member to subsequent.
	orgID, makeAdmin, err := ensureSSOOrg(ctx, tx, cfg.OrgName)
	if err != nil {
		return User{}, fmt.Errorf("oidc: org: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users(username, email, email_verified, oidc_provider, oidc_subject)
		VALUES ($1, $2, 1, $3, $4)
		RETURNING id
	`, c.Email, c.Email, cfg.ProviderName, c.Sub).Scan(&userID); err != nil {
		return User{}, fmt.Errorf("oidc: insert user: %w", err)
	}
	memberRole := RoleOrgMember
	if makeAdmin {
		memberRole = RoleOrgAdmin
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, $3)
	`, userID, orgID, memberRole); err != nil {
		return User{}, fmt.Errorf("oidc: insert membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return loadUserPostCommit(ctx, db, userID)
}

// ensureSSOOrg returns the orgID for the configured SSO org name,
// creating it on first use. The boolean return is true when the org
// was just created (the first user becomes org_admin in that case).
func ensureSSOOrg(ctx context.Context, tx *sql.Tx, name string) (int64, bool, error) {
	var orgID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM organizations WHERE name = $1`, name).Scan(&orgID)
	if err == nil {
		return orgID, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	slug, err := orgslug.Reserve(ctx, tx, name)
	if err != nil {
		return 0, false, fmt.Errorf("reserve slug: %w", err)
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, name, slug).Scan(&orgID); err != nil {
		return 0, false, err
	}
	return orgID, true, nil
}

// loadUserAfterOIDC hydrates a User row inside an open tx (used after
// the by-sub branch where we don't need to write anything before
// returning).
func loadUserAfterOIDC(ctx context.Context, tx *sql.Tx, userID int64) (User, error) {
	var u User
	var orgID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_admin, COALESCE(u.email,''), (u.email_verified <> 0),
		       (SELECT MIN(org_id) FROM org_memberships WHERE user_id = u.id)
		FROM users u
		WHERE u.id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Email, &u.EmailVerified, &orgID); err != nil {
		return User{}, err
	}
	if orgID.Valid {
		o := orgID.Int64
		u.OrgID = &o
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

// loadUserPostCommit re-reads the user after the tx has been
// committed. Separate from loadUserAfterOIDC because the link / JIT
// branches do their writes inside the tx and need the fresh row from
// outside.
func loadUserPostCommit(ctx context.Context, db *sql.DB, userID int64) (User, error) {
	var u User
	var orgID sql.NullInt64
	if err := db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_admin, COALESCE(u.email,''), (u.email_verified <> 0),
		       (SELECT MIN(org_id) FROM org_memberships WHERE user_id = u.id)
		FROM users u
		WHERE u.id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Email, &u.EmailVerified, &orgID); err != nil {
		return User{}, err
	}
	if orgID.Valid {
		o := orgID.Int64
		u.OrgID = &o
	}
	return u, nil
}

// amrIncludesMFA reports whether the IdP's `amr` claim includes a
// value that signals multi-factor authentication per RFC 8176. Only
// `mfa` and `mca` (multi-channel) are accepted; the previous lenient
// match included `otp`, `swk`, and `hwk`, but those are individual
// factors not a multi-factor combination — accepting them would
// allow a future "skip local TOTP because IdP did MFA" policy to
// under-step on a single-factor login.
func amrIncludesMFA(amr []string) bool {
	for _, v := range amr {
		switch strings.ToLower(v) {
		case "mfa", "mca":
			return true
		}
	}
	return false
}

// oidcRedirectErr lands the user on /arrange with a hash flag so the
// front-end can show an inline message. We don't echo the failure
// reason to the user (some are sensitive, e.g. nonce_mismatch =
// possible replay attempt) — the audit log carries the detail.
func oidcRedirectErr(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/arrange#sso-error="+code, http.StatusFound)
}

// setOIDCStateCookie writes the short-lived state+nonce cookie. Bound
// to /auth/oidc so the browser only sends it on the callback —
// reduces the blast radius if the cookie ever leaks via referrer.
func setOIDCStateCookie(w http.ResponseWriter, r *http.Request, value string, secureCfg bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    value,
		Path:     "/auth/oidc",
		HttpOnly: true,
		Secure:   CookieSecure(r, secureCfg),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(oidcStateCookieTTL),
		MaxAge:   int(oidcStateCookieTTL / time.Second),
	})
}

// readOIDCStateCookie pulls the state+nonce cookie. Returns an error
// when missing, so the caller can distinguish "expired / cleared"
// from "tampered".
func readOIDCStateCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(oidcStateCookieName)
	if err != nil {
		return "", err
	}
	if c.Value == "" {
		return "", errors.New("empty cookie")
	}
	return c.Value, nil
}

// clearOIDCStateCookie expires the state cookie post-callback so a
// concurrent tab can't reuse the same state. Always called even on
// error paths to keep the cookie store tidy.
func clearOIDCStateCookie(w http.ResponseWriter, r *http.Request, secureCfg bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     "/auth/oidc",
		HttpOnly: true,
		Secure:   CookieSecure(r, secureCfg),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// randomURLSafeToken is a thin wrapper around tokenx.New so the OIDC
// state/nonce paths use the same primitive as the rest of the auth
// package. Kept as a named function only because the call sites read
// "randomURLSafeToken(24)" more clearly than "tokenx.New(24)" in
// context — a future cleanup can inline.
func randomURLSafeToken(n int) (string, error) {
	return tokenx.New(n)
}

// subtleEqualString is a constant-time equality check on two strings.
// Used for state and nonce comparison so a timing oracle can't leak
// which prefix matched. (Not load-bearing — the values are random
// and short — but cheap to do right.)
func subtleEqualString(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
