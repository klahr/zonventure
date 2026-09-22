package admin

// Graph authoring endpoints: load, validate, save and convert an
// adventure's structure as a graph (internal/graph).
//
//	GET  /api/admin/adventures/{slug}/graph           latest saved version
//	PUT  /api/admin/adventures/{slug}/graph           save a new version
//	POST /api/admin/adventures/{slug}/graph/validate  check without saving
//	POST /api/admin/adventures/{slug}/graph/convert   build from lanes, unsaved
//
// All four use requireEditAdventure: only someone who can edit the
// adventure may see its graph, because the graph carries every answer.
//
// Responses carry the validator's full report rather than the first
// error, so the editor can mark every problem on the canvas at once.
// Refusals use a stable "code" alongside the English "error", which is
// what a client should branch on.

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/convert"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
)

// maxGraphBodyBytes bounds a graph upload. Matches graph.ParseEnvelope's
// own cap, applied here too so an oversized body is refused before it is
// read into memory.
const maxGraphBodyBytes = 8 << 20

type graphResponse struct {
	Version  int            `json:"version,omitempty"`
	Envelope graph.Envelope `json:"envelope"`
	Report   valid.Report   `json:"report"`
}

type graphRefusal struct {
	Error  string        `json:"error"`
	Code   string        `json:"code"`
	Report *valid.Report `json:"report,omitempty"`
}

// GetAdventureGraph returns the latest saved graph with a fresh
// validation report. The report is recomputed rather than stored, so a
// graph saved under an older validator still shows current findings.
func (h *Handlers) GetAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	s, err := graphstore.Latest(r.Context(), h.DB, advID)
	if errors.Is(err, graphstore.ErrNotFound) {
		httpx.JSON(w, http.StatusNotFound, graphRefusal{
			Error: "this adventure has no graph yet", Code: "graph_not_found",
		})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, graphResponse{
		Version:  s.Version,
		Envelope: graph.Envelope{Format: graph.FormatV2, Graph: *s.Graph},
		Report:   valid.Check(s.Graph),
	})
}

// PutAdventureGraph validates and saves a new version. An invalid graph
// is refused with 422 and the full report; nothing is written.
func (h *Handlers) PutAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	env, ok := readEnvelope(w, r)
	if !ok {
		return
	}

	var userID int64
	if me, ok := auth.FromContext(r.Context()); ok {
		userID = me.ID
	}

	s, report, err := graphstore.Save(r.Context(), h.DB, advID, userID, &env.Graph)
	var invalid *graphstore.InvalidError
	if errors.As(err, &invalid) {
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{
			Error: "the graph has errors and was not saved", Code: "graph_invalid", Report: &report,
		})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving graph failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, graphResponse{
		Version:  s.Version,
		Envelope: graph.Envelope{Format: graph.FormatV2, Graph: *s.Graph},
		Report:   report,
	})
}

// ValidateAdventureGraph checks a graph without saving it. This is what
// the editor calls as the author works, so problems appear on the canvas
// before they press save.
func (h *Handlers) ValidateAdventureGraph(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.graphTarget(w, r); !ok {
		return
	}
	env, ok := readEnvelope(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, graphResponse{Envelope: *env, Report: valid.Check(&env.Graph)})
}

// ConvertAdventureGraph builds a graph from the adventure's current lanes
// and pages. It does not save: the author opens the result in the editor
// and saves it themselves, so converting is never a one-way door.
//
// The lanes and pages are read through buildExport — the same code path
// export uses — so conversion sees exactly what an export would contain,
// including the legacy accept-list shapes ParseAcceptJSON tolerates.
func (h *Handlers) ConvertAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	env, _, err := h.buildExport(r.Context(), advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reading lanes failed", err)
		return
	}

	in := convert.Adventure{}
	for _, l := range env.Adventure.Lanes {
		in.Lanes = append(in.Lanes, convert.Lane{Idx: l.Idx, Mode: l.Mode, Name: l.Name})
	}
	for _, p := range env.Adventure.Pages {
		cp := convert.Page{
			Idx: p.Idx, LaneIdx: p.LaneIdx, Title: p.Title, Prompt: p.Prompt,
			Lat: p.Lat, Lng: p.Lng, RadiusM: p.RadiusM, Disabled: p.Disabled,
		}
		for _, a := range p.Accept {
			cp.Accept = append(cp.Accept, convert.Accept{Text: a.Text, Regex: a.Regex})
		}
		for _, hint := range p.Hints {
			cp.Hints = append(cp.Hints, convert.Hint{Idx: hint.Idx, Text: hint.Text, DelaySeconds: hint.DelaySeconds})
		}
		in.Pages = append(in.Pages, cp)
	}

	g := convert.ToGraph(in)
	httpx.JSON(w, http.StatusOK, graphResponse{
		Envelope: graph.Envelope{Format: graph.FormatV2, Graph: *g},
		Report:   valid.Check(g),
	})
}

// graphTarget authorises the caller against the adventure in the URL and
// returns its id.
func (h *Handlers) graphTarget(w http.ResponseWriter, r *http.Request) (int64, bool) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return 0, false
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return 0, false
	}
	return advID, true
}

func readEnvelope(w http.ResponseWriter, r *http.Request) (*graph.Envelope, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGraphBodyBytes))
	if err != nil {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, graphRefusal{
			Error: "graph is too large", Code: "graph_too_large",
		})
		return nil, false
	}
	env, err := graph.ParseEnvelope(body)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "graph_malformed"})
		return nil, false
	}
	return env, true
}
