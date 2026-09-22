package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
)

// SystemUpdate returns the cached changelog snapshot: running
// version, latest released version, and the markdown notes for
// every release strictly newer than the running one (rendered to
// HTML server-side). Admin-only — surfacing "what does this
// operator's binary say about itself" to org users would leak
// unrelated info to a multi-tenant deployment.
//
// Returns 503 with a stable shape when the Updates service wasn't
// wired in. The UI keys off the same Status fields either way, so
// it can render "couldn't check" without a separate code path.
func (h *Handlers) SystemUpdate(w http.ResponseWriter, r *http.Request) {
	if h.Updates == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "update checks not configured")
		return
	}
	httpx.JSON(w, http.StatusOK, h.Updates.Status())
}

// SystemUpdateCheck forces an immediate refetch and returns the
// resulting Status. Admin-triggered "Check now" button — bypasses
// the daily background poll so the operator gets a fresh answer
// without waiting. Errors are reported via Status.FetchError, not
// via a non-2xx response, so the UI can render the "couldn't check"
// hint with whatever last-good data the service still has cached.
func (h *Handlers) SystemUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if h.Updates == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "update checks not configured")
		return
	}
	status, _ := h.Updates.CheckNow(r.Context())
	httpx.JSON(w, http.StatusOK, status)
}
