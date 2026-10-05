package player

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// A dev team plays the adventure's draft as it is edited (admin's dev
// tickets). Its phone shows a DEV bar: the steps of the draft to jump
// to — back to one played, or on to one not reached — and a start over.

// PlayDev is set only for a dev team.
type PlayDev struct {
	Steps []DevStep `json:"steps"`
}

// DevStep is a step a dev team may be put on, in the draft's order.
type DevStep struct {
	Slug  string `json:"slug"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type"`
}

// devInfo returns the dev bar's steps for a dev event, or nil for a real one.
func devInfo(ctx context.Context, db *sql.DB, eventID int64) (*PlayDev, error) {
	var (
		dev     bool
		graphID sql.NullInt64
	)
	if err := db.QueryRowContext(ctx, `SELECT dev, graph_id FROM events WHERE id = $1`, eventID).Scan(&dev, &graphID); err != nil || !dev || !graphID.Valid {
		return nil, err
	}
	s, err := graphstore.ByID(ctx, db, graphID.Int64)
	if err != nil {
		return nil, err
	}
	out := &PlayDev{Steps: []DevStep{}}
	for _, n := range s.Graph.Nodes {
		if play.Visible(n.Type) {
			out.Steps = append(out.Steps, DevStep{Slug: n.Slug, Title: n.Title, Type: string(n.Type)})
		}
	}
	return out, nil
}

// DevGoTo serves POST /api/player/dev/goto {node}: a dev team's phone
// moves itself to a step of the draft, to play it afresh.
func (h *Handlers) DevGoTo(w http.ResponseWriter, r *http.Request) {
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var req struct {
		Node string `json:"node"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	h.devMove(w, slot.EventID, h.Engine.DevMoveTo(r.Context(), slot.TeamID, req.Node))
}

// DevReset serves POST /api/player/dev/reset: a dev team starts over.
func (h *Handlers) DevReset(w http.ResponseWriter, r *http.Request) {
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "not signed in")
		return
	}
	h.devMove(w, slot.EventID, h.Engine.DevReset(r.Context(), slot.TeamID))
}

func (h *Handlers) devMove(w http.ResponseWriter, eventID int64, err error) {
	switch {
	case errors.Is(err, play.ErrNotDev):
		httpx.Error(w, http.StatusForbidden, "only a dev ticket can move about the adventure")
		return
	case errors.Is(err, play.ErrNoSuchStep):
		httpx.Error(w, http.StatusBadRequest, "no such step to show")
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "dev move failed", err)
		return
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}
