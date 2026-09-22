package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
)

// PreviewMarkdown is the live-preview counterpart of the goldmark
// rendering that every player-visible field goes through on egress.
// The arrange editor's textareas call this endpoint (debounced) so
// authors see exactly the HTML their adventure will emit — no
// client-side markdown library to keep in lockstep with the server.
// Any authoring-tier staff role can hit it; the endpoint only
// transforms text, it doesn't read or write the database.
func (h *Handlers) PreviewMarkdown(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"html": mdx.Render(req.Text)})
}
