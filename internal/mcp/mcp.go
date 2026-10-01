// Package mcp implements a Model Context Protocol server so an LLM
// client (Claude desktop / Claude.ai connectors) can author zv
// adventures via tool calls instead of the arranger UI.
//
// Transport: streamable HTTP. The client POSTs JSON-RPC 2.0 requests
// to a single endpoint (mounted as /mcp by the server package). For
// the tools shipped here a single JSON response per request is
// sufficient — no SSE streaming required.
//
// Auth: Bearer token in the Authorization header. The token is
// resolved into an auth.User and the rest of the request runs with
// the same authoring policy as a logged-in arranger — so an MCP
// caller can only see / write adventures their org already owns.
//
// Disabled by default. Self-host operators opt in via
// ZV_MCP_ENABLED=1. Exposing remote tool execution is exactly the
// kind of surface that should be off until the operator asks for it.
package mcp

import (
	"context"
	"encoding/json"
)

// protocolVersion is the MCP wire version the server advertises
// during initialize. The client sends its own version too; we echo
// back whatever it asked for if we recognise it, otherwise we pin
// to this baseline. Bump when adopting newer spec features.
const protocolVersion = "2025-06-18"

// Request is the inbound JSON-RPC envelope. ID is omitted on
// notifications (initialized, etc.) — when absent we MUST NOT send
// a response.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is the success / error envelope. Result and Error are
// mutually exclusive — exactly one is populated.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC error object. Code uses the standard
// codes (-32600 invalid request, -32601 method not found, -32602
// invalid params, -32603 internal error) for protocol-level
// problems; tool-level failures bubble up inside a successful
// tools/call response with isError=true (per MCP convention) so
// the model can react.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// initializeResult is what the server returns to the client's
// initialize call. capabilities advertises only what we actually
// support — tools and prompts — to keep the surface honest.
// Instructions is how to use the server, which clients put in the
// model's context (serverInstructions).
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      serverInfo     `json:"serverInfo"`
	Capabilities    map[string]any `json:"capabilities"`
	Instructions    string         `json:"instructions,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolDescriptor is the public schema of one tool, as returned
// from tools/list. InputSchema is a JSON Schema object — clients
// validate arguments against it before calling.
//
// Scope is server-side metadata (json:"-") used to gate which
// tokens may invoke this tool — see scopeRead / scopeWrite. Reads
// are tools that don't mutate DB state and don't trigger side
// effects (email, webhooks); writes are everything else. The
// registry refuses to register a tool with an empty Scope so a
// new tool can't accidentally land without classification.
//
// Audience is who may call the tool at all, by the token owner's role:
// the zero value is org users (authoring and running events), which
// the platform admin may not call; audienceAdmin is the platform
// operator only; audienceAny is everyone. tools/list only shows a
// caller the tools of their audience.
type ToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Scope       string         `json:"-"`
	Audience    string         `json:"-"`
}

// Audience identifiers for ToolDescriptor.Audience.
const (
	audienceOrg   = ""
	audienceAdmin = "admin"
	audienceAny   = "any"
)

// Scope identifiers — kept in sync with admin.MCPScope* constants.
// Defined here too so internal code (registry, tools) doesn't need
// to import admin just to tag a descriptor.
const (
	scopeRead  = "read"
	scopeWrite = "write"
)

// scopesCtxKey is the private key under which serve() stores the
// authenticated token's scope set. The registry pulls it back out
// at call time so handlers don't need a parallel parameter.
type scopesCtxKey struct{}

// withScopes attaches a token's scope set to the request context.
// nil scopes (i.e. malformed DB row, fail-closed in LookupMCPToken)
// produce a non-nil empty slice so callers can rely on the value
// always being non-nil after this returns.
func withScopes(ctx context.Context, scopes []string) context.Context {
	if scopes == nil {
		scopes = []string{}
	}
	return context.WithValue(ctx, scopesCtxKey{}, scopes)
}

// scopesFromContext retrieves the authenticated token's scopes.
// Returns an empty slice when no scopes were attached (e.g. unit
// tests calling the registry directly without going through serve).
func scopesFromContext(ctx context.Context) []string {
	v, _ := ctx.Value(scopesCtxKey{}).([]string)
	return v
}

// scopesAllow reports whether a token with `have` scopes may invoke
// a tool requiring `need`. A token's scope set is interpreted as
// pure capabilities — no implication relationships (a write-only
// token cannot call read tools). UI defaults check both boxes so
// this only bites a user who deliberately narrowed their token.
func scopesAllow(have []string, need string) bool {
	for _, s := range have {
		if s == need {
			return true
		}
	}
	return false
}

// ToolCallParams is the params payload for tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult is the MCP-shaped response the model sees. Content
// is rendered into the conversation; isError flips the model into
// "the call failed, decide whether to retry" mode without breaking
// the JSON-RPC envelope.
type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is one chunk of tool output: "text", mostly JSON the
// model reads, or an "image" — base64 Data of MimeType — for a tool
// that shows the model what a player sees (play.go).
type ToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// textResult is shorthand for "wrap a string into the standard
// single-text-chunk shape". Most tools encode their structured
// output as a JSON string and pass it here.
func textResult(s string) ToolCallResult {
	return ToolCallResult{Content: []ToolContent{{Type: "text", Text: s}}}
}

// multiTextResult emits one ToolCallResult with several text blocks
// in order. Used when a tool wants to surface a human-readable
// rendering alongside the canonical JSON envelope (get_adventure):
// MCP clients render text blocks as markdown, so the markdown view
// shows up nicely in the tool-result panel while the model still
// gets the JSON for round-tripping. Empty strings are skipped so a
// missing rendering doesn't produce an empty content block.
func multiTextResult(blocks ...string) ToolCallResult {
	out := make([]ToolContent, 0, len(blocks))
	for _, b := range blocks {
		if b == "" {
			continue
		}
		out = append(out, ToolContent{Type: "text", Text: b})
	}
	return ToolCallResult{Content: out}
}

// errorResult flags the call as failed but still returns a 200
// JSON-RPC response — the model needs to see the error text to
// self-correct on the next turn.
func errorResult(msg string) ToolCallResult {
	return ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
