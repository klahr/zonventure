package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// An MCP tool's confirm: true stands in for the password; a plain
// request without the header is still refused.
func TestRequirePasswordConfirmWithConfirmed(t *testing.T) {
	h := &Handlers{}
	r := httptest.NewRequest("POST", "/x", nil)
	w := httptest.NewRecorder()
	if h.requirePasswordConfirm(w, r) || w.Code != http.StatusUnauthorized {
		t.Fatalf("no header: passed or code %d, want 401", w.Code)
	}
	w = httptest.NewRecorder()
	if !h.requirePasswordConfirm(w, r.WithContext(WithConfirmed(r.Context()))) {
		t.Fatalf("confirmed call refused: %d %s", w.Code, w.Body)
	}
}
