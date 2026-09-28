package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	mailerpkg "github.com/klahr/zonventure/internal/mailer"
)

// Partner box redemptions. Experience partners (Live it, TrueStory) sell
// gift boxes for an organisation's self-guided adventures. The buyer
// redeems the box's code on /redeem; the request waits until an organiser
// has checked the code in the partner's portal. Approving it creates a
// team on the product's event, as a paid /store ticket does, and emails
// the team link. The postal address, post-on date and posted flag track
// what the organisation mails to the buyer. See migration 0072.

const (
	partnerLiveit    = "liveit"
	partnerTrueStory = "truestory"
)

// partnerNames are the partners' own spellings, shown to buyers.
var partnerNames = map[string]string{
	partnerLiveit:    "Live it",
	partnerTrueStory: "TrueStory",
}

// redemptionSlotTTL is how long an approved box's player links last: a
// gift box may sit on a shelf for months, so a year rather than the
// 30 days a bought-to-play-now ticket gets.
const redemptionSlotTTL = 365 * 24 * time.Hour

type partnerProductDTO struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	EventID        int64  `json:"event_id"`
	EventName      string `json:"event_name"`
	AdventureTitle string `json:"adventure_title"`
	TeamSize       int    `json:"team_size"`
	Enabled        bool   `json:"enabled"`
}

type partnerDTO struct {
	ID       int64               `json:"id"`
	Kind     string              `json:"kind"`
	Name     string              `json:"name"`
	Enabled  bool                `json:"enabled"`
	Products []partnerProductDTO `json:"products"`
}

type redemptionDTO struct {
	ID            int64  `json:"id"`
	State         string `json:"state"`
	PartnerKind   string `json:"partner_kind"`
	PartnerName   string `json:"partner_name"`
	ProductID     int64  `json:"product_id"`
	ProductName   string `json:"product_name"`
	EventID       int64  `json:"event_id"`
	EventName     string `json:"event_name"`
	BoxNumber     string `json:"box_number"`
	SecurityCode  string `json:"security_code"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	StreetAddress string `json:"street_address"`
	Address2      string `json:"address_2"`
	PostalCode    string `json:"postal_code"`
	City          string `json:"city"`
	PostAt        string `json:"post_at"`
	PostedAt      string `json:"posted_at"`
	Note          string `json:"note"`
	CreatedAt     string `json:"created_at"`
	DecidedAt     string `json:"decided_at"`
	DecidedBy     string `json:"decided_by"`
	TeamID        int64  `json:"team_id,omitempty"`
	TeamName      string `json:"team_name"`
}

// RedeemRequest is a box redemption, from a buyer on /redeem or an
// organiser entering a phone or email order.
type RedeemRequest struct {
	ProductID     int64  `json:"product_id"`
	BoxNumber     string `json:"box_number"`
	SecurityCode  string `json:"security_code"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	StreetAddress string `json:"street_address"`
	Address2      string `json:"address_2"`
	PostalCode    string `json:"postal_code"`
	City          string `json:"city"`
	AcceptedTerms bool   `json:"accepted_terms"`
	// Organiser-only.
	PostAt string `json:"post_at"`
	Note   string `json:"note"`
}

// redeemError is a request failure with the reason the forms switch on.
type redeemError struct {
	status int
	reason string
	msg    string
}

func (e *redeemError) write(w http.ResponseWriter) {
	httpx.JSON(w, e.status, map[string]string{"error": e.msg, "reason": e.reason})
}

// normalizeCode upper-cases a code and drops the spaces and dashes
// people type or copy along with it.
func normalizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(s)))
}

// checkPartnerCode normalises a box's codes and checks them against the
// partner's format: a numeric box number plus a security code for Live
// it, a 9-character experience code (and no box number) for TrueStory.
func checkPartnerCode(kind, box, code string) (string, string, bool) {
	box, code = normalizeCode(box), normalizeCode(code)
	isAlnum := func(s string) bool {
		for _, r := range s {
			if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				return false
			}
		}
		return true
	}
	switch kind {
	case partnerLiveit:
		if box == "" || len(box) > 20 || code == "" || len(code) > 32 || !isAlnum(code) {
			return "", "", false
		}
		for _, r := range box {
			if r < '0' || r > '9' {
				return "", "", false
			}
		}
		return box, code, true
	case partnerTrueStory:
		if box != "" || len(code) != 9 || !isAlnum(code) {
			return "", "", false
		}
		return box, code, true
	}
	return "", "", false
}

// Buyers' redemption attempts are capped per IP: every attempt, good or
// bad, costs one, so the form can't be used to guess codes or to fill
// the approval queue. A household redeems a box or two.
const (
	redeemWindow   = time.Hour
	redeemPerIPMax = 10
)

type redeemLimiter struct {
	mu    sync.Mutex
	perIP map[string][]time.Time
}

var redeemLimit = redeemLimiter{perIP: map[string][]time.Time{}}

// allow records an attempt from ip and reports whether it may proceed,
// with the wait to surface as Retry-After when not.
func (l *redeemLimiter) allow(now time.Time, ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-redeemWindow)
	if len(l.perIP) > 10000 {
		for k, ts := range l.perIP {
			if ts = trimBefore(ts, cutoff); len(ts) == 0 {
				delete(l.perIP, k)
			} else {
				l.perIP[k] = ts
			}
		}
	}
	ts := trimBefore(l.perIP[ip], cutoff)
	if len(ts) >= redeemPerIPMax {
		l.perIP[ip] = ts
		return false, ts[0].Add(redeemWindow).Sub(now).Round(time.Second)
	}
	l.perIP[ip] = append(ts, now)
	return true, 0
}

// maxRedeemField caps every free-text field of a redemption.
const maxRedeemField = 200

// createRedemption validates req and stores it as a pending redemption.
// orgID, when non-zero, confines the product to that organisation;
// createdBy is the organiser entering it, nil for a buyer.
func (h *Handlers) createRedemption(ctx context.Context, req RedeemRequest, orgID int64, createdBy *int64) (int64, *redeemError) {
	for _, f := range []*string{&req.Name, &req.Email, &req.StreetAddress, &req.Address2, &req.PostalCode, &req.City, &req.Note} {
		*f = strings.TrimSpace(*f)
	}
	if req.ProductID <= 0 || req.Name == "" || req.Email == "" || req.StreetAddress == "" ||
		req.PostalCode == "" || req.City == "" || strings.TrimSpace(req.SecurityCode) == "" {
		return 0, &redeemError{http.StatusBadRequest, "missing_field", "fill in every required field"}
	}
	for _, f := range []string{req.Name, req.Email, req.StreetAddress, req.Address2, req.PostalCode, req.City} {
		if len(f) > maxRedeemField {
			return 0, &redeemError{http.StatusBadRequest, "missing_field", "a field is too long"}
		}
	}
	req.Email = strings.ToLower(req.Email)
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return 0, &redeemError{http.StatusBadRequest, "invalid_email", "not a valid email address"}
	}
	postAt := strings.TrimSpace(req.PostAt)
	if postAt != "" {
		if _, err := time.Parse("2006-01-02", postAt); err != nil {
			return 0, &redeemError{http.StatusBadRequest, "missing_field", "post_at must be YYYY-MM-DD"}
		}
	}

	var (
		partnerID   int64
		kind        string
		productOrg  int64
		productName string
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT p.id, p.kind, p.org_id, pp.name
		FROM partner_products pp JOIN partners p ON p.id = pp.partner_id
		WHERE pp.id = $1 AND pp.enabled AND p.enabled
	`, req.ProductID).Scan(&partnerID, &kind, &productOrg, &productName)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && orgID != 0 && productOrg != orgID) {
		return 0, &redeemError{http.StatusBadRequest, "unknown_product", "unknown product"}
	}
	if err != nil {
		logx.Warn("admin.redeem.product_lookup", "err", err.Error())
		return 0, &redeemError{http.StatusInternalServerError, "", "lookup failed"}
	}
	box, code, ok := checkPartnerCode(kind, req.BoxNumber, req.SecurityCode)
	if !ok {
		return 0, &redeemError{http.StatusBadRequest, "invalid_code", "that doesn't look like a " + partnerNames[kind] + " code"}
	}

	var id int64
	err = h.DB.QueryRowContext(ctx, `
		INSERT INTO partner_redemptions(partner_id, product_id, product_name, kind, box_number, security_code,
		    name, email, street_address, address_2, postal_code, city, post_at, note, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id
	`, partnerID, req.ProductID, productName, kind, box, code, req.Name, req.Email, req.StreetAddress,
		req.Address2, req.PostalCode, req.City, postAt, req.Note, createdBy).Scan(&id)
	if isUniqueViolation(err) {
		return 0, &redeemError{http.StatusConflict, "code_used", "this code has already been redeemed"}
	}
	if err != nil {
		logx.Warn("admin.redeem.insert", "err", err.Error())
		return 0, &redeemError{http.StatusInternalServerError, "", "save failed"}
	}
	h.notifyNewRedemption(ctx, id, productOrg)
	return id, nil
}

/* ---------------- Public: /redeem ---------------- */

type redeemOptionProduct struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	AdventureTitle string `json:"adventure_title"`
	OrgName        string `json:"org_name"`
}

type redeemOptionPartner struct {
	Kind     string                `json:"kind"`
	Name     string                `json:"name"`
	Products []redeemOptionProduct `json:"products"`
}

// RedeemOptions lists what boxes can be redeemed for, grouped by
// partner: every enabled product on a running self-guided event.
func (h *Handlers) RedeemOptions(w http.ResponseWriter, r *http.Request) {
	h.redeemOptions(w, r, 0)
}

// RedeemOptionsByOrg is RedeemOptions for one organisation's /o/{slug}/redeem.
func (h *Handlers) RedeemOptionsByOrg(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.redeemOrg(w, r)
	if !ok {
		return
	}
	h.redeemOptions(w, r, orgID)
}

func (h *Handlers) redeemOptions(w http.ResponseWriter, r *http.Request, orgID int64) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT p.kind, pp.id, pp.name, COALESCE(a.title, e.adventure_slug), o.name
		FROM partner_products pp
		JOIN partners p ON p.id = pp.partner_id
		JOIN events e ON e.id = pp.event_id
		JOIN organizations o ON o.id = p.org_id
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE pp.enabled AND p.enabled AND e.public_for_sale AND e.state = 'running'
		  AND ($1::bigint = 0 OR p.org_id = $1)
		ORDER BY p.kind, pp.name, pp.id
	`, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	partners := []redeemOptionPartner{}
	for rows.Next() {
		var kind string
		var p redeemOptionProduct
		if err := rows.Scan(&kind, &p.ID, &p.Name, &p.AdventureTitle, &p.OrgName); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		if n := len(partners); n == 0 || partners[n-1].Kind != kind {
			partners = append(partners, redeemOptionPartner{Kind: kind, Name: partnerNames[kind]})
		}
		last := &partners[len(partners)-1]
		last.Products = append(last.Products, p)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"partners": partners})
}

// Redeem is the buyer's redemption from /redeem.
func (h *Handlers) Redeem(w http.ResponseWriter, r *http.Request) {
	h.redeem(w, r, 0)
}

// RedeemByOrg is Redeem for /o/{slug}/redeem: the product must be the
// organisation's.
func (h *Handlers) RedeemByOrg(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.redeemOrg(w, r)
	if !ok {
		return
	}
	h.redeem(w, r, orgID)
}

func (h *Handlers) redeem(w http.ResponseWriter, r *http.Request, orgID int64) {
	if ok, wait := redeemLimit.allow(time.Now(), auth.ClientIP(r)); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		(&redeemError{http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later"}).write(w)
		return
	}
	var req RedeemRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !req.AcceptedTerms {
		(&redeemError{http.StatusBadRequest, "missing_field", "accept the terms to continue"}).write(w)
		return
	}
	// A buyer never sets the organiser's fields.
	req.PostAt, req.Note = "", ""
	if _, rerr := h.createRedemption(r.Context(), req, orgID, nil); rerr != nil {
		rerr.write(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *Handlers) redeemOrg(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var orgID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM organizations WHERE slug = $1`, strings.TrimSpace(chi.URLParam(r, "slug"))).Scan(&orgID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "organisation not found")
		return 0, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, false
	}
	return orgID, true
}

/* ---------------- Organiser: partners ---------------- */

// redemptionOrg returns the caller's active organisation, for org admins
// and members. adminOnly narrows it to org admins (partner setup).
func redemptionOrg(w http.ResponseWriter, r *http.Request, adminOnly bool) (auth.User, int64, bool) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || (me.Role != auth.RoleOrgAdmin && (adminOnly || me.Role != auth.RoleOrgMember)) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return me, 0, false
	}
	return me, *me.OrgID, true
}

func (h *Handlers) loadPartners(ctx context.Context, orgID int64, partnerID int64) ([]partnerDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT p.id, p.kind, p.enabled,
		       COALESCE(pp.id, 0), COALESCE(pp.name, ''), COALESCE(pp.event_id, 0),
		       COALESCE(e.name, ''), COALESCE(a.title, e.adventure_slug, ''),
		       COALESCE(pp.team_size, 0), COALESCE(pp.enabled, FALSE)
		FROM partners p
		LEFT JOIN partner_products pp ON pp.partner_id = p.id
		LEFT JOIN events e ON e.id = pp.event_id
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE p.org_id = $1 AND ($2::bigint = 0 OR p.id = $2)
		ORDER BY p.kind, pp.name, pp.id
	`, orgID, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []partnerDTO{}
	for rows.Next() {
		var p partnerDTO
		var pr partnerProductDTO
		if err := rows.Scan(&p.ID, &p.Kind, &p.Enabled, &pr.ID, &pr.Name, &pr.EventID,
			&pr.EventName, &pr.AdventureTitle, &pr.TeamSize, &pr.Enabled); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != p.ID {
			p.Name = partnerNames[p.Kind]
			p.Products = []partnerProductDTO{}
			out = append(out, p)
		}
		if pr.ID != 0 {
			last := &out[len(out)-1]
			last.Products = append(last.Products, pr)
		}
	}
	return out, rows.Err()
}

// ListPartners returns the organisation's partners and their products.
func (h *Handlers) ListPartners(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	partners, err := h.loadPartners(r.Context(), orgID, 0)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"partners": partners})
}

func (h *Handlers) writePartner(w http.ResponseWriter, r *http.Request, status int, orgID, partnerID int64) {
	partners, err := h.loadPartners(r.Context(), orgID, partnerID)
	if err != nil || len(partners) == 0 {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, status, partners[0])
}

// CreatePartner adds a partner the organisation takes boxes from.
func (h *Handlers) CreatePartner(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if _, known := partnerNames[req.Kind]; !known {
		httpx.Error(w, http.StatusBadRequest, "unknown partner")
		return
	}
	var id int64
	err := h.DB.QueryRowContext(r.Context(),
		`INSERT INTO partners(org_id, kind) VALUES ($1, $2) RETURNING id`, orgID, req.Kind).Scan(&id)
	if isUniqueViolation(err) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "partner already added", "reason": "exists"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.writePartner(w, r, http.StatusCreated, orgID, id)
}

// partnerFrom reads {partnerID} and checks it is the organisation's.
func (h *Handlers) partnerFrom(w http.ResponseWriter, r *http.Request, orgID int64) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "partnerID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad partner id")
		return 0, false
	}
	var owner int64
	err = h.DB.QueryRowContext(r.Context(), `SELECT org_id FROM partners WHERE id = $1`, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != orgID) {
		httpx.Error(w, http.StatusNotFound, "partner not found")
		return 0, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, false
	}
	return id, true
}

// UpdatePartner enables or disables a partner.
func (h *Handlers) UpdatePartner(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, true)
	if !ok {
		return
	}
	id, ok := h.partnerFrom(w, r, orgID)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Enabled != nil {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE partners SET enabled = $1 WHERE id = $2`, *req.Enabled, id); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
			return
		}
	}
	h.writePartner(w, r, http.StatusOK, orgID, id)
}

// DeletePartner removes a partner with no redemptions; one with
// redemptions can only be disabled, so their history stays.
func (h *Handlers) DeletePartner(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, true)
	if !ok {
		return
	}
	id, ok := h.partnerFrom(w, r, orgID)
	if !ok {
		return
	}
	var used bool
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM partner_redemptions WHERE partner_id = $1)`, id).Scan(&used); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if used {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "partner has redemptions; disable it instead", "reason": "in_use"})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM partners WHERE id = $1`, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "delete failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type partnerProductRequest struct {
	Name     *string `json:"name"`
	EventID  *int64  `json:"event_id"`
	TeamSize *int    `json:"team_size"`
	Enabled  *bool   `json:"enabled"`
}

// checkProductEvent confirms eventID is one of the organisation's
// self-guided events and returns its ticket team size.
func (h *Handlers) checkProductEvent(ctx context.Context, orgID, eventID int64) (int, bool, error) {
	var teamSize int
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(ticket_team_size, 1) FROM events
		WHERE id = $1 AND org_id = $2 AND public_for_sale
	`, eventID, orgID).Scan(&teamSize)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return teamSize, err == nil, err
}

// CreatePartnerProduct adds what a partner's boxes can be redeemed for.
func (h *Handlers) CreatePartnerProduct(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, true)
	if !ok {
		return
	}
	partnerID, ok := h.partnerFrom(w, r, orgID)
	if !ok {
		return
	}
	var req partnerProductRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" || len(*req.Name) > maxRedeemField || req.EventID == nil {
		httpx.Error(w, http.StatusBadRequest, "name and event_id are required")
		return
	}
	defaultSize, found, err := h.checkProductEvent(r.Context(), orgID, *req.EventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !found {
		httpx.Error(w, http.StatusBadRequest, "event must be one of your self-guided adventures")
		return
	}
	size := defaultSize
	if req.TeamSize != nil {
		size = *req.TeamSize
	}
	if size < 1 || size > 50 {
		httpx.Error(w, http.StatusBadRequest, "team_size must be 1–50")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO partner_products(partner_id, name, event_id, team_size) VALUES ($1, $2, $3, $4) RETURNING id
	`, partnerID, strings.TrimSpace(*req.Name), *req.EventID, size).Scan(&id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.writeProduct(w, r, http.StatusCreated, orgID, id)
}

func (h *Handlers) writeProduct(w http.ResponseWriter, r *http.Request, status int, orgID, productID int64) {
	partners, err := h.loadPartners(r.Context(), orgID, 0)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	for _, p := range partners {
		for _, pr := range p.Products {
			if pr.ID == productID {
				httpx.JSON(w, status, pr)
				return
			}
		}
	}
	httpx.Error(w, http.StatusNotFound, "product not found")
}

// productFrom reads {productID} and checks it is the organisation's.
func (h *Handlers) productFrom(w http.ResponseWriter, r *http.Request, orgID int64) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "productID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad product id")
		return 0, false
	}
	var owner int64
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.org_id FROM partner_products pp JOIN partners p ON p.id = pp.partner_id WHERE pp.id = $1
	`, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != orgID) {
		httpx.Error(w, http.StatusNotFound, "product not found")
		return 0, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, false
	}
	return id, true
}

// UpdatePartnerProduct changes a product's name, event, team size or
// whether it is offered.
func (h *Handlers) UpdatePartnerProduct(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, true)
	if !ok {
		return
	}
	id, ok := h.productFrom(w, r, orgID)
	if !ok {
		return
	}
	var req partnerProductRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil && (strings.TrimSpace(*req.Name) == "" || len(*req.Name) > maxRedeemField) {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.EventID != nil {
		if _, found, err := h.checkProductEvent(r.Context(), orgID, *req.EventID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		} else if !found {
			httpx.Error(w, http.StatusBadRequest, "event must be one of your self-guided adventures")
			return
		}
	}
	if req.TeamSize != nil && (*req.TeamSize < 1 || *req.TeamSize > 50) {
		httpx.Error(w, http.StatusBadRequest, "team_size must be 1–50")
		return
	}
	var name *string
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		name = &n
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE partner_products SET
		    name = COALESCE($1, name), event_id = COALESCE($2, event_id),
		    team_size = COALESCE($3, team_size), enabled = COALESCE($4, enabled)
		WHERE id = $5
	`, name, req.EventID, req.TeamSize, req.Enabled, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.writeProduct(w, r, http.StatusOK, orgID, id)
}

// DeletePartnerProduct removes a product no redemption names.
func (h *Handlers) DeletePartnerProduct(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, true)
	if !ok {
		return
	}
	id, ok := h.productFrom(w, r, orgID)
	if !ok {
		return
	}
	var used bool
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM partner_redemptions WHERE product_id = $1)`, id).Scan(&used); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if used {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "product has redemptions; disable it instead", "reason": "in_use"})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM partner_products WHERE id = $1`, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "delete failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ---------------- Organiser: redemptions ---------------- */

const redemptionSelect = `
	SELECT r.id, r.state, r.kind, COALESCE(r.product_id, 0), COALESCE(pp.name, r.product_name),
	       COALESCE(pp.event_id, 0), COALESCE(e.name, ''),
	       r.box_number, r.security_code, r.name, r.email, r.street_address, r.address_2,
	       r.postal_code, r.city, r.post_at, COALESCE(r.posted_at, ''), r.note, r.created_at,
	       COALESCE(r.decided_at, ''), COALESCE(u.username, ''),
	       COALESCE(r.team_id, 0), COALESCE(t.name, '')
	FROM partner_redemptions r
	JOIN partners p ON p.id = r.partner_id
	LEFT JOIN partner_products pp ON pp.id = r.product_id
	LEFT JOIN events e ON e.id = pp.event_id
	LEFT JOIN users u ON u.id = r.decided_by_user_id
	LEFT JOIN teams t ON t.id = r.team_id
`

func scanRedemption(sc interface{ Scan(...any) error }) (redemptionDTO, error) {
	var d redemptionDTO
	err := sc.Scan(&d.ID, &d.State, &d.PartnerKind, &d.ProductID, &d.ProductName, &d.EventID, &d.EventName,
		&d.BoxNumber, &d.SecurityCode, &d.Name, &d.Email, &d.StreetAddress, &d.Address2,
		&d.PostalCode, &d.City, &d.PostAt, &d.PostedAt, &d.Note, &d.CreatedAt,
		&d.DecidedAt, &d.DecidedBy, &d.TeamID, &d.TeamName)
	d.PartnerName = partnerNames[d.PartnerKind]
	return d, err
}

// ListRedemptions returns the organisation's redemptions in one state
// (pending by default, or all) with a count per state.
func (h *Handlers) ListRedemptions(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	switch state {
	case "":
		state = "pending"
	case "pending", "approved", "rejected", "cancelled", "all":
	default:
		httpx.Error(w, http.StatusBadRequest, "bad state")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), redemptionSelect+`
		WHERE p.org_id = $1 AND ($2::text = 'all' OR r.state = $2)
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT 500
	`, orgID, state)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	out := []redemptionDTO{}
	for rows.Next() {
		d, err := scanRedemption(rows)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out = append(out, d)
	}
	counts := map[string]int{"pending": 0, "approved": 0, "rejected": 0, "cancelled": 0}
	crows, err := h.DB.QueryContext(r.Context(), `
		SELECT r.state, COUNT(*) FROM partner_redemptions r JOIN partners p ON p.id = r.partner_id
		WHERE p.org_id = $1 GROUP BY r.state
	`, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer crows.Close()
	for crows.Next() {
		var s string
		var n int
		if err := crows.Scan(&s, &n); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		counts[s] = n
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"redemptions": out, "counts": counts})
}

// CreateRedemption is an organiser entering a box redemption taken by
// phone or email. It lands as pending, like a buyer's.
func (h *Handlers) CreateRedemption(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	var req RedeemRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	id, rerr := h.createRedemption(r.Context(), req, orgID, &me.ID)
	if rerr != nil {
		rerr.write(w)
		return
	}
	h.writeRedemption(w, r, http.StatusCreated, id)
}

func (h *Handlers) writeRedemption(w http.ResponseWriter, r *http.Request, status int, id int64) {
	d, err := scanRedemption(h.DB.QueryRowContext(r.Context(), redemptionSelect+`WHERE r.id = $1`, id))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, status, d)
}

// redemptionFrom reads {redemptionID} and checks it is the organisation's.
func (h *Handlers) redemptionFrom(w http.ResponseWriter, r *http.Request, orgID int64) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "redemptionID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad redemption id")
		return 0, false
	}
	var owner int64
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.org_id FROM partner_redemptions r JOIN partners p ON p.id = r.partner_id WHERE r.id = $1
	`, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != orgID) {
		httpx.Error(w, http.StatusNotFound, "redemption not found")
		return 0, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, false
	}
	return id, true
}

// ApproveRedemption accepts a checked code: it creates the team on the
// product's event and emails the buyer the team link.
func (h *Handlers) ApproveRedemption(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	id, ok := h.redemptionFrom(w, r, orgID)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		state, email, kind, eventState string
		eventID                        int64
		teamSize                       int
	)
	// A product deleted since (with its event, say) leaves nothing to
	// approve into: that reads as a closed event.
	if err := tx.QueryRowContext(ctx, `
		SELECT r.state, r.email, r.kind, COALESCE(pp.event_id, 0), COALESCE(pp.team_size, 0), COALESCE(e.state, '')
		FROM partner_redemptions r
		LEFT JOIN partner_products pp ON pp.id = r.product_id
		LEFT JOIN events e ON e.id = pp.event_id
		WHERE r.id = $1 FOR UPDATE OF r
	`, id).Scan(&state, &email, &kind, &eventID, &teamSize, &eventState); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	if state != "pending" {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "redemption is " + state, "reason": "not_pending"})
		return
	}
	if eventState != "running" {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "the product's event isn't running", "reason": "event_closed"})
		return
	}
	teamID, teamToken, err := createTicketTeam(ctx, tx, eventID, email, teamSize)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE player_slots SET expires_at = $1 WHERE team_id = $2`,
		time.Now().UTC().Add(redemptionSlotTTL).Format("2006-01-02 15:04:05"), teamID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE partner_redemptions
		   SET state = 'approved', team_id = $1, decided_by_user_id = $2, decided_at = now_text()
		 WHERE id = $3
	`, teamID, me.ID, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	if err := h.sendRedemptionApproved(ctx, email, kind, teamToken); err != nil {
		logx.Warn("admin.redemption.approve.mail", "redemption_id", id, "team_id", teamID, "err", err.Error())
	}
	logx.Info("admin.redemption.approved", "redemption_id", id, "team_id", teamID)
	h.writeRedemption(w, r, http.StatusOK, id)
}

// RejectRedemption turns down a code the partner doesn't recognise. The
// code is free to be redeemed again, correctly this time.
func (h *Handlers) RejectRedemption(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	id, ok := h.redemptionFrom(w, r, orgID)
	if !ok {
		return
	}
	var email, kind string
	err := h.DB.QueryRowContext(r.Context(), `
		UPDATE partner_redemptions
		   SET state = 'rejected', decided_by_user_id = $1, decided_at = now_text()
		 WHERE id = $2 AND state = 'pending'
		RETURNING email, kind
	`, me.ID, id).Scan(&email, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "redemption isn't pending", "reason": "not_pending"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reject failed", err)
		return
	}
	h.sendRedemptionMail(r.Context(), "redemption_rejected", mailerpkg.Message{
		To:      []string{email},
		Subject: "About your " + partnerNames[kind] + " box",
		Text: "We couldn't confirm the code from your " + partnerNames[kind] + " box with " + partnerNames[kind] + ".\r\n\r\n" +
			"Please check the code and redeem it again, or reply to this email and we'll sort it out.\r\n",
	}, id)
	h.writeRedemption(w, r, http.StatusOK, id)
}

// CancelRedemption withdraws a pending or approved redemption, for a
// buyer who returns the box. An approved one's team is withdrawn and
// its links stop working.
func (h *Handlers) CancelRedemption(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	id, ok := h.redemptionFrom(w, r, orgID)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "cancel failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		email, kind string
		teamID      sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		UPDATE partner_redemptions
		   SET state = 'cancelled', decided_by_user_id = $1, decided_at = now_text()
		 WHERE id = $2 AND state IN ('pending', 'approved')
		RETURNING email, kind, team_id
	`, me.ID, id).Scan(&email, &kind, &teamID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "redemption can't be cancelled", "reason": "not_cancellable"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "cancel failed", err)
		return
	}
	if teamID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE teams SET withdrawn_at = now_text() WHERE id = $1 AND withdrawn_at IS NULL
		`, teamID.Int64); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "cancel failed", err)
			return
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE player_slots SET expires_at = now_text() WHERE team_id = $1`, teamID.Int64); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "cancel failed", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "cancel failed", err)
		return
	}
	h.sendRedemptionMail(ctx, "redemption_cancelled", mailerpkg.Message{
		To:      []string{email},
		Subject: "Your " + partnerNames[kind] + " box redemption is cancelled",
		Text:    "Your redemption of a " + partnerNames[kind] + " box has been cancelled. Reply to this email if that's a surprise.\r\n",
	}, id)
	h.writeRedemption(w, r, http.StatusOK, id)
}

// UpdateRedemption sets what the organisation tracks by hand: when to
// post the box, whether it has been posted, and a note.
func (h *Handlers) UpdateRedemption(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	id, ok := h.redemptionFrom(w, r, orgID)
	if !ok {
		return
	}
	var req struct {
		PostAt *string `json:"post_at"`
		Note   *string `json:"note"`
		Posted *bool   `json:"posted"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.PostAt != nil {
		*req.PostAt = strings.TrimSpace(*req.PostAt)
		if *req.PostAt != "" {
			if _, err := time.Parse("2006-01-02", *req.PostAt); err != nil {
				httpx.Error(w, http.StatusBadRequest, "post_at must be YYYY-MM-DD")
				return
			}
		}
	}
	if req.Note != nil && len(*req.Note) > 2000 {
		httpx.Error(w, http.StatusBadRequest, "note is too long")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE partner_redemptions SET
		    post_at = COALESCE($1, post_at),
		    note = COALESCE($2, note),
		    posted_at = CASE WHEN $3::boolean IS NULL THEN posted_at
		                     WHEN $3 THEN COALESCE(posted_at, now_text())
		                     ELSE NULL END
		WHERE id = $4
	`, req.PostAt, req.Note, req.Posted, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.writeRedemption(w, r, http.StatusOK, id)
}

/* ---------------- Mail ---------------- */

// sendRedemptionMail sends through the outbox when there is one, and
// logs rather than fails: the redemption itself has already been saved.
func (h *Handlers) sendRedemptionMail(ctx context.Context, kind string, msg mailerpkg.Message, redemptionID int64) {
	var err error
	switch {
	case h.MailOutbox != nil:
		err = h.MailOutbox.Enqueue(ctx, kind, msg)
	case h.Mailer != nil:
		err = h.Mailer.Send(ctx, msg)
	default:
		err = errors.New("no mailer configured")
	}
	if err != nil {
		logx.Warn("admin.redemption.mail", "kind", kind, "redemption_id", redemptionID, "err", err.Error())
	}
}

func (h *Handlers) sendRedemptionApproved(ctx context.Context, email, kind, teamToken string) error {
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		return errors.New("redemption email requires PublicURL")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Your %s box is confirmed! Open the link below to set up your team — claim your own player slot, share the rest with your teammates, and start playing whenever you're ready.\r\n\r\n", partnerNames[kind])
	fmt.Fprintf(&b, "Team page: %s/team/%s\r\n", publicURL, teamToken)
	b.WriteString("\r\nThe link stays valid for a year. Lose it? Use the \"I lost my link\" recovery on the play page with this same email address.\r\n")
	msg := mailerpkg.Message{
		To:      []string{email},
		Subject: "Your Zonventure adventure — team link inside",
		Text:    b.String(),
	}
	if h.MailOutbox != nil {
		return h.MailOutbox.Enqueue(ctx, "redemption_team_link", msg)
	}
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	return h.Mailer.Send(ctx, msg)
}

// notifyNewRedemption tells the organisation's admins a code is waiting
// to be checked.
func (h *Handlers) notifyNewRedemption(ctx context.Context, id, orgID int64) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT u.email FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role = 'org_admin' AND COALESCE(u.email, '') <> ''
	`, orgID)
	if err != nil {
		logx.Warn("admin.redemption.notify", "redemption_id", id, "err", err.Error())
		return
	}
	var to []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			to = append(to, e)
		}
	}
	rows.Close()
	if len(to) == 0 {
		return
	}
	link := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/") + "/arrange#redemptions"
	h.sendRedemptionMail(ctx, "redemption_new", mailerpkg.Message{
		To:      to,
		Subject: "A box redemption is waiting for review",
		Text:    "Someone has redeemed a partner box. Check the code in the partner's portal, then approve or reject it:\r\n\r\n" + link + "\r\n",
	}, id)
}
