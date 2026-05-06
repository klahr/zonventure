package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestCallAdmin_PropagatesContextAndParams locks in the two main
// jobs of the adapter: the user context survives the round-trip
// (so handlers reading auth.FromContext see the same caller MCP
// authenticated) and chi.URLParam resolves against the synthesised
// route context.
func TestCallAdmin_PropagatesContextAndParams(t *testing.T) {
	type ctxKey struct{}
	var seenParam, seenCtx string
	h := func(w http.ResponseWriter, r *http.Request) {
		seenParam = chi.URLParam(r, "id")
		if v, ok := r.Context().Value(ctxKey{}).(string); ok {
			seenCtx = v
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, "alice")
	status, body, err := callAdmin(ctx, h, "GET", "/api/admin/x/42",
		map[string]string{"id": "42"}, nil, nil)
	if err != nil {
		t.Fatalf("callAdmin err: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status=%d, want 200", status)
	}
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("body=%q, want ok:true", body)
	}
	if seenParam != "42" {
		t.Errorf("URL param=%q, want 42", seenParam)
	}
	if seenCtx != "alice" {
		t.Errorf("ctx value=%q, want alice", seenCtx)
	}
}

// TestCallAdmin_HeadersAndBody covers the cancel-event path: a JSON
// body, a custom header (X-Confirm-Password), and a non-2xx status
// flowing through to the caller untouched.
func TestCallAdmin_HeadersAndBody(t *testing.T) {
	var sawHeader, sawBody string
	h := func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("X-Confirm-Password")
		buf, _ := io.ReadAll(r.Body)
		sawBody = string(buf)
		http.Error(w, "wrong password", http.StatusUnauthorized)
	}

	status, body, err := callAdmin(context.Background(), h,
		"POST", "/api/admin/foo", nil,
		map[string]string{"X-Confirm-Password": "hunter2"},
		map[string]any{"event_id": 7})
	if err != nil {
		t.Fatalf("callAdmin err: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", status)
	}
	if !strings.Contains(string(body), "wrong password") {
		t.Errorf("body=%q, want wrong password", body)
	}
	if sawHeader != "hunter2" {
		t.Errorf("header=%q, want hunter2", sawHeader)
	}
	if !strings.Contains(sawBody, `"event_id":7`) {
		t.Errorf("body received by handler=%q, want event_id:7", sawBody)
	}
}

// TestAdminCallResult_StatusMapping confirms the success/error
// envelope split that tools rely on: 2xx → text result with body,
// non-2xx → error result wrapping {status, body} so the LLM sees
// both the HTTP code and the admin handler's error message.
func TestAdminCallResult_StatusMapping(t *testing.T) {
	// 200 with body
	r := adminCallResult(200, []byte(`{"events":[]}`))
	if r.IsError {
		t.Error("200 result marked as error")
	}
	if textOf(r) != `{"events":[]}` {
		t.Errorf("body=%q", textOf(r))
	}

	// 204 No Content (empty body) → synthetic ack
	r = adminCallResult(204, []byte(""))
	if r.IsError {
		t.Error("204 result marked as error")
	}
	if textOf(r) != `{"ok":true}` {
		t.Errorf("204 body=%q, want ok ack", textOf(r))
	}

	// 409 conflict → error envelope carrying status + body
	r = adminCallResult(409, []byte(`{"error":"already running"}`))
	if !r.IsError {
		t.Error("409 result not marked as error")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(textOf(r)), &got); err != nil {
		t.Fatalf("error envelope not JSON: %v body=%s", err, textOf(r))
	}
	if got["status"] != float64(409) {
		t.Errorf("envelope status=%v, want 409", got["status"])
	}
}

// helper that mirrors the registry-test ones in scope_test.go
// without relying on httptest internals.
var _ = httptest.NewRecorder // keep import live across compiles
