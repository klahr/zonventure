package admin

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/domains"
	"github.com/klahr/zonventure/internal/emailtpl"
	glook "github.com/klahr/zonventure/internal/graph/look"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
)

// An organisation's own domain: see zonventure-infra/notes/CUSTOM_DOMAINS.md
// and internal/domains. A platform admin grants it (custom_domain_until);
// the organisation's admins claim a hostname, add one CNAME, and the
// check below takes it to live.

// domainState is the claimed domain as the console shows it.
type domainState struct {
	Host      string     `json:"host"`
	Name      string     `json:"name"`
	Apex      string     `json:"apex"`
	Value     string     `json:"value"`
	Status    string     `json:"status"`
	Problem   string     `json:"problem"`
	Seen      string     `json:"seen"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	LiveAt    *time.Time `json:"live_at,omitempty"`
	DownSince *time.Time `json:"down_since,omitempty"`
}

// domainDTO is GET /api/admin/orgs/me/domain.
type domainDTO struct {
	// Enabled is whether this instance offers custom domains at all.
	Enabled  bool `json:"enabled"`
	Entitled bool `json:"entitled"`
	// Until is the entitlement's last day; GraceUntil the last day it
	// keeps serving after it.
	Until      string `json:"until,omitempty"`
	GraceUntil string `json:"grace_until,omitempty"`
	InGrace    bool   `json:"in_grace"`
	// Forever is an entitlement with no end date.
	Forever        bool   `json:"forever"`
	ContactURL     string `json:"contact_url"`
	PriceYearCents int64  `json:"price_year_cents"`
	Currency       string `json:"currency"`
	VATExcluded    bool   `json:"vat_excluded"`
	// Suggestion is the subdomain the console proposes.
	Suggestion string `json:"suggestion"`
	// StoreURL is the store on the instance's own address.
	StoreURL  string             `json:"store_url"`
	OrgName   string             `json:"org_name"`
	Domain    *domainState       `json:"domain"`
	Providers []domains.Provider `json:"providers"`
}

const dateLayout = "2006-01-02"

// GetMyDomain is the caller's organisation's domain and what it may do.
//
// GET /api/admin/orgs/me/domain
func (h *Handlers) GetMyDomain(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	h.writeDomain(w, r, *me.OrgID)
}

func (h *Handlers) writeDomain(w http.ResponseWriter, r *http.Request, orgID int64) {
	out, err := h.domainOf(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handlers) domainOf(ctx context.Context, orgID int64) (domainDTO, error) {
	p := settings.CurrentPricing()
	out := domainDTO{
		Enabled:        domains.Enabled(),
		ContactURL:     p.DomainContactURL,
		PriceYearCents: p.DomainYearCents,
		Currency:       settings.Currency,
		VATExcluded:    p.VATExcluded,
		Providers:      domains.Providers,
	}
	var (
		slug, lang string
		until      sql.NullTime
	)
	if err := h.DB.QueryRowContext(ctx, `SELECT name, slug, COALESCE(language, ''), custom_domain_until FROM organizations WHERE id = $1`, orgID).
		Scan(&out.OrgName, &slug, &lang, &until); err != nil {
		return out, err
	}
	out.StoreURL = storeURLFromSlug(h.PublicURL, slug)
	out.Suggestion = "adventures"
	if emailtpl.Lang(lang) == "sv" {
		out.Suggestion = "aventyr"
	}
	now := time.Now()
	if until.Valid {
		e := domains.Entry{Until: until.Time}
		out.Until = until.Time.Format(dateLayout)
		out.Forever = out.Until == domains.Forever
		out.GraceUntil = until.Time.Add(domains.Grace).Format(dateLayout)
		out.Entitled = e.Active(now)
		out.InGrace = e.InGrace(now)
	}
	d, err := h.domainRow(ctx, orgID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		out.Domain = &d
	}
	return out, nil
}

func (h *Handlers) domainRow(ctx context.Context, orgID int64) (domainState, error) {
	var (
		d                        domainState
		target                   string
		checked, live, downSince sql.NullTime
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT hostname, target, status, problem, seen, checked_at, live_at, down_since
		FROM org_domains WHERE org_id = $1 AND status <> 'removed'
	`, orgID).Scan(&d.Host, &target, &d.Status, &d.Problem, &d.Seen, &checked, &live, &downSince)
	if err != nil {
		return d, err
	}
	d.Name = domains.Name(d.Host)
	d.Apex = domains.Apex(d.Host)
	d.Value = domains.TargetFor(target)
	if checked.Valid {
		d.CheckedAt = &checked.Time
	}
	if live.Valid {
		d.LiveAt = &live.Time
	}
	if downSince.Valid {
		d.DownSince = &downSince.Time
	}
	return d, nil
}

// orgAdminOnly answers 403 unless the caller is an admin of an organisation.
func orgAdminOnly(w http.ResponseWriter, r *http.Request) (int64, bool) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins set up its domain")
		return 0, false
	}
	return *me.OrgID, true
}

// publicHost is the instance's own hostname (ZV_PUBLIC_URL's), "" on a
// development instance, where *.localhost names stand in for
// organisations' domains.
func (h *Handlers) publicHost() string {
	u := strings.TrimSpace(h.PublicURL)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	u = strings.SplitN(u, "/", 2)[0]
	if host, _, err := net.SplitHostPort(u); err == nil {
		u = host
	}
	if u == "localhost" {
		return ""
	}
	return strings.ToLower(u)
}

// ourHosts are the names no organisation may claim, nor any name under
// them: the instance's own hostname (v4.zonventure.com). When that is
// the bare registered domain, the apex rule refuses it already and its
// subdomains are ownZone's to judge.
func (h *Handlers) ourHosts() []string {
	if p := h.publicHost(); p != "" && p != domains.Apex(p) {
		return []string{p}
	}
	return nil
}

// ownZone reports whether host is under the instance's registered
// domain (adventures.zonventure.com on zonventure.com). Only an
// organisation given its domain with no end date may claim one: only
// the operator can point such a name, so for anyone else a claim would
// just reserve it.
func (h *Handlers) ownZone(host string) bool {
	apex := domains.Apex(h.publicHost())
	return apex != "" && strings.HasSuffix(host, "."+apex)
}

// SetMyDomain claims a hostname for the caller's organisation, replacing
// the one it had: {"host": "adventures.theirbrand.se"}. Checked at once.
//
// PUT /api/admin/orgs/me/domain
func (h *Handlers) SetMyDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgAdminOnly(w, r)
	if !ok {
		return
	}
	var req struct {
		Host string `json:"host"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	cur, err := h.domainOf(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !cur.Enabled || !cur.Entitled {
		httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "your organisation doesn't have its own domain", "reason": "not_entitled"})
		return
	}
	host, refuse := domains.Normalize(req.Host, h.ourHosts()...)
	if refuse != "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "that address can't be used", "reason": refuse})
		return
	}
	if h.ownZone(host) && !cur.Forever {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "that address can't be used", "reason": domains.RefuseOurs})
		return
	}
	if cur.Domain != nil && cur.Domain.Host == host {
		h.writeDomain(w, r, orgID)
		return
	}
	var slug string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT slug FROM organizations WHERE id = $1`, orgID).Scan(&slug); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	target := domains.TargetLabel(slug)
	var clash bool
	_ = h.DB.QueryRowContext(r.Context(), `SELECT EXISTS (SELECT 1 FROM org_domains WHERE target = $1 AND org_id <> $2)`, target, orgID).Scan(&clash)
	if clash {
		target = target + "-" + strconv.FormatInt(orgID, 10)
	}
	// A new name keeps the value they were told before.
	_ = h.DB.QueryRowContext(r.Context(), `SELECT target FROM org_domains WHERE org_id = $1`, orgID).Scan(&target)
	// A name another organisation gave up is free again.
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM org_domains WHERE hostname = $1 AND status = 'removed' AND org_id <> $2`, host, orgID)
	_, err = h.DB.ExecContext(r.Context(), `
		INSERT INTO org_domains (org_id, hostname, target) VALUES ($1, $2, $3)
		ON CONFLICT (org_id) DO UPDATE SET hostname = excluded.hostname, status = 'waiting', problem = '', seen = '',
			checked_at = NULL, live_at = NULL, down_since = NULL, down_notified = false
	`, orgID, host, target)
	if err != nil {
		if isUniqueViolation(err) {
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": "another organisation uses that address", "reason": "taken"})
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	noteAction(r, host)
	h.reloadDomains(r.Context())
	h.checkDomain(r.Context(), orgID)
	h.writeDomain(w, r, orgID)
}

// CheckMyDomain looks up the domain's DNS now.
//
// POST /api/admin/orgs/me/domain/check
func (h *Handlers) CheckMyDomain(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	h.checkDomainThrottled(r.Context(), *me.OrgID)
	h.writeDomain(w, r, *me.OrgID)
}

// DeleteMyDomain gives the domain up. The row stays, marked removed, so
// links already sent with it redirect back to the instance's own address
// for as long as the name still points here.
//
// DELETE /api/admin/orgs/me/domain
func (h *Handlers) DeleteMyDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgAdminOnly(w, r)
	if !ok {
		return
	}
	var host string
	err := h.DB.QueryRowContext(r.Context(), `
		UPDATE org_domains SET status = 'removed', problem = '', down_since = NULL, down_notified = false
		WHERE org_id = $1 AND status <> 'removed' RETURNING hostname`, orgID).Scan(&host)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		httpx.ServerErr(w, http.StatusInternalServerError, "removing failed", err)
		return
	}
	noteAction(r, host)
	h.reloadDomains(r.Context())
	h.writeDomain(w, r, orgID)
}

// DomainGuide is one provider's steps for the caller's domain, Markdown
// in ?lang=.
//
// GET /api/admin/orgs/me/domain/guide/{provider}
func (h *Handlers) DomainGuide(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	d, err := h.domainRow(r.Context(), *me.OrgID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no domain yet")
		return
	}
	lang := emailtpl.Lang(r.URL.Query().Get("lang"))
	httpx.JSON(w, http.StatusOK, map[string]string{"steps": domains.Guide(chi.URLParam(r, "provider"), lang, d.Host, d.Value)})
}

// sendLimit keeps an organisation from mailing instructions too often.
var sendLimit = struct {
	sync.Mutex
	sent map[int64][]time.Time
}{sent: map[int64][]time.Time{}}

func allowSend(orgID int64) bool {
	sendLimit.Lock()
	defer sendLimit.Unlock()
	now := time.Now()
	var keep []time.Time
	for _, t := range sendLimit.sent[orgID] {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 10 {
		sendLimit.sent[orgID] = keep
		return false
	}
	sendLimit.sent[orgID] = append(keep, now)
	return true
}

// SendDomainInstructions emails the record and a provider's steps to
// whoever runs the domain: {"email": "it@theirbrand.se", "provider":
// "loopia", "lang": "sv"}.
//
// POST /api/admin/orgs/me/domain/send
func (h *Handlers) SendDomainInstructions(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgAdminOnly(w, r)
	if !ok {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Provider string `json:"provider"`
		Lang     string `json:"lang"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	to := strings.TrimSpace(req.Email)
	if a, err := mail.ParseAddress(to); err != nil || a.Address != to || len(to) > 254 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "not a valid email address", "reason": "invalid_email"})
		return
	}
	d, err := h.domainRow(r.Context(), orgID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "choose the address first")
		return
	}
	if !allowSend(orgID) {
		httpx.JSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many emails; try again in an hour", "reason": "rate_limited"})
		return
	}
	me, _ := auth.FromContext(r.Context())
	from := me.Username
	if me.Email != "" {
		from = me.Email
	}
	lang := emailtpl.Lang(req.Lang)
	var org string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&org)
	rec := emailtpl.DomainRecord{Host: d.Host, Name: d.Name, Value: d.Value}
	steps := ""
	if req.Provider != "" && req.Provider != "other" {
		steps = domains.Guide(req.Provider, lang, d.Host, d.Value)
	}
	subject, c := emailtpl.DomainInstructions(lang, org, from, rec, steps, h.domainCheckLink(d.Host))
	msg := emailtpl.Message([]string{to}, subject, emailtpl.Classic, emailtpl.Platform(), c)
	msg.ReplyTo = me.Email
	if err := h.send(r.Context(), "domain_instructions", msg); err != nil {
		httpx.ServerErr(w, http.StatusBadGateway, "sending failed", err)
		return
	}
	noteAction(r, to)
	httpx.JSON(w, http.StatusOK, map[string]string{"sent_to": to})
}

// domainCheckLink is the public page that shows whether host works.
func (h *Handlers) domainCheckLink(host string) string {
	base := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if base == "" {
		return ""
	}
	return base + "/domain-check/" + host
}

// publicDomainDTO is what the public check page shows: nothing a DNS
// lookup wouldn't tell anyone.
type publicDomainDTO struct {
	Host      string     `json:"host"`
	Name      string     `json:"name"`
	Apex      string     `json:"apex"`
	Value     string     `json:"value"`
	Status    string     `json:"status"`
	Problem   string     `json:"problem"`
	Seen      string     `json:"seen"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	OrgName   string     `json:"org_name"`
}

// PublicDomainStatus checks a claimed hostname for whoever was sent its
// instructions; no account needed. A host checked in the last 20
// seconds answers from the last check.
//
// POST /api/domain-check/{host}
func (h *Handlers) PublicDomainStatus(w http.ResponseWriter, r *http.Request) {
	out, ok := h.PublicDomain(r.Context(), strings.ToLower(chi.URLParam(r, "host")), true)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no organisation uses that address")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PublicDomain is host's state for the public check page; check runs a
// check first when the last is old enough.
func (h *Handlers) PublicDomain(ctx context.Context, host string, check bool) (publicDomainDTO, bool) {
	e, ok := domains.ByHost(host)
	if !ok || e.Status == domains.StatusRemoved {
		return publicDomainDTO{}, false
	}
	if check {
		h.checkDomainThrottled(ctx, e.OrgID)
	}
	d, err := h.domainRow(ctx, e.OrgID)
	if err != nil {
		return publicDomainDTO{}, false
	}
	return publicDomainDTO{Host: d.Host, Name: d.Name, Apex: d.Apex, Value: d.Value, Status: d.Status, Problem: d.Problem, Seen: d.Seen, CheckedAt: d.CheckedAt, OrgName: e.Name}, true
}

// checkDomainThrottled checks unless the last check is under 20
// seconds old: the console polls, and the public page may be open in
// several tabs.
func (h *Handlers) checkDomainThrottled(ctx context.Context, orgID int64) {
	var recent bool
	_ = h.DB.QueryRowContext(ctx, `SELECT checked_at > now() - interval '20 seconds' FROM org_domains WHERE org_id = $1`, orgID).Scan(&recent)
	if !recent {
		h.checkDomain(ctx, orgID)
	}
}

var domainLocks sync.Map

// checkDomain looks the organisation's domain up, probes it once its
// record is right, and saves what it found; it mails the admins when
// it goes live and when a live domain has been down half an hour. A
// live domain stays live through a day of failed checks, so a DNS
// hiccup doesn't move every link.
func (h *Handlers) checkDomain(ctx context.Context, orgID int64) {
	mu, _ := domainLocks.LoadOrStore(orgID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	var (
		host, target, prev string
		downSince          sql.NullTime
		downNotified       bool
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT hostname, target, status, down_since, down_notified FROM org_domains WHERE org_id = $1 AND status <> 'removed'
	`, orgID).Scan(&host, &target, &prev, &downSince, &downNotified); err != nil {
		return
	}
	value := domains.TargetFor(target)
	res := domains.Check(ctx, h.domainResolver(host, value), host, value)
	if res.Status == domains.StatusCertificate {
		// The record is right: say so before probing, so the TLS
		// proxy's question (tls-allowed) is answered yes.
		if prev != domains.StatusLive && prev != domains.StatusCertificate {
			_, _ = h.DB.ExecContext(ctx, `UPDATE org_domains SET status = 'certificate', problem = '', seen = $2, checked_at = now() WHERE org_id = $1`, orgID, res.Seen)
			h.reloadDomains(ctx)
		}
		if devDomain(host) {
			res.Status = domains.StatusLive
		} else if err := domains.Probe(ctx, host); err != nil {
			res.Problem = domains.ProblemCertificate
			logx.Info("domains.probe", "host", host, "err", err.Error())
		} else {
			res.Status = domains.StatusLive
		}
	}
	now := time.Now()
	switch {
	case res.Status == domains.StatusLive:
		_, _ = h.DB.ExecContext(ctx, `
			UPDATE org_domains SET status = 'live', problem = '', seen = $2, checked_at = now(),
				live_at = CASE WHEN status = 'live' THEN live_at ELSE now() END,
				down_since = NULL, down_notified = false
			WHERE org_id = $1
		`, orgID, res.Seen)
		if prev != domains.StatusLive {
			h.reloadDomains(ctx)
			h.mailDomainAdmins(ctx, orgID, func(lang string) (string, emailtpl.Content) {
				return emailtpl.DomainLive(lang, host, domains.Origin(host), h.arrangeLink("#/settings/domain"))
			})
		}
	case prev == domains.StatusLive:
		since := now
		if downSince.Valid {
			since = downSince.Time
		}
		status := domains.StatusLive
		if now.Sub(since) >= 24*time.Hour {
			status = res.Status
		}
		notify := !downNotified && now.Sub(since) >= 30*time.Minute
		_, _ = h.DB.ExecContext(ctx, `
			UPDATE org_domains SET status = $2, problem = $3, seen = $4, checked_at = now(),
				down_since = COALESCE(down_since, now()), down_notified = down_notified OR $5
			WHERE org_id = $1
		`, orgID, status, res.Problem, res.Seen, notify)
		if notify {
			h.mailDomainAdmins(ctx, orgID, func(lang string) (string, emailtpl.Content) {
				return emailtpl.DomainDown(lang, host, value, h.arrangeLink("#/settings/domain"))
			})
		}
		if status != domains.StatusLive {
			h.reloadDomains(ctx)
		}
	default:
		_, _ = h.DB.ExecContext(ctx, `
			UPDATE org_domains SET status = $2, problem = $3, seen = $4, checked_at = now() WHERE org_id = $1
		`, orgID, res.Status, res.Problem, res.Seen)
		if res.Status != prev {
			h.reloadDomains(ctx)
		}
	}
}

// devDomain is a *.localhost name on a development instance: taken as
// pointing here, so the whole flow can be tried without real DNS.
func devDomain(host string) bool {
	env := strings.ToLower(os.Getenv("ZV_ENV"))
	return (env == "dev" || env == "development") && strings.HasSuffix(host, ".localhost")
}

type devResolver struct{ host, value string }

func (d devResolver) LookupCNAME(context.Context, string) (string, error) { return d.value + ".", nil }
func (d devResolver) LookupHost(context.Context, string) ([]string, error) {
	return []string{"127.0.0.1"}, nil
}

func (h *Handlers) domainResolver(host, value string) domains.Resolver {
	if devDomain(host) {
		return devResolver{host, value}
	}
	return net.DefaultResolver
}

func (h *Handlers) reloadDomains(ctx context.Context) {
	if err := domains.Load(ctx, h.DB); err != nil {
		logx.Warn("domains.load", "err", err.Error())
	}
}

// mailDomainAdmins emails an organisation's admins, each in their
// language, from the platform: it is about their account with us.
func (h *Handlers) mailDomainAdmins(ctx context.Context, orgID int64, build func(lang string) (string, emailtpl.Content)) {
	var orgLang string
	_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(language, '') FROM organizations WHERE id = $1`, orgID).Scan(&orgLang)
	byLang, err := h.emailsByLang(ctx, emailtpl.Lang(orgLang), `
		SELECT u.email, u.language FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role = 'org_admin' AND COALESCE(u.email, '') <> ''`, orgID)
	if err != nil {
		logx.Warn("domains.notify", "org_id", orgID, "err", err.Error())
		return
	}
	for lang, to := range byLang {
		subject, c := build(lang)
		msg := emailtpl.Message(to, subject, emailtpl.Classic, emailtpl.Platform(), c)
		if err := h.send(ctx, "domain", msg); err != nil {
			logx.Warn("domains.notify", "org_id", orgID, "err", err.Error())
		}
	}
}

// SweepDomains checks domains on a schedule — every 10 minutes until
// live (the console checks faster while it is open), daily once live,
// every 10 minutes while a live one fails — reloads the registry so
// name, slug and entitlement changes reach the request path, and tells
// an organisation once when its entitlement has ended.
func (h *Handlers) SweepDomains(ctx context.Context) {
	if !domains.Enabled() {
		return
	}
	h.reloadDomains(ctx)
	rows, err := h.DB.QueryContext(ctx, `
		SELECT d.org_id FROM org_domains d JOIN organizations o ON o.id = d.org_id
		WHERE d.status <> 'removed' AND o.custom_domain_until IS NOT NULL AND o.custom_domain_until + interval '31 days' > now()
		  AND (d.checked_at IS NULL
		    OR (d.status <> 'live' AND d.checked_at < now() - interval '10 minutes')
		    OR (d.down_since IS NOT NULL AND d.checked_at < now() - interval '10 minutes')
		    OR d.checked_at < now() - interval '1 day')
		ORDER BY d.checked_at NULLS FIRST LIMIT 20
	`)
	if err != nil {
		logx.Warn("domains.sweep", "err", err.Error())
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		h.checkDomain(ctx, id)
	}
	h.sweepDomainEndings(ctx)
}

func (h *Handlers) sweepDomainEndings(ctx context.Context) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT d.org_id, d.hostname, o.custom_domain_until FROM org_domains d JOIN organizations o ON o.id = d.org_id
		WHERE d.status <> 'removed' AND o.custom_domain_until < current_date AND o.custom_domain_until + interval '30 days' >= current_date
		  AND d.ending_notified_for IS DISTINCT FROM o.custom_domain_until
	`)
	if err != nil {
		logx.Warn("domains.endings", "err", err.Error())
		return
	}
	type ending struct {
		org   int64
		host  string
		until time.Time
	}
	var es []ending
	for rows.Next() {
		var e ending
		if rows.Scan(&e.org, &e.host, &e.until) == nil {
			es = append(es, e)
		}
	}
	rows.Close()
	for _, e := range es {
		res, err := h.DB.ExecContext(ctx, `UPDATE org_domains SET ending_notified_for = $2 WHERE org_id = $1 AND ending_notified_for IS DISTINCT FROM $2`, e.org, e.until)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		last := e.until.Add(domains.Grace).Format(dateLayout)
		h.mailDomainAdmins(ctx, e.org, func(lang string) (string, emailtpl.Content) {
			return emailtpl.DomainEnding(lang, e.host, last, h.arrangeLink("#/settings/domain"))
		})
	}
}

// orgCustomDomainDTO is the platform admin's view of an organisation's
// entitlement and domain.
type orgCustomDomainDTO struct {
	Until  string       `json:"until"`
	Domain *domainState `json:"domain"`
}

// GetOrgCustomDomain is an organisation's entitlement and domain.
// Platform admins.
//
// GET /api/admin/organizations/{orgID}/custom-domain
func (h *Handlers) GetOrgCustomDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad organisation id")
		return
	}
	h.writeOrgCustomDomain(w, r, id)
}

func (h *Handlers) writeOrgCustomDomain(w http.ResponseWriter, r *http.Request, id int64) {
	var until sql.NullTime
	if err := h.DB.QueryRowContext(r.Context(), `SELECT custom_domain_until FROM organizations WHERE id = $1`, id).Scan(&until); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "organization not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out := orgCustomDomainDTO{}
	if until.Valid {
		out.Until = until.Time.Format(dateLayout)
	}
	if d, err := h.domainRow(r.Context(), id); err == nil {
		out.Domain = &d
	}
	httpx.JSON(w, http.StatusOK, out)
}

// SetOrgCustomDomain grants an organisation its own domain until a day,
// for good, or takes it away: {"until": "2027-10-14"}, {"until":
// "9999-12-31"} (domains.Forever: no end date, nothing to renew) or
// {"until": ""}. Granting
// it to an organisation that had none mails its admins how to start.
//
// PUT /api/admin/organizations/{orgID}/custom-domain
func (h *Handlers) SetOrgCustomDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad organisation id")
		return
	}
	var req struct {
		Until string `json:"until"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	var until any
	if s := strings.TrimSpace(req.Until); s != "" {
		t, err := time.Parse(dateLayout, s)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "until must be a date like 2027-10-14")
			return
		}
		until = t
	}
	var before sql.NullTime
	if err := h.DB.QueryRowContext(r.Context(), `SELECT custom_domain_until FROM organizations WHERE id = $1`, id).Scan(&before); err != nil {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE organizations SET custom_domain_until = $2 WHERE id = $1`, id, until); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	now := time.Now()
	wasOn := before.Valid && (domains.Entry{Until: before.Time}).Active(now)
	if until == nil {
		noteAction(r, "off")
	} else {
		noteAction(r, req.Until)
		if !wasOn && (domains.Entry{Until: until.(time.Time)}).Active(now) {
			var org string
			_ = h.DB.QueryRowContext(r.Context(), `SELECT name FROM organizations WHERE id = $1`, id).Scan(&org)
			h.mailDomainAdmins(r.Context(), id, func(lang string) (string, emailtpl.Content) {
				return emailtpl.DomainReady(lang, org, h.arrangeLink("#/settings/domain"))
			})
		}
	}
	h.reloadDomains(r.Context())
	h.writeOrgCustomDomain(w, r, id)
}

// eventBase is the origin an event's player-facing links use: its
// organisation's live domain, else the instance's.
func (h *Handlers) eventBase(ctx context.Context, eventID int64) string {
	return domains.BaseForEvent(ctx, h.DB, eventID, h.PublicURL)
}

// requestBase is the origin to send someone back to: the organisation's
// domain the request came in on, else the instance's.
func (h *Handlers) requestBase(ctx context.Context) string {
	if e, ok := domains.FromContext(ctx); ok {
		return domains.Origin(e.Host)
	}
	return strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
}

// domainBrandDTO is how an organisation presents itself on its own
// domain: GET/PUT /api/admin/orgs/me/domain/brand. DisplayName "" goes
// by the organisation's name; Font is a look font key, "" the site's.
type domainBrandDTO struct {
	DisplayName string `json:"display_name"`
	Font        string `json:"font"`
	TermsURL    string `json:"terms_url"`
	PrivacyURL  string `json:"privacy_url"`
}

// GetMyDomainBrand is the caller's organisation's own-domain branding.
//
// GET /api/admin/orgs/me/domain/brand
func (h *Handlers) GetMyDomainBrand(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	h.writeDomainBrand(w, r, *me.OrgID)
}

func (h *Handlers) writeDomainBrand(w http.ResponseWriter, r *http.Request, orgID int64) {
	var out domainBrandDTO
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT brand_display_name, brand_font, brand_terms_url, brand_privacy_url FROM organizations WHERE id = $1
	`, orgID).Scan(&out.DisplayName, &out.Font, &out.TermsURL, &out.PrivacyURL); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// SetMyDomainBrand saves it. Org admins only.
//
// PUT /api/admin/orgs/me/domain/brand
func (h *Handlers) SetMyDomainBrand(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgAdminOnly(w, r)
	if !ok {
		return
	}
	var req domainBrandDTO
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.TermsURL = strings.TrimSpace(req.TermsURL)
	req.PrivacyURL = strings.TrimSpace(req.PrivacyURL)
	if utf8.RuneCountInString(req.DisplayName) > 100 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the name is too long", "reason": "invalid_name"})
		return
	}
	if _, ok := glook.Fonts[req.Font]; req.Font != "" && !ok {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "unknown typeface", "reason": "invalid_font"})
		return
	}
	for _, u := range []string{req.TermsURL, req.PrivacyURL} {
		if u == "" {
			continue
		}
		p, err := url.Parse(u)
		if err != nil || p.Scheme != "https" || p.Host == "" || len(u) > 500 {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "links must be https:// addresses", "reason": "invalid_url"})
			return
		}
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE organizations SET brand_display_name = $2, brand_font = $3, brand_terms_url = $4, brand_privacy_url = $5 WHERE id = $1
	`, orgID, req.DisplayName, req.Font, req.TermsURL, req.PrivacyURL); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.reloadDomains(r.Context())
	h.writeDomainBrand(w, r, orgID)
}
