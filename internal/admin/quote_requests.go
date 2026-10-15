package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/klahr/zonventure/internal/domains"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/metrics"
	"github.com/klahr/zonventure/internal/settings"
)

// Requests for a quote (org_requests). A company planning a live event
// fills in a guided form on an organisation's public page
// (/o/{slug}/request, the party app's request page); the request lands
// in the organisation's inbox and under Requests in its console, and the
// organisation answers it with its own quote, outside Zonventure. One
// form for every organisation, which turns it on and writes its intro.
//
// The form is public, so it has a spam trap: a hidden field a person
// never fills in, and a signed stamp of when the form loaded, since a
// person needs far longer than a script to get through it. A caught
// submission is answered like a good one and dropped.

const (
	// quoteMinFill is the least time a person takes from the form
	// loading to sending it; quoteMaxAge the most a stamp is good for.
	quoteMinFill = 5 * time.Second
	quoteMaxAge  = 7 * 24 * time.Hour
	// Requests per IP an hour, across every organisation.
	quoteWindow   = time.Hour
	quotePerIPMax = 5

	maxQuoteIntro   = 600
	maxQuoteField   = 200
	maxQuoteMessage = 2000
	maxQuotePeople  = 10000
)

var (
	quoteOccasions = []string{"kickoff", "team_day", "conference", "customer_event", "other"}
	quoteTimes     = []string{"", "morning", "afternoon", "evening"}
	quoteDurations = []string{"", "1h", "2h", "3h", "half_day"}
	quoteScopes    = []string{"ready_made", "adapted", "new"}
	quoteExtras    = []string{"own_questions", "logo", "accessibility", "prize_ceremony", "big_screen"}
	quoteLanguages = []string{"sv", "en"}
	quoteBudgets   = []string{"", "under_5k", "5k_15k", "15k_40k", "over_40k", "unknown"}
	quoteStatuses  = []string{"new", "answered", "won", "lost"}
	// quoteOffers are what an organisation can switch off in its form
	// (organizations.requests_off): the adventure kinds, the game master,
	// the extras and the languages.
	quoteOffers = slices.Concat(quoteScopes, []string{"game_master"}, quoteExtras, quoteLanguages)
)

// quoteAnswers are the company form's answers, as stored in
// org_requests.answers. The choices are the keys above.
type quoteAnswers struct {
	Occasion string `json:"occasion"`
	// Date is YYYY-MM-DD; Month (YYYY-MM) when the date is open.
	Date       string   `json:"date,omitempty"`
	Month      string   `json:"month,omitempty"`
	TimeOfDay  string   `json:"time_of_day,omitempty"`
	Duration   string   `json:"duration,omitempty"`
	People     int      `json:"people"`
	City       string   `json:"city"`
	Venue      string   `json:"venue,omitempty"`
	HelpChoose bool     `json:"help_choose,omitempty"`
	Scope      string   `json:"scope"`
	GameMaster bool     `json:"game_master,omitempty"`
	Extras     []string `json:"extras,omitempty"`
	Languages  []string `json:"languages,omitempty"`
	Budget     string   `json:"budget,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// check trims the answers and says what is wrong with them, "" when
// nothing is.
func (a *quoteAnswers) check() string {
	for _, f := range []*string{&a.Occasion, &a.Date, &a.Month, &a.TimeOfDay, &a.Duration, &a.City, &a.Venue, &a.Scope, &a.Budget, &a.Message} {
		*f = strings.TrimSpace(*f)
	}
	if !slices.Contains(quoteOccasions, a.Occasion) || !slices.Contains(quoteScopes, a.Scope) ||
		!slices.Contains(quoteTimes, a.TimeOfDay) || !slices.Contains(quoteDurations, a.Duration) ||
		!slices.Contains(quoteBudgets, a.Budget) {
		return "missing_field"
	}
	if a.Date != "" {
		if _, err := time.Parse("2006-01-02", a.Date); err != nil {
			return "missing_field"
		}
		a.Month = ""
	} else if _, err := time.Parse("2006-01", a.Month); err != nil {
		return "missing_field"
	}
	if a.People < 1 || a.People > maxQuotePeople || a.City == "" {
		return "missing_field"
	}
	if len(a.City) > maxQuoteField || len(a.Venue) > maxQuoteField || len(a.Message) > maxQuoteMessage {
		return "too_long"
	}
	a.Extras = known(a.Extras, quoteExtras)
	a.Languages = known(a.Languages, quoteLanguages)
	return ""
}

// offered reports whether the answers ask only for what the
// organisation offers: nothing in off.
func (a *quoteAnswers) offered(off []string) bool {
	if slices.Contains(off, a.Scope) || (a.GameMaster && slices.Contains(off, "game_master")) {
		return false
	}
	for _, x := range slices.Concat(a.Extras, a.Languages) {
		if slices.Contains(off, x) {
			return false
		}
	}
	return true
}

// quoteOff is what the organisation doesn't offer through its form.
func (h *Handlers) quoteOff(ctx context.Context, orgID int64) ([]string, error) {
	var list string
	if err := h.DB.QueryRowContext(ctx, `SELECT array_to_string(requests_off, ',') FROM organizations WHERE id = $1`, orgID).Scan(&list); err != nil {
		return nil, err
	}
	if list == "" {
		return []string{}, nil
	}
	return strings.Split(list, ","), nil
}

// known is list's values that are in allowed, once each, in allowed's
// order.
func known(list, allowed []string) []string {
	var out []string
	for _, v := range allowed {
		if slices.Contains(list, v) {
			out = append(out, v)
		}
	}
	return out
}

// quoteStamp signs when the form loaded: "<unix seconds>.<mac>".
func quoteStamp(key []byte, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	return ts + "." + quoteMAC(key, ts)
}

func quoteMAC(key []byte, ts string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("org_request:" + ts))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// quoteStampOK reports whether stamp is ours and was made long enough
// before now for a person to fill in the form, and not so long ago that
// it is stale.
func quoteStampOK(key []byte, stamp string, now time.Time) bool {
	ts, mac, ok := strings.Cut(stamp, ".")
	if !ok || !hmac.Equal([]byte(mac), []byte(quoteMAC(key, ts))) {
		return false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	age := now.Sub(time.Unix(sec, 0))
	return age >= quoteMinFill && age <= quoteMaxAge
}

type quoteLimiter struct {
	mu    sync.Mutex
	perIP map[string][]time.Time
}

var quoteLimit = quoteLimiter{perIP: map[string][]time.Time{}}

// allow records a request from ip and reports whether it may go ahead,
// with the wait to surface as Retry-After when not.
func (l *quoteLimiter) allow(now time.Time, ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-quoteWindow)
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
	if len(ts) >= quotePerIPMax {
		l.perIP[ip] = ts
		return false, ts[0].Add(quoteWindow).Sub(now).Round(time.Second)
	}
	l.perIP[ip] = append(ts, now)
	return true, 0
}

/* ---------------- Public: /o/{slug}/request ---------------- */

// quoteOrg is the organisation behind the URL's slug, when it takes
// requests; it has answered 404 otherwise.
func (h *Handlers) quoteOrg(w http.ResponseWriter, r *http.Request) (orgID int64, name, slug, intro string, ok bool) {
	slug = strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" || strings.IndexByte(slug, 0) >= 0 || !utf8.ValidString(slug) {
		httpx.Error(w, http.StatusNotFound, "organisation not found")
		return 0, "", "", "", false
	}
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, name, requests_intro FROM organizations WHERE slug = $1 AND requests_open`, slug).Scan(&orgID, &name, &intro)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "this organisation doesn't take requests")
		return 0, "", "", "", false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, "", "", "", false
	}
	return orgID, name, slug, intro, true
}

// quoteFormDTO is what the request form shows: whose it is, in their
// look, their intro, the currency budgets are in, and the stamp of when
// it loaded.
type quoteFormDTO struct {
	OrgName  string `json:"org_name"`
	OrgSlug  string `json:"org_slug"`
	Intro    string `json:"intro"`
	Currency string `json:"currency"`
	Started  string `json:"started"`
	// Look is the organisation's brand as play.Brand writes it: its
	// colours and title typeface, as on its public page.
	Look     map[string]string `json:"look,omitempty"`
	LogoURL  string            `json:"logo_url,omitempty"`
	CoverURL string            `json:"cover_url,omitempty"`
	// Off is what the organisation doesn't offer: the form leaves it out.
	Off []string `json:"off"`
}

// GetQuoteForm is the request form's content.
//
// GET /api/o/{slug}/request
func (h *Handlers) GetQuoteForm(w http.ResponseWriter, r *http.Request) {
	orgID, name, slug, intro, ok := h.quoteOrg(w, r)
	if !ok {
		return
	}
	key, err := settings.FormKey(r.Context(), h.DB)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out := quoteFormDTO{OrgName: name, OrgSlug: slug, Intro: intro, Currency: strings.ToUpper(settings.Currency), Started: quoteStamp(key, time.Now())}
	var (
		accent, accent2, bg, ink sql.NullString
		font                     string
		hasLogo, hasCover        bool
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT accent_color, accent_secondary_color, background_color, text_color, brand_font,
		       (logo_content IS NOT NULL AND length(logo_content) > 0),
		       (cover_image_content IS NOT NULL AND length(cover_image_content) > 0)
		FROM organizations WHERE id = $1
	`, orgID).Scan(&accent, &accent2, &bg, &ink, &font, &hasLogo, &hasCover); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out.Look = map[string]string{}
	for k, v := range map[string]string{"accent": accent.String, "accent_secondary": accent2.String, "background": bg.String, "text": ink.String, "heading_font": font} {
		if v = strings.TrimSpace(v); v != "" {
			out.Look[k] = v
		}
	}
	if out.Off, err = h.quoteOff(r.Context(), orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	id := strconv.FormatInt(orgID, 10)
	if hasLogo {
		out.LogoURL = "/org-logo/" + id
	}
	if hasCover {
		out.CoverURL = "/org-cover/" + id
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

// quoteRequestBody is a request as the form sends it. Website is the
// hidden field; Started the stamp GetQuoteForm gave.
type quoteRequestBody struct {
	Answers quoteAnswers `json:"answers"`
	Name    string       `json:"name"`
	Company string       `json:"company"`
	Email   string       `json:"email"`
	Phone   string       `json:"phone"`
	Website string       `json:"website"`
	Started string       `json:"started"`
}

// SendQuoteRequest stores a request for the organisation and emails it
// to them, and a copy to whoever sent it.
//
// POST /api/o/{slug}/request
func (h *Handlers) SendQuoteRequest(w http.ResponseWriter, r *http.Request) {
	orgID, _, _, _, ok := h.quoteOrg(w, r)
	if !ok {
		return
	}
	now := time.Now()
	if ok, wait := quoteLimit.allow(now, auth.ClientIP(r)); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		httpx.JSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, try again later", "reason": "rate_limited"})
		return
	}
	var req quoteRequestBody
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	sent := func() {
		w.Header().Set("Cache-Control", "no-store")
		httpx.JSON(w, http.StatusCreated, map[string]bool{"ok": true})
	}
	if strings.TrimSpace(req.Website) != "" {
		metrics.RequestsTrapped.WithLabelValues("honeypot").Inc()
		sent()
		return
	}
	key, err := settings.FormKey(r.Context(), h.DB)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !quoteStampOK(key, req.Started, now) {
		metrics.RequestsTrapped.WithLabelValues("too_fast").Inc()
		sent()
		return
	}
	if reason := req.Answers.check(); reason != "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "fill in every required answer", "reason": reason})
		return
	}
	off, err := h.quoteOff(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !req.Answers.offered(off) {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the request asks for something this organisation doesn't offer", "reason": "not_offered"})
		return
	}
	req.Name, req.Company, req.Phone = strings.TrimSpace(req.Name), strings.TrimSpace(req.Company), strings.TrimSpace(req.Phone)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Name == "" || req.Company == "" || req.Email == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "fill in every required answer", "reason": "missing_field"})
		return
	}
	if len(req.Name) > maxQuoteField || len(req.Company) > maxQuoteField || len(req.Phone) > 40 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "an answer is too long", "reason": "too_long"})
		return
	}
	if a, err := mail.ParseAddress(req.Email); err != nil || a.Address != req.Email || len(req.Email) > 254 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "not a valid email address", "reason": "invalid_email"})
		return
	}
	answers, err := json.Marshal(req.Answers)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	lang := emailtpl.LangFromRequest(r)
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO org_requests(org_id, answers, name, company, email, phone, lang)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, orgID, answers, req.Name, req.Company, req.Email, req.Phone, lang).Scan(&id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.mailQuoteRequest(r.Context(), id)
	sent()
}

// mailQuoteRequest sends a new request to its organisation, replying to
// the requester, and a copy to the requester, replying to the
// organisation.
func (h *Handlers) mailQuoteRequest(ctx context.Context, id int64) {
	var (
		orgID   int64
		raw     []byte
		lang    string
		orgName string
		er      emailtpl.Request
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT q.org_id, q.answers, q.lang, q.name, q.company, q.email, q.phone, o.name
		FROM org_requests q JOIN organizations o ON o.id = q.org_id WHERE q.id = $1
	`, id).Scan(&orgID, &raw, &lang, &er.Name, &er.Company, &er.Email, &er.Phone, &orgName); err != nil {
		logx.Warn("admin.quote_request.mail", "request_id", id, "err", err.Error())
		return
	}
	var a quoteAnswers
	if err := json.Unmarshal(raw, &a); err != nil {
		logx.Warn("admin.quote_request.mail", "request_id", id, "err", err.Error())
		return
	}
	er.Org, er.Occasion, er.Date, er.Month, er.TimeOfDay, er.Duration = orgName, a.Occasion, a.Date, a.Month, a.TimeOfDay, a.Duration
	er.People, er.City, er.Venue, er.HelpChoose, er.Scope, er.GameMaster = a.People, a.City, a.Venue, a.HelpChoose, a.Scope, a.GameMaster
	er.Extras, er.Languages, er.Budget, er.Message, er.Currency = a.Extras, a.Languages, a.Budget, a.Message, settings.Currency

	l := h.orgLook(ctx, orgID)
	if l.Contact != "" {
		link := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/") + "/arrange#/requests"
		subject, c := emailtpl.RequestNew(l.Lang, er, link)
		msg := l.message([]string{l.Contact}, subject, c)
		msg.ReplyTo = er.Email
		if err := h.send(ctx, "quote_request_new", msg); err != nil {
			logx.Warn("admin.quote_request.mail", "kind", "quote_request_new", "request_id", id, "err", err.Error())
		}
	}
	if lang != "" {
		l.Lang = lang
	}
	subject, c := emailtpl.RequestReceived(l.Lang, er, l.Contact)
	if err := h.send(ctx, "quote_request_received", l.message([]string{er.Email}, subject, c)); err != nil {
		logx.Warn("admin.quote_request.mail", "kind", "quote_request_received", "request_id", id, "err", err.Error())
	}
}

/* ---------------- Organiser: Requests ---------------- */

// quoteAdminOrg is the caller's organisation, for its admins only:
// requests hold customers' personal data.
func quoteAdminOrg(w http.ResponseWriter, r *http.Request) (int64, bool) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins see its requests")
		return 0, false
	}
	return *me.OrgID, true
}

type quoteRequestDTO struct {
	ID        int64        `json:"id"`
	Form      string       `json:"form"`
	Answers   quoteAnswers `json:"answers"`
	Name      string       `json:"name"`
	Company   string       `json:"company"`
	Email     string       `json:"email"`
	Phone     string       `json:"phone"`
	Lang      string       `json:"lang"`
	Status    string       `json:"status"`
	Notes     string       `json:"notes"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

const quoteRequestCols = `id, form, answers, name, company, email, phone, lang, status, notes, created_at, updated_at`

func scanQuoteRequest(row interface{ Scan(...any) error }) (quoteRequestDTO, error) {
	var (
		q   quoteRequestDTO
		raw []byte
	)
	if err := row.Scan(&q.ID, &q.Form, &raw, &q.Name, &q.Company, &q.Email, &q.Phone, &q.Lang, &q.Status, &q.Notes, &q.CreatedAt, &q.UpdatedAt); err != nil {
		return q, err
	}
	return q, json.Unmarshal(raw, &q.Answers)
}

// ListQuoteRequests is the organisation's requests, newest first, with
// how many each status has. ?status= narrows the list; all by default.
//
// GET /api/admin/requests
func (h *Handlers) ListQuoteRequests(w http.ResponseWriter, r *http.Request) {
	orgID, ok := quoteAdminOrg(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains(quoteStatuses, status) {
		status = ""
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT `+quoteRequestCols+` FROM org_requests
		WHERE org_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC LIMIT 500
	`, orgID, status)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	out := []quoteRequestDTO{}
	for rows.Next() {
		q, err := scanQuoteRequest(rows)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	counts := map[string]int{}
	crows, err := h.DB.QueryContext(r.Context(), `SELECT status, COUNT(*) FROM org_requests WHERE org_id = $1 GROUP BY status`, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer crows.Close()
	for crows.Next() {
		var s string
		var n int
		if crows.Scan(&s, &n) == nil {
			counts[s] = n
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"requests": out, "counts": counts})
}

// quoteRequestID is the URL's request id, when it is the organisation's.
func (h *Handlers) quoteRequestID(w http.ResponseWriter, r *http.Request, orgID int64) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err == nil {
		var n int
		err = h.DB.QueryRowContext(r.Context(), `SELECT 1 FROM org_requests WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&n)
	}
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "request not found")
		return 0, false
	}
	return id, true
}

// UpdateQuoteRequest sets a request's status or the organisation's notes
// on it: {"status": "answered"}, {"notes": "…"}.
//
// PATCH /api/admin/requests/{id}
func (h *Handlers) UpdateQuoteRequest(w http.ResponseWriter, r *http.Request) {
	orgID, ok := quoteAdminOrg(w, r)
	if !ok {
		return
	}
	id, ok := h.quoteRequestID(w, r, orgID)
	if !ok {
		return
	}
	var req struct {
		Status *string `json:"status"`
		Notes  *string `json:"notes"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Status != nil && !slices.Contains(quoteStatuses, *req.Status) {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "unknown status", "reason": "invalid_status"})
		return
	}
	if req.Notes != nil && len(*req.Notes) > maxQuoteMessage {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the notes are too long", "reason": "too_long"})
		return
	}
	q, err := scanQuoteRequest(h.DB.QueryRowContext(r.Context(), `
		UPDATE org_requests SET status = COALESCE($2, status), notes = COALESCE($3, notes), updated_at = now()
		WHERE id = $1 RETURNING `+quoteRequestCols, id, req.Status, req.Notes))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, q)
}

// DeleteQuoteRequest deletes a request at once: junk, or the requester
// asked.
//
// DELETE /api/admin/requests/{id}
func (h *Handlers) DeleteQuoteRequest(w http.ResponseWriter, r *http.Request) {
	orgID, ok := quoteAdminOrg(w, r)
	if !ok {
		return
	}
	id, ok := h.quoteRequestID(w, r, orgID)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM org_requests WHERE id = $1`, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "deleting failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ---------------- Organiser: settings ---------------- */

type quoteSettingsDTO struct {
	Open  bool   `json:"open"`
	Intro string `json:"intro"`
	// URL is the form's address; Contact where requests are emailed.
	URL     string `json:"url"`
	Contact string `json:"contact"`
	// Off is what the form leaves out (quoteOffers' keys).
	Off []string `json:"off"`
}

func (h *Handlers) writeQuoteSettings(w http.ResponseWriter, r *http.Request, orgID int64) {
	var (
		out  quoteSettingsDTO
		slug string
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT o.requests_open, o.requests_intro, o.slug, COALESCE(`+emailtpl.ContactSQL+`, '')
		FROM organizations o WHERE o.id = $1
	`, orgID).Scan(&out.Open, &out.Intro, &slug, &out.Contact); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out.URL = domains.Base(orgID, h.PublicURL) + "/o/" + slug + "/request"
	var err error
	if out.Off, err = h.quoteOff(r.Context(), orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GetMyQuoteSettings is whether the caller's organisation takes requests,
// and its intro.
//
// GET /api/admin/orgs/me/requests
func (h *Handlers) GetMyQuoteSettings(w http.ResponseWriter, r *http.Request) {
	orgID, ok := quoteAdminOrg(w, r)
	if !ok {
		return
	}
	h.writeQuoteSettings(w, r, orgID)
}

// SetMyQuoteSettings turns requests on or off, sets the intro and what
// the organisation doesn't offer: {"open": true, "intro": "…", "off":
// ["game_master"]}. Without off, that stays as it is. At least one kind
// of adventure stays offered.
//
// PUT /api/admin/orgs/me/requests
func (h *Handlers) SetMyQuoteSettings(w http.ResponseWriter, r *http.Request) {
	orgID, ok := quoteAdminOrg(w, r)
	if !ok {
		return
	}
	var req struct {
		Open  bool      `json:"open"`
		Intro string    `json:"intro"`
		Off   *[]string `json:"off"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	intro := strings.TrimSpace(req.Intro)
	if utf8.RuneCountInString(intro) > maxQuoteIntro {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the intro is too long", "reason": "too_long"})
		return
	}
	var off any // nil keeps what is saved
	if req.Off != nil {
		for _, k := range *req.Off {
			if !slices.Contains(quoteOffers, k) {
				httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "unknown option: " + k, "reason": "invalid_option"})
				return
			}
		}
		list := known(*req.Off, quoteOffers)
		if !slices.ContainsFunc(quoteScopes, func(s string) bool { return !slices.Contains(list, s) }) {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "offer at least one kind of adventure", "reason": "no_scope"})
			return
		}
		off = strings.Join(list, ",")
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE organizations SET requests_open = $2, requests_intro = $3,
		       requests_off = CASE WHEN $4::text IS NULL THEN requests_off ELSE COALESCE(string_to_array(NULLIF($4::text, ''), ','), '{}') END
		WHERE id = $1`, orgID, req.Open, intro, off); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.writeQuoteSettings(w, r, orgID)
}
