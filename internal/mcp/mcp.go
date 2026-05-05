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

import "encoding/json"

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
// support — tools/list and tools/call — to keep the surface honest.
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      serverInfo     `json:"serverInfo"`
	Capabilities    map[string]any `json:"capabilities"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolDescriptor is the public schema of one tool, as returned
// from tools/list. InputSchema is a JSON Schema object — clients
// validate arguments against it before calling.
type ToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
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

// ToolContent is one chunk of tool output. We only emit "text" for
// now — image/audio variants exist in the spec but tool results
// here are JSON the model reads, not media.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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
