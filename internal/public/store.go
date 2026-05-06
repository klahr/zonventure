package public

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// StoreItemDTO is a single row on the public /store listing — one
// always-on, public-for-sale event surfaced as a card. The shape is
// deliberately narrow: nothing leaks that a buyer doesn't need to
// decide whether to buy. No live_token, no team list, no GPS.
type StoreItemDTO struct {
	EventID         int64  `json:"event_id"`
	EventName       string `json:"event_name,omitempty"`
	AdventureSlug   string `json:"adventure_slug"`
	AdventureTitle  string `json:"adventure_title"`
	OrgName         string `json:"org_name"`
	Blurb           string `json:"blurb,omitempty"`
	PriceCents      int    `json:"price_cents"`
	Currency        string `json:"currency"`
	TeamSize        int    `json:"team_size"`
	StartLabel      string `json:"start_label,omitempty"`
	StartLat        float64 `json:"start_lat,omitempty"`
	StartLng        float64 `json:"start_lng,omitempty"`
	HasStartLatLng  bool    `json:"has_start_latlng"`
	// CoverImageURL is set when the event has uploaded a hero image.
	// Empty means the store card renders text-only — the JS template
	// already has a fallback layout.
	CoverImageURL string `json:"cover_image_url,omitempty"`
}

// StoreList serves GET /api/store/items: the JSON catalogue of
// public-for-sale events. Only events in 'running' or 'paused'
// state with a non-NULL ticket_price_cents are returned. Results
// are ordered by org name then event name so a multi-tenant
// instance lists predictably.
func (h *Handlers) StoreList(w http.ResponseWriter, r *http.Request) {
	items, err := h.loadStoreItems(r.Context())
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "store list failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, struct {
		Items []StoreItemDTO `json:"items"`
	}{items})
}

// StoreEvent serves GET /api/store/items/{event_id}: a single store
// listing fetched by event id, used by the buy page when the buyer
// lands on a deep link. Returns 404 when the event is not currently
// for sale (so a stale link doesn't expose past prices).
func (h *Handlers) StoreEvent(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "event_id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	item, err := h.loadStoreItem(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "store event failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, item)
}

const storeItemSelect = `
	SELECT e.id, e.name, e.adventure_slug,
	       COALESCE(a.name, e.adventure_slug) AS adv_title,
	       o.name AS org_name,
	       e.public_blurb,
	       e.ticket_price_cents,
	       e.ticket_currency,
	       e.ticket_team_size,
	       a.start_label, a.start_lat, a.start_lng,
	       (e.cover_image_mime_type IS NOT NULL) AS has_cover
	FROM events e
	JOIN organizations o ON o.id = e.org_id
	LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
`

// storeItemRowFilter is the WHERE clause shared by listing + single-
// item lookup. A row is "for sale right now" when the event is
// publicly listed, has a non-NULL price, and is in a state where
// fresh tickets make sense — running or paused. 'pending',
// 'scheduled', 'completed', 'cancelled' are deliberately excluded.
const storeItemRowFilter = `
	WHERE e.public_for_sale
	  AND e.ticket_price_cents IS NOT NULL
	  AND e.state IN ('running', 'paused')
`

func (h *Handlers) loadStoreItems(ctx context.Context) ([]StoreItemDTO, error) {
	rows, err := h.DB.QueryContext(ctx,
		storeItemSelect+storeItemRowFilter+`
		ORDER BY o.name, e.name, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoreItemDTO
	for rows.Next() {
		item, err := scanStoreItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (h *Handlers) loadStoreItem(ctx context.Context, eventID int64) (StoreItemDTO, error) {
	row := h.DB.QueryRowContext(ctx,
		storeItemSelect+storeItemRowFilter+` AND e.id = $1`, eventID)
	return scanStoreItem(row)
}

// rowScanner is the subset of *sql.Row / *sql.Rows scanStoreItem
// uses, so the same code can be shared between list + detail paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanStoreItem(s rowScanner) (StoreItemDTO, error) {
	var (
		item       StoreItemDTO
		eventName  sql.NullString
		blurb      sql.NullString
		startLabel sql.NullString
		startLat   sql.NullFloat64
		startLng   sql.NullFloat64
		priceCents sql.NullInt64
		hasCover   bool
	)
	if err := s.Scan(
		&item.EventID, &eventName, &item.AdventureSlug, &item.AdventureTitle,
		&item.OrgName, &blurb, &priceCents, &item.Currency, &item.TeamSize,
		&startLabel, &startLat, &startLng, &hasCover,
	); err != nil {
		return StoreItemDTO{}, err
	}
	if hasCover {
		item.CoverImageURL = "/store-cover/event/" + strconv.FormatInt(item.EventID, 10)
	}
	if eventName.Valid {
		item.EventName = strings.TrimSpace(eventName.String)
	}
	if blurb.Valid {
		item.Blurb = strings.TrimSpace(blurb.String)
	}
	if priceCents.Valid {
		item.PriceCents = int(priceCents.Int64)
	}
	if startLabel.Valid {
		item.StartLabel = strings.TrimSpace(startLabel.String)
	}
	if startLat.Valid && startLng.Valid {
		item.StartLat = startLat.Float64
		item.StartLng = startLng.Float64
		item.HasStartLatLng = true
	}
	return item, nil
}
