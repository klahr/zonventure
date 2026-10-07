package admin

import (
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
)

// GetLegal serves Settings → Legal's values. Admin-only at the route level.
func (h *Handlers) GetLegal(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, settings.CurrentLegal())
}

// SetLegal saves every legal value at once; the pages, the seller line
// and the retention sweepers use them from the next request or run. A
// save that changes what the pages print moves Last updated to today,
// unless the admin set the date themselves. Logged with who saved.
// Admin-only at the route level.
func (h *Handlers) SetLegal(w http.ResponseWriter, r *http.Request) {
	var l settings.Legal
	if !httpx.DecodeJSON(w, r, &l) {
		return
	}
	if err := l.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	before := settings.CurrentLegal()
	printed := func(x settings.Legal) settings.Legal {
		x.Reviewed, x.LastUpdated = false, ""
		return x
	}
	if printed(l) != printed(before) && l.LastUpdated == before.LastUpdated {
		l.LastUpdated = time.Now().UTC().Format("2006-01-02")
	}
	if err := settings.SetLegal(r.Context(), h.DB, l); err != nil {
		logx.Error("admin.legal.set_failed", "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "could not save the legal settings")
		return
	}
	me, _ := auth.FromContext(r.Context())
	logx.Info("admin.legal", "by", me.Username, "before", before, "after", l)
	httpx.JSON(w, http.StatusOK, settings.CurrentLegal())
}
