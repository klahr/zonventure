package admin

// Event simulation: many bot teams play the unsaved draft at once, on
// the live engine's rules, and the designer plays the run back at any
// speed.
//
//	POST /api/admin/adventures/{slug}/simulate-event
//
// Like the single-team simulator it keeps nothing: the client sends the
// draft and the settings, and gets the whole run back. internal/sim
// does the playing.

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/httpx"
	eventsim "github.com/klahr/zonventure/internal/sim"
)

type simulateEventRequest struct {
	Envelope json.RawMessage `json:"envelope"`
	Config   eventsim.Config `json:"config"`
}

// SimulateEvent runs one simulated event.
func (h *Handlers) SimulateEvent(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.graphTarget(w, r); !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGraphBodyBytes))
	if err != nil {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, graphRefusal{Error: "request is too large", Code: "graph_too_large"})
		return
	}
	var req simulateEventRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "simulate_malformed"})
		return
	}
	env, err := graph.ParseEnvelope(req.Envelope)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "graph_malformed"})
		return
	}
	if req.Config.Start.IsZero() {
		req.Config.Start = time.Now().UTC()
	}
	if err := req.Config.Check(); err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "simulate_malformed"})
		return
	}
	res, err := eventsim.Run(&env.Graph, req.Config)
	if err != nil {
		// The draft would not let a team begin.
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{Error: err.Error(), Code: "simulate_refused"})
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}
