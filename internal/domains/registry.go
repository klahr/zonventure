package domains

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Entry is one organisation's domain as the request path sees it.
type Entry struct {
	OrgID int64
	Slug  string
	// Name is what it goes by on its domain: its display name there,
	// else OrgName, its name.
	Name    string
	OrgName string
	Host    string
	Status  string
	// HasLogo is whether the organisation has a logo, its icon there.
	HasLogo bool
	// Font is its title typeface (a look font key), "" for the site's.
	Font string
	// TermsURL and PrivacyURL are its own policies, shown beside the
	// platform's; "" for none.
	TermsURL   string
	PrivacyURL string
	// Until is the organisation's entitlement, zero when it has none.
	Until time.Time
}

// Active reports whether the entitlement, with its grace, covers now.
func (e Entry) Active(now time.Time) bool {
	return e.Status != StatusRemoved && !e.Until.IsZero() && now.Before(e.Until.Add(24*time.Hour+Grace))
}

// InGrace reports whether the entitlement has ended but its grace has not.
func (e Entry) InGrace(now time.Time) bool {
	return e.Active(now) && !now.Before(e.Until.Add(24*time.Hour))
}

// Serving reports whether requests for the hostname are answered in the
// organisation's brand: entitled and its record found right.
func (e Entry) Serving(now time.Time) bool {
	return e.Active(now) && (e.Status == StatusLive || e.Status == StatusCertificate)
}

// Live reports whether the domain is the organisation's address: its
// links point there and its pages on the instance's own host redirect.
func (e Entry) Live(now time.Time) bool {
	return e.Active(now) && e.Status == StatusLive
}

type registry struct {
	byHost map[string]Entry
	byOrg  map[int64]Entry
	bySlug map[string]Entry
}

var current atomic.Pointer[registry]

// Load reads every claimed domain into memory. Called at startup, after
// every change and by the sweep, so the request path never queries.
func Load(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT d.org_id, o.slug, COALESCE(NULLIF(o.brand_display_name, ''), o.name), o.name, d.hostname, d.status, o.custom_domain_until,
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0), o.brand_font, o.brand_terms_url, o.brand_privacy_url
		FROM org_domains d JOIN organizations o ON o.id = d.org_id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var es []Entry
	for rows.Next() {
		var e Entry
		var until sql.NullTime
		if err := rows.Scan(&e.OrgID, &e.Slug, &e.Name, &e.OrgName, &e.Host, &e.Status, &until, &e.HasLogo, &e.Font, &e.TermsURL, &e.PrivacyURL); err != nil {
			return err
		}
		if until.Valid {
			e.Until = until.Time
		}
		es = append(es, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	Set(es)
	return nil
}

// Set replaces the registry with es: Load's last step, and how tests
// stage domains without a database.
func Set(es []Entry) {
	r := &registry{byHost: map[string]Entry{}, byOrg: map[int64]Entry{}, bySlug: map[string]Entry{}}
	for _, e := range es {
		r.byHost[e.Host] = e
		if e.Status == StatusRemoved {
			continue
		}
		r.byOrg[e.OrgID] = e
		r.bySlug[e.Slug] = e
	}
	current.Store(r)
}

func reg() *registry {
	if r := current.Load(); r != nil {
		return r
	}
	return &registry{}
}

// ByHost finds the organisation a request's Host belongs to.
func ByHost(host string) (Entry, bool) {
	h := strings.ToLower(host)
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	e, ok := reg().byHost[strings.TrimSuffix(h, ".")]
	return e, ok
}

// ByOrg finds an organisation's domain.
func ByOrg(orgID int64) (Entry, bool) {
	e, ok := reg().byOrg[orgID]
	return e, ok
}

// BySlug finds the domain of the organisation with slug.
func BySlug(slug string) (Entry, bool) {
	e, ok := reg().bySlug[slug]
	return e, ok
}

// Base is the origin an organisation's player-facing links use: its
// live domain, else fallback (the instance's ZV_PUBLIC_URL).
func Base(orgID int64, fallback string) string {
	if e, ok := ByOrg(orgID); ok && e.Live(time.Now()) {
		return Origin(e.Host)
	}
	return strings.TrimRight(strings.TrimSpace(fallback), "/")
}

// Querier is what BaseForEvent reads through.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// BaseForEvent is Base for the organisation running eventID.
func BaseForEvent(ctx context.Context, q Querier, eventID int64, fallback string) string {
	if len(reg().byOrg) == 0 || eventID <= 0 {
		return strings.TrimRight(strings.TrimSpace(fallback), "/")
	}
	var orgID int64
	if err := q.QueryRowContext(ctx, `SELECT org_id FROM events WHERE id = $1`, eventID).Scan(&orgID); err != nil {
		return strings.TrimRight(strings.TrimSpace(fallback), "/")
	}
	return Base(orgID, fallback)
}

// WhiteLabel reports whether an organisation's player-facing pages and
// emails carry only its own name: true while its domain is live.
func WhiteLabel(orgID int64) bool {
	e, ok := ByOrg(orgID)
	return ok && e.Live(time.Now())
}

type ctxKey struct{}

// WithEntry marks ctx as a request that came in on e's domain.
func WithEntry(ctx context.Context, e Entry) context.Context {
	return context.WithValue(ctx, ctxKey{}, e)
}

// FromContext is the organisation whose domain the request came in on.
func FromContext(ctx context.Context) (Entry, bool) {
	e, ok := ctx.Value(ctxKey{}).(Entry)
	return e, ok
}

// Origin is the scheme and host links to host use: https, except for a
// *.localhost name, which a development instance serves over plain
// http on ZV_PUBLIC_URL's port.
func Origin(host string) string {
	if strings.HasSuffix(host, ".localhost") {
		port := ""
		if u, err := url.Parse(os.Getenv("ZV_PUBLIC_URL")); err == nil && u.Port() != "" {
			port = ":" + u.Port()
		}
		return "http://" + host + port
	}
	return "https://" + host
}
