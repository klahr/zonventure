package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/ws"
)

// Adapting a party's adventure to the party, from the host page, before
// it starts: the host's words for what the author marked as theirs to
// change (graph.HostValues, Block.HostEdit, PuzzleConfig.HostAnswer), and
// where it is played — with cards anywhere (QR mode), or at places the
// host puts in their park (park mode, for zones marked Movable). All of it
// lands in events.party_overrides and is applied when the party starts
// (play.pinParty).

type hostValueDTO struct {
	Slug    string `json:"slug"`
	Label   string `json:"label"`
	Default string `json:"default"`
	Value   string `json:"value"`
}

type hostEditableDTO struct {
	graph.HostEditable
	// Mine is the host's version, when they changed it.
	Mine *graph.BlockOverride `json:"mine,omitempty"`
	// MyAccepts is the host's answers, when they set them.
	MyAccepts []string `json:"my_accepts,omitempty"`
}

type hostPlaceDTO struct {
	Slug    string  `json:"slug"`
	Name    string  `json:"name"`
	Movable bool    `json:"movable"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM float64 `json:"radius_m"`
	// Placed says the host has put it; Lat and Lng are then theirs.
	Placed bool `json:"placed"`
}

// parkOK reports whether a party may play g in a park: it has places,
// and the host may move every one of them.
func parkOK(g *graph.Graph) bool {
	if g == nil || len(g.Zones) == 0 {
		return false
	}
	for _, z := range g.Zones {
		if !z.Movable {
			return false
		}
	}
	return true
}

// describeAdaptation fills in the host page's view of how the adventure
// is adapted: the mode, the host's words, and the places.
func (h *Handlers) describeAdaptation(out *hostPartyDTO, p hostParty, g *graph.Graph) {
	out.Mode = p.Mode
	out.ParkOK = parkOK(g)
	out.CardsOK = g != nil && len(g.StationsProblems()) == 0
	out.Values, out.Editables, out.Places = []hostValueDTO{}, []hostEditableDTO{}, []hostPlaceDTO{}
	if g == nil {
		return
	}
	for _, v := range g.HostValues {
		out.Values = append(out.Values, hostValueDTO{Slug: v.Slug, Label: v.Label, Default: v.Default, Value: p.Overrides.Values[v.Slug]})
	}
	for _, e := range g.HostEditables() {
		d := hostEditableDTO{HostEditable: e}
		if b, ok := p.Overrides.Blocks[e.Key]; ok {
			d.Mine = &b
		}
		if a, ok := p.Overrides.Answers[e.Key]; ok {
			d.MyAccepts = a
		}
		out.Editables = append(out.Editables, d)
	}
	for _, z := range g.Zones {
		d := hostPlaceDTO{Slug: z.Slug, Name: z.Name, Movable: z.Movable, Lat: z.Lat, Lng: z.Lng, RadiusM: z.RadiusM}
		if d.Name == "" || z.Hidden {
			d.Name = z.Slug
		}
		if pl, ok := p.Overrides.Zones[z.Slug]; ok && z.Movable {
			d.Lat, d.Lng, d.Placed = pl.Lat, pl.Lng, true
			d.RadiusM = max(d.RadiusM, graph.MinPartyZoneRadius)
		}
		out.Places = append(out.Places, d)
	}
}

// hostAdaptable is the start of every change to how the party is
// adapted: the party, and the graph a change is checked against. Once
// the party has started its graph is set, so nothing changes any more.
func (h *Handlers) hostAdaptable(w http.ResponseWriter, r *http.Request) (hostParty, *graph.Graph, bool) {
	p, ok := h.partyByHostToken(w, r)
	if !ok {
		return p, nil, false
	}
	if !h.partyOpen(w, p) {
		return p, nil, false
	}
	if p.State != "scheduled" {
		httpx.Error(w, http.StatusConflict, "the party has started: it plays as it was set up")
		return p, nil, false
	}
	g, err := h.hostGraph(r.Context(), p)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "graph lookup failed", err)
		return p, nil, false
	}
	if g == nil {
		httpx.Error(w, http.StatusConflict, "the adventure is not ready; contact the organiser")
		return p, nil, false
	}
	return p, g, true
}

func (h *Handlers) saveOverrides(ctx context.Context, eventID int64, o graph.PartyOverrides) error {
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = h.DB.ExecContext(ctx, `UPDATE events SET party_overrides = $1 WHERE id = $2`, raw, eventID)
	if err == nil {
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	return err
}

type personaliseRequest struct {
	Values  map[string]string              `json:"values"`
	Blocks  map[string]graph.BlockOverride `json:"blocks"`
	Answers map[string][]string            `json:"answers"`
}

// PutHostPersonalise saves the host's words: their values, questions and
// answers, replacing what they saved before. Empty takes the author's.
//
// PUT /api/party/{token}/personalise
func (h *Handlers) PutHostPersonalise(w http.ResponseWriter, r *http.Request) {
	p, g, ok := h.hostAdaptable(w, r)
	if !ok {
		return
	}
	var req personaliseRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	o := p.Overrides
	o.Values, o.Blocks, o.Answers = map[string]string{}, map[string]graph.BlockOverride{}, map[string][]string{}
	for k, v := range req.Values {
		if v = strings.TrimSpace(v); v != "" {
			o.Values[k] = v
		}
	}
	for k, b := range req.Blocks {
		b.Text = strings.TrimSpace(b.Text)
		if b.Text != "" || len(b.Options) > 0 {
			o.Blocks[k] = b
		}
	}
	for k, as := range req.Answers {
		if strings.TrimSpace(strings.Join(as, "")) != "" {
			o.Answers[k] = as
		}
	}
	if err := o.Check(g); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.saveOverrides(r.Context(), p.EventID, o); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PutHostMode chooses where the party is played: "qr", with cards
// anywhere, or "park", at places outdoors (only when the host may move
// every one of them).
//
// PUT /api/party/{token}/mode
func (h *Handlers) PutHostMode(w http.ResponseWriter, r *http.Request) {
	p, g, ok := h.hostAdaptable(w, r)
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	switch {
	case req.Mode == "park" && !parkOK(g):
		httpx.Error(w, http.StatusConflict, "this adventure can't be moved to a park; play it with cards")
		return
	case req.Mode == "qr" && len(g.StationsProblems()) > 0:
		httpx.Error(w, http.StatusConflict, "this adventure can't be played with cards")
		return
	case req.Mode != "qr" && req.Mode != "park":
		httpx.Error(w, http.StatusBadRequest, "mode is qr or park")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE events SET party_mode = $1 WHERE id = $2`, req.Mode, p.EventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.Hub.Publish(ws.EventTopic(p.EventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}

// PutHostPlace puts one of the adventure's movable places where the host
// stands — or anywhere they say — for a party in a park.
//
// PUT /api/party/{token}/places/{slug}
func (h *Handlers) PutHostPlace(w http.ResponseWriter, r *http.Request) {
	p, g, ok := h.hostAdaptable(w, r)
	if !ok {
		return
	}
	var at graph.ZonePlace
	if !httpx.DecodeJSON(w, r, &at) {
		return
	}
	o := p.Overrides
	if o.Zones == nil {
		o.Zones = map[string]graph.ZonePlace{}
	}
	o.Zones[chi.URLParam(r, "slug")] = at
	if err := o.Check(g); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.saveOverrides(r.Context(), p.EventID, o); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PostHostShift moves every movable place at once, keeping the shape the
// author drew: the places' middle lands where the host says. A start the
// host then walks and corrects place by place — a shifted layout can
// land a place in a road or a lake.
//
// POST /api/party/{token}/places/shift
func (h *Handlers) PostHostShift(w http.ResponseWriter, r *http.Request) {
	p, g, ok := h.hostAdaptable(w, r)
	if !ok {
		return
	}
	var to graph.ZonePlace
	if !httpx.DecodeJSON(w, r, &to) {
		return
	}
	var lat, lng float64
	var n int
	for _, z := range g.Zones {
		if z.Movable {
			lat, lng, n = lat+z.Lat, lng+z.Lng, n+1
		}
	}
	if n == 0 {
		httpx.Error(w, http.StatusConflict, "the adventure has no places to move")
		return
	}
	dLat, dLng := to.Lat-lat/float64(n), to.Lng-lng/float64(n)
	o := p.Overrides
	o.Zones = map[string]graph.ZonePlace{}
	for _, z := range g.Zones {
		if z.Movable {
			o.Zones[z.Slug] = graph.ZonePlace{Lat: z.Lat + dLat, Lng: z.Lng + dLng}
		}
	}
	if err := o.Check(g); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.saveOverrides(r.Context(), p.EventID, o); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// errPlacesMissing refuses to start a party in a park with a place the
// host has not put yet: it would be wherever the author drew it.
var errPlacesMissing = errors.New("put every place in your park before starting")

// checkReadyToStart is what a party needs from how it is adapted before
// it can start.
func (h *Handlers) checkReadyToStart(ctx context.Context, p hostParty) error {
	if p.Mode != "park" {
		return nil
	}
	g, err := h.hostGraph(ctx, p)
	if err != nil || g == nil {
		return err
	}
	for _, z := range g.Zones {
		if _, ok := p.Overrides.Zones[z.Slug]; z.Movable && !ok {
			return errPlacesMissing
		}
	}
	return nil
}
