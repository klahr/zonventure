package admin

import (
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/simulator"
)

/* ---------------- Race simulator control surface ---------------- */
//
// Thin HTTP wrappers over internal/simulator. Each handler resolves the
// caller's org slot and forwards. The simulator is org-scoped: each
// tenant has its own slot so arrangers can preview their adventures in
// parallel without colliding.

// callerOrgID resolves the org slot the caller's simulator request
// targets: their own org. Callers outside any org (the platform admin,
// lobby users) have no slot — a fallback to some other org would let
// them start/pause/reset a simulator inside someone else's org.
func (h *Handlers) callerOrgID(r *http.Request) (int64, error) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID != nil && *me.OrgID > 0 {
		return *me.OrgID, nil
	}
	return 0, errors.New("simulator requires an organisation; create or join one first")
}

// callerOrgIDOrFail is the boilerplate shortcut every sim handler
// opens with: resolve the org slot, or write a 400 and stop. The bool
// is "continue" — false means we already responded.
func (h *Handlers) callerOrgIDOrFail(w http.ResponseWriter, r *http.Request) (int64, bool) {
	orgID, err := h.callerOrgID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return orgID, true
}

// SimulatorStatus returns the sim stage for the caller's org
// (idle/running/paused), plus the event id + live token when one is
// active so the UI can jump straight to the live view.
func (h *Handlers) SimulatorStatus(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.callerOrgIDOrFail(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status(orgID))
}

type simStartRequest struct {
	AdventureSlug string  `json:"adventure_slug"`
	TeamCount     int     `json:"team_count"`
	SpeedMul      float64 `json:"speed_mul"`
}

// SimulatorStart kicks off a new simulation in the caller's org slot.
// Fails 409 if one is already active for that org — the caller must
// Reset first so the old run can be cleaned up deterministically.
func (h *Handlers) SimulatorStart(w http.ResponseWriter, r *http.Request) {
	var req simStartRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AdventureSlug == "" {
		httpx.Error(w, http.StatusBadRequest, "adventure_slug required")
		return
	}
	orgID, ok := h.callerOrgIDOrFail(w, r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	state, err := h.Sim.Start(r.Context(), orgID, req.AdventureSlug, req.TeamCount, req.SpeedMul, me.ID)
	if err != nil {
		// "Already active" is a state collision; the UI offers a Reset
		// in response to a 409. Everything else is a 400 — bad input,
		// missing puzzles, or DB hiccup the caller has no special
		// recovery for.
		if errors.Is(err, simulator.ErrAlreadyActive) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

// SimulatorPause freezes the caller's sim tick loop. Idempotent.
func (h *Handlers) SimulatorPause(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.callerOrgIDOrFail(w, r)
	if !ok {
		return
	}
	if err := h.Sim.Pause(orgID); err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status(orgID))
}

// SimulatorResume thaws the caller's paused sim. Idempotent.
func (h *Handlers) SimulatorResume(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.callerOrgIDOrFail(w, r)
	if !ok {
		return
	}
	if err := h.Sim.Resume(orgID); err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status(orgID))
}

// SimulatorReset stops the caller's tick loop and deletes every row
// the sim created in their org. Idempotent — resetting from idle is
// a no-op.
func (h *Handlers) SimulatorReset(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.callerOrgIDOrFail(w, r)
	if !ok {
		return
	}
	if err := h.Sim.Reset(r.Context(), orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reset failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status(orgID))
}
