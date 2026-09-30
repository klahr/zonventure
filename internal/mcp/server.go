package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/storage"
)

// Server holds the dependencies the JSON-RPC handlers need. One
// instance per zv process; safe for concurrent use (the underlying
// *sql.DB, storage.Blobs and *admin.Handlers are the only mutable
// references and each is internally goroutine-safe).
type Server struct {
	DB      *sql.DB
	Storage storage.Blobs
	// Admin is the same handler set the HTTP /api/admin routes are
	// bound to. The events / teams / observation tools dispatch to
	// these methods via the callAdmin adapter so policy lives in
	// exactly one place — no parallel implementation in the MCP
	// package, no surface drift when the admin API evolves.
	Admin   *admin.Handlers
	version string
	tools   *registry
}

// New wires up the server and registers the tool set. Tools are
// registered once at startup so tools/list is a cheap snapshot
// instead of a reflective walk.
//
// version is surfaced in the initialize handshake's serverInfo so
// MCP clients can show the same string /api/version reports. Empty
// falls back to "dev", matching buildVersion's posture.
//
// blobs is the same storage.Blobs the admin handlers use; the
// attach_image tool calls PutAdventureAvatar through it so an S3
// (or future localfs) backend gets the bytes through the same seam
// as the multipart UI flow.
//
// adminH is the live admin handler set; events / teams / observation
// tools call its methods via callAdmin. nil leaves the event-side
// tools unable to dispatch — register them anyway so the schema
// stays stable across deployments, but every call returns a
// "server misconfigured" error.
func New(db *sql.DB, version string, blobs storage.Blobs, adminH *admin.Handlers) *Server {
	if version == "" {
		version = "dev"
	}
	s := &Server{DB: db, Storage: blobs, Admin: adminH, version: version, tools: newRegistry()}
	s.registerTools()
	return s
}

// Enabled reports whether the operator has opted in. The server
// package gates mounting the route on this so a self-host install
// without ZV_MCP_ENABLED never exposes /mcp at all — an unset env
// var means "off", same posture as the metrics endpoint.
func Enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ZV_MCP_ENABLED")))
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

// Handler returns the HTTP handler that decodes JSON-RPC, resolves
// the bearer token, and dispatches to method handlers.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	user, scopes, ok := s.authenticate(r)
	if !ok {
		// Per MCP guidance, auth failure is an HTTP 401 — JSON-RPC
		// errors are reserved for protocol / method problems on an
		// already-authenticated channel.
		w.Header().Set("WWW-Authenticate", `Bearer realm="zv-mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req Request
	// 16 MiB request cap. The default 1 MiB was too tight once
	// attach_image came online: a 5 MiB image (the default per-image
	// upload cap) base64-encodes to ~6.7 MiB, so we need headroom for
	// the encoded payload + JSON envelope. Operators who raised
	// ZV_MAX_IMAGE_BYTES well past the default may need to bump this
	// further; the ceiling stays a hard cap so a runaway client can't
	// exhaust memory streaming garbage at us.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, nil, codeParseError, "parse error: "+err.Error())
		return
	}
	if req.JSONRPC != "2.0" {
		writeError(w, req.ID, codeInvalidRequest, "jsonrpc must be 2.0")
		return
	}

	ctx := auth.WithUser(r.Context(), user)
	ctx = withScopes(ctx, scopes)
	resp, suppress := s.dispatch(ctx, req)
	if suppress {
		// Notifications don't get a response body — the spec says
		// 202 Accepted with no payload.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logx.Warn("mcp.encode_response", "err", err.Error())
	}
}

// dispatch routes a single request. The bool return is true when
// the request was a notification and no response should be written.
func (s *Server) dispatch(ctx context.Context, req Request) (Response, bool) {
	isNotification := len(req.ID) == 0
	out := Response{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		out.Result = initializeResult{
			ProtocolVersion: protocolVersion,
			ServerInfo:      serverInfo{Name: "Zonventure", Version: s.version},
			Capabilities: map[string]any{
				"tools":   map[string]any{},
				"prompts": map[string]any{},
			},
			Instructions: serverInstructions,
		}
	case "notifications/initialized", "notifications/cancelled":
		// Acknowledged silently.
		return out, true
	case "ping":
		out.Result = map[string]any{}
	case "tools/list":
		out.Result = map[string]any{"tools": s.tools.list(ctx)}
	case "prompts/list":
		out.Result = map[string]any{"prompts": prompts}
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			out.Error = &RPCError{Code: codeInvalidParams, Message: "bad prompts/get params: " + err.Error()}
			break
		}
		res, ok := getPrompt(p.Name, p.Arguments)
		if !ok {
			out.Error = &RPCError{Code: codeInvalidParams, Message: "unknown prompt: " + p.Name}
			break
		}
		out.Result = res
	case "tools/call":
		var p ToolCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			out.Error = &RPCError{Code: codeInvalidParams, Message: "bad tools/call params: " + err.Error()}
			break
		}
		res, err := s.tools.call(ctx, s, p.Name, p.Arguments)
		if err != nil {
			// Distinguish "no such tool" (protocol error) from
			// "the tool ran and reported a failure" (in-band).
			if errors.Is(err, errUnknownTool) {
				out.Error = &RPCError{Code: codeMethodNotFound, Message: err.Error()}
				break
			}
			out.Result = errorResult(err.Error())
			break
		}
		out.Result = res
	default:
		out.Error = &RPCError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	if isNotification {
		return out, true
	}
	return out, false
}

// authenticate resolves the Authorization header into an auth.User
// and the token's scope set by looking the bearer token up in
// mcp_tokens. Tokens are minted via POST /api/admin/me/mcp-tokens;
// see admin.LookupMCPToken for the lookup details (sha256 of
// plaintext, best-effort last_used_at update).
//
// Returns (zero, nil, false) on every failure path — missing
// header, unknown token, DB hiccup. Successes attach the resolved
// user and scopes to the request context downstream so the registry
// can gate tool calls on scope.
func (s *Server) authenticate(r *http.Request) (auth.User, []string, bool) {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	got = strings.TrimSpace(got)
	if got == "" {
		return auth.User{}, nil, false
	}
	u, scopes, err := admin.LookupMCPToken(r.Context(), s.DB, got)
	if err != nil {
		if !errors.Is(err, admin.ErrUnknownMCPToken) {
			logx.Warn("mcp.auth.lookup", "err", err.Error())
		}
		return auth.User{}, nil, false
	}
	return u, scopes, true
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logx.Warn("mcp.encode_error", "err", err.Error())
	}
}
