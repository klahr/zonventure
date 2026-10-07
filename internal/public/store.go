package public

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/httpx"
)

// StoreItemDTO is a single row on the public /store listing — one
// always-on, public-for-sale event surfaced as a card. The shape is
// deliberately narrow: nothing leaks that a buyer doesn't need to
// decide whether to buy. No live_token, no team list, no GPS.
type StoreItemDTO struct {
	EventID        int64   `json:"event_id"`
	EventName      string  `json:"event_name,omitempty"`
	AdventureSlug  string  `json:"adventure_slug"`
	AdventureTitle string  `json:"adventure_title"`
	OrgName        string  `json:"org_name"`
	Blurb          string  `json:"blurb,omitempty"`
	PriceCents     int     `json:"price_cents"`
	Currency       string  `json:"currency"`
	TeamSize       int     `json:"team_size"`
	StartLabel     string  `json:"start_label,omitempty"`
	StartLat       float64 `json:"start_lat,omitempty"`
	StartLng       float64 `json:"start_lng,omitempty"`
	HasStartLatLng bool    `json:"has_start_latlng"`
	// CoverImageURL is the event's uploaded hero image, else its look's cover.
	// Empty means the store card renders text-only — the JS template
	// already has a fallback layout.
	CoverImageURL string `json:"cover_image_url,omitempty"`
	// AdventureIconURL is set when the adventure has an icon — the one
	// its players see in their top bar.
	AdventureIconURL string `json:"adventure_icon_url,omitempty"`
	// Look is the adventure's title typeface and cover picture. The
	// cover also stands in for CoverImageURL when the event has none.
	Look *PageLook `json:"look,omitempty"`
}

// TicketTeamTokenDTO is the response shape for the post-purchase
// poll endpoint. The buyer's browser hits the endpoint after Stripe
// redirects back to /store?paid={id}&session_id={cs_...}; the JS
// flips on `state == "paid"` and forwards to /team/{team_token} so
// the buyer never has to leave the page to find their team link.
//
//	{ "state": "pending" }                               — webhook hasn't fired yet
//	{ "state": "paid", "team_token": "abc123…" }         — ready to redirect
//	{ "state": "failed" }                                — Stripe declined / cancelled-then-failed
type TicketTeamTokenDTO struct {
	State     string `json:"state"`
	TeamToken string `json:"team_token,omitempty"`
}

// TicketTeamToken is the post-purchase handoff endpoint. Verifies
// that the supplied session_id matches the one we recorded for the
// purchase before returning the team token, so a guess at the
// purchase id from a third party can't surface someone else's
// team-share URL.
//
// 404 on:
//   - purchase id doesn't exist
//   - session_id query param missing
//   - session_id doesn't match the recorded stripe_session_id
//
// Returning 404 (rather than 403) on the auth failure is
// deliberate: indistinguishable from "no such purchase" so a
// scraper can't tell whether they hit a real id with a wrong
// session or just a bogus id.
//
// Cache-Control: no-store because the JSON contains a security
// token and the body changes once between webhook arrival and
// the buyer's next poll.
func (h *Handlers) TicketTeamToken(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "purchase_id")
	purchaseID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || purchaseID <= 0 {
		http.NotFound(w, r)
		return
	}
	suppliedSession := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if suppliedSession == "" {
		http.NotFound(w, r)
		return
	}

	var (
		state      string
		recordedSI sql.NullString
		teamToken  sql.NullString
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.state, p.stripe_session_id, t.token
		FROM ticket_purchases p
		LEFT JOIN teams t ON t.id = p.team_id
		WHERE p.id = $1
	`, purchaseID).Scan(&state, &recordedSI, &teamToken)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !recordedSI.Valid || recordedSI.String == "" || recordedSI.String != suppliedSession {
		// Session id doesn't match (or wasn't recorded yet — the
		// webhook handler stamps it via COALESCE, so a brief race
		// where the buyer polls before checkout.completed lands
		// produces the same 404 the unauthorised case does).
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	resp := TicketTeamTokenDTO{State: state}
	if state == "paid" && teamToken.Valid {
		resp.TeamToken = teamToken.String
	}
	httpx.JSON(w, http.StatusOK, resp)
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

// StoreListByOrg serves GET /api/o/{slug}/store/items — same shape
// as StoreList but filtered to one organization. Used by the per-org
// store page (zonventure.com/o/{slug}/store) so a single company's
// page only lists their quests, not every multi-tenant neighbour.
// 404s if the slug doesn't resolve.
func (h *Handlers) StoreListByOrg(w http.ResponseWriter, r *http.Request) {
	orgID, _, err := h.resolveOrgSlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "org lookup failed", err)
		return
	}
	items, err := h.loadStoreItemsByOrg(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "store list failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, struct {
		Items []StoreItemDTO `json:"items"`
	}{items})
}

// StoreEventByOrg serves GET /api/o/{slug}/store/items/{event_id}.
// Refuses (404) when the event exists but belongs to a different
// org — prevents a deep link from leaking another org's content
// through the per-org URL.
func (h *Handlers) StoreEventByOrg(w http.ResponseWriter, r *http.Request) {
	orgID, _, err := h.resolveOrgSlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "org lookup failed", err)
		return
	}
	idStr := chi.URLParam(r, "event_id")
	id, perr := strconv.ParseInt(idStr, 10, 64)
	if perr != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	item, err := h.loadStoreItemByOrg(r.Context(), orgID, id)
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

// resolveOrgSlug maps a URL slug to (id, name). Returns sql.ErrNoRows
// when the slug doesn't exist so the caller can branch on 404 cleanly.
func (h *Handlers) resolveOrgSlug(ctx context.Context, slug string) (int64, string, error) {
	slug = strings.TrimSpace(slug)
	// A NUL byte or invalid UTF-8 can't match a stored slug, but handed
	// to Postgres it errors the query rather than returning no rows —
	// which callers would surface as a 500. Fail closed to "not found".
	if slug == "" || strings.IndexByte(slug, 0) >= 0 || !utf8.ValidString(slug) {
		return 0, "", sql.ErrNoRows
	}
	var (
		id   int64
		name string
	)
	err := h.DB.QueryRowContext(ctx,
		`SELECT id, name FROM organizations WHERE slug = $1`, slug).Scan(&id, &name)
	return id, name, err
}

const storeItemSelect = `
	SELECT e.id, e.name, e.adventure_slug,
	       COALESCE(a.title, e.adventure_slug) AS adv_title,
	       o.name AS org_name,
	       e.public_blurb,
	       e.ticket_price_cents,
	       e.ticket_currency,
	       e.ticket_team_size,
	       a.start_label, a.start_lat, a.start_lng,
	       (e.cover_image_mime_type IS NOT NULL) AS has_cover,
	       COALESCE(a.avatar_mime_type IS NOT NULL, FALSE) AS has_icon,` + pageLookColumns + `
	FROM events e
	JOIN organizations o ON o.id = e.org_id
	LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
`

// storeItemRowFilterBase is the always-on portion of the listing
// WHERE clause. A row is "for sale right now" when the event is
// publicly listed, has a non-NULL price, and is in a state where
// fresh tickets make sense — running or paused. 'pending',
// 'scheduled', 'completed', 'cancelled' are deliberately excluded.
const storeItemRowFilterBase = `
	WHERE e.public_for_sale
	  AND e.ticket_price_cents IS NOT NULL
	  AND e.state IN ('running', 'paused')
`

// storeItemRowFilter returns the WHERE clause to use given the
// platform-fee config. When a platform fee is configured (= the
// hosted instance's marketplace mode), the listing also requires
// the org to have completed Stripe Connect onboarding so the
// purchase flow can actually take a payment. Self-host (no fee,
// no marketplace) renders the base filter only.
func (h *Handlers) storeItemRowFilter() string {
	if h.platformFee().Enabled() {
		return storeItemRowFilterBase + `
		  AND o.stripe_connect_charges_enabled
		`
	}
	return storeItemRowFilterBase
}

func (h *Handlers) loadStoreItem(ctx context.Context, eventID int64) (StoreItemDTO, error) {
	row := h.DB.QueryRowContext(ctx,
		storeItemSelect+h.storeItemRowFilter()+` AND e.id = $1`, eventID)
	return h.scanStoreItem(row)
}

// loadStoreItemsByOrg lists one organisation's store items. The
// extra `e.org_id = $1` predicate piggybacks on storeItemRowFilter so
// the platform-fee + Stripe-Connect gates still apply (a self-host
// org without Connect onboarding would still surface here because
// the fee is disabled; on hosted, the join still requires onboarding).
func (h *Handlers) loadStoreItemsByOrg(ctx context.Context, orgID int64) ([]StoreItemDTO, error) {
	rows, err := h.DB.QueryContext(ctx,
		storeItemSelect+h.storeItemRowFilter()+`
		AND e.org_id = $1
		ORDER BY e.name, e.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoreItemDTO
	for rows.Next() {
		item, err := h.scanStoreItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// loadStoreItemByOrg is loadStoreItem with a cross-org guard: when
// the event_id matches a different org the row filter excludes it,
// so the caller sees sql.ErrNoRows rather than another org's data.
func (h *Handlers) loadStoreItemByOrg(ctx context.Context, orgID, eventID int64) (StoreItemDTO, error) {
	row := h.DB.QueryRowContext(ctx,
		storeItemSelect+h.storeItemRowFilter()+` AND e.id = $1 AND e.org_id = $2`,
		eventID, orgID)
	return h.scanStoreItem(row)
}

// rowScanner is the subset of *sql.Row / *sql.Rows scanStoreItem
// uses, so the same code can be shared between list + detail paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func (h *Handlers) scanStoreItem(s rowScanner) (StoreItemDTO, error) {
	var (
		item       StoreItemDTO
		eventName  sql.NullString
		blurb      sql.NullString
		startLabel sql.NullString
		startLat   sql.NullFloat64
		startLng   sql.NullFloat64
		priceCents sql.NullInt64
		hasCover   bool
		hasIcon    bool
		lookFont   string
		lookTheme  string
		ownCover   bool
	)
	if err := s.Scan(
		&item.EventID, &eventName, &item.AdventureSlug, &item.AdventureTitle,
		&item.OrgName, &blurb, &priceCents, &item.Currency, &item.TeamSize,
		&startLabel, &startLat, &startLng, &hasCover, &hasIcon,
		&lookFont, &lookTheme, &ownCover,
	); err != nil {
		return StoreItemDTO{}, err
	}
	item.Look = h.Looks.pageLook(item.AdventureSlug, lookFont, lookTheme, ownCover)
	if hasCover {
		item.CoverImageURL = "/store-cover/event/" + strconv.FormatInt(item.EventID, 10)
	} else if item.Look != nil {
		item.CoverImageURL = item.Look.CoverURL
	}
	if hasIcon {
		item.AdventureIconURL = "/adventure-assets/avatar/" + url.PathEscape(item.AdventureSlug)
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
