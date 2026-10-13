// Package oauth is a small OAuth 2.1 authorization server for the MCP
// endpoint, so a client such as a claude.ai connector can sign its user
// in instead of being handed a pasted token.
//
//	GET  /.well-known/oauth-protected-resource   what protects /mcp (RFC 9728)
//	GET  /.well-known/oauth-authorization-server  this server (RFC 8414)
//	POST /oauth/register                          a client registers (RFC 7591)
//	GET  /oauth/authorize                         the user is sent here; on to the console's consent page
//	POST /oauth/token                             a code or refresh token for tokens
//	GET  /api/admin/oauth/request                 the consent page reads what is asked
//	POST /api/admin/oauth/approve                 the signed-in user approves or declines
//
// Every client is public — no secret — and must use PKCE with S256.
// Redirect URIs must be on an allowlist and match exactly. What the
// client gets is an ordinary MCP token (admin.LookupMCPToken) that
// expires after an hour and is renewed by a rotating refresh token, so
// every tool, scope check and the Settings list work unchanged.
package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/sitename"
	"github.com/klahr/zonventure/internal/tokenx"
)

const (
	accessTTL  = time.Hour
	refreshTTL = 30 * 24 * time.Hour
	codeTTL    = 60 * time.Second

	// Same shape as a pasted token, so a leaked one is recognisable.
	accessPrefix  = "zvmcp_"
	refreshPrefix = "zvmcpr_"
	secretBytes   = 32

	maxClientNameLen = 100
	maxRedirectURIs  = 10
)

var allScopes = []string{"read", "write"}

// defaultRedirectHosts are where Claude's connectors return to. Loopback
// addresses, for desktop and command-line clients, are always allowed.
var defaultRedirectHosts = []string{"claude.ai", "claude.com"}

// Server serves the OAuth endpoints. Issuer is the public origin
// (ZV_PUBLIC_URL); without one there is no stable issuer and Enabled is
// false.
type Server struct {
	DB            *sql.DB
	Issuer        string
	RedirectHosts []string
}

// New configures a server for the public origin, adding the hosts in
// ZV_OAUTH_REDIRECT_HOSTS (comma-separated) to the redirect allowlist.
func New(db *sql.DB, publicURL string) *Server {
	hosts := slices.Clone(defaultRedirectHosts)
	for _, h := range strings.Split(os.Getenv("ZV_OAUTH_REDIRECT_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts = append(hosts, h)
		}
	}
	return &Server{DB: db, Issuer: strings.TrimRight(publicURL, "/"), RedirectHosts: hosts}
}

func (s *Server) Enabled() bool { return s.Issuer != "" }

// ResourceMetadataURL is what /mcp's 401 points a client at.
func (s *Server) ResourceMetadataURL() string {
	return s.Issuer + "/.well-known/oauth-protected-resource"
}

func (s *Server) ProtectedResource(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"resource":                 s.Issuer + "/mcp",
		"authorization_servers":    []string{s.Issuer},
		"scopes_supported":         allScopes,
		"bearer_methods_supported": []string{"header"},
		"resource_name":            sitename.Name(),
	})
}

func (s *Server) AuthorizationServer(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.Issuer,
		"authorization_endpoint":                s.Issuer + "/oauth/authorize",
		"token_endpoint":                        s.Issuer + "/oauth/token",
		"registration_endpoint":                 s.Issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      allScopes,
	})
}

// redirectAllowed reports whether a client may register uri: https on
// an allowlisted host, or http on loopback (RFC 8252 §7.3).
func (s *Server) redirectAllowed(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Fragment != "" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch u.Scheme {
	case "http":
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	case "https":
		return slices.Contains(s.RedirectHosts, host)
	}
	return false
}

// oauthError writes an RFC 6749 §5.2 error body.
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	httpx.JSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "body must be JSON client metadata")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxRedirectURIs {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("give 1 to %d redirect_uris", maxRedirectURIs))
		return
	}
	for _, u := range req.RedirectURIs {
		if !s.redirectAllowed(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri not allowed on this server: "+u)
			return
		}
	}
	if m := req.TokenEndpointAuthMethod; m != "" && m != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method none) are supported")
		return
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant type "+g)
			return
		}
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if r := []rune(name); len(r) > maxClientNameLen {
		name = string(r[:maxClientNameLen])
	}
	secret, err := tokenx.New(24)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "client id", err)
		return
	}
	clientID := "zvc_" + secret
	uris, _ := json.Marshal(req.RedirectURIs)
	var issued time.Time
	if err := s.DB.QueryRowContext(r.Context(), `
		INSERT INTO oauth_clients (client_id, name, redirect_uris) VALUES ($1, $2, $3) RETURNING created_at
	`, clientID, name, uris).Scan(&issued); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "registering client failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        issued.Unix(),
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

type client struct {
	id           int64
	clientID     string
	name         string
	redirectURIs []string
}

var errUnknownClient = errors.New("unknown client")

func (s *Server) loadClient(ctx context.Context, clientID string) (client, error) {
	c := client{clientID: clientID}
	var uris []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id, name, redirect_uris FROM oauth_clients WHERE client_id = $1`, clientID).
		Scan(&c.id, &c.name, &uris)
	if errors.Is(err, sql.ErrNoRows) {
		return c, errUnknownClient
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(uris, &c.redirectURIs)
}

// authRequest is an authorization request, as the client sent it to
// /oauth/authorize and the consent page sends it back on approval.
type authRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	ResponseType        string `json:"response_type"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Scope               string `json:"scope"`
	Resource            string `json:"resource"`
}

func requestFromQuery(q url.Values) authRequest {
	return authRequest{
		ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), ResponseType: q.Get("response_type"),
		CodeChallenge: q.Get("code_challenge"), CodeChallengeMethod: q.Get("code_challenge_method"),
		State: q.Get("state"), Scope: q.Get("scope"), Resource: q.Get("resource"),
	}
}

// check validates a request. A bad client or redirect URI is fatal —
// the error must not be sent to an unverified redirect — so it comes
// back as fatal; anything else can go to the client's redirect URI.
func (s *Server) check(ctx context.Context, a authRequest) (c client, scopes []string, fatal, redirectErr error) {
	c, err := s.loadClient(ctx, a.ClientID)
	if err != nil {
		return c, nil, err, nil
	}
	if !slices.Contains(c.redirectURIs, a.RedirectURI) {
		return c, nil, errors.New("redirect_uri is not one this client registered"), nil
	}
	if a.ResponseType != "code" {
		return c, nil, nil, errors.New("unsupported_response_type")
	}
	if a.CodeChallengeMethod != "S256" || len(a.CodeChallenge) < 43 || len(a.CodeChallenge) > 128 {
		return c, nil, nil, errors.New("invalid_request")
	}
	if a.Resource != "" && strings.TrimRight(a.Resource, "/") != s.Issuer+"/mcp" && strings.TrimRight(a.Resource, "/") != s.Issuer {
		return c, nil, nil, errors.New("invalid_target")
	}
	scopes = allScopes
	if a.Scope != "" {
		scopes = nil
		for _, sc := range strings.Fields(a.Scope) {
			if !slices.Contains(allScopes, sc) {
				return c, nil, nil, errors.New("invalid_scope")
			}
			if !slices.Contains(scopes, sc) {
				scopes = append(scopes, sc)
			}
		}
	}
	return c, scopes, nil, nil
}

func redirectWith(uri string, params url.Values) string {
	sep := "?"
	if strings.Contains(uri, "?") {
		sep = "&"
	}
	return uri + sep + params.Encode()
}

// Authorize checks the request and sends the browser on to the console,
// which signs the user in if needed and asks for consent. The query is
// carried along in the fragment, which the console's router reads.
func (s *Server) Authorize(w http.ResponseWriter, r *http.Request) {
	a := requestFromQuery(r.URL.Query())
	_, _, fatal, redirectErr := s.check(r.Context(), a)
	if fatal != nil {
		http.Error(w, "This sign-in link is not valid: "+fatal.Error(), http.StatusBadRequest)
		return
	}
	if redirectErr != nil {
		http.Redirect(w, r, redirectWith(a.RedirectURI, url.Values{"error": {redirectErr.Error()}, "state": {a.State}, "iss": {s.Issuer}}), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/app/arrange/#/authorize?"+r.URL.RawQuery, http.StatusFound)
}

// Request tells the consent page who is asking for what.
func (s *Server) Request(w http.ResponseWriter, r *http.Request) {
	a := requestFromQuery(r.URL.Query())
	c, scopes, fatal, redirectErr := s.check(r.Context(), a)
	if fatal != nil {
		httpx.Error(w, http.StatusBadRequest, fatal.Error())
		return
	}
	if redirectErr != nil {
		httpx.Error(w, http.StatusBadRequest, redirectErr.Error())
		return
	}
	u, _ := url.Parse(a.RedirectURI)
	// Only an allowlisted https host vouches for who the client is; any
	// local app can register a loopback redirect under any name.
	verified := u.Scheme == "https"
	httpx.JSON(w, http.StatusOK, map[string]any{"client_name": c.name, "redirect_host": u.Host, "verified": verified, "scopes": scopes})
}

// Approve records the signed-in user's answer and returns where the
// browser goes next: back to the client with a code, or with
// access_denied. It sits under /api/admin, behind the session and CSRF
// checks; an API token may not approve anything.
func (s *Server) Approve(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || auth.ViaAPIToken(r.Context()) {
		httpx.Error(w, http.StatusForbidden, "sign in to approve")
		return
	}
	var req struct {
		authRequest
		Approve bool `json:"approve"`
		// OrgID is the organisation to act in, for someone in several.
		OrgID int64 `json:"org_id"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	c, scopes, fatal, redirectErr := s.check(r.Context(), req.authRequest)
	if fatal != nil {
		httpx.Error(w, http.StatusBadRequest, fatal.Error())
		return
	}
	back := url.Values{"state": {req.State}, "iss": {s.Issuer}}
	switch {
	case redirectErr != nil:
		back.Set("error", redirectErr.Error())
	case !req.Approve:
		back.Set("error", "access_denied")
	default:
		org, err := s.pickOrg(r.Context(), me.ID, req.OrgID)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		code, err := tokenx.New(secretBytes)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "code", err)
			return
		}
		sc, _ := json.Marshal(scopes)
		if _, err := s.DB.ExecContext(r.Context(), `
			INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, scopes, org_id, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, hash(code), c.id, me.ID, req.RedirectURI, req.CodeChallenge, sc, org, time.Now().Add(codeTTL)); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "saving code failed", err)
			return
		}
		back.Set("code", code)
		logx.Info("oauth.approved", "user_id", me.ID, "client", c.clientID)
		s.sweep(r.Context())
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"redirect": redirectWith(req.RedirectURI, back)})
}

// sweep forgets what can no longer be used: expired codes, grants whose
// refresh token has lapsed, and clients registered a month ago that
// never got a grant. Run on each approval, which is rare enough to be
// cheap and frequent enough to keep the tables small.
func (s *Server) sweep(ctx context.Context) {
	for _, q := range []string{
		`DELETE FROM oauth_codes WHERE expires_at < now()`,
		`DELETE FROM mcp_tokens WHERE oauth_client_id IS NOT NULL AND refresh_expires_at < now()`,
		`DELETE FROM oauth_clients c WHERE created_at < now() - interval '30 days'
		   AND NOT EXISTS (SELECT 1 FROM mcp_tokens t WHERE t.oauth_client_id = c.id)
		   AND NOT EXISTS (SELECT 1 FROM oauth_codes o WHERE o.client_id = c.id)`,
	} {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			logx.Warn("oauth.sweep", "err", err.Error())
		}
	}
}

// pickOrg checks the organisation the user chose to act in. Someone in
// several must choose; someone in one acts in it; someone in none (the
// platform admin, a user with no organisation yet) acts as before.
func (s *Server) pickOrg(ctx context.Context, userID, orgID int64) (sql.NullInt64, error) {
	var orgs []int64
	rows, err := s.DB.QueryContext(ctx, `SELECT org_id FROM org_memberships WHERE user_id = $1 ORDER BY org_id`, userID)
	if err != nil {
		return sql.NullInt64{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return sql.NullInt64{}, err
		}
		orgs = append(orgs, id)
	}
	if err := rows.Err(); err != nil {
		return sql.NullInt64{}, err
	}
	switch {
	case orgID != 0 && slices.Contains(orgs, orgID):
		return sql.NullInt64{Int64: orgID, Valid: true}, nil
	case orgID != 0:
		return sql.NullInt64{}, errors.New("you are not in that organization")
	case len(orgs) == 1:
		return sql.NullInt64{Int64: orgs[0], Valid: true}, nil
	case len(orgs) > 1:
		return sql.NullInt64{}, errors.New("choose the organization to connect")
	}
	return sql.NullInt64{}, nil
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// Token exchanges an authorization code or a refresh token.
func (s *Server) Token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "form body required")
		return
	}
	c, err := s.loadClient(r.Context(), r.PostForm.Get("client_id"))
	if err != nil {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r, c)
	case "refresh_token":
		s.refresh(w, r, c)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request, c client) {
	var (
		userID      int64
		redirectURI string
		challenge   string
		scopes      []byte
		org         sql.NullInt64
		orgName     sql.NullString
	)
	// DELETE … RETURNING makes the code single-use even under a race.
	err := s.DB.QueryRowContext(r.Context(), `
		DELETE FROM oauth_codes WHERE code_hash = $1 AND client_id = $2 AND expires_at > now()
		RETURNING user_id, redirect_uri, code_challenge, scopes, org_id,
		          (SELECT name FROM organizations WHERE id = oauth_codes.org_id)
	`, hash(r.PostForm.Get("code")), c.id).Scan(&userID, &redirectURI, &challenge, &scopes, &org, &orgName)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logx.Warn("oauth.code", "err", err.Error())
		}
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code is unknown, used or expired")
		return
	}
	if r.PostForm.Get("redirect_uri") != redirectURI {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	}
	if !pkceMatches(r.PostForm.Get("code_verifier"), challenge) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match")
		return
	}
	access, refresh, err := newPair()
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "token", err)
		return
	}
	now := time.Now()
	name := c.name + " (signed in)"
	if orgName.Valid {
		name += " · " + orgName.String
	}
	if _, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO mcp_tokens (user_id, token_hash, name, token_prefix, scopes, expires_at, oauth_client_id, refresh_hash, refresh_expires_at, org_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, userID, hash(access), name, access[:12], scopes, now.Add(accessTTL), c.id, hash(refresh), now.Add(refreshTTL), org); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving token failed", err)
		return
	}
	writeTokens(w, access, refresh, scopes)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request, c client) {
	old := hash(r.PostForm.Get("refresh_token"))
	access, refresh, err := newPair()
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "token", err)
		return
	}
	now := time.Now()
	var scopes []byte
	err = s.DB.QueryRowContext(r.Context(), `
		UPDATE mcp_tokens
		SET token_hash = $1, expires_at = $2, refresh_hash = $3, refresh_expires_at = $4, previous_refresh_hash = refresh_hash
		WHERE refresh_hash = $5 AND oauth_client_id = $6 AND refresh_expires_at > now()
		RETURNING scopes
	`, hash(access), now.Add(accessTTL), hash(refresh), now.Add(refreshTTL), old, c.id).Scan(&scopes)
	if errors.Is(err, sql.ErrNoRows) {
		// A refresh token already rotated away is being replayed: one of
		// the two holders is not the client. Revoke the whole grant.
		if res, err := s.DB.ExecContext(r.Context(), `DELETE FROM mcp_tokens WHERE previous_refresh_hash = $1 AND oauth_client_id = $2`, old, c.id); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				logx.Warn("oauth.refresh_reuse", "client", c.clientID)
			}
		}
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is unknown, used or expired")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "refreshing failed", err)
		return
	}
	writeTokens(w, access, refresh, scopes)
}

func newPair() (access, refresh string, err error) {
	a, err := tokenx.New(secretBytes)
	if err != nil {
		return "", "", err
	}
	b, err := tokenx.New(secretBytes)
	if err != nil {
		return "", "", err
	}
	return accessPrefix + a, refreshPrefix + b, nil
}

func writeTokens(w http.ResponseWriter, access, refresh string, scopesJSON []byte) {
	var scopes []string
	_ = json.Unmarshal(scopesJSON, &scopes)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         strings.Join(scopes, " "),
	})
}
