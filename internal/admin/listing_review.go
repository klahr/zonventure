package admin

// Listing review: what an organisation puts up for sale goes public
// only once a platform admin approves it (migration 0123).
//
//	GET  /api/admin/listing-reviews                           what waits for review
//	POST /api/admin/listing-reviews/{kind}/{id}/approve       {revision}
//	POST /api/admin/listing-reviews/{kind}/{id}/reject        {revision, reason}
//	POST /api/admin/organizations/{orgID}/verified-creator    {verified}
//
// kind is "event" (a self-guided event's store listing) or "party" (a
// party offer). The organisation edits the draft; the public sees the
// approved copy (event_listings, party_offer_listings). After every
// change to a draft's public fields the handler calls
// submitEventListing / submitPartyOffer, which decides what happens:
// nothing to review, approved as it is (a verified creator, or an edit
// undone), or pending.
//
// A decision names the revision the reviewer saw, a hash of the draft's
// public fields: when the draft has changed since, it is refused, so
// nothing goes public that nobody looked at.

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
	"github.com/klahr/zonventure/internal/logx"
)

const (
	listingEvent = "event"
	listingParty = "party"
)

// MaxRejectReasonLen caps what a reviewer writes back.
const MaxRejectReasonLen = 1000

// eventListingRevision and partyListingRevision hash a draft's public
// fields: what a reviewer approves.
const eventListingRevision = `md5(row(e.name, e.public_blurb, e.ticket_price_cents, e.ticket_currency, e.ticket_team_size,
	e.cover_image_mime_type, md5(e.cover_image_content))::text)`

const partyListingRevision = `md5(row(o.blurb, o.base_cents, o.per_kid_cents, o.currency, o.max_kids)::text)`

/* ---------------- submitting ---------------- */

// submitEventListing settles an event's listing after its public fields
// changed. Only a self-guided event (public_for_sale) has one.
func (h *Handlers) submitEventListing(ctx context.Context, eventID int64) error {
	var (
		orgID               int64
		forSale, priced     bool
		verified, unchanged bool
		state               string
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT e.org_id, e.public_for_sale, e.ticket_price_cents IS NOT NULL, g.verified_creator,
		       COALESCE(e.listing_review, ''),
		       COALESCE(l.event_id IS NOT NULL
		         AND l.name = e.name AND l.blurb = e.public_blurb
		         AND l.price_cents = e.ticket_price_cents AND l.currency = e.ticket_currency
		         AND l.team_size = e.ticket_team_size
		         AND l.cover_mime_type IS NOT DISTINCT FROM e.cover_image_mime_type
		         AND l.cover_content IS NOT DISTINCT FROM e.cover_image_content, FALSE)
		FROM events e
		JOIN organizations g ON g.id = e.org_id
		LEFT JOIN event_listings l ON l.event_id = e.id
		WHERE e.id = $1
	`, eventID).Scan(&orgID, &forSale, &priced, &verified, &state, &unchanged)
	if err != nil || !forSale {
		return err
	}
	switch {
	case unchanged:
		// Nothing new to look at: the draft is what is up.
		_, err = h.DB.ExecContext(ctx, `
			UPDATE events SET listing_review = 'approved', listing_reject_reason = ''
			WHERE id = $1 AND listing_review IS DISTINCT FROM 'approved'`, eventID)
	case !priced:
		// No price, nothing to sell: the store hides it already.
		_, err = h.DB.ExecContext(ctx, `
			UPDATE events SET listing_review = NULL, listing_reject_reason = '' WHERE id = $1`, eventID)
	case verified:
		err = h.approveEventListing(ctx, h.DB, eventID, 0)
	default:
		_, err = h.DB.ExecContext(ctx, `
			UPDATE events SET listing_review = 'pending', listing_submitted_at = now(), listing_reject_reason = ''
			WHERE id = $1`, eventID)
		if err == nil && state != "pending" {
			h.notifyListingSubmitted(ctx, orgID)
		}
	}
	return err
}

// submitPartyOffer settles a party offer's listing after it was saved.
// A disabled offer is off the page already and needs no review until it
// is turned on.
func (h *Handlers) submitPartyOffer(ctx context.Context, offerID int64) error {
	var (
		orgID                        int64
		enabled, verified, unchanged bool
		state                        string
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT o.org_id, o.enabled, g.verified_creator, COALESCE(o.listing_review, ''),
		       COALESCE(l.offer_id IS NOT NULL
		         AND l.blurb = o.blurb AND l.base_cents = o.base_cents
		         AND l.per_kid_cents = o.per_kid_cents AND l.currency = o.currency
		         AND l.max_kids = o.max_kids, FALSE)
		FROM party_offers o
		JOIN organizations g ON g.id = o.org_id
		LEFT JOIN party_offer_listings l ON l.offer_id = o.id
		WHERE o.id = $1
	`, offerID).Scan(&orgID, &enabled, &verified, &state, &unchanged)
	if err != nil {
		return err
	}
	switch {
	case unchanged:
		_, err = h.DB.ExecContext(ctx, `
			UPDATE party_offers SET listing_review = 'approved', listing_reject_reason = ''
			WHERE id = $1 AND listing_review IS DISTINCT FROM 'approved'`, offerID)
	case !enabled:
		// Changes to a disabled offer wait until it is turned on.
		_, err = h.DB.ExecContext(ctx, `
			UPDATE party_offers SET listing_review = NULL, listing_reject_reason = '' WHERE id = $1`, offerID)
	case verified:
		err = h.approvePartyListing(ctx, h.DB, offerID, 0)
	default:
		_, err = h.DB.ExecContext(ctx, `
			UPDATE party_offers SET listing_review = 'pending', listing_submitted_at = now(), listing_reject_reason = ''
			WHERE id = $1`, offerID)
		if err == nil && state != "pending" {
			h.notifyListingSubmitted(ctx, orgID)
		}
	}
	return err
}

// execer is what the approve helpers write through: the pool or a tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// approveEventListing copies an event's draft over its public listing.
// reviewer 0 is an automatic approval (a verified creator).
func (h *Handlers) approveEventListing(ctx context.Context, q execer, eventID, reviewer int64) error {
	by := sql.NullInt64{Int64: reviewer, Valid: reviewer > 0}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO event_listings (event_id, name, blurb, price_cents, currency, team_size, cover_mime_type, cover_content, approved_at, approved_by)
		SELECT id, name, public_blurb, ticket_price_cents, ticket_currency, ticket_team_size,
		       cover_image_mime_type, cover_image_content, now(), $2
		FROM events WHERE id = $1 AND ticket_price_cents IS NOT NULL
		ON CONFLICT (event_id) DO UPDATE SET
		    name = EXCLUDED.name, blurb = EXCLUDED.blurb, price_cents = EXCLUDED.price_cents,
		    currency = EXCLUDED.currency, team_size = EXCLUDED.team_size,
		    cover_mime_type = EXCLUDED.cover_mime_type, cover_content = EXCLUDED.cover_content,
		    approved_at = EXCLUDED.approved_at, approved_by = EXCLUDED.approved_by
	`, eventID, by); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `
		UPDATE events SET listing_review = 'approved', listing_reviewed_at = now(),
		       listing_reviewed_by = $2, listing_reject_reason = ''
		WHERE id = $1`, eventID, by)
	return err
}

// approvePartyListing copies a party offer's draft over its listing.
func (h *Handlers) approvePartyListing(ctx context.Context, q execer, offerID, reviewer int64) error {
	by := sql.NullInt64{Int64: reviewer, Valid: reviewer > 0}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO party_offer_listings (offer_id, blurb, base_cents, per_kid_cents, currency, max_kids, approved_at, approved_by)
		SELECT id, blurb, base_cents, per_kid_cents, currency, max_kids, now(), $2
		FROM party_offers WHERE id = $1
		ON CONFLICT (offer_id) DO UPDATE SET
		    blurb = EXCLUDED.blurb, base_cents = EXCLUDED.base_cents, per_kid_cents = EXCLUDED.per_kid_cents,
		    currency = EXCLUDED.currency, max_kids = EXCLUDED.max_kids,
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
	Draft          listingFieldsDTO  `json:"draft"`
	Live           *listingFieldsDTO `json:"live,omitempty"`
}

// ListListingReviews lists what waits for review, oldest first.
// Global admin only (route group).
func (h *Handlers) ListListingReviews(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []listingReviewDTO{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT e.id, `+eventListingRevision+`, to_char(e.listing_submitted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       g.id, g.name, COALESCE(g.slug, ''), e.adventure_slug, COALESCE(NULLIF(a.title, ''), e.adventure_slug),
		       e.name, e.public_blurb, COALESCE(e.ticket_price_cents, 0), e.ticket_currency, e.ticket_team_size,
		       e.cover_image_mime_type IS NOT NULL,
		       l.event_id IS NOT NULL, COALESCE(l.name, ''), COALESCE(l.blurb, ''), COALESCE(l.price_cents, 0),
		       COALESCE(l.currency, ''), COALESCE(l.team_size, 0), l.cover_mime_type IS NOT NULL
		FROM events e
		JOIN organizations g ON g.id = e.org_id
		LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
		LEFT JOIN event_listings l ON l.event_id = e.id
		WHERE e.listing_review = 'pending'
		ORDER BY e.listing_submitted_at, e.id
	`)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
		return
	}
	for rows.Next() {
		var (
			d                  listingReviewDTO
			live               listingFieldsDTO
			draftCover, isLive bool
			liveCover          bool
		)
		if err := rows.Scan(&d.ID, &d.Revision, &d.SubmittedAt, &d.OrgID, &d.OrgName, &d.OrgSlug,
			&d.AdventureSlug, &d.AdventureTitle,
			&d.Draft.Name, &d.Draft.Blurb, &d.Draft.PriceCents, &d.Draft.Currency, &d.Draft.TeamSize, &draftCover,
			&isLive, &live.Name, &live.Blurb, &live.PriceCents, &live.Currency, &live.TeamSize, &liveCover); err != nil {
			rows.Close()
			httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
			return
		}
		d.Kind = listingEvent
		cover := eventCoverURL(d.ID)
		if draftCover {
			d.Draft.CoverURL = cover + "?draft=1"
		}
		if isLive {
			if liveCover {
				live.CoverURL = cover
			}
			d.Live = &live
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
		return
	}
	rows.Close()

	rows, err = h.DB.QueryContext(ctx, `
		SELECT o.id, `+partyListingRevision+`, to_char(o.listing_submitted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       g.id, g.name, COALESCE(g.slug, ''), o.adventure_slug, COALESCE(NULLIF(a.title, ''), o.adventure_slug),
		       o.blurb, o.base_cents, o.per_kid_cents, o.currency, o.max_kids,
		       l.offer_id IS NOT NULL, COALESCE(l.blurb, ''), COALESCE(l.base_cents, 0), COALESCE(l.per_kid_cents, 0),
		       COALESCE(l.currency, ''), COALESCE(l.max_kids, 0)
		FROM party_offers o
		JOIN organizations g ON g.id = o.org_id
		LEFT JOIN adventures a ON a.slug = o.adventure_slug
		LEFT JOIN party_offer_listings l ON l.offer_id = o.id
		WHERE o.listing_review = 'pending'
		ORDER BY o.listing_submitted_at, o.id
	`)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			d      listingReviewDTO
			live   listingFieldsDTO
			isLive bool
		)
		if err := rows.Scan(&d.ID, &d.Revision, &d.SubmittedAt, &d.OrgID, &d.OrgName, &d.OrgSlug,
			&d.AdventureSlug, &d.AdventureTitle,
			&d.Draft.Blurb, &d.Draft.PriceCents, &d.Draft.PerKidCents, &d.Draft.Currency, &d.Draft.MaxKids,
			&isLive, &live.Blurb, &live.PriceCents, &live.PerKidCents, &live.Currency, &live.MaxKids); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
			return
		}
		d.Kind = listingParty
		if isLive {
			d.Live = &live
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review list failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"reviews": out})
}

/* ---------------- decisions ---------------- */

type listingDecisionRequest struct {
	Revision string `json:"revision"`
	Reason   string `json:"reason,omitempty"`
}

// ApproveListing makes a pending draft public. Global admin only.
func (h *Handlers) ApproveListing(w http.ResponseWriter, r *http.Request) {
	h.decideListing(w, r, true)
}

// RejectListing turns a pending draft down with a reason the
// organisation is told. What is public stays up. Global admin only.
func (h *Handlers) RejectListing(w http.ResponseWriter, r *http.Request) {
	h.decideListing(w, r, false)
}

func (h *Handlers) decideListing(w http.ResponseWriter, r *http.Request, approve bool) {
	ctx := r.Context()
	me, _ := auth.FromContext(ctx)
	kind := chi.URLParam(r, "kind")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 || (kind != listingEvent && kind != listingParty) {
		httpx.Error(w, http.StatusNotFound, "no such listing")
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

	table, revision := "events e", eventListingRevision
	if kind == listingParty {
		table, revision = "party_offers o", partyListingRevision
	}
	alias := table[len(table)-1:]

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	defer tx.Rollback()
	var (
		orgID      int64
		state, rev string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT `+alias+`.org_id, COALESCE(`+alias+`.listing_review, ''), `+revision+`
		FROM `+table+` WHERE `+alias+`.id = $1 FOR UPDATE`, id).Scan(&orgID, &state, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such listing")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
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
	switch {
	case approve && kind == listingEvent:
		err = h.approveEventListing(ctx, tx, id, me.ID)
	case approve:
		err = h.approvePartyListing(ctx, tx, id, me.ID)
	default:
		col := "events"
		if kind == listingParty {
			col = "party_offers"
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE `+col+` SET listing_review = 'rejected', listing_reviewed_at = now(),
			       listing_reviewed_by = $2, listing_reject_reason = $3
			WHERE id = $1`, id, me.ID, reason)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	logx.Info("admin.listing.reviewed", "kind", kind, "id", id, "approved", approve, "by", me.ID)
	h.notifyListingDecided(ctx, kind, id, orgID, approve, reason)
	httpx.JSON(w, http.StatusOK, map[string]any{"state": map[bool]string{true: "approved", false: "rejected"}[approve]})
}

/* ---------------- verified creators ---------------- */

// SetOrgVerifiedCreator marks an organisation as a verified creator:
// its listings skip review, and the store says so. Turning it on
// approves what of theirs is waiting. Global admin only.
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
	if req.Verified {
		if err := h.approveWaitingListings(ctx, id); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "approving waiting listings failed", err)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"verified_creator": req.Verified})
}

// approveWaitingListings approves an organisation's pending listings,
// as its saves would have been had it been verified then.
func (h *Handlers) approveWaitingListings(ctx context.Context, orgID int64) error {
	ids := func(q string) ([]int64, error) {
		rows, err := h.DB.QueryContext(ctx, q, orgID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
	events, err := ids(`SELECT id FROM events WHERE org_id = $1 AND listing_review = 'pending'`)
	if err != nil {
		return err
	}
	for _, id := range events {
		if err := h.approveEventListing(ctx, h.DB, id, 0); err != nil {
			return err
		}
	}
	offers, err := ids(`SELECT id FROM party_offers WHERE org_id = $1 AND listing_review = 'pending'`)
	if err != nil {
		return err
	}
	for _, id := range offers {
		if err := h.approvePartyListing(ctx, h.DB, id, 0); err != nil {
			return err
		}
	}
	return nil
}

/* ---------------- emails ---------------- */

// notifyListingSubmitted tells the platform's admins a listing waits.
func (h *Handlers) notifyListingSubmitted(ctx context.Context, orgID int64) {
	to, err := h.emailsOf(ctx, `SELECT email FROM users WHERE is_admin AND COALESCE(email, '') <> ''`)
	if err != nil {
		logx.Warn("admin.listing.notify", "org_id", orgID, "err", err.Error())
		return
	}
	if len(to) == 0 {
		return
	}
	var org string
	_ = h.DB.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&org)
	l := h.orgLook(ctx, 0)
	subject, c := emailtpl.ListingWaiting(l.Lang, org, h.arrangeLink("#listing-reviews"))
	if err := h.send(ctx, "listing_submitted", l.message(to, subject, c)); err != nil {
		logx.Warn("admin.listing.notify", "org_id", orgID, "err", err.Error())
	}
}

// notifyListingDecided tells the organisation's admins what became of
// their listing.
func (h *Handlers) notifyListingDecided(ctx context.Context, kind string, id, orgID int64, approved bool, reason string) {
	to, err := h.emailsOf(ctx, `
		SELECT u.email FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role = 'org_admin' AND COALESCE(u.email, '') <> ''`, orgID)
	if err != nil {
		logx.Warn("admin.listing.notify", "kind", kind, "id", id, "err", err.Error())
		return
	}
	if len(to) == 0 {
		return
	}
	var what, link string
	if kind == listingEvent {
		_ = h.DB.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(e.name, ''), a.title, e.adventure_slug)
			FROM events e LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
			WHERE e.id = $1`, id).Scan(&what)
		link = h.arrangeLink("#/events/" + strconv.FormatInt(id, 10))
	} else {
		_ = h.DB.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(a.title, ''), o.adventure_slug)
			FROM party_offers o LEFT JOIN adventures a ON a.slug = o.adventure_slug
			WHERE o.id = $1`, id).Scan(&what)
		link = h.arrangeLink("#parties")
	}
	l := h.orgLook(ctx, orgID)
	var (
		subject string
		c       emailtpl.Content
	)
	if approved {
		subject, c = emailtpl.ListingApproved(l.Lang, what, link)
	} else {
		subject, c = emailtpl.ListingRejected(l.Lang, what, reason, link)
	}
	if err := h.send(ctx, "listing_"+map[bool]string{true: "approved", false: "rejected"}[approved], l.message(to, subject, c)); err != nil {
		logx.Warn("admin.listing.notify", "kind", kind, "id", id, "err", err.Error())
	}
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
