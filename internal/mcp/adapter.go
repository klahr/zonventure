package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
)

// callAdmin invokes an admin handler in-process, synthesising the
// HTTP plumbing the handler expects. Returns the captured status
// code and response body so the calling tool can re-marshal them
// into an MCP envelope.
//
// The adapter exists because every admin handler is written against
// (http.ResponseWriter, *http.Request) and reaches for chi.URLParam
// to recover routing params; rather than refactoring 30+ handlers
// to expose pure-data helpers, the adapter gives the MCP tools a
// thin shim into the same path the web UI uses. Policy, validation
// and side-effects (webhooks, activity, WS publishes) all run
// exactly once at the admin layer.
//
// Inputs:
//   - ctx     — must already carry an authenticated auth.User
//     (set by Server.serve via auth.WithUser); the handler reads
//     it via auth.FromContext for permission checks.
//   - handler — the admin method to invoke, e.g. s.Admin.CreateEvent.
//   - method  — HTTP verb. Most handlers branch on body presence,
//     not method, so this is mostly cosmetic but kept for clarity.
//   - path    — request URL path. Used by the metrics middleware
//     and any handler that inspects r.URL; safe to pass the route
//     pattern with the URL params already substituted.
//   - urlParams — chi URL parameters. The adapter loads these into
//     a chi.RouteContext so chi.URLParam(r, "k") resolves at the
//     handler. Pass nil when the handler doesn't read URL params.
//   - headers — extra request headers (e.g. X-Confirm-Password for
//     gated routes like CancelEvent). nil/empty is fine.
//   - body    — JSON-serialisable struct or nil. nil sends an
//     empty body; a non-nil value is json.Marshalled and content
//     type set to application/json.
func callAdmin(
	ctx context.Context,
	handler http.HandlerFunc,
	method, path string,
	urlParams map[string]string,
	headers map[string]string,
	body any,
) (int, []byte, error) {
	var br io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode body: %w", err)
		}
		br = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, br)
	req = req.WithContext(ctx)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if len(urlParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range urlParams {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Code, rec.Body.Bytes(), nil
}

// adminCallResult turns the (status, body) pair from callAdmin into
// the MCP-shaped result the model sees. 2xx becomes a successful
// tool result with the body verbatim; everything else becomes
// IsError=true with a structured "{status,body}" payload so the
// LLM can react to e.g. 409 conflicts (event already running)
// without us re-mapping every possible admin error to a string.
func adminCallResult(status int, body []byte) ToolCallResult {
	if status >= 200 && status < 300 {
		// Empty body (204 No Content) is fine — surface a tiny
		// JSON ack so the model has something to read instead of
		// an empty content block.
		if len(bytes.TrimSpace(body)) == 0 {
			return textResult(`{"ok":true}`)
		}
		return textResult(string(body))
	}
	// Compose a structured error so the model sees both the HTTP
	// status (useful for distinguishing 404 from 403 from 409)
	// and the admin handler's own error message.
	wrapped, _ := json.Marshal(map[string]any{
		"status": status,
		"body":   json.RawMessage(body),
	})
	return errorResult(string(wrapped))
}
