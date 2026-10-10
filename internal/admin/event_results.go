package admin

// The event's tamper-evident results record (internal/results), checked.
//
//	GET /api/admin/events/{eventID}/results/verify   the event's organisers, platform admins

import (
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/results"
)

// VerifyEventResults recomputes an event's results record: every
// entry's hash and link, and each team's result now against the one
// recorded. Its fingerprint is what the event's teams' receipts lead to.
func (h *Handlers) VerifyEventResults(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if me, _ := auth.FromContext(r.Context()); !me.IsAdmin {
		if _, ok := h.authorizeEvent(w, r, eventID); !ok {
			return
		}
	}
	rep, err := results.Verify(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "results check failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, rep)
}
