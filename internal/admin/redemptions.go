package admin

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/klahr/zonventure/internal/emailtpl"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/printmat"
	"github.com/klahr/zonventure/internal/results"
)

// Partner box redemptions. Experience partners, each organisation's own
// (partner_fields.go), sell gift boxes for its self-guided adventures. The buyer
// redeems the box's code on /redeem; the request waits until an organiser
// has checked the code in the partner's portal. Approving it creates a
// team on the product's event, as a paid /store ticket does, and emails
// the team link. The postal address, post-on date and posted flag track
// what the organisation mails to the buyer. See migration 0072.

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
	// Letter is what a redemption of it chooses for the letter posted,
	// as on /redeem; nil when its event posts nothing.
	Letter *redeemLetter `json:"letter"`
}

type partnerDTO struct {
	ID       int64               `json:"id"`
	Name     string              `json:"name"`
	Fields   []partnerField      `json:"fields"`
	Hint     map[string]string   `json:"hint"`
	Enabled  bool                `json:"enabled"`
	Products []partnerProductDTO `json:"products"`
}

type redemptionDTO struct {
	// Kind is redemption, or purchase for a store ticket whose letter is
	// posted: an order to post either way, the console lists both.
	Kind        string `json:"kind"`
	ID          int64  `json:"id"`
	State       string `json:"state"`
	PartnerName string `json:"partner_name"`
	// PartnerFields name BoxNumber and SecurityCode (checkPartnerCode).
	PartnerFields []partnerField `json:"partner_fields"`
	ProductID     int64          `json:"product_id"`
	ProductName   string         `json:"product_name"`
	EventID       int64          `json:"event_id"`
	EventName     string         `json:"event_name"`
	BoxNumber     string         `json:"box_number"`
	SecurityCode  string         `json:"security_code"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	StreetAddress string         `json:"street_address"`
	Address2      string         `json:"address_2"`
	PostalCode    string         `json:"postal_code"`
	City          string         `json:"city"`
	Country       string         `json:"country"`
	LetterLang    string         `json:"letter_lang"`
	PostAt        string         `json:"post_at"`
	PostedAt      string         `json:"posted_at"`
	Note          string         `json:"note"`
	CreatedAt     string         `json:"created_at"`
	DecidedAt     string         `json:"decided_at"`
	DecidedBy     string         `json:"decided_by"`
	TeamID        int64          `json:"team_id,omitempty"`
	TeamName      string         `json:"team_name"`
	// LetterLanguages are those the letter_lang may be changed to.
	LetterLanguages []string `json:"letter_languages"`
	// Print is what the organiser prints for it, in its letter's language.
	Print []printLink `json:"print"`

	graphID       int64
	printDelivery []byte
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
	Country       string `json:"country"`
	LetterLang    string `json:"letter_lang"`
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

// versions holds the adventure versions a list's rows play, by id, as
// most rows share one.
type versions map[int64]*graph.Graph

// get returns version id, or nil for an event not yet pinned to one.
func (v versions) get(ctx context.Context, q graphstore.Querier, id int64) (*graph.Graph, error) {
	if id == 0 {
		return nil, nil
	}
	if g, ok := v[id]; ok {
		return g, nil
	}
	s, err := graphstore.ByID(ctx, q, id)
	if err != nil {
		return nil, err
	}
	v[id] = s.Graph
	return s.Graph, nil
}

// letterLanguages lists the languages a product's letter can be posted
// in: those of the adventure version its event plays, main language
// first. An event not yet pinned to a version has none to offer.
func letterLanguages(ctx context.Context, q graphstore.Querier, graphID int64) ([]string, error) {
	g, err := versions{}.get(ctx, q, graphID)
	return graphLanguages(g), err
}

func graphLanguages(g *graph.Graph) []string {
	if g == nil {
		return nil
	}
	if l := g.Languages(); l != nil {
		return l
	}
	return []string{g.Language}
}

// printLink is a print material an organiser prints for one order.
type printLink struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Lang    string `json:"lang"`
	MediaID int64  `json:"media_id"`
}

// printLinks lists what the organiser prints for an order in lang: the
// version's materials the event posts or keeps for organisers, each in
// lang, else in the main language, else in any it has. owed is an order
// that gave an address, so a letter is posted to it: it keeps the
// adventure's posted materials after the event switches them to emailed.
func printLinks(g *graph.Graph, o printmat.Overrides, lang string, owed bool) []printLink {
	out := []printLink{}
	for i, pm := range printmat.Effective(g, o) {
		posted := pm.Delivery == graph.DeliveryPosted || owed && g.PrintMaterials[i].Delivery == graph.DeliveryPosted
		if !posted && pm.Delivery != graph.DeliveryOrganiser {
			continue
		}
		if l, id := printmat.File(pm, lang, g.Language); id > 0 {
			out = append(out, printLink{ID: pm.ID, Name: pm.Name, Lang: l, MediaID: id})
		}
	}
	return out
}

// pickLetterLang checks a requested letter language against those
// offered. None requested is the first, so a form that predates the
// choice still redeems.
func pickLetterLang(langs []string, want string) (string, bool) {
	want = strings.TrimSpace(want)
	if len(langs) == 0 {
		return "", want == ""
	}
	if want == "" {
		return langs[0], true
	}
	for _, l := range langs {
		if l == want {
			return l, true
		}
	}
	return "", false
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
// createdBy is the organiser entering it, nil for a buyer. buyerLang is
// the language the buyer redeemed in, which the mails to them follow;
// empty for an organiser's entry.
func (h *Handlers) createRedemption(ctx context.Context, req RedeemRequest, orgID int64, createdBy *int64, buyerLang string) (int64, *redeemError) {
	for _, f := range []*string{&req.Name, &req.Email, &req.StreetAddress, &req.Address2, &req.PostalCode, &req.City, &req.Note} {
		*f = strings.TrimSpace(*f)
	}
	if req.ProductID <= 0 || req.Name == "" || req.Email == "" || strings.TrimSpace(req.SecurityCode) == "" {
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
		partnerName string
		fieldsRaw   []byte
		productOrg  int64
		productName string
		eventID     int64
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.fields, p.org_id, pp.name, pp.event_id
		FROM partner_products pp JOIN partners p ON p.id = pp.partner_id
		WHERE pp.id = $1 AND pp.enabled AND p.enabled
	`, req.ProductID).Scan(&partnerID, &partnerName, &fieldsRaw, &productOrg, &productName, &eventID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && orgID != 0 && productOrg != orgID) {
		return 0, &redeemError{http.StatusBadRequest, "unknown_product", "unknown product"}
	}
	if err != nil {
		logx.Warn("admin.redeem.product_lookup", "err", err.Error())
		return 0, &redeemError{http.StatusInternalServerError, "", "lookup failed"}
	}
	box, code, ok := checkPartnerCode(scanPartnerFields(fieldsRaw), req.BoxNumber, req.SecurityCode)
	if !ok {
		return 0, &redeemError{http.StatusBadRequest, "invalid_code", "that doesn't look like a " + partnerName + " code"}
	}
	// An event that posts a letter needs to know where, and in what.
	letter, err := h.eventLetter(ctx, eventID)
	if err != nil {
		logx.Warn("admin.redeem.letter", "err", err.Error())
		return 0, &redeemError{http.StatusInternalServerError, "", "lookup failed"}
	}
	var letterLang, country string
	if letter != nil {
		if req.StreetAddress == "" || req.PostalCode == "" || req.City == "" {
			return 0, &redeemError{http.StatusBadRequest, "missing_field", "fill in every required field"}
		}
		if letterLang, ok = pickLetterLang(letter.Languages, req.LetterLang); !ok {
			return 0, &redeemError{http.StatusBadRequest, "invalid_language", "the letter can't be posted in that language"}
		}
		country = strings.ToUpper(strings.TrimSpace(req.Country))
		if country == "" && len(letter.Countries) == 1 {
			country = letter.Countries[0]
		}
		if !slices.Contains(letter.Countries, country) {
			return 0, &redeemError{http.StatusBadRequest, "invalid_country", "letters aren't posted to that country"}
		}
	}

	var id int64
	err = h.DB.QueryRowContext(ctx, `
		INSERT INTO partner_redemptions(partner_id, product_id, product_name, box_number, security_code,
		    name, email, street_address, address_2, postal_code, city, country, letter_lang, post_at, note, created_by_user_id, buyer_lang)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id
	`, partnerID, req.ProductID, productName, box, code, req.Name, req.Email, req.StreetAddress,
		req.Address2, req.PostalCode, req.City, country, letterLang, postAt, req.Note, createdBy, buyerLang).Scan(&id)
	if isUniqueViolation(err) {
		return 0, &redeemError{http.StatusConflict, "code_used", "this code has already been redeemed"}
	}
	if err != nil {
		logx.Warn("admin.redeem.insert", "err", err.Error())
		return 0, &redeemError{http.StatusInternalServerError, "", "save failed"}
	}
	if createdBy == nil {
		h.notifyNewRedemption(ctx, id, productOrg)
	}
	return id, nil
}

/* ---------------- Public: /redeem ---------------- */

type redeemOptionProduct struct {
	ID             int64         `json:"id"`
	Name           string        `json:"name"`
	AdventureTitle string        `json:"adventure_title"`
	OrgName        string        `json:"org_name"`
	Letter         *redeemLetter `json:"letter"`
}

// redeemLetter is what the buyer chooses for the letter posted to them.
type redeemLetter struct {
	Languages []string `json:"languages"`
	Countries []string `json:"countries"`
}

type redeemOptionPartner struct {
	ID int64 `json:"id"`
	// Name has the organisation's after it on the bare /redeem when
	// another organisation's partner shares it.
	Name     string                `json:"name"`
	Fields   []partnerField        `json:"fields"`
	Hint     map[string]string     `json:"hint"`
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
		SELECT p.id, p.name, p.fields, p.hint, pp.id, pp.name, COALESCE(a.title, e.adventure_slug), o.name, e.id
		FROM partner_products pp
		JOIN partners p ON p.id = pp.partner_id
		JOIN events e ON e.id = pp.event_id
		JOIN organizations o ON o.id = p.org_id
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE pp.enabled AND p.enabled AND e.public_for_sale AND e.state = 'running'
		  AND ($1::bigint = 0 OR p.org_id = $1)
		ORDER BY p.name, o.name, p.id, pp.name, pp.id
	`, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	partners := []redeemOptionPartner{}
	type product struct {
		partner      redeemOptionPartner
		fields, hint []byte
		eventID      int64
		p            redeemOptionProduct
	}
	var products []product
	names := map[string]map[int64]bool{}
	for rows.Next() {
		var x product
		if err := rows.Scan(&x.partner.ID, &x.partner.Name, &x.fields, &x.hint, &x.p.ID, &x.p.Name, &x.p.AdventureTitle, &x.p.OrgName, &x.eventID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		products = append(products, x)
		if names[x.partner.Name] == nil {
			names[x.partner.Name] = map[int64]bool{}
		}
		names[x.partner.Name][x.partner.ID] = true
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	letters := map[int64]*redeemLetter{}
	for _, x := range products {
		p := x.p
		l, ok := letters[x.eventID]
		if !ok {
			var err error
			if l, err = h.eventLetter(r.Context(), x.eventID); err != nil {
				httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
				return
			}
			letters[x.eventID] = l
		}
		p.Letter = l
		if n := len(partners); n == 0 || partners[n-1].ID != x.partner.ID {
			pt := x.partner
			pt.Fields = scanPartnerFields(x.fields)
			_ = json.Unmarshal(x.hint, &pt.Hint)
			if len(names[pt.Name]) > 1 {
				pt.Name += " · " + p.OrgName
			}
			partners = append(partners, pt)
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
	id, rerr := h.createRedemption(r.Context(), req, orgID, nil, emailtpl.LangFromRequest(r))
	if rerr != nil && rerr.reason == "code_used" {
		// The page tells the buyer whom to write to about it.
		var contact string
		_ = h.DB.QueryRowContext(r.Context(), `
			SELECT `+emailtpl.ContactSQL+` FROM partner_products pp JOIN partners p ON p.id = pp.partner_id JOIN organizations o ON o.id = p.org_id WHERE pp.id = $1
		`, req.ProductID).Scan(&contact)
		httpx.JSON(w, rerr.status, map[string]string{"error": rerr.msg, "reason": rerr.reason, "contact": contact})
		return
	}
	if rerr != nil {
		rerr.write(w)
		return
	}
	h.sendRedemptionReceived(r.Context(), id)
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *Handlers) redeemOrg(w http.ResponseWriter, r *http.Request) (int64, bool) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	// A NUL byte or invalid UTF-8 errors the Postgres query rather than
	// returning no rows; fail closed to 404 instead of a 500.
	if slug == "" || strings.IndexByte(slug, 0) >= 0 || !utf8.ValidString(slug) {
		httpx.Error(w, http.StatusNotFound, "organisation not found")
		return 0, false
	}
	var orgID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM organizations WHERE slug = $1`, slug).Scan(&orgID)
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
		SELECT p.id, p.name, p.fields, p.hint, p.enabled,
		       COALESCE(pp.id, 0), COALESCE(pp.name, ''), COALESCE(pp.event_id, 0),
		       COALESCE(e.name, ''), COALESCE(a.title, e.adventure_slug, ''),
		       COALESCE(pp.team_size, 0), COALESCE(pp.enabled, FALSE)
		FROM partners p
		LEFT JOIN partner_products pp ON pp.partner_id = p.id
		LEFT JOIN events e ON e.id = pp.event_id
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE p.org_id = $1 AND ($2::bigint = 0 OR p.id = $2)
		ORDER BY p.name, p.id, pp.name, pp.id
	`, orgID, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []partnerDTO{}
	for rows.Next() {
		var p partnerDTO
		var pr partnerProductDTO
		var fields, hint []byte
		if err := rows.Scan(&p.ID, &p.Name, &fields, &hint, &p.Enabled, &pr.ID, &pr.Name, &pr.EventID,
			&pr.EventName, &pr.AdventureTitle, &pr.TeamSize, &pr.Enabled); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != p.ID {
			p.Fields = scanPartnerFields(fields)
			_ = json.Unmarshal(hint, &p.Hint)
			p.Products = []partnerProductDTO{}
			out = append(out, p)
		}
		if pr.ID != 0 {
			last := &out[len(out)-1]
			last.Products = append(last.Products, pr)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	letters := map[int64]*redeemLetter{}
	for i := range out {
		for j := range out[i].Products {
			pr := &out[i].Products[j]
			l, ok := letters[pr.EventID]
			if !ok {
				var err error
				if l, err = h.eventLetter(ctx, pr.EventID); err != nil {
					return nil, err
				}
				letters[pr.EventID] = l
			}
			pr.Letter = l
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
	var req partnerSetup
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := req.clean(); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "reason": "invalid_partner"})
		return
	}
	fields, _ := json.Marshal(req.Fields)
	hint, _ := json.Marshal(req.Hint)
	var id int64
	err := h.DB.QueryRowContext(r.Context(),
		`INSERT INTO partners(org_id, name, fields, hint) VALUES ($1, $2, $3, $4) RETURNING id`, orgID, req.Name, fields, hint).Scan(&id)
	if isUniqueViolation(err) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "a partner by that name exists", "reason": "exists"})
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

// UpdatePartner enables or disables a partner, or sets it up anew: a
// name, fields and hint together. Codes already redeemed stay as typed.
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
		*partnerSetup
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.partnerSetup != nil {
		if err := req.clean(); err != nil {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "reason": "invalid_partner"})
			return
		}
		fields, _ := json.Marshal(req.Fields)
		hint, _ := json.Marshal(req.Hint)
		_, err := h.DB.ExecContext(r.Context(),
			`UPDATE partners SET name = $1, fields = $2, hint = $3 WHERE id = $4`, req.Name, fields, hint, id)
		if isUniqueViolation(err) {
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": "a partner by that name exists", "reason": "exists"})
			return
		}
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
			return
		}
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
	SELECT r.id, r.state, p.name, p.fields, COALESCE(r.product_id, 0), COALESCE(pp.name, r.product_name),
	       COALESCE(pp.event_id, 0), COALESCE(e.name, ''),
	       r.box_number, r.security_code, r.name, r.email, r.street_address, r.address_2,
	       r.postal_code, r.city, r.country, r.letter_lang, r.post_at, COALESCE(r.posted_at, ''), r.note, r.created_at,
	       COALESCE(r.decided_at, ''), COALESCE(u.username, ''),
	       COALESCE(r.team_id, 0), COALESCE(t.name, ''), COALESCE(e.graph_id, 0), COALESCE(e.print_delivery, '{}')
	FROM partner_redemptions r
	JOIN partners p ON p.id = r.partner_id
	LEFT JOIN partner_products pp ON pp.id = r.product_id
	LEFT JOIN events e ON e.id = pp.event_id
	LEFT JOIN users u ON u.id = r.decided_by_user_id
	LEFT JOIN teams t ON t.id = r.team_id
`

func scanRedemption(sc interface{ Scan(...any) error }) (redemptionDTO, error) {
	var d redemptionDTO
	var fields []byte
	err := sc.Scan(&d.ID, &d.State, &d.PartnerName, &fields, &d.ProductID, &d.ProductName, &d.EventID, &d.EventName,
		&d.BoxNumber, &d.SecurityCode, &d.Name, &d.Email, &d.StreetAddress, &d.Address2,
		&d.PostalCode, &d.City, &d.Country, &d.LetterLang, &d.PostAt, &d.PostedAt, &d.Note, &d.CreatedAt,
		&d.DecidedAt, &d.DecidedBy, &d.TeamID, &d.TeamName, &d.graphID, &d.printDelivery)
	d.Kind = "redemption"
	d.PartnerFields = scanPartnerFields(fields)
	return d, err
}

// fillVersion adds what the row's adventure version decides: the letter's
// languages and what to print.
func (d *redemptionDTO) fillVersion(ctx context.Context, q graphstore.Querier, vs versions) error {
	g, err := vs.get(ctx, q, d.graphID)
	if err != nil {
		return err
	}
	d.LetterLanguages = graphLanguages(g)
	d.Print = printLinks(g, printmat.Parse(d.printDelivery), d.LetterLang, d.StreetAddress != "")
	return nil
}

// ListRedemptions returns the organisation's redemptions in one state
// (pending by default, or all) with a count per state. to_post is those
// still to be posted: approved, not yet marked posted, the
// ones to post first first.
func (h *Handlers) ListRedemptions(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	switch state {
	case "":
		state = "pending"
	case "pending", "approved", "rejected", "cancelled", "all", "to_post":
	default:
		httpx.Error(w, http.StatusBadRequest, "bad state")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), redemptionSelect+`
		WHERE p.org_id = $1 AND CASE $2::text
		    WHEN 'all' THEN TRUE
		    WHEN 'to_post' THEN `+redemptionToPost+`
		    ELSE r.state = $2 END
		ORDER BY CASE WHEN $2 = 'to_post' THEN r.post_at = '' END, CASE WHEN $2 = 'to_post' THEN r.post_at END,
		         CASE WHEN $2 = 'to_post' THEN r.created_at END, r.created_at DESC, r.id DESC
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
	if state == "to_post" || state == "all" {
		bought, err := h.purchaseOrders(r.Context(), orgID, state == "to_post", 0)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out = append(out, bought...)
		slices.SortStableFunc(out, func(a, b redemptionDTO) int {
			if state == "to_post" {
				// The ones to post first first: by post date, undated last.
				if (a.PostAt == "") != (b.PostAt == "") {
					if a.PostAt == "" {
						return 1
					}
					return -1
				}
				if c := cmp.Compare(a.PostAt, b.PostAt); c != 0 {
					return c
				}
				return cmp.Compare(a.CreatedAt, b.CreatedAt)
			}
			return cmp.Compare(b.CreatedAt, a.CreatedAt)
		})
	}
	vs := versions{}
	for i := range out {
		if err := out[i].fillVersion(r.Context(), h.DB, vs); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
	}
	counts := map[string]int{"pending": 0, "approved": 0, "rejected": 0, "cancelled": 0, "to_post": 0}
	crows, err := h.DB.QueryContext(r.Context(), `
		SELECT r.state, COUNT(*) FROM partner_redemptions r JOIN partners p ON p.id = r.partner_id
		WHERE p.org_id = $1 GROUP BY r.state
		UNION ALL
		SELECT 'to_post', COUNT(*) FROM partner_redemptions r JOIN partners p ON p.id = r.partner_id
		WHERE p.org_id = $1 AND `+redemptionToPost+`
		UNION ALL
		SELECT 'to_post', COUNT(*) FROM ticket_purchases tp JOIN events e ON e.id = tp.event_id
		WHERE e.org_id = $1 AND `+postedPurchase+` AND tp.posted_at IS NULL
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
		counts[s] += n
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"redemptions": out, "counts": counts})
}

// redemptionToPost picks the redemptions still to post: approved (a
// pending code isn't checked yet), with an address (asked only when a
// letter is posted), not yet posted.
const redemptionToPost = `r.state = 'approved' AND r.street_address <> '' AND r.posted_at IS NULL`

// postedPurchase picks the store purchases with a letter to post: paid
// tickets whose checkout collected an address, which it does only when
// the event posts something.
const postedPurchase = `tp.kind = 'ticket' AND tp.state = 'paid' AND tp.street_address <> ''`

// purchaseOrders are the organisation's store purchases with a letter to
// post, as orders beside its redemptions: only those not yet posted when
// toPost, only purchase id when it isn't 0.
func (h *Handlers) purchaseOrders(ctx context.Context, orgID int64, toPost bool, id int64) ([]redemptionDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT tp.id, tp.event_id, COALESCE(NULLIF(e.name, ''), a.title, e.adventure_slug), COALESCE(NULLIF(a.title, ''), e.adventure_slug),
		       tp.buyer_email, tp.ship_name, tp.street_address, tp.address_2, tp.postal_code, tp.city, tp.country,
		       tp.letter_lang, tp.post_at, COALESCE(tp.posted_at, ''), tp.note, COALESCE(tp.paid_at, tp.created_at),
		       COALESCE(tp.team_id, 0), COALESCE(t.name, ''), COALESCE(e.graph_id, 0), e.print_delivery
		FROM ticket_purchases tp
		JOIN events e ON e.id = tp.event_id
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		LEFT JOIN teams t ON t.id = tp.team_id
		WHERE e.org_id = $1 AND `+postedPurchase+` AND (NOT $2 OR tp.posted_at IS NULL) AND ($3::bigint = 0 OR tp.id = $3)
		ORDER BY tp.id DESC
		LIMIT 500
	`, orgID, toPost, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []redemptionDTO
	for rows.Next() {
		d := redemptionDTO{Kind: "purchase", State: "paid"}
		if err := rows.Scan(&d.ID, &d.EventID, &d.EventName, &d.ProductName, &d.Email, &d.Name, &d.StreetAddress, &d.Address2,
			&d.PostalCode, &d.City, &d.Country, &d.LetterLang, &d.PostAt, &d.PostedAt, &d.Note, &d.CreatedAt,
			&d.TeamID, &d.TeamName, &d.graphID, &d.printDelivery); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateRedemption is an organiser entering a box redemption taken by
// phone or email. Review checks customers, not staff, so it is approved
// at once; one that can't be (its event closed) stays pending.
func (h *Handlers) CreateRedemption(w http.ResponseWriter, r *http.Request) {
	me, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	var req RedeemRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	id, rerr := h.createRedemption(r.Context(), req, orgID, &me.ID, "")
	if rerr != nil {
		rerr.write(w)
		return
	}
	if rerr, err := h.approveRedemption(r.Context(), id, me.ID); err != nil {
		logx.Warn("admin.redemption.auto_approve", "redemption_id", id, "err", err.Error())
	} else if rerr != nil {
		logx.Info("admin.redemption.auto_approve", "redemption_id", id, "reason", rerr.reason)
	}
	h.writeRedemption(w, r, http.StatusCreated, id)
}

func (h *Handlers) writeRedemption(w http.ResponseWriter, r *http.Request, status int, id int64) {
	d, err := scanRedemption(h.DB.QueryRowContext(r.Context(), redemptionSelect+`WHERE r.id = $1`, id))
	if err == nil {
		err = d.fillVersion(r.Context(), h.DB, versions{})
	}
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
	rerr, err := h.approveRedemption(r.Context(), id, me.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "approve failed", err)
		return
	}
	if rerr != nil {
		rerr.write(w)
		return
	}
	h.writeRedemption(w, r, http.StatusOK, id)
}

// approveRedemption approves pending redemption id for user by. One that
// can't be approved gives a redeemError; a failure, err.
func (h *Handlers) approveRedemption(ctx context.Context, id, by int64) (*redeemError, error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		state, email, partner, eventState, letterLang, mailLang string
		eventID                                                 int64
		teamSize                                                int
	)
	// A product deleted since (with its event, say) leaves nothing to
	// approve into: that reads as a closed event.
	if err := tx.QueryRowContext(ctx, `
		SELECT r.state, r.email, p.name, COALESCE(pp.event_id, 0), COALESCE(pp.team_size, 0), COALESCE(e.state, ''),
		       r.letter_lang, `+redemptionMailLang+`
		FROM partner_redemptions r
		JOIN partners p ON p.id = r.partner_id
		LEFT JOIN partner_products pp ON pp.id = r.product_id
		LEFT JOIN events e ON e.id = pp.event_id
		WHERE r.id = $1 FOR UPDATE OF r
	`, id).Scan(&state, &email, &partner, &eventID, &teamSize, &eventState, &letterLang, &mailLang); err != nil {
		return nil, err
	}
	if state != "pending" {
		return &redeemError{http.StatusConflict, "not_pending", "redemption is " + state}, nil
	}
	if eventState != "running" {
		return &redeemError{http.StatusConflict, "event_closed", "the product's event isn't running"}, nil
	}
	teamID, teamToken, voucher, err := createTicketTeam(ctx, tx, eventID, email, teamSize)
	if err != nil {
		return nil, err
	}
	// The team starts in the letter's language; it can still switch.
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET lang = $1 WHERE id = $2`, letterLang, teamID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE player_slots SET expires_at = $1 WHERE team_id = $2`,
		time.Now().UTC().Add(redemptionSlotTTL).Format("2006-01-02 15:04:05"), teamID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE partner_redemptions
		   SET state = 'approved', team_id = $1, decided_by_user_id = $2, decided_at = now_text()
		 WHERE id = $3
	`, teamID, by, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := h.sendRedemptionApproved(ctx, eventID, email, partner, mailLang, teamToken, voucher); err != nil {
		logx.Warn("admin.redemption.approve.mail", "redemption_id", id, "team_id", teamID, "err", err.Error())
	}
	logx.Info("admin.redemption.approved", "redemption_id", id, "team_id", teamID)
	return nil, nil
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
	// An optional reason, passed on to the customer.
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 && !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) > 1000 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the reason is too long", "reason": "missing_field"})
		return
	}
	var email, partner, mailLang string
	err := h.DB.QueryRowContext(r.Context(), `
		UPDATE partner_redemptions r
		   SET state = 'rejected', decided_by_user_id = $1, decided_at = now_text()
		  FROM partners p
		 WHERE r.id = $2 AND r.state = 'pending' AND p.id = r.partner_id
		RETURNING r.email, p.name, `+redemptionMailLang+`
	`, me.ID, id).Scan(&email, &partner, &mailLang)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "redemption isn't pending", "reason": "not_pending"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reject failed", err)
		return
	}
	h.sendRedemptionMail(r.Context(), "redemption_rejected", mailLang, []string{email}, func(lang string) (string, emailtpl.Content) {
		return emailtpl.BoxRejected(lang, partner, req.Reason, h.orgContact(r.Context(), orgID))
	}, id, orgID)
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
		email, partner, mailLang string
		teamID                   sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		UPDATE partner_redemptions r
		   SET state = 'cancelled', decided_by_user_id = $1, decided_at = now_text()
		  FROM partners p
		 WHERE r.id = $2 AND r.state IN ('pending', 'approved') AND p.id = r.partner_id
		RETURNING r.email, p.name, r.team_id, `+redemptionMailLang+`
	`, me.ID, id).Scan(&email, &partner, &teamID, &mailLang)
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
		if _, err := results.AppendTeam(ctx, tx, teamID.Int64, results.TeamWithdrawn, map[string]any{"reason": "gift box redemption cancelled"}); err != nil {
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
	h.sendRedemptionMail(ctx, "redemption_cancelled", mailLang, []string{email}, func(lang string) (string, emailtpl.Content) {
		return emailtpl.BoxCancelled(lang, partner)
	}, id, orgID)
	h.writeRedemption(w, r, http.StatusOK, id)
}

// UpdateRedemption sets what the organisation tracks by hand: when to
// post the box, whether it has been posted, the letter's language, and a
// note.
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
		PostAt     *string `json:"post_at"`
		Note       *string `json:"note"`
		Posted     *bool   `json:"posted"`
		LetterLang *string `json:"letter_lang"`
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
	if req.LetterLang != nil {
		var graphID int64
		if err := h.DB.QueryRowContext(r.Context(), `
			SELECT COALESCE(e.graph_id, 0) FROM partner_redemptions r
			LEFT JOIN partner_products pp ON pp.id = r.product_id
			LEFT JOIN events e ON e.id = pp.event_id
			WHERE r.id = $1
		`, id).Scan(&graphID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		langs, err := letterLanguages(r.Context(), h.DB, graphID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		l, ok := pickLetterLang(langs, *req.LetterLang)
		if !ok {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the letter can't be posted in that language", "reason": "invalid_language"})
			return
		}
		req.LetterLang = &l
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE partner_redemptions SET
		    post_at = COALESCE($1, post_at),
		    note = COALESCE($2, note),
		    posted_at = CASE WHEN $3::boolean IS NULL THEN posted_at
		                     WHEN $3 THEN COALESCE(posted_at, now_text())
		                     ELSE NULL END,
		    letter_lang = COALESCE($5, letter_lang)
		WHERE id = $4
	`, req.PostAt, req.Note, req.Posted, id, req.LetterLang); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.writeRedemption(w, r, http.StatusOK, id)
}

// UpdatePurchaseOrder is UpdateRedemption for a store purchase whose
// letter is posted: when to post it, whether it has been, its language
// and a note.
//
// PATCH /api/admin/ticket-purchases/{purchaseID}
func (h *Handlers) UpdatePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := redemptionOrg(w, r, false)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "purchaseID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad purchase id")
		return
	}
	var eventID int64
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT tp.event_id FROM ticket_purchases tp JOIN events e ON e.id = tp.event_id
		WHERE tp.id = $1 AND e.org_id = $2 AND `+postedPurchase, id, orgID).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	var req struct {
		PostAt     *string `json:"post_at"`
		Note       *string `json:"note"`
		Posted     *bool   `json:"posted"`
		LetterLang *string `json:"letter_lang"`
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
	if req.LetterLang != nil {
		letter, err := h.eventLetter(r.Context(), eventID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		var langs []string
		if letter != nil {
			langs = letter.Languages
		}
		l, ok := pickLetterLang(langs, *req.LetterLang)
		if !ok {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the letter can't be posted in that language", "reason": "invalid_language"})
			return
		}
		req.LetterLang = &l
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE ticket_purchases SET
		    post_at = COALESCE($1, post_at),
		    note = COALESCE($2, note),
		    posted_at = CASE WHEN $3::boolean IS NULL THEN posted_at
		                     WHEN $3 THEN COALESCE(posted_at, now_text())
		                     ELSE NULL END,
		    letter_lang = COALESCE($5, letter_lang)
		WHERE id = $4
	`, req.PostAt, req.Note, req.Posted, id, req.LetterLang); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	got, err := h.purchaseOrders(r.Context(), orgID, false, id)
	if err == nil && len(got) == 1 {
		err = got[0].fillVersion(r.Context(), h.DB, versions{})
	}
	if err != nil || len(got) != 1 {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, got[0])
}

/* ---------------- Mail ---------------- */

// sendRedemptionMail sends through the outbox when there is one, and
// logs rather than fails: the redemption itself has already been saved.
func (h *Handlers) sendRedemptionMail(ctx context.Context, kind, lang string, to []string, build func(lang string) (string, emailtpl.Content), redemptionID, orgID int64) {
	l := h.orgLook(ctx, orgID)
	if lang != "" {
		l.Lang = lang
	}
	subject, c := build(l.Lang)
	msg := l.message(to, subject, c)
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

func (h *Handlers) sendRedemptionApproved(ctx context.Context, eventID int64, email, partner, lang, teamToken, voucher string) error {
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		return errors.New("redemption email requires PublicURL")
	}
	l := h.eventLook(ctx, eventID)
	if lang != "" {
		l.Lang = lang
	}
	subject, c := emailtpl.BoxConfirmed(l.Lang, partner, publicURL+"/team/"+teamToken, publicURL+"/play", auth.FormatVoucherCode(voucher), l.Brand.Name)
	c = l.withBuyerNote(c)
	c = emailtpl.WithPrints(c, h.printEmailLinks(ctx, eventID, publicURL, teamToken))
	c = emailtpl.WithContact(c, l.Contact)
	c.Picture = l.Cover
	msg := l.message([]string{email}, subject, c)
	if h.MailOutbox != nil {
		return h.MailOutbox.Enqueue(ctx, "redemption_team_link", msg)
	}
	if h.Mailer == nil {
		return errors.New("no mailer configured")
	}
	return h.Mailer.Send(ctx, msg)
}

// addressLines is a postal address as an email shows it, a line each.
func addressLines(name, street, address2, postal, city string) []string {
	var out []string
	for _, line := range []string{name, street, address2, strings.TrimSpace(postal + " " + city)} {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// redemptionMailLang is the language of the mails to a redemption's
// buyer: the one they redeemed in, else the posted letter's.
const redemptionMailLang = `COALESCE(NULLIF(buyer_lang, ''), letter_lang)`

// sendRedemptionReceived tells a buyer their code has arrived and where
// their envelope goes, in the event's look and the buyer's language.
// Organisers entering a redemption by hand have the buyer on the phone,
// so only the public form sends it.
func (h *Handlers) sendRedemptionReceived(ctx context.Context, id int64) {
	var (
		email, partner, lang, name, street, address2, postal, city string
		eventID, orgID                                             int64
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT r.email, p.name, COALESCE(NULLIF(r.buyer_lang, ''), r.letter_lang), r.name, r.street_address, r.address_2, r.postal_code, r.city,
		       COALESCE(pp.event_id, 0), p.org_id
		FROM partner_redemptions r JOIN partners p ON p.id = r.partner_id
		LEFT JOIN partner_products pp ON pp.id = r.product_id
		WHERE r.id = $1
	`, id).Scan(&email, &partner, &lang, &name, &street, &address2, &postal, &city, &eventID, &orgID); err != nil {
		logx.Warn("admin.redemption.received", "redemption_id", id, "err", err.Error())
		return
	}
	l := h.orgLook(ctx, orgID)
	if eventID > 0 {
		l = h.eventLook(ctx, eventID)
	}
	if lang != "" {
		l.Lang = lang
	}
	// An address was asked only when a letter is posted.
	var address []string
	if street != "" {
		address = addressLines(name, street, address2, postal, city)
	}
	subject, c := emailtpl.BoxReceived(l.Lang, partner, address, l.Contact)
	c.Picture = l.Cover
	msg := l.message([]string{email}, subject, c)
	var err error
	switch {
	case h.MailOutbox != nil:
		err = h.MailOutbox.Enqueue(ctx, "redemption_received", msg)
	case h.Mailer != nil:
		err = h.Mailer.Send(ctx, msg)
	default:
		err = errors.New("no mailer configured")
	}
	if err != nil {
		logx.Warn("admin.redemption.mail", "kind", "redemption_received", "redemption_id", id, "err", err.Error())
	}
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
	h.sendRedemptionMail(ctx, "redemption_new", "", to, func(lang string) (string, emailtpl.Content) {
		subject, c := emailtpl.BoxWaiting(lang, link)
		c.Picture = h.house(lang, emailtpl.PictureGiftBox)
		return subject, c
	}, id, orgID)
}
