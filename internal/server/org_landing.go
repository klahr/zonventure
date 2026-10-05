package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// orgProfile is the public face of an organisation: what /o/{slug}
// shows about it.
type orgProfile struct {
	ID       int64
	Slug     string
	Name     string
	Tagline  sql.NullString
	Website  sql.NullString
	HasLogo  bool
	HasCover bool
	Accent   sql.NullString
	Accent2  sql.NullString
	Bg       sql.NullString
	Ink      sql.NullString
}

// loadOrgProfile looks an organisation up by slug. sql.ErrNoRows means
// no such organisation.
func loadOrgProfile(ctx context.Context, db *sql.DB, slug string) (orgProfile, error) {
	p := orgProfile{Slug: slug}
	err := db.QueryRowContext(ctx, `
		SELECT id, name, tagline, public_website,
		       (logo_content IS NOT NULL AND length(logo_content) > 0),
		       (cover_image_content IS NOT NULL AND length(cover_image_content) > 0),
		       accent_color, accent_secondary_color, background_color, text_color
		FROM organizations
		WHERE slug = $1
	`, slug).Scan(&p.ID, &p.Name, &p.Tagline, &p.Website, &p.HasLogo, &p.HasCover,
		&p.Accent, &p.Accent2, &p.Bg, &p.Ink)
	return p, err
}

// heroBgStyle layers the org's cover under the accent gradient so dark
// and light covers both stay readable. Empty without a cover.
func (p orgProfile) heroBgStyle() string {
	if !p.HasCover {
		return ""
	}
	// Same nested-context concern as store_event.go's COVER_URL: the URL
	// lands inside a `style=` attribute, then a CSS url('…') literal.
	// cssURLQuote percent-encodes the CSS-meaningful chars so a future
	// cover-URL shape can't escape the quotes.
	return fmt.Sprintf(
		"background-image: linear-gradient(135deg, rgba(173,19,87,.6), rgba(226,64,128,.5)), url('%s'); background-size: cover; background-position: center;",
		html.EscapeString(cssURLQuote("/org-cover/"+strconv.FormatInt(p.ID, 10))),
	)
}

// logoBlock renders the org's logo server-side so a no-JS reader sees
// it. Empty without a logo.
func (p orgProfile) logoBlock() string {
	if !p.HasLogo {
		return ""
	}
	return fmt.Sprintf(
		`<div class="logo-wrap"><span class="logo"><img src="%s" alt="%s logo"></span></div>`,
		html.EscapeString("/org-logo/"+strconv.FormatInt(p.ID, 10)),
		html.EscapeString(p.Name),
	)
}

// brandStyle is the org's colour override block; see orgBrandStyleBlock.
func (p orgProfile) brandStyle() string {
	return orgBrandStyleBlock(p.Accent.String, p.Accent2.String, p.Bg.String, p.Ink.String)
}

// orgBrandStyleBlock emits a `<style>:root { --foo: #bar; }</style>`
// snippet that overrides the platform-default CSS variables on the
// org-landing + store templates. Any of the four colours can be
// empty — empty drops that variable from the override and the
// template's fallback applies. When ALL are empty the function
// returns "" so the placeholder is a no-op (no extra <style> tag,
// no chrome bytes).
//
// All four inputs go through isValidHexColor at the API boundary
// (SetMyOrgProfile rejects anything else), so we trust them here
// without re-validating. The output is HTML-safe by construction:
// only #-prefixed hex values reach the substitution.
func orgBrandStyleBlock(accent, accent2, bg, ink string) string {
	type override struct {
		name string
		val  string
	}
	overrides := []override{}
	if accent != "" {
		overrides = append(overrides, override{"--accent", accent})
	}
	if accent2 != "" {
		overrides = append(overrides, override{"--accent-2", accent2})
	}
	if bg != "" {
		overrides = append(overrides, override{"--bg", bg})
	}
	if ink != "" {
		overrides = append(overrides, override{"--ink", ink})
	}
	if len(overrides) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<style>:root{")
	for _, o := range overrides {
		b.WriteString(o.name)
		b.WriteString(":")
		b.WriteString(o.val)
		b.WriteString(";")
	}
	b.WriteString("}</style>")
	return b.String()
}

// buildOrgSitemapURLs returns <url>… entries for every org with at
// least one currently-for-sale event. Empty when no orgs qualify
// (fresh self-host with no public events) — the substitution
// produces a no-op then. host is the absolute origin already
// resolved by the caller, so we just concat.
func buildOrgSitemapURLs(ctx context.Context, db *sql.DB, host string) string {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT o.slug
		FROM organizations o
		JOIN events e ON e.org_id = o.id
		WHERE e.public_for_sale
		  AND e.ticket_price_cents IS NOT NULL
		  AND e.state IN ('running', 'paused')
		ORDER BY o.slug
	`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			continue
		}
		b.WriteString("  <url>\n    <loc>")
		b.WriteString(host)
		b.WriteString("/o/")
		b.WriteString(slug)
		b.WriteString("</loc>\n    <changefreq>weekly</changefreq>\n    <priority>0.7</priority>\n  </url>\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// orgSlugExists is a fast existence check used by route handlers
// that want to 404 before investing render work in a bogus slug.
// Wraps a single SELECT so the caller can stay terse.
func orgSlugExists(ctx context.Context, db *sql.DB, slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return false
	}
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM organizations WHERE slug = $1)`, slug).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// redirectLegacyStoreEvent serves the old /store/event/{id} URL by
// looking up the event's owning org and 301'ing the buyer to
// /o/{slug}/store/event/{id}. Search engines treat 301 as a permanent
// move so link equity from old indexed URLs migrates to the
// canonical org-scoped path.
//
// 404 (rather than redirect) when the event doesn't exist, isn't
// for-sale, or its owning org has no slug — a safety net rather
// than a path the live system should reach.
func redirectLegacyStoreEvent(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	idStr := chi.URLParam(r, "event_id")
	if idStr == "" {
		servePublicPage("web/404.html", http.StatusNotFound)(w, r)
		return
	}
	// Parse the URL param to int64 so the WHERE clause can use the
	// indexed integer column directly. The previous `e.id::TEXT = $1`
	// cast forced a sequential scan once events grew. Garbage in the
	// path falls through to a clean 404.
	eventID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		servePublicPage("web/404.html", http.StatusNotFound)(w, r)
		return
	}
	var slug string
	err = db.QueryRowContext(r.Context(), `
		SELECT o.slug
		FROM events e
		JOIN organizations o ON o.id = e.org_id
		WHERE e.id = $1
		  AND e.public_for_sale
		  AND e.ticket_price_cents IS NOT NULL
		  AND e.state IN ('running', 'paused')
	`, eventID).Scan(&slug)
	if errors.Is(err, sql.ErrNoRows) || slug == "" {
		servePublicPage("web/404.html", http.StatusNotFound)(w, r)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/o/"+slug+"/store/event/"+idStr, http.StatusMovedPermanently)
}
