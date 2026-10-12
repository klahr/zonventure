package admin

import (
	"database/sql"
	"net/http"

	"github.com/klahr/zonventure/internal/httpx"
)

// eventVersion is which adventure version a self-guided event plays,
// which it is moving to, and the adventure's latest published one.
type eventVersion struct {
	// Version is what new teams start on; 0 before the event starts,
	// when it starts on the latest.
	Version int `json:"version"`
	// Next is the version it moves to once its listing is approved.
	Next int `json:"next,omitempty"`
	// Latest is the adventure's latest published version.
	Latest int `json:"latest"`
	// CanUpdate says it plays an older version than the latest and isn't
	// already moving to it.
	CanUpdate bool `json:"can_update"`
	// Reviewed says a move waits for the listing's approval: a self-guided
	// event for sale. One that isn't moves at once.
	Reviewed      bool   `json:"reviewed"`
	ListingReview string `json:"listing_review"`
}

func (h *Handlers) readEventVersion(r *http.Request, eventID int64) (eventVersion, bool, error) {
	var (
		v                   eventVersion
		forSale             bool
		version, next, last sql.NullInt64
		review              sql.NullString
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT e.public_for_sale, e.listing_review,
		       (SELECT version FROM adventure_graphs WHERE id = e.graph_id),
		       (SELECT version FROM adventure_graphs WHERE id = e.next_graph_id),
		       (SELECT version FROM adventure_graphs WHERE id = a.published_graph_id)
		FROM events e LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE e.id = $1`, eventID).Scan(&forSale, &review, &version, &next, &last)
	if err != nil {
		return v, false, err
	}
	v.Version, v.Next, v.Latest = int(version.Int64), int(next.Int64), int(last.Int64)
	v.ListingReview = review.String
	v.Reviewed = review.Valid
	v.CanUpdate = forSale && version.Valid && last.Valid && v.Latest != v.Version && v.Latest != v.Next
	return v, forSale, nil
}

// EventVersion says which adventure version a self-guided event plays
// and whether a newer one is published.
//
// GET /api/admin/events/{eventID}/version
func (h *Handlers) EventVersion(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	v, _, err := h.readEventVersion(r, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// UpdateEventVersion moves a running self-guided event to its adventure's
// latest published version, such as one with new print materials. One
// for sale moves once its listing is approved, submitted like any other
// change to it (at once for a verified creator): until then new teams
// play the version last reviewed. One not for sale, which nothing
// reviews, moves at once, as starting an event pins a version. Teams
// already playing keep the version they started on.
//
// POST /api/admin/events/{eventID}/version/update
func (h *Handlers) UpdateEventVersion(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	v, forSale, err := h.readEventVersion(r, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	refuse := func(code, msg string) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": msg, "code": code})
	}
	switch {
	case !forSale:
		refuse("not_self_guided", "only a self-guided event moves to a newer version")
		return
	case v.Version == 0:
		refuse("not_started", "the event starts on the latest version")
		return
	case v.Latest == 0:
		refuse("not_published", "the adventure has no published version")
		return
	case !v.CanUpdate:
		refuse("up_to_date", "the event already plays, or is moving to, the latest version")
		return
	}
	if v.Reviewed {
		_, err = h.DB.ExecContext(r.Context(), `
			UPDATE events e SET next_graph_id = a.published_graph_id
			FROM adventures a WHERE a.slug = e.adventure_slug AND e.id = $1`, eventID)
		if err == nil {
			err = h.settleEventListing(r.Context(), eventID)
		}
	} else {
		_, err = h.DB.ExecContext(r.Context(), `
			UPDATE events e SET graph_id = a.published_graph_id, next_graph_id = NULL
			FROM adventures a WHERE a.slug = e.adventure_slug AND e.id = $1`, eventID)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	v, _, err = h.readEventVersion(r, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}
