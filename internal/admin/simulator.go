package admin

import (
	"net/http"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
)

/* ---------------- Race simulator control surface (admin-only) ---------------- */
//
// These endpoints start, pause, resume and tear down a fake race inside
// the server. The heavy lifting lives in internal/simulator — these
// handlers are thin glue onto the admin REST surface.

// SimulatorStatus returns the current sim stage (idle/running/paused)
// plus the event id + live token when one is active, so the UI can jump
// straight to the live view for viewing.
func (h *Handlers) SimulatorStatus(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.Sim.Status())
}

type simStartRequest struct {
	AdventureSlug string  `json:"adventure_slug"`
	TeamCount     int     `json:"team_count"`
	SpeedMul      float64 `json:"speed_mul"`
}

// SimulatorStart kicks off a new simulation. Fails 409 if one is
// already active — the caller must Reset first so the old run can be
// cleaned up deterministically.
func (h *Handlers) SimulatorStart(w http.ResponseWriter, r *http.Request) {
	var req simStartRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AdventureSlug == "" {
		httpx.Error(w, http.StatusBadRequest, "adventure_slug required")
		return
	}
	me, _ := auth.FromContext(r.Context())
	state, err := h.Sim.Start(r.Context(), req.AdventureSlug, req.TeamCount, req.SpeedMul, me.ID)
	if err != nil {
		// Treat "already active" as a 409 so the UI can offer a Reset.
		if err.Error() == "a simulation is already active; reset first" {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

// SimulatorPause freezes the tick loop. Idempotent.
func (h *Handlers) SimulatorPause(w http.ResponseWriter, r *http.Request) {
	if err := h.Sim.Pause(); err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status())
}

// SimulatorResume thaws a paused sim. Idempotent.
func (h *Handlers) SimulatorResume(w http.ResponseWriter, r *http.Request) {
	if err := h.Sim.Resume(); err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status())
}

// SimulatorReset stops the tick loop and deletes every row the sim
// created. Idempotent — resetting from idle is a no-op.
func (h *Handlers) SimulatorReset(w http.ResponseWriter, r *http.Request) {
	if err := h.Sim.Reset(r.Context()); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.Sim.Status())
}
