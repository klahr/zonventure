package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/components"
	"github.com/klahr/zonventure/internal/httpx"
)

// ListComponents returns the build's wired-in plugin seams — the
// mailer / payments / storage / metrics impls picked at compile time
// plus their enabled/disabled state. Read-only: the GUI surfaces this
// as an observability panel, not a configurator. Admin-only because
// it's operator-level information and shouldn't leak which providers
// are installed to org-scoped users.
func (h *Handlers) ListComponents(w http.ResponseWriter, r *http.Request) {
	var out []components.Component
	if h.Components != nil {
		out = h.Components()
	}
	if out == nil {
		out = []components.Component{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"components": out})
}
