package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/tokenx"
)

// MCP API tokens — long-lived bearer credentials a user pastes into
// Claude desktop or claude.ai's connector UI so the LLM can author
// adventures via /mcp on this user's behalf.
//
// The full plaintext is shown ONCE at creation (POST). After that
// the DB only stores sha256(token); the list view returns the
// user-chosen name and the first few visible chars (token_prefix)
// so the user can identify which token is which without us being
// able to re-reveal them.

// mcpTokenPlaintextPrefix is the visible literal prepended to every
// minted token. Lets a leaked token be grep'd out of logs / paste
// histories and identified as a zv MCP credential before its owner
// is contacted.
const mcpTokenPlaintextPrefix = "zvmcp_"

// mcpTokenSecretBytes is the entropy of the random suffix. 32 bytes
// (256 bits) matches session tokens; the prefix is purely for
// identification and is not counted against entropy.
const mcpTokenSecretBytes = 32

// mcpTokenPrefixDisplay is how many leading characters of the
// plaintext we keep in token_prefix for display. Long enough to
// disambiguate two tokens at a glance, short enough that the rest
// of the secret remains unbruteforceable from this stub alone.
const mcpTokenPrefixDisplay = 12

// MaxMCPTokenNameLen caps the user-chosen label. Same generous
// 5–10× headroom as other naming caps in this package.
const MaxMCPTokenNameLen = 200

// MCP token scopes. read covers list/get/validate; write covers every
// mutation, transition, and side-effecting tool. The set is closed —
// adding a new scope requires a migration default + a UI update.
const (
	MCPScopeRead  = "read"
	MCPScopeWrite = "write"
)

// validMCPScopes is the closed set the API accepts. Intentionally
// small for now (Slice 0): a coarse read/write split is enough for
// the dashboard-token use case without committing to a per-tool
// scope taxonomy we'd later have to migrate.
var validMCPScopes = map[string]bool{MCPScopeRead: true, MCPScopeWrite: true}

// normaliseMCPScopes deduplicates, validates, and sort-stabilises a
// caller-supplied scope list. Empty input falls back to the full set
// so legacy clients posting {name} keep getting a fully-powered
// token (matches the column DEFAULT). Returns an error string the
// handler can pass straight to httpx.Error.
func normaliseMCPScopes(in []string) ([]string, string) {
	if len(in) == 0 {
		return []string{MCPScopeRead, MCPScopeWrite}, ""
	}
	seen := map[string]bool{}
	for _, s := range in {
		if !validMCPScopes[s] {
			return nil, "unknown scope: " + s
		}
		seen[s] = true
	}
	// Stable canonical ordering so a token created with
	// ["write","read"] stores the same bytes as ["read","write"].
	out := make([]string, 0, len(seen))
	for _, s := range []string{MCPScopeRead, MCPScopeWrite} {
		if seen[s] {
			out = append(out, s)
		}
	}
	return out, ""
}

type mcpTokenDTO struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

type createMCPTokenReq struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

type createMCPTokenResp struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
	// Token is the plaintext. Returned ONCE; subsequent GETs only
	// surface Prefix.
	Token string `json:"token"`
}

// CreateMCPToken mints a fresh token for the calling user.
//
// POST /api/admin/me/mcp-tokens
func (h *Handlers) CreateMCPToken(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var req createMCPTokenReq
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad request body")
		return
	}
	name, err := validateName(req.Name, MaxMCPTokenNameLen)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "name "+err.Error())
		return
	}
	scopes, scopeErr := normaliseMCPScopes(req.Scopes)
	if scopeErr != "" {
		httpx.Error(w, http.StatusBadRequest, scopeErr)
		return
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "scopes encode", err)
		return
	}
	secret, err := tokenx.New(mcpTokenSecretBytes)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "token gen", err)
		return
	}
	plaintext := mcpTokenPlaintextPrefix + secret
	hash := sha256.Sum256([]byte(plaintext))
	prefix := plaintext
	if len(prefix) > mcpTokenPrefixDisplay {
		prefix = prefix[:mcpTokenPrefixDisplay]
	}

	var (
		id        int64
		createdAt string
	)
	err = h.DB.QueryRowContext(r.Context(), `
		INSERT INTO mcp_tokens (user_id, token_hash, name, token_prefix, scopes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, me.ID, hash[:], name, prefix, scopesJSON).Scan(&id, &createdAt)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "insert", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, createMCPTokenResp{
		ID:        id,
		Name:      name,
		Prefix:    prefix,
		Scopes:    scopes,
		CreatedAt: createdAt,
		Token:     plaintext,
	})
}

// ListMCPTokens returns every token the calling user has minted.
// Plaintext is never re-emitted; the response carries name + prefix
// only.
//
// GET /api/admin/me/mcp-tokens
func (h *Handlers) ListMCPTokens(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, name, token_prefix, scopes, created_at, COALESCE(last_used_at, '')
		FROM mcp_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []mcpTokenDTO{}
	for rows.Next() {
		var (
			t          mcpTokenDTO
			scopesJSON []byte
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopesJSON, &t.CreatedAt, &t.LastUsedAt); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if err := json.Unmarshal(scopesJSON, &t.Scopes); err != nil {
			// A malformed scopes column would render the whole list
			// useless; fall back to "no scopes" so the row still
			// surfaces and the user can revoke it.
			t.Scopes = []string{}
		}
		out = append(out, t)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// RevokeMCPToken deletes a single token by its id. 404 when no
// matching row belongs to the caller — same probing-resistant shape
// RevokeMySession uses.
//
// DELETE /api/admin/me/mcp-tokens/{id}
func (h *Handlers) RevokeMCPToken(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad id")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM mcp_tokens WHERE id = $1 AND user_id = $2`, id, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete check failed")
		return
	}
	if n == 0 {
		httpx.Error(w, http.StatusNotFound, "token not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// LookupMCPToken resolves a plaintext bearer token into the owning
// user and the token's scope set. Used by the MCP server's
// authenticate path; lives in this package because mcp_tokens is
// admin-side state and the password / session helpers it sits next
// to do their lookups here too.
//
// Returns ErrUnknownMCPToken when no row matches; any other error
// is a DB-side failure. On success the user row is returned in the
// same shape auth.Lookup yields and the scopes slice mirrors the
// JSONB column (canonical order, validated at mint time).
//
// last_used_at is updated best-effort (a failure to update doesn't
// fail the lookup) — the worst case is one stale "never used"
// banner in the settings UI.
func LookupMCPToken(ctx context.Context, db *sql.DB, plaintext string) (auth.User, []string, error) {
	if plaintext == "" {
		return auth.User{}, nil, ErrUnknownMCPToken
	}
	hash := sha256.Sum256([]byte(plaintext))
	var (
		uid        int64
		scopesJSON []byte
		pinned     sql.NullInt64
	)
	err := db.QueryRowContext(ctx, `
		SELECT user_id, scopes, org_id FROM mcp_tokens
		WHERE token_hash = $1 AND (expires_at IS NULL OR expires_at > now())
	`, hash[:]).Scan(&uid, &scopesJSON, &pinned)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.User{}, nil, ErrUnknownMCPToken
		}
		return auth.User{}, nil, err
	}
	var scopes []string
	if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
		// Corrupt scopes column — fail closed (treat as no scopes)
		// rather than gifting full powers on a parse hiccup.
		scopes = nil
	}
	// Best-effort touch.
	_, _ = db.ExecContext(ctx,
		`UPDATE mcp_tokens SET last_used_at = now_text() WHERE token_hash = $1`,
		hash[:])
	u, err := tokenUser(ctx, db, uid, pinned)
	if err != nil {
		return auth.User{}, nil, err
	}
	return u, scopes, nil
}

// tokenUser resolves the user a token acts as. Tokens have no session,
// so the role and organisation come from the user's membership: the
// pinned organisation's, or else their first. A pinned organisation the
// user has left is ErrUnknownMCPToken.
func tokenUser(ctx context.Context, db *sql.DB, uid int64, pinned sql.NullInt64) (auth.User, error) {

	// users has no role / org_id columns — those live on
	// org_memberships, joined per active session in auth.Lookup. MCP
	// tokens have no session, so we resolve the role ourselves: admins
	// short-circuit; everyone else gets the role + org from their
	// lowest-id membership (deterministic pick for users in multiple
	// orgs, unless the token is pinned to one).
	var (
		u                 auth.User
		scheduledDeletion sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT id, username, is_admin, COALESCE(email, ''),
		       (email_verified <> 0), scheduled_deletion_at, listing_reviewer
		FROM users WHERE id = $1
	`, uid).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Email, &u.EmailVerified, &scheduledDeletion, &u.ListingReviewer)
	if err != nil {
		return auth.User{}, err
	}
	// An account scheduled for deletion is being wound down; its tokens
	// must stop working immediately, the same way its sessions are
	// cleared when deletion is scheduled.
	if scheduledDeletion.Valid && scheduledDeletion.String != "" {
		return auth.User{}, ErrUnknownMCPToken
	}
	if u.IsAdmin {
		u.Role = auth.RoleAdmin
		return u, nil
	}
	var (
		role  sql.NullString
		orgID sql.NullInt64
	)
	// A token pinned to an organisation acts there, and only there.
	err = db.QueryRowContext(ctx, `
		SELECT role, org_id FROM org_memberships
		WHERE user_id = $1 AND ($2::bigint IS NULL OR org_id = $2)
		ORDER BY org_id ASC
		LIMIT 1
	`, uid, pinned).Scan(&role, &orgID)
	if errors.Is(err, sql.ErrNoRows) && pinned.Valid {
		return auth.User{}, ErrUnknownMCPToken
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, err
	}
	if role.Valid {
		u.Role = role.String
	} else {
		u.Role = auth.RoleLobby
	}
	if orgID.Valid {
		v := orgID.Int64
		u.OrgID = &v
	}
	return u, nil
}

// APITokenAuth lets the /api/admin REST tree accept the same tokens
// as /mcp. A request carrying "Authorization: Bearer <token>" runs as
// the token's user regardless of any session cookie; an unknown token
// is a 401, never a silent fall-back to the cookie. Scopes map onto
// HTTP methods: safe methods need read, everything else needs write.
// Requests without a bearer header pass through untouched.
func APITokenAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			u, scopes, err := LookupMCPToken(r.Context(), db, strings.TrimSpace(tok))
			if err != nil {
				if !errors.Is(err, ErrUnknownMCPToken) {
					logx.Warn("admin.api_token.lookup", "err", err.Error())
				}
				w.Header().Set("WWW-Authenticate", `Bearer realm="zv-api"`)
				httpx.Error(w, http.StatusUnauthorized, "invalid API token")
				return
			}
			need := MCPScopeWrite
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				need = MCPScopeRead
			}
			if !slices.Contains(scopes, need) {
				httpx.Error(w, http.StatusForbidden, "API token lacks "+need+" scope")
				return
			}
			ctx := auth.WithAPIToken(auth.WithUser(r.Context(), u))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ErrUnknownMCPToken is returned by LookupMCPToken when the bearer
// token doesn't match any row. Sentinel so the MCP server can map
// it to a 401 without inspecting message strings.
var ErrUnknownMCPToken = errors.New("unknown mcp token")
