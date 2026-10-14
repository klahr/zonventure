package admin

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/icscal"
	"github.com/klahr/zonventure/internal/logx"
)

// ExportEventInviteICS writes an RFC 5545 iCalendar file for one
// scheduled event. Same authorize-event gate as the recap PDF and
// CSV — only org members can pull invites for their own events.
//
// The actual format work lives in internal/icscal; this handler is
// the HTTP wrapper. Player magic-link recovery emails attach the
// same bytes via icscal.BuildForEvent so what the arranger
// downloads matches what participants receive.
func (h *Handlers) ExportEventInviteICS(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	evt, err := icscal.BuildForEvent(r.Context(), h.DB, h.Lib, eventID, h.eventBase(r.Context(), eventID))
	if err != nil {
		if errors.Is(err, icscal.ErrNoSchedule) {
			httpx.Error(w, http.StatusBadRequest, "event has no scheduled time")
			return
		}
		logx.Warn("admin.ics.lookup", "event", eventID, "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8; method=PUBLISH")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename=%q`, icscal.Filename(eventID)))
	if err := icscal.Write(w, evt); err != nil {
		// Headers already on the wire — partial body is the only
		// signal of truncation. Logging it lets ops correlate a
		// support ticket back to the cause.
		logx.Warn("admin.ics.render", "event", eventID, "err", err.Error())
	}
}
