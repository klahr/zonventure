package admin

// Listing review: what an organisation puts up for sale goes public
// only once a listing reviewer approves it (migrations 0123, 0124): a
// person, or an AI assistant's account, a platform admin made one. No
// one reviews their own organisation's listings. Its
// point is to keep offensive content off the platform, so a review
// covers the listing, the adventure version it plays and the
// organisation's page (listing_content.go).
//
//	POST /api/admin/events/{eventID}/listing/submit           {attest}  the event's org
//	POST /api/admin/party-offers/{slug}/submit                {attest}  an org admin
//	GET  /api/admin/listing-reviewers                         who may review       platform admins
//	POST /api/admin/listing-reviewers                         {username, reviewer} platform admins
//	GET  /api/admin/listing-reviews                           what waits for review  (listing reviewers from here)
//	GET  /api/admin/listing-reviews/log                       recent submissions and decisions
//	GET  /api/admin/listing-reviews/log/verify                recompute its hash chain
//	GET  /api/admin/listing-reviews/{kind}/{id}               everything it puts in front of people
//	GET  /api/admin/listing-reviews/{kind}/{id}/media/{ref}   one picture, sound or file of it
//	POST /api/admin/listing-reviews/{kind}/{id}/approve       {revision}
//	POST /api/admin/listing-reviews/{kind}/{id}/reject        {revision, reason}
//	POST /api/admin/organizations/{orgID}/verified-creator    {verified}
//
// kind is "event" (a self-guided event's store listing) or "party" (a
// party offer). The organisation edits the draft; the public sees the
// approved copy (event_listings, party_offer_listings), which also pins
// the adventure version players get.
//
// A draft's state (listing_review): NULL when there is nothing to sell,
// draft when it differs from what is approved, pending once submitted,
// approved when it is what is up, rejected after a reviewer turned it
// down. After every change to a draft the handler calls
// settleEventListing / settlePartyOffer; submitting takes the creator's
// word that the content follows the terms. A verified creator's
// submission is approved at once.
//
// A decision names the revision the reviewer saw, a hash of the draft
// and the adventure version: when either changed since, it is refused,
// so nothing goes public that nobody looked at. Every submission and
// decision is logged (listing_review_log), with whether an AI reviewer
// made it through an API token.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/listing"
	"github.com/klahr/zonventure/internal/logx"
)

const (
	listingEvent = "event"
	listingParty = "party"
)

// MaxRejectReasonLen caps what a reviewer writes back.
const MaxRejectReasonLen = 1000

// eventListingRevision and partyListingRevision hash a draft and the
// adventure version it plays: what a reviewer approves. They read from
// events e, or party_offers o joined to adventures a. The listing's
// translations are appended outside row(), so a draft without any keeps
// the revision it had before listings had them.
const eventListingRevision = `md5(row(e.name, e.public_blurb, e.ticket_price_cents, e.ticket_currency, e.ticket_team_size,
	e.cover_image_mime_type, md5(e.cover_image_content), COALESCE(e.next_graph_id, e.graph_id),
	(SELECT string_agg(f.id::text || ':' || md5(f.content), ',' ORDER BY f.id) FROM event_files f WHERE f.event_id = e.id))::text
	|| CASE WHEN e.translations = '{}'::jsonb THEN '' ELSE e.translations::text END)`

// The offer's name is appended outside row(), so a draft without one
// keeps the revision it had before offers had names.
const partyListingRevision = `md5(row(o.blurb, o.base_cents, o.per_kid_cents, o.currency, o.max_kids,
	a.published_graph_id)::text || o.name)`

// eventUnchanged and partyUnchanged say a draft is what is approved.
const eventUnchanged = `COALESCE(l.event_id IS NOT NULL
	AND l.name = e.name AND l.blurb = e.public_blurb AND l.translations = e.translations
	AND l.price_cents = e.ticket_price_cents AND l.currency = e.ticket_currency
	AND l.team_size = e.ticket_team_size
	AND l.cover_mime_type IS NOT DISTINCT FROM e.cover_image_mime_type
	AND l.cover_content IS NOT DISTINCT FROM e.cover_image_content
	AND l.graph_id IS NOT DISTINCT FROM COALESCE(e.next_graph_id, e.graph_id)
	AND NOT EXISTS (SELECT 1 FROM event_files f WHERE f.event_id = e.id AND f.approved_at IS NULL), FALSE)`

const partyUnchanged = `COALESCE(l.offer_id IS NOT NULL
	AND l.name = o.name AND l.blurb = o.blurb AND l.base_cents = o.base_cents
	AND l.per_kid_cents = o.per_kid_cents AND l.currency = o.currency
	AND l.max_kids = o.max_kids
	AND l.graph_id IS NOT DISTINCT FROM a.published_graph_id, FALSE)`

var (
	errNothingToSubmit = errors.New("nothing has changed since it was approved")
	errNotForSale      = errors.New("set a price, or turn the offer on, before submitting it")
	errStillRejected   = errors.New("change what was rejected before submitting it again")
	errNoAttestation   = errors.New("confirm the content follows the terms to submit it")
)

/* ---------------- drafts ---------------- */

// settleEventListing sets an event's listing state after its draft
// changed. Only a self-guided event (public_for_sale) has one.
func (h *Handlers) settleEventListing(ctx context.Context, eventID int64) error {
	return h.settleEvent(ctx, eventID, true)
}

// settleEvent sets the state; edited says the draft was just changed,
// which withdraws a submission and lifts a rejection.
func (h *Handlers) settleEvent(ctx context.Context, eventID int64, edited bool) error {
	var forSale, priced, unchanged bool
	err := h.DB.QueryRowContext(ctx, `
		SELECT e.public_for_sale, e.ticket_price_cents IS NOT NULL, `+eventUnchanged+`
		FROM events e LEFT JOIN event_listings l ON l.event_id = e.id
		WHERE e.id = $1`, eventID).Scan(&forSale, &priced, &unchanged)
	if err != nil || !forSale {
		return err
	}
	_, err = h.DB.ExecContext(ctx, settleSQL("events", unchanged, priced, edited), eventID)
	return err
}

// settlePartyOffer sets a party offer's listing state after it was
// saved. A disabled offer is off the page and has nothing to review.
func (h *Handlers) settlePartyOffer(ctx context.Context, offerID int64) error {
	return h.settleParty(ctx, offerID, true)
}

func (h *Handlers) settleParty(ctx context.Context, offerID int64, edited bool) error {
	var enabled, unchanged bool
	err := h.DB.QueryRowContext(ctx, `
		SELECT o.enabled, `+partyUnchanged+`
		FROM party_offers o
		JOIN adventures a ON a.slug = o.adventure_slug
		LEFT JOIN party_offer_listings l ON l.offer_id = o.id
		WHERE o.id = $1`, offerID).Scan(&enabled, &unchanged)
	if err != nil {
		return err
	}
	_, err = h.DB.ExecContext(ctx, settleSQL("party_offers", unchanged, enabled, edited), offerID)
	return err
}

// settleSQL is the state a draft lands in: approved when it is what is
// up (an edit undone), nothing to review when it can't be sold, else a
// draft. After an edit that holds even for one pending, since what was
// submitted changed, and one rejected. Otherwise (submitting) a pending
// or rejected draft keeps its state: a rejection stands until the draft
// changes.
func settleSQL(table string, unchanged, sellable, edited bool) string {
	switch {
	case unchanged:
		return `UPDATE ` + table + ` SET listing_review = 'approved', listing_reject_reason = ''
			WHERE id = $1 AND listing_review IS DISTINCT FROM 'approved'`
	case !sellable:
		return `UPDATE ` + table + ` SET listing_review = NULL WHERE id = $1`
	case edited:
		return `UPDATE ` + table + ` SET listing_review = 'draft' WHERE id = $1`
	}
	return `UPDATE ` + table + ` SET listing_review = 'draft'
		WHERE id = $1 AND (listing_review IS NULL OR listing_review = 'approved')`
}

/* ---------------- submitting ---------------- */

type submitListingRequest struct {
	// Attest is the creator's word that the listing and the adventure it
	// plays contain nothing offensive, hateful or illegal and follow the
	// terms.
	Attest bool `json:"attest"`
}

// SubmitEventListing sends a self-guided event's listing for review.
//
// POST /api/admin/events/{eventID}/listing/submit
func (h *Handlers) SubmitEventListing(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	h.submitListing(w, r, listingEvent, eventID)
}

// SubmitPartyOffer sends a party offer for review. Org admins only, as
// for saving one.
//
// POST /api/admin/party-offers/{slug}/submit
func (h *Handlers) SubmitPartyOffer(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins submit party offers")
		return
	}
	var offerID int64
	err := h.DB.QueryRowContext(r.Context(), `SELECT id FROM party_offers WHERE org_id = $1 AND adventure_slug = $2`,
		*me.OrgID, strings.TrimSpace(chi.URLParam(r, "slug"))).Scan(&offerID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "save the offer first")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "offer lookup failed", err)
		return
	}
	h.submitListing(w, r, listingParty, offerID)
}

func (h *Handlers) submitListing(w http.ResponseWriter, r *http.Request, kind string, id int64) {
	ctx := r.Context()
	me, _ := auth.FromContext(ctx)
	var req submitListingRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !req.Attest {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": errNoAttestation.Error(), "code": "attest_required"})
		return
	}
	state, err := h.submit(ctx, kind, id, me.ID, viaOf(ctx))
	if err != nil {
		code := map[error]string{errNothingToSubmit: "unchanged", errNotForSale: "not_for_sale", errStillRejected: "unchanged_since_rejected"}[err]
		if code == "" {
			httpx.ServerErr(w, http.StatusInternalServerError, "submit failed", err)
			return
		}
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": code})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"listing_review": state})
}

// submit records the creator's word and sends the draft for review, or
// approves it for a verified creator. It returns the state it lands in.
func (h *Handlers) submit(ctx context.Context, kind string, id, userID int64, via string) (string, error) {
	t := listingTables(kind)
	var err error
	switch kind {
	case listingEvent:
		err = h.settleEvent(ctx, id, false)
	case listingParty:
		err = h.settleParty(ctx, id, false)
	}
	if err != nil {
		return "", err
	}
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var (
		orgID           int64
		verified        bool
		state, revision string
		graphID         sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT `+t.alias+`.org_id, g.verified_creator, COALESCE(`+t.alias+`.listing_review, ''), `+t.revision+`, `+t.graph+`
		FROM `+t.from+` JOIN organizations g ON g.id = `+t.alias+`.org_id
		WHERE `+t.alias+`.id = $1 FOR UPDATE OF `+t.lock, id).Scan(&orgID, &verified, &state, &revision, &graphID); err != nil {
		return "", err
	}
	switch state {
	case "approved":
		return "", errNothingToSubmit
	case "":
		return "", errNotForSale
	case "rejected":
		return "", errStillRejected
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE `+t.table+` SET listing_review = 'pending', listing_submitted_at = now(), listing_reject_reason = '',
		       listing_attested_at = now(), listing_attested_by = $2
		WHERE id = $1`, id, userID); err != nil {
		return "", err
	}
	if err := h.logListing(ctx, tx, kind, id, orgID, "submitted", via, userID, revision, graphID, "", decisionSignature{}); err != nil {
		return "", err
	}
	state = "pending"
	if verified {
		if err := h.approve(ctx, tx, kind, id, 0); err != nil {
			return "", err
		}
		if err := h.logListing(ctx, tx, kind, id, orgID, "approved", "auto", 0, revision, graphID, "", decisionSignature{}); err != nil {
			return "", err
		}
		state = "approved"
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if !verified {
		h.notifyListingSubmitted(ctx, orgID)
	}
	return state, nil
}

// listingTable is how the SQL of one kind reads its draft.
type listingTable struct {
	table, alias, from, revision, graph string
	// lock is what a decision locks: the draft, and for a party the
	// adventure, whose published version it approves.
	lock string
}

func listingTables(kind string) listingTable {
	if kind == listingOrg || kind == listingAdventure {
		return listingTable{table: "presentation_proposals", alias: "p", from: "presentation_proposals p",
			revision: presentationRevision, graph: "NULL::bigint", lock: "p"}
	}
	if kind == listingParty {
		return listingTable{table: "party_offers", alias: "o",
			from:     "party_offers o JOIN adventures a ON a.slug = o.adventure_slug",
			revision: partyListingRevision, graph: "a.published_graph_id", lock: "o, a"}
	}
	return listingTable{table: "events", alias: "e", from: "events e", revision: eventListingRevision, graph: "COALESCE(e.next_graph_id, e.graph_id)", lock: "e"}
}

// viaOf is how a request reached the platform: through an API token
// (an AI assistant over MCP) or the console.
func viaOf(ctx context.Context) string {
	if auth.ViaAPIToken(ctx) {
		return "mcp"
	}
	return "console"
}

// execer is what the approve helpers write through: the pool or a tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (h *Handlers) approve(ctx context.Context, q execer, kind string, id, reviewer int64) error {
	if kind == listingOrg || kind == listingAdventure {
		return h.approveProposal(ctx, q, id, reviewer)
	}
	if kind == listingEvent {
		return h.approveEventListing(ctx, q, id, reviewer)
	}
	return h.approvePartyListing(ctx, q, id, reviewer)
}

// approveEventListing copies an event's draft over its public listing,
// with the adventure version it plays: the one it asked to move to, when
// it asked, which teams then start on. reviewer 0 is an automatic
// approval (a verified creator).
func (h *Handlers) approveEventListing(ctx context.Context, q execer, eventID, reviewer int64) error {
	by := sql.NullInt64{Int64: reviewer, Valid: reviewer > 0}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO event_listings (event_id, name, blurb, translations, price_cents, currency, team_size, cover_mime_type, cover_content, graph_id, approved_at, approved_by)
		SELECT id, name, public_blurb, translations, ticket_price_cents, ticket_currency, ticket_team_size,
		       cover_image_mime_type, cover_image_content, COALESCE(next_graph_id, graph_id), now(), $2
		FROM events WHERE id = $1 AND ticket_price_cents IS NOT NULL
		ON CONFLICT (event_id) DO UPDATE SET
		    name = EXCLUDED.name, blurb = EXCLUDED.blurb, translations = EXCLUDED.translations, price_cents = EXCLUDED.price_cents,
		    currency = EXCLUDED.currency, team_size = EXCLUDED.team_size,
		    cover_mime_type = EXCLUDED.cover_mime_type, cover_content = EXCLUDED.cover_content,
		    graph_id = EXCLUDED.graph_id, approved_at = EXCLUDED.approved_at, approved_by = EXCLUDED.approved_by
	`, eventID, by); err != nil {
		return err
	}
	// The files reviewed with it reach players.
	if _, err := q.ExecContext(ctx, `
		UPDATE event_files SET approved_at = now() WHERE event_id = $1 AND approved_at IS NULL`, eventID); err != nil {
		return err
	}
	// Teams that haven't made a move yet play the approved version.
	if err := unplaceIdleTeams(ctx, q, eventID); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `
		UPDATE events SET listing_review = 'approved', listing_reviewed_at = now(),
		       listing_reviewed_by = $2, listing_reject_reason = '',
		       graph_id = COALESCE(next_graph_id, graph_id), next_graph_id = NULL
		WHERE id = $1`, eventID, by)
	return err
}

// approvePartyListing copies a party offer's draft over its listing,
// pinning its adventure's published version: what booked parties play.
func (h *Handlers) approvePartyListing(ctx context.Context, q execer, offerID, reviewer int64) error {
	by := sql.NullInt64{Int64: reviewer, Valid: reviewer > 0}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO party_offer_listings (offer_id, name, blurb, base_cents, per_kid_cents, currency, max_kids, graph_id, approved_at, approved_by)
		SELECT o.id, o.name, o.blurb, o.base_cents, o.per_kid_cents, o.currency, o.max_kids, a.published_graph_id, now(), $2
		FROM party_offers o JOIN adventures a ON a.slug = o.adventure_slug WHERE o.id = $1
		ON CONFLICT (offer_id) DO UPDATE SET
		    name = EXCLUDED.name, blurb = EXCLUDED.blurb, base_cents = EXCLUDED.base_cents, per_kid_cents = EXCLUDED.per_kid_cents,
		    currency = EXCLUDED.currency, max_kids = EXCLUDED.max_kids, graph_id = EXCLUDED.graph_id,
		    approved_at = EXCLUDED.approved_at, approved_by = EXCLUDED.approved_by
	`, offerID, by); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `
		UPDATE party_offers SET listing_review = 'approved', listing_reviewed_at = now(),
		       listing_reviewed_by = $2, listing_reject_reason = ''
		WHERE id = $1`, offerID, by)
	return err
}

/* ---------------- the review queue ---------------- */

// listingFieldsDTO is what a listing shows the public. Price is the
// event's ticket price, or a party's base price.
type listingFieldsDTO struct {
	Name        string `json:"name,omitempty"`
	Blurb       string `json:"blurb"`
	PriceCents  int    `json:"price_cents"`
	PerKidCents int    `json:"per_kid_cents,omitempty"`
	Currency    string `json:"currency"`
	TeamSize    int    `json:"team_size,omitempty"`
	MaxKids     int    `json:"max_kids,omitempty"`
	CoverURL    string `json:"cover_url,omitempty"`
	// GraphID is the adventure version it plays.
	GraphID int64 `json:"graph_id,omitempty"`
	// Translations are its name, blurb and meeting place in other
	// languages.
	Translations listing.Translations `json:"translations,omitempty"`
}

// listingReviewDTO is one listing waiting for review. Live is what the
// public sees now, when an edit of a live listing waits.
type listingReviewDTO struct {
	Kind           string            `json:"kind"`
	ID             int64             `json:"id"`
	Revision       string            `json:"revision"`
	SubmittedAt    string            `json:"submitted_at"`
	OrgID          int64             `json:"org_id"`
	OrgName        string            `json:"org_name"`
	OrgSlug        string            `json:"org_slug,omitempty"`
	AdventureSlug  string            `json:"adventure_slug"`
	AdventureTitle string            `json:"adventure_title"`
	AttestedBy     string            `json:"attested_by,omitempty"`
	Draft          listingFieldsDTO  `json:"draft"`
	Live           *listingFieldsDTO `json:"live,omitempty"`
}

const isoTime = `'YYYY-MM-DD"T"HH24:MI:SS"Z"'`

// ListListingReviews lists what waits for review, oldest first, but
// for the reviewer's own organisations'. Listing reviewers only.
func (h *Handlers) ListListingReviews(w http.ResponseWriter, r *http.Request) {
	me, ok := listingReader(w, r)
	if !ok {
		return
	}
	out, err := h.pendingListings(r.Context(), me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"reviews": out})
}

func (h *Handlers) pendingListings(ctx context.Context, reviewerID int64) ([]listingReviewDTO, error) {
	out := []listingReviewDTO{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT e.id, `+eventListingRevision+`, to_char(e.listing_submitted_at AT TIME ZONE 'UTC', `+isoTime+`),
		       g.id, g.name, COALESCE(g.slug, ''), e.adventure_slug, COALESCE(NULLIF(a.title, ''), e.adventure_slug),
		       COALESCE(u.username, ''),
		       e.name, e.public_blurb, COALESCE(e.ticket_price_cents, 0), e.ticket_currency, e.ticket_team_size,
		       e.cover_image_mime_type IS NOT NULL, COALESCE(e.next_graph_id, e.graph_id, 0),
		       l.event_id IS NOT NULL, COALESCE(l.name, ''), COALESCE(l.blurb, ''), COALESCE(l.price_cents, 0),
		       COALESCE(l.currency, ''), COALESCE(l.team_size, 0), l.cover_mime_type IS NOT NULL, COALESCE(l.graph_id, 0)
		FROM events e
		JOIN organizations g ON g.id = e.org_id
		LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
		LEFT JOIN event_listings l ON l.event_id = e.id
		LEFT JOIN users u ON u.id = e.listing_attested_by
		WHERE e.listing_review = 'pending' AND NOT EXISTS (SELECT 1 FROM org_memberships m WHERE m.user_id = $1 AND m.org_id = g.id)
		ORDER BY e.listing_submitted_at, e.id
	`, reviewerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			d                  listingReviewDTO
			live               listingFieldsDTO
			draftCover, isLive bool
			liveCover          bool
		)
		if err := rows.Scan(&d.ID, &d.Revision, &d.SubmittedAt, &d.OrgID, &d.OrgName, &d.OrgSlug,
			&d.AdventureSlug, &d.AdventureTitle, &d.AttestedBy,
			&d.Draft.Name, &d.Draft.Blurb, &d.Draft.PriceCents, &d.Draft.Currency, &d.Draft.TeamSize, &draftCover, &d.Draft.GraphID,
			&isLive, &live.Name, &live.Blurb, &live.PriceCents, &live.Currency, &live.TeamSize, &liveCover, &live.GraphID); err != nil {
			rows.Close()
			return nil, err
		}
		d.Kind = listingEvent
		if draftCover {
			d.Draft.CoverURL = reviewMediaURL(listingEvent, d.ID, "event-cover")
		}
		if isLive {
			if liveCover {
				live.CoverURL = reviewMediaURL(listingEvent, d.ID, "event-cover-live")
			}
			d.Live = &live
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = h.DB.QueryContext(ctx, `
		SELECT o.id, `+partyListingRevision+`, to_char(o.listing_submitted_at AT TIME ZONE 'UTC', `+isoTime+`),
		       g.id, g.name, COALESCE(g.slug, ''), o.adventure_slug, COALESCE(NULLIF(a.title, ''), o.adventure_slug),
		       COALESCE(u.username, ''),
		       o.name, o.blurb, o.base_cents, o.per_kid_cents, o.currency, o.max_kids, COALESCE(a.published_graph_id, 0),
		       l.offer_id IS NOT NULL, COALESCE(l.name, ''), COALESCE(l.blurb, ''), COALESCE(l.base_cents, 0), COALESCE(l.per_kid_cents, 0),
		       COALESCE(l.currency, ''), COALESCE(l.max_kids, 0), COALESCE(l.graph_id, 0)
		FROM party_offers o
		JOIN organizations g ON g.id = o.org_id
		JOIN adventures a ON a.slug = o.adventure_slug
		LEFT JOIN party_offer_listings l ON l.offer_id = o.id
		LEFT JOIN users u ON u.id = o.listing_attested_by
		WHERE o.listing_review = 'pending' AND NOT EXISTS (SELECT 1 FROM org_memberships m WHERE m.user_id = $1 AND m.org_id = g.id)
		ORDER BY o.listing_submitted_at, o.id
	`, reviewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			d      listingReviewDTO
			live   listingFieldsDTO
			isLive bool
		)
		if err := rows.Scan(&d.ID, &d.Revision, &d.SubmittedAt, &d.OrgID, &d.OrgName, &d.OrgSlug,
			&d.AdventureSlug, &d.AdventureTitle, &d.AttestedBy,
			&d.Draft.Name, &d.Draft.Blurb, &d.Draft.PriceCents, &d.Draft.PerKidCents, &d.Draft.Currency, &d.Draft.MaxKids, &d.Draft.GraphID,
			&isLive, &live.Name, &live.Blurb, &live.PriceCents, &live.PerKidCents, &live.Currency, &live.MaxKids, &live.GraphID); err != nil {
			return nil, err
		}
		d.Kind = listingParty
		if isLive {
			d.Live = &live
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return h.pendingProposals(ctx, reviewerID, out)
}

// pendingProposals adds the organisation pages and adventure
// presentations waiting for review.
func (h *Handlers) pendingProposals(ctx context.Context, reviewerID int64, out []listingReviewDTO) ([]listingReviewDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT p.kind, p.id, `+presentationRevision+`, to_char(p.listing_submitted_at AT TIME ZONE 'UTC', `+isoTime+`),
		       g.id, g.name, COALESCE(g.slug, ''), COALESCE(a.slug, ''), COALESCE(NULLIF(a.title, ''), a.slug, ''),
		       COALESCE(u.username, '')
		FROM presentation_proposals p
		JOIN organizations g ON g.id = p.org_id
		LEFT JOIN adventures a ON p.kind = 'adventure' AND a.id = p.target_id
		LEFT JOIN users u ON u.id = p.listing_attested_by
		WHERE p.listing_review = 'pending' AND NOT EXISTS (SELECT 1 FROM org_memberships m WHERE m.user_id = $1 AND m.org_id = g.id)
		ORDER BY p.listing_submitted_at, p.id`, reviewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d listingReviewDTO
		if err := rows.Scan(&d.Kind, &d.ID, &d.Revision, &d.SubmittedAt, &d.OrgID, &d.OrgName, &d.OrgSlug,
			&d.AdventureSlug, &d.AdventureTitle, &d.AttestedBy); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// listingLogDTO is one submission or decision.
type listingLogDTO struct {
	Kind      string `json:"kind"`
	ID        int64  `json:"id"`
	OrgID     int64  `json:"org_id"`
	OrgName   string `json:"org_name"`
	Title     string `json:"title"`
	Action    string `json:"action"`
	Via       string `json:"via"`
	By        string `json:"by,omitempty"`
	Reason    string `json:"reason,omitempty"`
	GraphID   int64  `json:"graph_id,omitempty"`
	CreatedAt string `json:"created_at"`
	// ContentHash fingerprints what was reviewed; EntryHash is the
	// entry's link in the hash chain (listing_review_chain.go).
	ContentHash string `json:"content_hash"`
	EntryHash   string `json:"entry_hash"`
	// Signed: a reviewer's passkey signed the decision.
	Signed bool `json:"signed"`
}

// ListListingLog lists recent submissions and decisions, newest first:
// for spot-checks of what an AI reviewer or a verified creator let
// through. ?via=mcp narrows it to decisions made through API tokens.
// Listing reviewers only.
func (h *Handlers) ListListingLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := listingReader(w, r); !ok {
		return
	}
	via := r.URL.Query().Get("via")
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT l.kind, l.item_id, COALESCE(l.org_id, 0), l.org_name,
		       COALESCE(CASE l.kind
		         WHEN 'org' THEN l.org_name
		         WHEN 'adventure' THEN (SELECT COALESCE(NULLIF(a.title, ''), a.slug) FROM presentation_proposals p
		               JOIN adventures a ON a.id = p.target_id WHERE p.id = l.item_id)
		         WHEN 'event' THEN (SELECT COALESCE(NULLIF(e.name, ''), e.adventure_slug) FROM events e WHERE e.id = l.item_id)
		         ELSE (SELECT COALESCE(NULLIF(a.title, ''), o.adventure_slug) FROM party_offers o
		               LEFT JOIN adventures a ON a.slug = o.adventure_slug WHERE o.id = l.item_id)
		       END, ''),
		       l.action, l.via, l.username, l.reason, COALESCE(l.graph_id, 0),
		       to_char(l.created_at AT TIME ZONE 'UTC', `+isoTime+`), l.content_hash, l.entry_hash,
		       l.sig_signature <> ''
		FROM listing_review_log l
		WHERE $1 = '' OR l.via = $1
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT 200
	`, via)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review log failed", err)
		return
	}
	defer rows.Close()
	out := []listingLogDTO{}
	for rows.Next() {
		var d listingLogDTO
		if err := rows.Scan(&d.Kind, &d.ID, &d.OrgID, &d.OrgName, &d.Title, &d.Action, &d.Via, &d.By,
			&d.Reason, &d.GraphID, &d.CreatedAt, &d.ContentHash, &d.EntryHash, &d.Signed); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "review log failed", err)
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review log failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"log": out})
}

/* ---------------- decisions ---------------- */

type listingDecisionRequest struct {
	Revision string `json:"revision"`
	Reason   string `json:"reason,omitempty"`
	// Signature is the reviewer's passkey signature over this decision
	// (reviewer_passkeys.go): required in the console, absent through
	// an API token.
	Signature *signatureInput `json:"signature,omitempty"`
}

// ApproveListing makes a pending draft public. Listing reviewers only.
func (h *Handlers) ApproveListing(w http.ResponseWriter, r *http.Request) {
	h.decideListing(w, r, true)
}

// RejectListing turns a pending draft down with a reason the
// organisation is told. What is public stays up. Listing reviewers only.
func (h *Handlers) RejectListing(w http.ResponseWriter, r *http.Request) {
	h.decideListing(w, r, false)
}

// reviewer is the caller when a platform admin made them a listing
// reviewer; otherwise it answers 403. Support never decides, even if
// made a reviewer.
func reviewer(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	me, ok := auth.FromContext(r.Context())
	if !ok || !me.ListingReviewer || me.Role == auth.RoleSupport {
		httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "only listing reviewers review listings", "code": "not_reviewer"})
		return me, false
	}
	return me, true
}

// listingReader is the caller when they may read the review queue and
// its log: a listing reviewer, or any global admin (full or Support),
// who reads but decides only when also a reviewer.
func listingReader(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	if me, ok := auth.FromContext(r.Context()); ok && readsAsAdmin(me) {
		return me, true
	}
	return reviewer(w, r)
}

// ownOrg reports whether the reviewer belongs to the organisation: no
// one reviews their own organisation's listings.
func (h *Handlers) ownOrg(ctx context.Context, userID, orgID int64) (bool, error) {
	var own bool
	err := h.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2)`, userID, orgID).Scan(&own)
	return own, err
}

// listingParams reads {kind} and {id}, answering 404 when they name no
// kind of listing.
func listingParams(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	kind := chi.URLParam(r, "kind")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 || (kind != listingEvent && kind != listingParty && kind != listingOrg && kind != listingAdventure) {
		httpx.Error(w, http.StatusNotFound, "no such listing")
		return "", 0, false
	}
	return kind, id, true
}

func (h *Handlers) decideListing(w http.ResponseWriter, r *http.Request, approve bool) {
	ctx := r.Context()
	me, ok := reviewer(w, r)
	if !ok {
		return
	}
	kind, id, ok := listingParams(w, r)
	if !ok {
		return
	}
	var req listingDecisionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if !approve {
		if reason == "" {
			httpx.Error(w, http.StatusBadRequest, "say why it is rejected")
			return
		}
		if len(reason) > MaxRejectReasonLen {
			httpx.Error(w, http.StatusBadRequest, "reason is too long")
			return
		}
	}

	t := listingTables(kind)
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	defer tx.Rollback()
	var (
		orgID      int64
		state, rev string
		graphID    sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT `+t.alias+`.org_id, COALESCE(`+t.alias+`.listing_review, ''), `+t.revision+`, `+t.graph+`
		FROM `+t.from+` WHERE `+t.alias+`.id = $1 FOR UPDATE OF `+t.lock, id).Scan(&orgID, &state, &rev, &graphID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such listing")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	if own, err := h.ownOrg(ctx, me.ID, orgID); err != nil || own {
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
			return
		}
		httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "someone outside your organisation has to review its listings", "code": "own_org"})
		return
	}
	if state != "pending" {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this listing is not waiting for review", "code": "not_pending"})
		return
	}
	if rev != req.Revision {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "the listing changed while you looked: reload and review it again", "code": "changed"})
		return
	}
	// A person in the console may sign what they decide with their
	// passkey; a signature sent must check out. An assistant through an
	// API token can't sign, and is logged as one.
	var sig decisionSignature
	if !auth.ViaAPIToken(ctx) && req.Signature != nil {
		verb := map[bool]string{true: "approve", false: "reject"}[approve]
		if sig, err = h.checkDecisionSignature(ctx, tx, me, req.Signature, verb, kind, id, req.Revision, reason); err != nil {
			if errors.Is(err, errBadSignature) {
				httpx.JSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "bad_signature"})
				return
			}
			httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
			return
		}
	}
	action := "approved"
	if approve {
		err = h.approve(ctx, tx, kind, id, me.ID)
	} else {
		action = "rejected"
		_, err = tx.ExecContext(ctx, `
			UPDATE `+t.table+` SET listing_review = 'rejected', listing_reviewed_at = now(),
			       listing_reviewed_by = $2, listing_reject_reason = $3
			WHERE id = $1`, id, me.ID, reason)
	}
	if err == nil {
		err = h.logListing(ctx, tx, kind, id, orgID, action, viaOf(ctx), me.ID, rev, graphID, reason, sig)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	logx.Info("admin.listing.reviewed", "kind", kind, "id", id, "action", action, "by", me.ID, "via", viaOf(ctx))
	h.notifyListingDecided(ctx, kind, id, orgID, approve, reason)
	httpx.JSON(w, http.StatusOK, map[string]any{"state": action})
}

/* ---------------- reviewers ---------------- */

// listingReviewerDTO is a user who may review listings.
type listingReviewerDTO struct {
	ID       int64    `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email,omitempty"`
	IsAdmin  bool     `json:"is_admin"`
	Orgs     []string `json:"orgs"`
}

// ListListingReviewers lists who may review listings, with the
// organisations whose listings each can't review. Global admin only.
//
// GET /api/admin/listing-reviewers
func (h *Handlers) ListListingReviewers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT u.id, u.username, COALESCE(u.email, ''), u.is_admin,
		       COALESCE(array_to_string(ARRAY(
		         SELECT g.name FROM org_memberships m JOIN organizations g ON g.id = m.org_id
		         WHERE m.user_id = u.id ORDER BY g.name), E'\x1f'), '')
		FROM users u WHERE u.listing_reviewer ORDER BY LOWER(u.username)`)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reviewers lookup failed", err)
		return
	}
	defer rows.Close()
	out := []listingReviewerDTO{}
	for rows.Next() {
		var (
			d    listingReviewerDTO
			orgs string
		)
		if err := rows.Scan(&d.ID, &d.Username, &d.Email, &d.IsAdmin, &orgs); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "reviewers lookup failed", err)
			return
		}
		d.Orgs = []string{}
		if orgs != "" {
			d.Orgs = strings.Split(orgs, "\x1f")
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reviewers lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"reviewers": out})
}

// SetListingReviewer makes a user a listing reviewer, or no longer one.
// Global admin only.
//
// POST /api/admin/listing-reviewers  {username, reviewer}
func (h *Handlers) SetListingReviewer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Reviewer bool   `json:"reviewer"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	var id int64
	err := h.DB.QueryRowContext(r.Context(), `
		UPDATE users SET listing_reviewer = $2 WHERE username = $1 RETURNING id`,
		strings.TrimSpace(req.Username), req.Reviewer).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such user")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	logx.Info("admin.listing.reviewer", "user_id", id, "reviewer", req.Reviewer, "by", me.ID)
	noteTarget(r, strings.TrimSpace(req.Username))
	noteAction(r, onOff(req.Reviewer))
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "reviewer": req.Reviewer})
}

/* ---------------- verified creators ---------------- */

// SetOrgVerifiedCreator marks an organisation as a verified creator:
// its submissions are approved at once, and the store says so. Turning
// it on approves what of theirs is waiting. Global admin only.
func (h *Handlers) SetOrgVerifiedCreator(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad organization id")
		return
	}
	var req struct {
		Verified bool `json:"verified"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	res, err := h.DB.ExecContext(ctx,
		`UPDATE organizations SET verified_creator = $1 WHERE id = $2`, req.Verified, id)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	approved := 0
	if req.Verified {
		if approved, err = h.approveWaitingListings(ctx, id); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "approving waiting listings failed", err)
			return
		}
	}
	noteAction(r, onOff(req.Verified))
	httpx.JSON(w, http.StatusOK, map[string]any{"verified_creator": req.Verified, "approved": approved})
}

// approveWaitingListings approves an organisation's pending listings,
// as their submissions would have been had it been verified then, and
// says how many it approved.
func (h *Handlers) approveWaitingListings(ctx context.Context, orgID int64) (int, error) {
	approved := 0
	for _, kind := range []string{listingEvent, listingParty, listingOrg, listingAdventure} {
		t := listingTables(kind)
		tx, err := h.DB.BeginTx(ctx, nil)
		if err != nil {
			return approved, err
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT `+t.alias+`.id, `+t.revision+`, `+t.graph+` FROM `+t.from+`
			WHERE `+t.alias+`.org_id = $1 AND `+t.alias+`.listing_review = 'pending'
			FOR UPDATE OF `+t.lock, orgID)
		if err != nil {
			tx.Rollback()
			return approved, err
		}
		type waiting struct {
			id      int64
			rev     string
			graphID sql.NullInt64
		}
		var list []waiting
		for rows.Next() {
			var x waiting
			if err := rows.Scan(&x.id, &x.rev, &x.graphID); err != nil {
				rows.Close()
				tx.Rollback()
				return approved, err
			}
			list = append(list, x)
		}
		rows.Close()
		for _, x := range list {
			if err := h.approve(ctx, tx, kind, x.id, 0); err != nil {
				tx.Rollback()
				return approved, err
			}
			if err := h.logListing(ctx, tx, kind, x.id, orgID, "approved", "auto", 0, x.rev, x.graphID, "", decisionSignature{}); err != nil {
				tx.Rollback()
				return approved, err
			}
		}
		if err := tx.Commit(); err != nil {
			return approved, err
		}
		approved += len(list)
	}
	return approved, nil
}

/* ---------------- emails ---------------- */

// notifyListingSubmitted tells the platform's admins a listing waits.
func (h *Handlers) notifyListingSubmitted(ctx context.Context, orgID int64) {
	l := h.orgLook(ctx, 0)
	byLang, err := h.emailsByLang(ctx, l.Lang, `SELECT email, language FROM users WHERE is_admin AND NOT is_support AND COALESCE(email, '') <> ''`)
	if err != nil {
		logx.Warn("admin.listing.notify", "org_id", orgID, "err", err.Error())
		return
	}
	if len(byLang) == 0 {
		return
	}
	var org string
	_ = h.DB.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&org)
	for lang, to := range byLang {
		subject, c := emailtpl.ListingWaiting(lang, org, h.arrangeLink("#listing-reviews"))
		if err := h.send(ctx, "listing_submitted", l.message(to, subject, c)); err != nil {
			logx.Warn("admin.listing.notify", "org_id", orgID, "err", err.Error())
		}
	}
}

// notifyListingDecided tells the organisation's admins what became of
// their listing.
func (h *Handlers) notifyListingDecided(ctx context.Context, kind string, id, orgID int64, approved bool, reason string) {
	l := h.orgLook(ctx, orgID)
	byLang, err := h.emailsByLang(ctx, l.Lang, `
		SELECT u.email, u.language FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role = 'org_admin' AND COALESCE(u.email, '') <> ''`, orgID)
	if err != nil {
		logx.Warn("admin.listing.notify", "kind", kind, "id", id, "err", err.Error())
		return
	}
	if len(byLang) == 0 {
		return
	}
	var what, link string
	switch kind {
	case listingOrg:
		_ = h.DB.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&what)
		link = h.arrangeLink("#/settings/organization")
	case listingAdventure:
		var slug string
		_ = h.DB.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(a.title, ''), a.slug), a.slug FROM presentation_proposals p JOIN adventures a ON a.id = p.target_id
			WHERE p.id = $1`, id).Scan(&what, &slug)
		link = h.arrangeLink("#/adventures/" + slug)
	case listingEvent:
		_ = h.DB.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(e.name, ''), a.title, e.adventure_slug)
			FROM events e LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
			WHERE e.id = $1`, id).Scan(&what)
		link = h.arrangeLink("#/quests/" + strconv.FormatInt(id, 10))
	default:
		_ = h.DB.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(a.title, ''), o.adventure_slug)
			FROM party_offers o LEFT JOIN adventures a ON a.slug = o.adventure_slug
			WHERE o.id = $1`, id).Scan(&what)
		link = h.arrangeLink("#parties")
	}
	for lang, to := range byLang {
		var (
			subject string
			c       emailtpl.Content
		)
		switch {
		case kind == listingOrg && approved:
			subject, c = emailtpl.OrgPageApproved(lang, what, link)
		case kind == listingOrg:
			subject, c = emailtpl.OrgPageRejected(lang, what, reason, link)
		case approved:
			subject, c = emailtpl.ListingApproved(lang, what, link)
		default:
			subject, c = emailtpl.ListingRejected(lang, what, reason, link)
		}
		if err := h.send(ctx, "listing_"+map[bool]string{true: "approved", false: "rejected"}[approved], l.message(to, subject, c)); err != nil {
			logx.Warn("admin.listing.notify", "kind", kind, "id", id, "err", err.Error())
		}
	}
}

// emailsByLang runs a query of an email and a nullable language column
// and groups the addresses by the language each recipient uses the
// console in; one who never chose gets fallback.
func (h *Handlers) emailsByLang(ctx context.Context, fallback, q string, args ...any) (map[string][]string, error) {
	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var (
			e    string
			lang sql.NullString
		)
		if err := rows.Scan(&e, &lang); err != nil {
			return nil, err
		}
		l := fallback
		if lang.Valid && lang.String != "" {
			l = emailtpl.Lang(lang.String)
		}
		out[l] = append(out[l], e)
	}
	return out, rows.Err()
}

// emailsOf runs a query of one email column.
func (h *Handlers) emailsOf(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// arrangeLink is a link into the console, "" without a public URL.
func (h *Handlers) arrangeLink(fragment string) string {
	base := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if base == "" {
		return ""
	}
	return base + "/arrange" + fragment
}
