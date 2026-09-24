package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/httpx"
)

// ErrGraphAdventure refuses a lane edit to an adventure that has a
// published graph. Its events play the graph, so its lane pages are
// never shown again: an edit to them would look saved and change nothing.
var ErrGraphAdventure = errors.New("this adventure is designed as steps: edit it in the designer")

// RefuseIfGraph returns ErrGraphAdventure when the adventure has a
// published graph. The MCP lane tools call it; LaneOnly wraps the routes.
func RefuseIfGraph(ctx context.Context, db *sql.DB, advID int64) error {
	var published bool
	if err := db.QueryRowContext(ctx, `SELECT published_graph_id IS NOT NULL FROM adventures WHERE id = $1`, advID).Scan(&published); err != nil {
		return err
	}
	if published {
		return ErrGraphAdventure
	}
	return nil
}

// LaneOnly guards a lane-editing route: pages, lanes, hints and their
// attachments. It finds the adventure from the route's slug, pageID or
// hintID and refuses with 409 when it is a graph adventure. A route with
// none of them passes; the handler's own checks still apply.
func (h *Handlers) LaneOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		advID, err := h.laneRouteAdventure(r)
		if err == nil && advID != 0 {
			err = RefuseIfGraph(r.Context(), h.DB, advID)
		}
		switch {
		case errors.Is(err, ErrGraphAdventure):
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "graph_adventure"})
			return
		case errors.Is(err, sql.ErrNoRows):
			// Not found is the handler's to say, in its own words.
		case err != nil:
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		next(w, r)
	}
}

func (h *Handlers) laneRouteAdventure(r *http.Request) (int64, error) {
	ctx := r.Context()
	var id int64
	id64 := func(name string) (int64, bool) {
		v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
		return v, err == nil && v > 0
	}
	switch {
	case chi.URLParam(r, "slug") != "":
		return id, h.DB.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, chi.URLParam(r, "slug")).Scan(&id)
	case chi.URLParam(r, "pageID") != "":
		p, ok := id64("pageID")
		if !ok {
			return 0, nil
		}
		// A library puzzle has no adventure, so nothing to refuse.
		var adv sql.NullInt64
		err := h.DB.QueryRowContext(ctx, `SELECT adventure_id FROM adventure_pages WHERE id = $1`, p).Scan(&adv)
		return adv.Int64, err
	case chi.URLParam(r, "hintID") != "":
		hint, ok := id64("hintID")
		if !ok {
			return 0, nil
		}
		var adv sql.NullInt64
		err := h.DB.QueryRowContext(ctx, `
			SELECT p.adventure_id FROM adventure_page_hints hi JOIN adventure_pages p ON p.id = hi.page_id WHERE hi.id = $1`, hint).Scan(&adv)
		return adv.Int64, err
	}
	return 0, nil
}
