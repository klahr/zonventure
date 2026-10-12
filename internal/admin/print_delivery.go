package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/printmat"
)

// printEmailLinks are the event's emailed print materials as a team's
// email links them: by the team's token, so they download without
// signing in. A failure to read them leaves them out rather than holding
// up the email.
func (h *Handlers) printEmailLinks(ctx context.Context, eventID int64, publicURL, teamToken string) []emailtpl.FileLink {
	if publicURL == "" || teamToken == "" {
		return nil
	}
	g, o, err := printmat.ForEvent(ctx, h.DB, eventID)
	if err != nil {
		logx.Warn("admin.print.links", "event", eventID, "err", err.Error())
		return nil
	}
	var links []emailtpl.FileLink
	for _, pm := range printmat.Effective(g, o) {
		if pm.Delivery != graph.DeliveryEmailed {
			continue
		}
		if _, id := printmat.File(pm, "", g.Language); id > 0 {
			links = append(links, emailtpl.FileLink{Name: pm.Name, URL: publicURL + "/team/" + url.PathEscape(teamToken) + "/print/" + url.PathEscape(pm.ID)})
		}
	}
	return links
}

// eventPrintMaterial is one of an event's print materials: how the
// adventure delivers it and how the event does.
type eventPrintMaterial struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Adventure graph.Delivery `json:"adventure_delivery"`
	Delivery  graph.Delivery `json:"delivery"`
}

// EventPrintDelivery lists the event's print materials and how each is
// delivered: the adventure version it plays decides what there is, the
// event how it reaches the buyer.
//
// GET /api/admin/events/{eventID}/print-delivery
func (h *Handlers) EventPrintDelivery(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	h.writePrintDelivery(w, r, eventID)
}

// SetEventPrintDelivery sets how the event delivers its print materials:
// {"deliveries": {"<material id>": "posted"|"emailed"|"organiser"|""}},
// "" (or the adventure's own) going back to the adventure's.
//
// PUT /api/admin/events/{eventID}/print-delivery
func (h *Handlers) SetEventPrintDelivery(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	var req struct {
		Deliveries map[string]graph.Delivery `json:"deliveries"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	g, o, err := printmat.ForEvent(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	own := map[string]graph.Delivery{}
	if g != nil {
		for _, pm := range g.PrintMaterials {
			own[pm.ID] = pm.Delivery
		}
	}
	for id, d := range req.Deliveries {
		adventure, ok := own[id]
		if !ok || d != "" && !printmat.Valid(d) {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "unknown print material or delivery", "reason": "invalid_delivery"})
			return
		}
		if d == "" || d == adventure {
			delete(o, id)
		} else {
			o[id] = d
		}
	}
	raw, err := json.Marshal(o)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE events SET print_delivery = $1 WHERE id = $2`, raw, eventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.writePrintDelivery(w, r, eventID)
}

func (h *Handlers) writePrintDelivery(w http.ResponseWriter, r *http.Request, eventID int64) {
	g, o, err := printmat.ForEvent(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out := []eventPrintMaterial{}
	if g != nil {
		eff := printmat.Effective(g, o)
		for i, pm := range g.PrintMaterials {
			out = append(out, eventPrintMaterial{ID: pm.ID, Name: pm.Name, Adventure: pm.Delivery, Delivery: eff[i].Delivery})
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"materials": out})
}
