package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/klahr/zonventure/internal/settings"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/results"
	"github.com/klahr/zonventure/internal/tokenx"
	"github.com/klahr/zonventure/internal/ws"
	qrcode "github.com/skip2/go-qrcode"
)

// Parties: a parent books one of an org's adventures for a children's
// party, with no account, and runs it from a host page whose link is
// their only key. A booking is a ticket purchase of kind 'party'; once it
// is paid it becomes an event of the seller's (events.party), played in
// QR mode: the adventure's zones are cards the host prints and hides
// (party_stations). The retention sweeper deletes the event a while after
// the party, keeping only the purchase. See migration 0108.

// formatMinor shows minor units as an amount: "149 SEK".
func formatMinor(cents int64, currency string) string {
	if cents%100 == 0 {
		return fmt.Sprintf("%d %s", cents/100, strings.ToUpper(currency))
	}
	return fmt.Sprintf("%.2f %s", float64(cents)/100, strings.ToUpper(currency))
}

// PartyValidity is how long a booked party may wait for its first game.
const PartyValidity = 60 * 24 * time.Hour

// maxPartyNameLen caps the party's name ("Elsa turns 8").
const maxPartyNameLen = 80

var errNoPartyGraph = errors.New("publish the adventure before offering it as a party")

/* ---------------- offers: the seller's console ---------------- */

// partyOfferDTO is one adventure the org may sell as a party, with its
// offer when it has one. Problems say why it cannot be offered yet: no
// published version, or something cards cannot play (StationsProblems).
// Name is the offer's own name; empty, it is sold under Title.
type partyOfferDTO struct {
	AdventureSlug string   `json:"adventure_slug"`
	Title         string   `json:"title"`
	Name          string   `json:"name"`
	OfferID       int64    `json:"offer_id,omitempty"`
	Enabled       bool     `json:"enabled"`
	BaseCents     int      `json:"base_cents"`
	PerKidCents   int      `json:"per_kid_cents"`
	Currency      string   `json:"currency"`
	MaxKids       int      `json:"max_kids"`
	Blurb         string   `json:"blurb"`
	Stations      int      `json:"stations"`
	Problems      []string `json:"problems,omitempty"`
	// Places counts the zones that are real places on the map, which
	// the host cannot move: a story that sends players there may not
	// suit a party at home.
	Places int `json:"places,omitempty"`
	// BookURL is the public booking page, once the offer is saved and the
	// org has a page of its own (a slug).
	BookURL string `json:"book_url,omitempty"`
	// ListingReview is where the offer's last changes are in review:
	// "", pending, approved or rejected (with RejectReason). Listed is
	// whether an approved version is up on the page.
	ListingReview string `json:"listing_review,omitempty"`
	RejectReason  string `json:"reject_reason,omitempty"`
	Listed        bool   `json:"listed"`
	// NewerVersion: the adventure has a newer published version than
	// the one approved, which booked parties play. Saving and
	// submitting the offer sends the newer one for review.
	NewerVersion bool `json:"newer_version,omitempty"`
}

// ListPartyOffers lists the adventures the caller's org may sell as
// parties — its own and those shared with it — with their offers.
//
// GET /api/admin/party-offers
func (h *Handlers) ListPartyOffers(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT a.id, a.slug, COALESCE(NULLIF(a.title, ''), a.slug),
		       COALESCE(o.id, 0), COALESCE(o.enabled, FALSE), COALESCE(o.base_cents, 0),
		       COALESCE(o.per_kid_cents, 0), COALESCE(o.currency, $2), COALESCE(o.max_kids, 20),
		       COALESCE(o.name, ''), COALESCE(o.blurb, ''), COALESCE(o.listing_review, ''), COALESCE(o.listing_reject_reason, ''),
		       EXISTS (SELECT 1 FROM party_offer_listings l WHERE l.offer_id = o.id),
		       EXISTS (SELECT 1 FROM party_offer_listings l WHERE l.offer_id = o.id AND l.graph_id IS DISTINCT FROM a.published_graph_id)
		FROM adventures a
		LEFT JOIN party_offers o ON o.org_id = $1 AND o.adventure_slug = a.slug
		WHERE NOT a.trail_hidden
		  AND (a.org_id = $1
		   OR EXISTS (SELECT 1 FROM adventure_shares s WHERE s.adventure_slug = a.slug AND s.org_id = $1))
		ORDER BY 3
	`, *me.OrgID, settings.Currency)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "offers lookup failed", err)
		return
	}
	type row struct {
		advID int64
		dto   partyOfferDTO
	}
	var list []row
	for rows.Next() {
		var x row
		d := &x.dto
		if err := rows.Scan(&x.advID, &d.AdventureSlug, &d.Title, &d.OfferID, &d.Enabled, &d.BaseCents,
			&d.PerKidCents, &d.Currency, &d.MaxKids, &d.Name, &d.Blurb, &d.ListingReview, &d.RejectReason, &d.Listed, &d.NewerVersion); err != nil {
			rows.Close()
			httpx.ServerErr(w, http.StatusInternalServerError, "offers lookup failed", err)
			return
		}
		list = append(list, x)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "offers lookup failed", err)
		return
	}
	rows.Close()
	var orgSlug sql.NullString
	if err := h.DB.QueryRowContext(r.Context(), `SELECT slug FROM organizations WHERE id = $1`, *me.OrgID).Scan(&orgSlug); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "org lookup failed", err)
		return
	}
	out := make([]partyOfferDTO, 0, len(list))
	for _, x := range list {
		if x.dto.OfferID != 0 && orgSlug.String != "" {
			x.dto.BookURL = fmt.Sprintf("/o/%s/party/%d", orgSlug.String, x.dto.OfferID)
		}
		g, err := h.partyGraph(r.Context(), x.advID)
		switch {
		case errors.Is(err, errNoPartyGraph):
			x.dto.Problems = []string{err.Error()}
		case err != nil:
			httpx.ServerErr(w, http.StatusInternalServerError, "graph lookup failed", err)
			return
		default:
			// A party plays with cards, or in a park when the host may
			// move every place: one of the two is enough.
			if !parkOK(g) {
				x.dto.Problems = g.StationsProblems()
			}
			x.dto.Stations = len(g.Zones)
			for _, z := range g.Zones {
				if !z.Movable && (z.Lat != 0 || z.Lng != 0 || z.IsPolygon()) {
					x.dto.Places++
				}
			}
		}
		out = append(out, x.dto)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"offers": out})
}

// partyGraph is the published graph a party of the adventure would play.
func (h *Handlers) partyGraph(ctx context.Context, adventureID int64) (*graph.Graph, error) {
	s, err := graphstore.Published(ctx, h.DB, adventureID)
	if errors.Is(err, graphstore.ErrNotFound) {
		return nil, errNoPartyGraph
	}
	if err != nil {
		return nil, err
	}
	return s.Graph, nil
}

type putPartyOfferRequest struct {
	Enabled     bool   `json:"enabled"`
	BaseCents   int    `json:"base_cents"`
	PerKidCents int    `json:"per_kid_cents"`
	Currency    string `json:"currency"`
	MaxKids     int    `json:"max_kids"`
	Name        string `json:"name"`
	Blurb       string `json:"blurb"`
}

// PutPartyOffer creates or changes the org's party offer for an
// adventure. Org admins only: it sets prices. An offer can be saved off
// at any time, but only switched on for an adventure a party can play.
//
// PUT /api/admin/party-offers/{slug}
func (h *Handlers) PutPartyOffer(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins set party offers")
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	var req putPartyOfferRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	// Parties are sold in the platform's currency only; an empty currency means it.
	req.Currency = strings.ToLower(strings.TrimSpace(req.Currency))
	if req.Currency == "" {
		req.Currency = settings.Currency
	}
	switch {
	case req.BaseCents < 0 || req.PerKidCents < 0:
		httpx.Error(w, http.StatusBadRequest, "prices cannot be negative")
		return
	case req.MaxKids < 1 || req.MaxKids > 50:
		httpx.Error(w, http.StatusBadRequest, "max_kids must be 1–50")
		return
	case req.Currency != settings.Currency:
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "parties are sold in " + strings.ToUpper(settings.Currency), "code": "currency_not_supported"})
		return
	}
	blurb, err := validateBlurb(strings.TrimSpace(req.Blurb), MaxBlurbLen)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "blurb "+err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > maxPartyNameLen {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("name is too long (%d characters max)", maxPartyNameLen))
		return
	}
	var advID int64
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT a.id FROM adventures a
		WHERE a.slug = $1 AND (a.org_id = $2
		   OR EXISTS (SELECT 1 FROM adventure_shares s WHERE s.adventure_slug = a.slug AND s.org_id = $2))
	`, slug, *me.OrgID).Scan(&advID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "no such adventure for your organisation")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "adventure lookup failed", err)
		return
	}
	if req.Enabled {
		if req.BaseCents+req.PerKidCents <= 0 {
			httpx.Error(w, http.StatusBadRequest, "set a price before offering the party")
			return
		}
		// The platform's cut of a party is its pass price: the smallest
		// party must cost more than that.
		if h.platformFee().Enabled() && !h.hosted().Free() {
			if fee := h.hosted().Price(1); int64(req.BaseCents+req.PerKidCents) <= fee {
				httpx.JSON(w, http.StatusBadRequest, map[string]any{
					"error":     fmt.Sprintf("a party must cost more than the platform's %s", formatMinor(fee, h.hosted().Currency)),
					"code":      "price_below_fee",
					"fee_cents": fee,
				})
				return
			}
		}
		g, err := h.partyGraph(r.Context(), advID)
		if errors.Is(err, errNoPartyGraph) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "graph lookup failed", err)
			return
		}
		if p := g.StationsProblems(); len(p) > 0 && !parkOK(g) {
			httpx.JSON(w, http.StatusConflict, map[string]any{
				"error":    "this adventure cannot be played with cards: " + p[0],
				"code":     "not_playable_as_party",
				"problems": p,
			})
			return
		}
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO party_offers (org_id, adventure_slug, enabled, base_cents, per_kid_cents, currency, max_kids, blurb, name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (org_id, adventure_slug) DO UPDATE
		SET enabled = EXCLUDED.enabled, base_cents = EXCLUDED.base_cents, per_kid_cents = EXCLUDED.per_kid_cents,
		    currency = EXCLUDED.currency, max_kids = EXCLUDED.max_kids, blurb = EXCLUDED.blurb, name = EXCLUDED.name, updated_at = now()
		RETURNING id
	`, *me.OrgID, slug, req.Enabled, req.BaseCents, req.PerKidCents, req.Currency, req.MaxKids, blurb, name).Scan(&id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "offer save failed", err)
		return
	}
	// What the page shows changed: a draft until it is submitted for review.
	if err := h.settlePartyOffer(r.Context(), id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "listing review failed", err)
		return
	}
	var review string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(listing_review, '') FROM party_offers WHERE id = $1`, id).Scan(&review)
	httpx.JSON(w, http.StatusOK, map[string]any{"offer_id": id, "listing_review": review})
}

/* ---------------- offers: the public page ---------------- */

// PublicPartyOfferDTO is a party on an org's public page.
type PublicPartyOfferDTO struct {
	ID            int64  `json:"id"`
	AdventureSlug string `json:"adventure_slug"`
	Title         string `json:"title"`
	Blurb         string `json:"blurb"`
	BaseCents     int    `json:"base_cents"`
	PerKidCents   int    `json:"per_kid_cents"`
	Currency      string `json:"currency"`
	MaxKids       int    `json:"max_kids"`
	// Teams is how many teams a game of the booked party takes, so the
	// page can put the children in teams.
	Teams   int    `json:"teams"`
	OrgName string `json:"org_name"`
	OrgSlug string `json:"org_slug"`
	// OrgVerified is a verified creator: the page shows a badge.
	OrgVerified bool `json:"org_verified"`
	// PaymentsOpen is false when the org can't take the money yet: the
	// page then shows the party without a way to book it.
	PaymentsOpen bool `json:"payments_open"`
	// The adventure's look, starter theme and cover picture, on the
	// booking page only (GetPartyByOrg): see joinInfoDTO.
	Look     *adventure.Colors `json:"look,omitempty"`
	Theme    string            `json:"theme,omitempty"`
	CoverURL string            `json:"cover_url,omitempty"`
}

// publicPartyOffersSQL reads an offer's approved listing (listing
// review): an offer without one is not on the page, and an edit waiting
// for review shows, and charges, what was approved before.
const publicPartyOffersSQL = `
	SELECT o.id, o.adventure_slug, COALESCE(NULLIF(l.name, ''), NULLIF(a.title, ''), a.slug), l.blurb, l.base_cents,
	       l.per_kid_cents, l.currency, l.max_kids, g.name, COALESCE(g.slug, ''), g.verified_creator,
	       g.stripe_connect_account_id IS NOT NULL AND g.stripe_connect_charges_enabled
	FROM party_offers o
	JOIN party_offer_listings l ON l.offer_id = o.id
	JOIN organizations g ON g.id = o.org_id
	JOIN adventures a ON a.slug = o.adventure_slug
	WHERE o.enabled AND a.published_graph_id IS NOT NULL AND NOT a.trail_hidden AND g.slug = $1`

func (h *Handlers) scanPublicOffer(row interface{ Scan(...any) error }) (PublicPartyOfferDTO, error) {
	var (
		d         PublicPartyOfferDTO
		connectOK bool
	)
	err := row.Scan(&d.ID, &d.AdventureSlug, &d.Title, &d.Blurb, &d.BaseCents, &d.PerKidCents,
		&d.Currency, &d.MaxKids, &d.OrgName, &d.OrgSlug, &d.OrgVerified, &connectOK)
	// With a platform fee the money goes through the org's connected
	// account, which must be able to take charges.
	d.PaymentsOpen = h.paymentsEnabled() && (!h.platformFee().Enabled() || connectOK)
	d.Teams = teamsLimit(1)
	return d, err
}

// ListPartiesByOrg lists the parties an org offers.
//
// GET /api/o/{slug}/parties
func (h *Handlers) ListPartiesByOrg(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), publicPartyOffersSQL+` ORDER BY 3`, chi.URLParam(r, "slug"))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "parties lookup failed", err)
		return
	}
	defer rows.Close()
	out := []PublicPartyOfferDTO{}
	for rows.Next() {
		d, err := h.scanPublicOffer(rows)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "parties lookup failed", err)
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "parties lookup failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"parties": out})
}

// GetPartyByOrg returns one party an org offers.
//
// GET /api/o/{slug}/parties/{offerID}
func (h *Handlers) GetPartyByOrg(w http.ResponseWriter, r *http.Request) {
	d, ok := h.publicOffer(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var err error
	if d.Look, d.Theme, err = h.partyLook(ctx, d.AdventureSlug); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return
	}
	if cover, _, err := h.adventureCover(ctx, d.AdventureSlug, false); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return
	} else if cover > 0 {
		d.CoverURL = "/adventure-assets/cover/" + url.PathEscape(d.AdventureSlug) + "?v=" + strconv.FormatInt(cover, 10)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, d)
}

func (h *Handlers) publicOffer(w http.ResponseWriter, r *http.Request) (PublicPartyOfferDTO, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "offerID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusNotFound, "no such party")
		return PublicPartyOfferDTO{}, false
	}
	d, err := h.scanPublicOffer(h.DB.QueryRowContext(r.Context(), publicPartyOffersSQL+` AND o.id = $2`,
		chi.URLParam(r, "slug"), id))
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such party")
		return d, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return d, false
	}
	return d, true
}

// PartyPriceCents is what a party for kids children costs.
func PartyPriceCents(baseCents, perKidCents, kids int) int64 {
	return int64(baseCents) + int64(perKidCents)*int64(kids)
}

// BookPartyRequest is the booking form. Consent is the booker's
// declaration that they are the children's guardian, or have their
// guardians' consent.
type BookPartyRequest struct {
	Email     string `json:"email"`
	Kids      int    `json:"kids"`
	PartyName string `json:"party_name"`
	Consent   bool   `json:"consent"`
}

// BookParty opens a Checkout session for a party. Public: the booker has
// no account. Like BuyTicket, the price is computed here from the offer,
// never taken from the client.
//
// POST /api/o/{slug}/parties/{offerID}/book
func (h *Handlers) BookParty(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "party bookings are not configured on this server")
		return
	}
	offer, ok := h.publicOffer(w, r)
	if !ok {
		return
	}
	var req BookPartyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(email); err != nil || email == "" {
		httpx.Error(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	if req.Kids < 1 || req.Kids > offer.MaxKids {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("a party is for 1–%d children", offer.MaxKids))
		return
	}
	if !req.Consent {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{
			"error": "confirm that you are the children's guardian, or have their guardians' consent",
			"code":  "consent_required",
		})
		return
	}
	name := strings.TrimSpace(req.PartyName)
	if utf8.RuneCountInString(name) > maxPartyNameLen {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("party name is too long (%d characters max)", maxPartyNameLen))
		return
	}
	if !offer.PaymentsOpen {
		httpx.Error(w, http.StatusConflict, "this party can't be booked right now")
		return
	}
	amount := PartyPriceCents(offer.BaseCents, offer.PerKidCents, req.Kids)
	if amount <= 0 {
		httpx.Error(w, http.StatusConflict, "this party has no price set")
		return
	}

	var (
		connectedAccountID string
		applicationFee     int64
	)
	if h.platformFee().Enabled() {
		acct, err := h.stripeConnectAccountForOrgSlug(r.Context(), offer.OrgSlug)
		if errors.Is(err, ErrConnectNotReady) {
			httpx.Error(w, http.StatusConflict, "this party can't be booked right now")
			return
		}
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "connect account lookup failed", err)
			return
		}
		connectedAccountID = acct
		// A party is a hosted event of one block: the platform takes
		// the pass price.
		applicationFee = h.hosted().Fee(amount, 1)
	}

	var purchaseID int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO ticket_purchases (kind, party_offer_id, party_name, consent_at, buyer_email,
		                              team_size, amount_cents, currency, application_fee_cents, blocks)
		VALUES ('party', $1, $2, now(), $3, $4, $5, $6, $7, 1) RETURNING id
	`, offer.ID, name, email, req.Kids, amount, offer.Currency, applicationFee).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "booking failed", err)
		return
	}
	product := fmt.Sprintf("%s — party for %d", offer.Title, req.Kids)
	back := fmt.Sprintf("/app/party/book/%s/%d", offer.OrgSlug, offer.ID)
	url, sessID, err := h.createTicketCheckoutSession(r.Context(), purchaseID, product, offer.Currency, amount,
		email, connectedAccountID, applicationFee, back, nil)
	if err != nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE ticket_purchases SET state = 'failed' WHERE id = $1 AND state = 'pending'`, purchaseID)
		httpx.ServerErr(w, http.StatusBadGateway, "checkout create failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ticket_purchases SET stripe_session_id = $1 WHERE id = $2`, sessID, purchaseID); err != nil {
		logx.Warn("admin.party.book.session_id_persist", "purchase_id", purchaseID, "err", err.Error())
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, BuyTicketResponse{CheckoutURL: url})
}

// stripeConnectAccountForOrgSlug is stripeConnectAccountForEvent for a
// party, whose event does not exist until it is paid.
func (h *Handlers) stripeConnectAccountForOrgSlug(ctx context.Context, slug string) (string, error) {
	var (
		acct           sql.NullString
		chargesEnabled bool
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT stripe_connect_account_id, stripe_connect_charges_enabled FROM organizations WHERE slug = $1
	`, slug).Scan(&acct, &chargesEnabled); err != nil {
		return "", err
	}
	if !acct.Valid || acct.String == "" || !chargesEnabled {
		return "", ErrConnectNotReady
	}
	return acct.String, nil
}

/* ---------------- fulfilment ---------------- */

// fulfilParty turns a paid party booking into its event, inside the
// webhook's transaction: the seller's hosted event of the offered
// adventure. It returns the host link's secret, which only the email
// carries.
func fulfilParty(ctx context.Context, tx *sql.Tx, purchaseID, offerID int64, partyName string, kids int) (eventID int64, hostToken string, err error) {
	var (
		orgID   int64
		advSlug string
		title   string
		// approved is the adventure version the offer was approved with
		// (listing review): what the party plays.
		approved sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT o.org_id, o.adventure_slug, COALESCE(NULLIF(l.name, ''), NULLIF(a.title, ''), a.slug), l.graph_id
		FROM party_offers o JOIN adventures a ON a.slug = o.adventure_slug
		LEFT JOIN party_offer_listings l ON l.offer_id = o.id
		WHERE o.id = $1
	`, offerID).Scan(&orgID, &advSlug, &title, &approved); err != nil {
		return 0, "", fmt.Errorf("party offer %d: %w", offerID, err)
	}
	if strings.TrimSpace(partyName) == "" {
		partyName = title
	}
	eventID, hostToken, err = createHostedEvent(ctx, tx, orgID, advSlug, partyName, kids, 1, "")
	if err != nil {
		return 0, "", err
	}
	if approved.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE events SET base_graph_id = $1 WHERE id = $2`, approved.Int64, eventID); err != nil {
			return 0, "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ticket_purchases SET event_id = $1 WHERE id = $2`, eventID, purchaseID); err != nil {
		return 0, "", err
	}
	return eventID, hostToken, nil
}

// createHostedEvent makes a hosted event of orgID's, unstarted, playing
// advSlug: with a station card for every zone, a host link and a player
// link, blocks blocks of teams per game, and 60 days to start its first
// game. hostToken is the host link's secret when one was minted already
// (at checkout); empty, a new one is. It returns the event and the
// secret.
func createHostedEvent(ctx context.Context, tx *sql.Tx, orgID int64, advSlug, name string, kids, blocks int, hostToken string) (eventID int64, _ string, err error) {
	var advID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, advSlug).Scan(&advID); err != nil {
		return 0, "", fmt.Errorf("adventure %q: %w", advSlug, err)
	}
	if hostToken == "" {
		if hostToken, err = tokenx.New(24); err != nil {
			return 0, "", err
		}
	}
	liveToken, err := newLiveToken()
	if err != nil {
		return 0, "", err
	}
	playerToken, err := newPlayerToken()
	if err != nil {
		return 0, "", err
	}
	// Cards where the adventure allows them, which is anywhere; a park
	// otherwise. The host may change it between games.
	mode := "qr"
	published, err := graphstore.Published(ctx, tx, advID)
	if err != nil && !errors.Is(err, graphstore.ErrNotFound) {
		return 0, "", err
	}
	if err == nil && len(published.Graph.StationsProblems()) > 0 && parkOK(published.Graph) {
		mode = "park"
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO events (adventure_slug, org_id, state, live_token, name, attested_no_minors_at,
		                    party, host_token_hash, party_name, party_kids, party_mode, valid_until, player_token, hosted_blocks)
		VALUES ($1, $2, 'scheduled', $3, $4, now_text(), TRUE, $5, $4, $6, $7, $8, $9, $10)
		RETURNING id
	`, advSlug, orgID, liveToken, name, hashHostToken(hostToken), kids, mode,
		time.Now().Add(PartyValidity), playerToken, blocks).Scan(&eventID); err != nil {
		return 0, "", fmt.Errorf("hosted event: %w", err)
	}
	// Cards for the zones the adventure has now, so the host can print
	// them before the first game; any added later get theirs then.
	if published.Graph != nil {
		if err := play.EnsureStations(ctx, tx, eventID, published.Graph.Zones); err != nil {
			return 0, "", err
		}
	}
	activity.Record(ctx, tx, eventID, nil, activity.Actor{}, activity.EventCreated,
		map[string]any{"adventure_slug": advSlug, "party": true, "kids": kids, "blocks": blocks})
	return eventID, hostToken, nil
}

// hashHostToken is how a host link's secret is looked up.
func hashHostToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// replaceHostToken gives event id a new host link; the old one stops
// working.
func (h *Handlers) replaceHostToken(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, id int64) (string, error) {
	token, err := tokenx.New(24)
	if err != nil {
		return "", err
	}
	_, err = q.ExecContext(ctx, `UPDATE events SET host_token_hash = $1, host_token_sealed = $2 WHERE id = $3`, hashHostToken(token), h.sealHostToken(token), id)
	return token, err
}

// sealHostToken is token sealed with ZV_TOTP_KEY, to keep; nil, so
// nothing is kept, on a server without that key.
func (h *Handlers) sealHostToken(token string) []byte {
	if h.TOTPKey == nil {
		return nil
	}
	sealed, err := auth.SealSecret(h.TOTPKey, token)
	if err != nil {
		return nil
	}
	return sealed
}

// keepHostToken keeps a new party's host link, sealed, so that mailing
// it again sends the same link (see sealHostToken).
func (h *Handlers) keepHostToken(ctx context.Context, tx *sql.Tx, eventID int64, token string) error {
	sealed := h.sealHostToken(token)
	if sealed == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE events SET host_token_sealed = $1 WHERE id = $2`, sealed, eventID)
	return err
}

// currentHostToken is the host link event id has now, to mail again.
// A party whose link is not kept (made before links were, and not opened
// since, or on a server without ZV_TOTP_KEY) gets a new one.
func (h *Handlers) currentHostToken(ctx context.Context, id int64) (string, error) {
	if token, err := h.keptHostToken(ctx, id); err != nil || token != "" {
		return token, err
	}
	return h.replaceHostToken(ctx, h.DB, id)
}

// keptHostToken is the host link event id has now, or "" when it is not
// kept (see currentHostToken).
func (h *Handlers) keptHostToken(ctx context.Context, id int64) (string, error) {
	var sealed []byte
	if err := h.DB.QueryRowContext(ctx, `SELECT host_token_sealed FROM events WHERE id = $1`, id).Scan(&sealed); err != nil {
		return "", err
	}
	if sealed != nil && h.TOTPKey != nil {
		if token, err := auth.OpenSecret(h.TOTPKey, sealed); err == nil {
			return token, nil
		}
	}
	return "", nil
}

// sendPartyHostLink mails the booker their host link: again when it is
// sent anew, which a paid trail's mail tells apart (no receipt line).
func (h *Handlers) sendPartyHostLink(ctx context.Context, eventID int64, to, hostToken string, again bool) error {
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		return errors.New("party email requires PublicURL")
	}
	// A public quiz trail's buyer gets the trail's own mail.
	if _, ok, err := h.trailOfEvent(ctx, eventID); err != nil {
		return err
	} else if ok {
		return h.sendTrailPaid(ctx, eventID, to, hostToken, again)
	}
	l := h.eventLook(ctx, eventID)
	var name string
	_ = h.DB.QueryRowContext(ctx, `SELECT party_name FROM events WHERE id = $1`, eventID).Scan(&name)
	subject, c := emailtpl.PartyHost(l.Lang, publicURL+"/party/"+hostToken, name, l.Brand.Name)
	c = l.withBuyerNote(c)
	c.Picture = l.Cover
	if h.SellerIdentity != nil {
		if seller := h.SellerIdentity(l.Lang); seller != "" {
			c.Footer = strings.TrimSpace(c.Footer + " " + seller)
		}
	}
	msg := l.message([]string{to}, subject, c)
	if h.MailOutbox != nil {
		return h.MailOutbox.Enqueue(ctx, "party_host_link", msg)
	}
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	return h.Mailer.Send(ctx, msg)
}

/* ---------------- the host page ---------------- */

// hostParty is a party as its host link opens it.
type hostParty struct {
	EventID     int64
	OrgID       int64
	AdvSlug     string
	State       string
	Kids        int
	ValidUntil  time.Time
	GraphID     sql.NullInt64
	Mode        string
	Overrides   graph.PartyOverrides
	PlayerToken string
	Blocks      int
	Round       int
	// RoundOpen is whether game Round is being played.
	RoundOpen bool
	// HoldResults keeps each game's results secret until the host
	// finishes it (play.ResultsHeld).
	HoldResults bool
}

// hostedParty resolves the hosted event a request is about, writing a
// 404 when there is none. On the host page that is the host link in the
// URL — a wrong, rotated or purged link looks the same. In the console
// it is the event in the URL, which the arranger's org must own: the
// same handlers serve both, each on its own list of routes.
func (h *Handlers) hostedParty(w http.ResponseWriter, r *http.Request) (hostParty, bool) {
	if eventID, ok := eventIDFrom(r); ok {
		if _, ok := h.authorizeEvent(w, r, eventID); !ok {
			return hostParty{}, false
		}
		return h.partyWhere(w, r.Context(), "e.id = $1", eventID)
	}
	token := chi.URLParam(r, "token")
	if token == "" || len(token) > 128 {
		httpx.Error(w, http.StatusNotFound, "no such party")
		return hostParty{}, false
	}
	p, ok := h.partyWhere(w, r.Context(), "host_token_hash = $1", hashHostToken(token))
	if ok {
		// A party made before host links were kept learns its link
		// from the host page, so mailing it again need not replace it.
		if sealed := h.sealHostToken(token); sealed != nil {
			_, _ = h.DB.ExecContext(r.Context(), `UPDATE events SET host_token_sealed = $1 WHERE id = $2 AND host_token_sealed IS NULL`, sealed, p.EventID)
		}
	}
	return p, ok
}

// ReplaceHostLink gives the party a new host link and answers with it.
// Every older link, open pages included, stops working: the host's way
// out when a link went where it should not.
//
// POST /api/party/{token}/replace-link
func (h *Handlers) ReplaceHostLink(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	token, err := h.replaceHostToken(r.Context(), h.DB, p.EventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "replace failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{
		"token":    token,
		"host_url": strings.TrimRight(h.PublicURL, "/") + "/party/" + token,
	})
}

// partyWhere loads the party event cond picks, writing a 404 when there
// is none. It gives the event a player link the first time it is asked.
func (h *Handlers) partyWhere(w http.ResponseWriter, ctx context.Context, cond string, arg any) (hostParty, bool) {
	p, err := h.loadParty(ctx, h.DB, cond, arg)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such party")
		return p, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return p, false
	}
	return p, true
}

func (h *Handlers) loadParty(ctx context.Context, q graphstore.Querier, cond string, arg any) (hostParty, error) {
	var (
		p         hostParty
		overrides []byte
		player    sql.NullString
	)
	err := q.QueryRowContext(ctx, `
		SELECT e.id, e.org_id, e.adventure_slug, e.state, e.party_kids, e.valid_until, e.graph_id, e.party_mode,
		       e.party_overrides, e.player_token, e.hosted_blocks, e.current_round,
		       EXISTS (SELECT 1 FROM event_rounds r WHERE r.event_id = e.id AND r.round = e.current_round AND r.finished_at IS NULL),
		       e.party_hold_results
		FROM events e WHERE e.party AND `+cond, arg).Scan(&p.EventID, &p.OrgID, &p.AdvSlug, &p.State, &p.Kids, &p.ValidUntil,
		&p.GraphID, &p.Mode, &overrides, &player, &p.Blocks, &p.Round, &p.RoundOpen, &p.HoldResults)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(overrides, &p.Overrides); err != nil {
		return p, err
	}
	p.PlayerToken = player.String
	if p.PlayerToken == "" {
		if p.PlayerToken, err = ensurePlayerToken(ctx, q, p.EventID); err != nil {
			return p, err
		}
	}
	return p, nil
}

type hostStationDTO struct {
	Zone string `json:"zone"`
	Name string `json:"name"`
	Code string `json:"code"`
	// URL is what the card's QR code holds: the play page, told which
	// card it is, so a phone's own camera opens the game at it too.
	URL string `json:"url"`
}

type hostTeamDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Headcount int    `json:"headcount"`
	Finished  bool   `json:"finished"`
	Score     int    `json:"score"`
}

// hostRoundDTO is one game the party has played or is playing.
type hostRoundDTO struct {
	Round      int        `json:"round"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Teams      int        `json:"teams"`
	Players    int        `json:"players"`
	// Results are a finished game's teams and scores.
	Results []hostTeamDTO `json:"results,omitempty"`
}

type hostPartyDTO struct {
	Name           string           `json:"name"`
	AdventureTitle string           `json:"adventure_title"`
	OrgName        string           `json:"org_name"`
	State          string           `json:"state"`
	Kids           int              `json:"kids"`
	ValidUntil     time.Time        `json:"valid_until"`
	Expired        bool             `json:"expired"`
	Stations       []hostStationDTO `json:"stations"`
	LiveURL        string           `json:"live_url"`
	PlayURL        string           `json:"play_url"`

	// The player link teams join from, and the games played so far.
	// Teams are those of the game being played, or of the last one.
	PlayerURL  string `json:"player_url"`
	Round      int    `json:"round"`
	RoundOpen  bool   `json:"round_open"`
	TeamsLimit int    `json:"teams_limit"`
	// TurnedAway counts the teams the game being played turned away,
	// full.
	TurnedAway int `json:"turned_away"`
	// HoldResults keeps a game's results off the phones and the live
	// board until the host finishes it.
	HoldResults bool `json:"hold_results"`
	// MaxTeams is the most teams a game can be bought up to, and Price
	// what each block more costs (0 where hosting is free).
	MaxTeams int            `json:"max_teams"`
	Price    hostedPriceDTO `json:"price"`
	// Trail is set for a quiz trail, whose cards print as a PDF
	// (cards.pdf) instead of the station cards; Reprint when they
	// changed since they were printed.
	Trail   bool `json:"trail"`
	Reprint bool `json:"reprint"`
	// PublicTrail is set for a quiz trail made at the public door: the
	// host link edits its cards (cards), and from KeepFrom its buyer may
	// keep it (keep).
	PublicTrail bool           `json:"public_trail"`
	TrailID     int64          `json:"trail_id,omitempty"`
	KeepFrom    *time.Time     `json:"keep_from,omitempty"`
	FirstGameAt *time.Time     `json:"first_game_at,omitempty"`
	Teams       []hostTeamDTO  `json:"teams"`
	Rounds      []hostRoundDTO `json:"rounds"`

	// How the adventure is adapted to the party: see party_adapt.go.
	Mode      string            `json:"mode"`
	CardsOK   bool              `json:"cards_ok"`
	ParkOK    bool              `json:"park_ok"`
	Values    []hostValueDTO    `json:"values"`
	Editables []hostEditableDTO `json:"editables"`
	Places    []hostPlaceDTO    `json:"places"`

	// The adventure's look, starter theme and cover picture, as the join
	// page shows them: see joinInfoDTO.
	Look     *adventure.Colors `json:"look,omitempty"`
	Theme    string            `json:"theme,omitempty"`
	CoverURL string            `json:"cover_url,omitempty"`
}

// GetHostParty is everything the host page shows.
//
// GET /api/party/{token}
func (h *Handlers) GetHostParty(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	base := strings.TrimRight(h.PublicURL, "/")
	out := hostPartyDTO{State: p.State, Kids: p.Kids, ValidUntil: p.ValidUntil,
		Expired: partyLapsed(p), PlayURL: base + "/play",
		PlayerURL: base + "/j/" + p.PlayerToken, Round: p.Round, RoundOpen: p.RoundOpen, HoldResults: p.HoldResults,
		TeamsLimit: teamsLimit(p.Blocks), MaxTeams: maxBlocks * TeamsPerBlock, Price: h.hostedPrice()}
	var liveToken string
	if err := h.DB.QueryRowContext(ctx, `
		SELECT e.party_name, COALESCE(NULLIF(a.title, ''), a.slug), o.name, e.live_token, e.first_game_at
		FROM events e JOIN adventures a ON a.slug = e.adventure_slug JOIN organizations o ON o.id = e.org_id
		WHERE e.id = $1
	`, p.EventID).Scan(&out.Name, &out.AdventureTitle, &out.OrgName, &liveToken, &out.FirstGameAt); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return
	}
	out.LiveURL = base + "/live/" + liveToken
	look, theme, err := h.partyLook(ctx, p.AdvSlug)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return
	}
	out.Look, out.Theme = look, theme
	if cover, _, err := h.adventureCover(ctx, p.AdvSlug, false); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "party lookup failed", err)
		return
	} else if cover > 0 {
		out.CoverURL = "/api/join/" + p.PlayerToken + "/cover?v=" + strconv.FormatInt(cover, 10)
	}

	// The cards follow the zones of the graph the party plays: the
	// game's while one is being played, the adventure's between games.
	g, err := h.hostGraph(ctx, p)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "graph lookup failed", err)
		return
	}
	var zones []graph.Zone
	if g != nil {
		zones = g.Zones
	}
	h.describeAdaptation(&out, p, g)
	if p.Mode != "qr" {
		zones = nil // in a park the places are the stations: no cards
	}
	if !p.RoundOpen {
		if err := play.EnsureStations(ctx, h.DB, p.EventID, zones); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "stations failed", err)
			return
		}
	}
	codes := map[string]string{}
	rows, err := h.DB.QueryContext(ctx, `SELECT zone_slug, code FROM party_stations WHERE event_id = $1`, p.EventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "stations lookup failed", err)
		return
	}
	for rows.Next() {
		var z, c string
		if err := rows.Scan(&z, &c); err != nil {
			rows.Close()
			httpx.ServerErr(w, http.StatusInternalServerError, "stations lookup failed", err)
			return
		}
		codes[z] = c
	}
	rows.Close()
	out.Stations = []hostStationDTO{}
	for _, z := range zones {
		code, ok := codes[z.Slug]
		if !ok {
			continue
		}
		name := z.Name
		if z.Hidden || name == "" {
			name = fmt.Sprintf("Card %d", len(out.Stations)+1)
		}
		out.Stations = append(out.Stations, hostStationDTO{Zone: z.Slug, Name: name, Code: auth.FormatVoucherCode(code),
			URL: base + "/play?station=" + code})
	}

	if t, err := loadTrail(ctx, h.DB, p.AdvSlug, false); err == nil {
		out.Trail = true
		out.Reprint = t.printed.Valid && int(t.printed.Int64) != t.dto.Revision
	} else if !errors.Is(err, errNotTrail) {
		httpx.ServerErr(w, http.StatusInternalServerError, "trail lookup failed", err)
		return
	}
	if pt, ok, err := h.trailOfEvent(ctx, p.EventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "trail lookup failed", err)
		return
	} else if ok {
		out.PublicTrail, out.TrailID = true, pt.ID
		if pt.FirstGameAt.Valid {
			k := keepFrom(pt.FirstGameAt.Time)
			out.KeepFrom = &k
		}
	}
	if out.Teams, err = h.roundTeams(ctx, p.EventID, p.Round); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "teams lookup failed", err)
		return
	}
	if p.RoundOpen && g != nil {
		if err := h.addPickPoints(ctx, g, out.Teams); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "teams lookup failed", err)
			return
		}
	}
	if out.Rounds, err = h.partyRounds(ctx, p.EventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "games lookup failed", err)
		return
	}
	for i, rd := range out.Rounds {
		if rd.FinishedAt == nil {
			continue
		}
		if out.Rounds[i].Results, err = h.roundTeams(ctx, p.EventID, rd.Round); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "games lookup failed", err)
			return
		}
	}
	if p.RoundOpen {
		if err := h.DB.QueryRowContext(ctx, `SELECT cardinality(turned_away) FROM event_rounds WHERE event_id = $1 AND round = $2`,
			p.EventID, p.Round).Scan(&out.TurnedAway); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "games lookup failed", err)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

// roundTeams are the teams of one game, withdrawn ones left out.
func (h *Handlers) roundTeams(ctx context.Context, eventID int64, round int) ([]hostTeamDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(t.headcount, 1),
		       COALESCE(g.completed_at IS NOT NULL, FALSE), COALESCE(g.score - g.penalty_score, 0)::int
		FROM teams t LEFT JOIN team_graph_state g ON g.team_id = t.id
		WHERE t.event_id = $1 AND t.round = $2 AND t.withdrawn_at IS NULL ORDER BY t.id
	`, eventID, round)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hostTeamDTO{}
	for rows.Next() {
		var t hostTeamDTO
		if err := rows.Scan(&t.ID, &t.Name, &t.Headcount, &t.Finished, &t.Score); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// addPickPoints adds to the scores of the teams still playing what their
// quiz picks so far are worth. Only the host sees this: the phones and
// the live board score picks as a team finishes (session.Team.Apply).
func (h *Handlers) addPickPoints(ctx context.Context, g *graph.Graph, teams []hostTeamDTO) error {
	worth := map[[2]string]int{}
	answer := map[[2]string]string{}
	for _, n := range g.Nodes {
		for _, b := range n.Blocks() {
			if b.Kind == graph.BlockQuiz {
				worth[[2]string{n.Slug, b.ID}], answer[[2]string{n.Slug, b.ID}] = max(1, b.Worth), b.Answer
			}
		}
	}
	at := map[int64]int{}
	ids := []int64{}
	for i, t := range teams {
		if !t.Finished {
			at[t.ID] = i
			ids = append(ids, t.ID)
		}
	}
	if len(worth) == 0 || len(ids) == 0 {
		return nil
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT team_id, node, block_id, choice FROM team_block_picks
		WHERE team_id = ANY(string_to_array($1, ',')::bigint[])
	`, joinIDs(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id                  int64
			node, block, choice string
		)
		if err := rows.Scan(&id, &node, &block, &choice); err != nil {
			return err
		}
		k := [2]string{node, block}
		if choice != "" && choice == answer[k] {
			teams[at[id]].Score += worth[k]
		}
	}
	return rows.Err()
}

// partyRounds lists the party's games, latest first.
func (h *Handlers) partyRounds(ctx context.Context, eventID int64) ([]hostRoundDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT r.round, r.started_at, r.finished_at,
		       COUNT(t.id), COALESCE(SUM(COALESCE(t.headcount, 1)), 0)
		FROM event_rounds r
		LEFT JOIN teams t ON t.event_id = r.event_id AND t.round = r.round AND t.withdrawn_at IS NULL
		WHERE r.event_id = $1
		GROUP BY r.round, r.started_at, r.finished_at
		ORDER BY r.round DESC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hostRoundDTO{}
	for rows.Next() {
		var (
			d        hostRoundDTO
			finished sql.NullTime
		)
		if err := rows.Scan(&d.Round, &d.StartedAt, &finished, &d.Teams, &d.Players); err != nil {
			return nil, err
		}
		if finished.Valid {
			d.FinishedAt = &finished.Time
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// partyZones are the zones of the graph the party plays, in the
// author's order.
// hostGraph is the graph the party plays: the game's own while one is
// being played, the adventure's published one between games; nil when
// there is none.
func (h *Handlers) hostGraph(ctx context.Context, p hostParty) (*graph.Graph, error) {
	if p.RoundOpen && p.GraphID.Valid {
		s, err := graphstore.ByID(ctx, h.DB, p.GraphID.Int64)
		if err != nil {
			return nil, err
		}
		return s.Graph, nil
	}
	var advID int64
	if err := h.DB.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, p.AdvSlug).Scan(&advID); err != nil {
		return nil, err
	}
	g, err := h.partyGraph(ctx, advID)
	if errors.Is(err, errNoPartyGraph) {
		return nil, nil
	}
	return g, err
}

// createPartyTeam inserts a team of the game round with one player
// slot, for the phone that joined, and returns the team and the slot's
// magic-link token.
func createPartyTeam(ctx context.Context, tx *sql.Tx, eventID int64, round int, name string, headcount int) (int64, string, error) {
	teamTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, "", err
	}
	recapTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, "", err
	}
	voucher, err := uniqueVoucherCode(ctx, tx)
	if err != nil {
		return 0, "", err
	}
	var teamID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO teams (event_id, name, token, recap_token, email, voucher_code, round, headcount)
		VALUES ($1, $2, $3, $4, '', $5, $6, $7) RETURNING id
	`, eventID, name, teamTok, recapTok, voucher, round, headcount).Scan(&teamID); err != nil {
		return 0, "", fmt.Errorf("teams insert: %w", err)
	}
	slotTok, err := auth.NewSlotToken()
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO player_slots (team_id, name, token, expires_at) VALUES ($1, $2, $3, $4)`,
		teamID, name, slotTok, auth.DefaultSlotExpiresAt(ctx, tx, eventID)); err != nil {
		return 0, "", fmt.Errorf("player_slots insert: %w", err)
	}
	return teamID, slotTok, nil
}

// RemoveHostTeam takes a team out of the game being played: it stops
// counting towards the limit and leaves the ranking. Its phone is signed
// out.
//
// DELETE /api/party/{token}/teams/{teamID}
func (h *Handlers) RemoveHostTeam(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	teamID, err := strconv.ParseInt(chi.URLParam(r, "teamID"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such team")
		return
	}
	ctx := r.Context()
	res, err := h.DB.ExecContext(ctx, `
		UPDATE teams SET withdrawn_at = now_text()
		WHERE id = $1 AND event_id = $2 AND round = $3 AND withdrawn_at IS NULL
	`, teamID, p.EventID, p.Round)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "team remove failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "no such team")
		return
	}
	if _, err := results.Append(ctx, h.DB, p.EventID, teamID, results.TeamWithdrawn, map[string]any{"by": "host"}); err != nil {
		logx.Warn("results.append", "kind", results.TeamWithdrawn, "team_id", teamID, "err", err.Error())
	}
	if err := endTeamSessions(ctx, h.DB, "t.id = $1", teamID); err != nil {
		logx.Warn("admin.party.team.signout", "team_id", teamID, "err", err.Error())
	}
	activity.Record(ctx, h.DB, p.EventID, &teamID, activity.Actor{}, activity.TeamWithdrawn, map[string]any{"by_host": true})
	h.Hub.Publish(ws.EventTopic(p.EventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}

// partyOpen refuses changes to a party that is over or has lapsed.
func (h *Handlers) partyOpen(w http.ResponseWriter, p hostParty) bool {
	switch {
	case p.State == "completed" || p.State == "cancelled":
		httpx.Error(w, http.StatusConflict, "the party is over")
		return false
	case partyLapsed(p):
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this party's time is up", "code": "expired"})
		return false
	}
	return true
}

// partyLapsed is whether the party's time is up: its first game was not
// started in time, or its days of play are over.
func partyLapsed(p hostParty) bool {
	return p.State != "completed" && p.State != "cancelled" && time.Now().After(p.ValidUntil)
}

// HostPartyAction pauses or resumes the game being played. Games start
// when the first team joins and end with "Finish game" (FinishHostedGame).
//
// POST /api/party/{token}/{action}
func (h *Handlers) HostPartyAction(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	if !h.partyOpen(w, p) {
		return
	}
	ctx := r.Context()
	action := chi.URLParam(r, "action")
	var want, update, kind string
	switch action {
	case "pause":
		want, update, kind = "running", `UPDATE events SET state='paused', paused_at=now_text() WHERE id=$1 AND state=$2`, activity.EventPaused
	case "resume":
		want, update, kind = "paused", `UPDATE events SET state='running', `+activity.ClosePause+` WHERE id=$1 AND state=$2`, activity.EventResumed
	default:
		httpx.Error(w, http.StatusNotFound, "unknown action")
		return
	}
	if p.State != want || !p.RoundOpen {
		httpx.Error(w, http.StatusConflict, "no game is being played")
		return
	}
	res, err := h.DB.ExecContext(ctx, update, p.EventID, want)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, action+" failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusConflict, "the party changed meanwhile; reload the page")
		return
	}
	activity.Record(ctx, h.DB, p.EventID, nil, activity.Actor{}, kind, map[string]any{"by_host": true})
	h.Hub.Publish(ws.EventTopic(p.EventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}

// ResendPartyLinks mails a booker the host links of their parties that
// are still to come or under way: the links they have, so an open host
// page keeps working. Always 204, so the endpoint does not tell anyone
// which emails have booked.
//
// POST /api/party/resend
func (h *Handlers) ResendPartyLinks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		httpx.Error(w, http.StatusBadRequest, "enter your email address")
		return
	}
	ctx := r.Context()
	rows, err := h.DB.QueryContext(ctx, `SELECT e.id FROM events e WHERE `+partyOfHostSQL, email)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := h.resendHostLink(ctx, id, email); err != nil {
			logx.Warn("admin.party.resend", "event_id", id, "err", err.Error())
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// partyOfHostSQL matches a party still to come or under way that $1
// booked: paid for it, or saved its trail to that email.
const partyOfHostSQL = `e.party AND e.state IN ('scheduled', 'running', 'paused')
	  AND (EXISTS (SELECT 1 FROM ticket_purchases p WHERE p.event_id = e.id AND p.kind IN ('party', 'hosted')
	               AND p.state = 'paid' AND p.buyer_email = $1)
	    OR EXISTS (SELECT 1 FROM trails t WHERE t.event_id = e.id AND LOWER(t.email) = $1))`

// resendHostLink mails a party's current host link to email again; an
// open host page keeps working.
func (h *Handlers) resendHostLink(ctx context.Context, eventID int64, email string) error {
	token, err := h.currentHostToken(ctx, eventID)
	if err != nil {
		return err
	}
	return h.sendPartyHostLink(ctx, eventID, email, token, true)
}

// adminPartyDTO is a party found by its host's email.
type adminPartyDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Adventure string `json:"adventure"`
	OrgName   string `json:"org_name"`
	CreatedAt string `json:"created_at"`
}

// AdminPartiesByHost lists the parties still to come or under way that
// an email booked, so support can send a lost host link again.
// Admin-only.
//
// GET /api/admin/parties?email=
func (h *Handlers) AdminPartiesByHost(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		httpx.Error(w, http.StatusBadRequest, "enter the host's email address")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT e.id, e.party_name, e.state, COALESCE(a.title, e.adventure_slug), COALESCE(o.name, ''), e.created_at
		FROM events e
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		LEFT JOIN organizations o ON o.id = e.org_id
		WHERE `+partyOfHostSQL+`
		ORDER BY e.id DESC`, email)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	out := []adminPartyDTO{}
	for rows.Next() {
		var p adminPartyDTO
		if err := rows.Scan(&p.ID, &p.Name, &p.State, &p.Adventure, &p.OrgName, &p.CreatedAt); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out = append(out, p)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"parties": out})
}

// AdminResendHostLink mails a party's host a new host link, to the
// email that booked it (never another address). Admin-only.
//
// POST /api/admin/parties/{eventID}/resend-host-link {"email": "..."}
func (h *Handlers) AdminResendHostLink(w http.ResponseWriter, r *http.Request) {
	eventID, err := strconv.ParseInt(chi.URLParam(r, "eventID"), 10, 64)
	if err != nil || eventID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	var ok bool
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM events e WHERE e.id = $2 AND `+partyOfHostSQL+`)`, email, eventID).Scan(&ok); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no live party booked with that email")
		return
	}
	// Support sends a new link: the old one may be what was lost or leaked.
	token, err := h.replaceHostToken(r.Context(), h.DB, eventID)
	if err == nil {
		err = h.sendPartyHostLink(r.Context(), eventID, email, token, true)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "send failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResendHostLinkToBuyer mails a hosted event's buyer their host link
// again, for when the first mail was lost. The organisation never sees
// the link: it is the buyer's key to the host page.
//
// POST /api/admin/events/{eventID}/hosted/resend-host-link
func (h *Handlers) ResendHostLinkToBuyer(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	ctx := r.Context()
	var email string
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(
		         (SELECT p.buyer_email FROM ticket_purchases p WHERE p.event_id = e.id AND p.kind IN ('party', 'hosted') AND p.state = 'paid' ORDER BY p.id LIMIT 1),
		         (SELECT LOWER(t.email) FROM trails t WHERE t.event_id = e.id AND t.email <> '' ORDER BY t.id LIMIT 1),
		         '')
		FROM events e
		WHERE e.id = $1 AND e.party AND e.state IN ('scheduled', 'running', 'paused')`, eventID).Scan(&email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if email == "" {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "no buyer to send the host link to", "code": "no_buyer"})
		return
	}
	if err := h.resendHostLink(ctx, eventID, email); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "send failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fulfilPartyCheckout completes a paid party booking: the event, its
// cards, and the host link mailed to the booker. It owns tx from here.
func (h *Handlers) fulfilPartyCheckout(ctx context.Context, tx *sql.Tx, event payments.Event, purchaseID int64, offerID sql.NullInt64, partyName, email string, kids int) error {
	if !offerID.Valid {
		// The seller withdrew the offer while the payment was in flight;
		// the row is kept for bookkeeping and the seller refunds it.
		logx.Warn("payments.party_checkout.offer_gone", "ticket_purchase_id", purchaseID)
		return nil
	}
	eventID, hostToken, err := fulfilParty(ctx, tx, purchaseID, offerID.Int64, partyName, kids)
	if err != nil {
		return fmt.Errorf("party create: %w", err)
	}
	if err := h.keepHostToken(ctx, tx, eventID, hostToken); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_purchases
		   SET state = 'paid', paid_at = now_text(), stripe_session_id = COALESCE(stripe_session_id, $1)
		 WHERE id = $2
	`, event.Checkout.SessionID, purchaseID); err != nil {
		return fmt.Errorf("purchase update: %w", err)
	}
	if err := updateTicketStripeMetadata(ctx, tx, purchaseID, *event.Checkout); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Best effort, as for a ticket: the booker can have the link sent
	// again from the party page.
	if err := h.sendPartyHostLink(ctx, eventID, email, hostToken, false); err != nil {
		logx.Warn("admin.party.mail", "purchase_id", purchaseID, "event_id", eventID, "err", err.Error())
	}
	logx.Info("admin.party.paid", "purchase_id", purchaseID, "event_id", eventID, "kids", kids)
	return nil
}

// HostPartyQR draws a QR code for the host page: a station card's
// (?station=zone) or, without one, the player link. Only the party's
// own, so the endpoint is no QR maker for anything else.
//
// GET /api/party/{token}/qr
func (h *Handlers) HostPartyQR(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	base := strings.TrimRight(h.PublicURL, "/")
	if base == "" {
		httpx.Error(w, http.StatusServiceUnavailable, "the operator must set ZV_PUBLIC_URL before QR codes can be made")
		return
	}
	var (
		target string
		err    error
	)
	if zone := r.URL.Query().Get("station"); zone != "" {
		var code string
		err = h.DB.QueryRowContext(r.Context(),
			`SELECT code FROM party_stations WHERE event_id = $1 AND zone_slug = $2`, p.EventID, zone).Scan(&code)
		target = base + "/play?station=" + code
	} else {
		target = base + "/j/" + p.PlayerToken
	}
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such card")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	png, err := qrcode.Encode(target, qrcode.Medium, 360)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "qr encode failed", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(png)
}
