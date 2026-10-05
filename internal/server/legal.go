package server

import (
	"bytes"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/components"
)

// LegalConfig is the bag of human-facing strings we splice into the
// privacy + terms templates at request time. All optional — empty
// values render an obvious "[fill in via ZV_*]" so the operator
// notices unset config before a customer does.
type LegalConfig struct {
	CompanyName           string
	ContactEmail          string
	DPOEmail              string
	Jurisdiction          string
	LocationRetentionDays string // strings so the env passes through unchanged
	AuditRetentionDays    string
	PartyRetentionDays    string
	LastUpdated           string // YYYY-MM-DD; defaults to today on first render
	HostingProvider       string // sub-processor name shown in privacy policy
	EmailProvider         string // sub-processor name shown in privacy policy
	// Seller identity for e-commerce disclosure (e-handelslagen 8 §,
	// distansavtalslagen 2 kap. 2 §). Optional: each line renders on
	// the legal pages, the store footers and next to the buy button
	// only when set.
	CompanyOrgNumber string
	CompanyAddress   string
	CompanyVATNumber string
	// Map tiles the players' browsers fetch (live board, recap, the
	// player app's maps). Empty = OpenStreetMap's public tile server,
	// fine for development but not for production volume under its
	// tile usage policy. MapProvider names the tile host in the privacy
	// policy, since it receives every viewer's IP address.
	MapTileURL         string // {z}/{x}/{y} template, https
	MapTileAttribution string // HTML, as Leaflet expects
	MapProvider        string
	// Reviewed flips off the "draft template — needs lawyer review"
	// banner. Operators set ZV_LEGAL_REVIEWED=1 once their lawyer has
	// signed off on the wording; self-host without it keeps the
	// banner so an unconfigured operator doesn't ship a template
	// verbatim.
	Reviewed bool
}

// missingRequired returns the env-var names whose values are empty.
// CompanyName, ContactEmail, and Jurisdiction are load-bearing: a
// page that says "controlled by [set ZV_COMPANY_NAME]" or doesn't
// name a venue for disputes is worse than no page at all. DPO email
// falls back to ContactEmail so it's not on the required list.
func (c LegalConfig) missingRequired() []string {
	var missing []string
	if strings.TrimSpace(c.CompanyName) == "" {
		missing = append(missing, "ZV_COMPANY_NAME")
	}
	if strings.TrimSpace(c.ContactEmail) == "" {
		missing = append(missing, "ZV_CONTACT_EMAIL")
	}
	if strings.TrimSpace(c.Jurisdiction) == "" {
		missing = append(missing, "ZV_JURISDICTION")
	}
	return missing
}

// withLegalComponent adds the legal pages to the admin's Installed
// components panel, so an operator sees that /privacy and /terms are
// still off (and links to them hidden) before launch.
func withLegalComponent(list []components.Component, c LegalConfig) []components.Component {
	missing := c.missingRequired()
	comp := components.Component{Name: "legal-pages", Impl: "templates", Enabled: len(missing) == 0}
	if len(missing) > 0 {
		comp.Notes = "set $" + strings.Join(missing, ", $") + " to publish /privacy and /terms"
	}
	return append(append([]components.Component{}, list...), comp)
}

func (c LegalConfig) defaulted() LegalConfig {
	d := c
	if d.CompanyName == "" {
		d.CompanyName = "[set ZV_COMPANY_NAME]"
	}
	if d.ContactEmail == "" {
		d.ContactEmail = "[set ZV_CONTACT_EMAIL]"
	}
	if d.DPOEmail == "" {
		// Fall back to contact email so the page renders something
		// reachable even before a dedicated DPO address exists.
		d.DPOEmail = d.ContactEmail
	}
	if d.Jurisdiction == "" {
		d.Jurisdiction = "[set ZV_JURISDICTION]"
	}
	if d.LocationRetentionDays == "" {
		d.LocationRetentionDays = "30"
	}
	if d.AuditRetentionDays == "" {
		d.AuditRetentionDays = "90"
	}
	if d.PartyRetentionDays == "" {
		d.PartyRetentionDays = "14"
	}
	if d.LastUpdated == "" {
		d.LastUpdated = time.Now().UTC().Format("2006-01-02")
	}
	if d.HostingProvider == "" {
		d.HostingProvider = "[set ZV_HOSTING_PROVIDER]"
	}
	if d.EmailProvider == "" {
		d.EmailProvider = "[set ZV_EMAIL_PROVIDER]"
	}
	if d.MapTileURL == "" {
		d.MapTileURL = defaultMapTileURL
		if d.MapTileAttribution == "" {
			d.MapTileAttribution = defaultMapTileAttribution
		}
	}
	if d.MapProvider == "" {
		d.MapProvider = mapProviderFor(d.MapTileURL)
	}
	return d
}

const (
	defaultMapTileURL         = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	defaultMapTileAttribution = `&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors`
)

// mapProviderFor names the tile host for the privacy policy when the
// operator hasn't: the OpenStreetMap Foundation for the default, else
// the configured URL's host, so the disclosure never names the wrong
// recipient of the viewers' IP addresses.
func mapProviderFor(tileURL string) string {
	if tileURL == defaultMapTileURL {
		return "OpenStreetMap Foundation (tile.openstreetmap.org)"
	}
	if u, err := url.Parse(tileURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "[set ZV_MAP_PROVIDER]"
}

// mapConfigJS is /assets/map-config.js: the tile source every map in
// the browser reads from window.ZV_MAP, so the operator switches tile
// providers with an env var instead of a rebuild.
func mapConfigJS(c LegalConfig) []byte {
	d := c.defaulted()
	b, _ := json.Marshal(map[string]string{"tile_url": d.MapTileURL, "attribution": d.MapTileAttribution})
	return []byte("window.ZV_MAP=" + string(b) + ";\n")
}

// identityLines is the seller's registration data, each "label value"
// as plain text; empty fields are left out.
func (c LegalConfig) identityLines(lang string) []string {
	orgLabel, vatLabel := "Org. no.", "VAT no."
	if lang == "sv" {
		orgLabel, vatLabel = "Org.nr", "Momsreg.nr"
	}
	var out []string
	if v := strings.TrimSpace(c.CompanyOrgNumber); v != "" {
		out = append(out, orgLabel+" "+v)
	}
	if v := strings.TrimSpace(c.CompanyVATNumber); v != "" {
		out = append(out, vatLabel+" "+v)
	}
	if v := strings.TrimSpace(c.CompanyAddress); v != "" {
		out = append(out, v)
	}
	return out
}

// legalIdentityHTML is {{COMPANY_IDENTITY}} on /privacy and /terms: a
// paragraph with org number, VAT number and address, or nothing.
func legalIdentityHTML(c LegalConfig) string {
	lines := c.identityLines("en")
	if len(lines) == 0 {
		return ""
	}
	for i, l := range lines {
		lines[i] = html.EscapeString(l)
	}
	return "<p>" + strings.Join(lines, "<br>") + "</p>"
}

// sellerIdentityHTML is the one-line seller disclosure for the store
// pages: name, registration data, address and contact address.
// soldBy prefixes it for the line next to the buy button. Empty until
// the operator names the company.
func sellerIdentityHTML(c LegalConfig, lang string, soldBy bool) string {
	name := strings.TrimSpace(c.CompanyName)
	if name == "" {
		return ""
	}
	parts := []string{html.EscapeString(name)}
	for _, l := range c.identityLines(lang) {
		parts = append(parts, html.EscapeString(l))
	}
	if v := strings.TrimSpace(c.ContactEmail); v != "" {
		e := html.EscapeString(v)
		parts = append(parts, `<a href="mailto:`+e+`">`+e+`</a>`)
	}
	line := strings.Join(parts, " · ")
	if soldBy {
		label := "Sold by"
		if lang == "sv" {
			label = "Säljare:"
		}
		line = label + " " + line
	}
	return line
}

// SellerIdentityText is sellerIdentityHTML as plain text, for the
// footer of mail sent on a sale (the ticket email).
func (c LegalConfig) SellerIdentityText(lang string) string {
	name := strings.TrimSpace(c.CompanyName)
	if name == "" {
		return ""
	}
	parts := append([]string{name}, c.identityLines(lang)...)
	if v := strings.TrimSpace(c.ContactEmail); v != "" {
		parts = append(parts, v)
	}
	label := "Sold by"
	if lang == "sv" {
		label = "Säljare:"
	}
	return label + " " + strings.Join(parts, " · ")
}

// Markers a page puts where the seller line goes: in the footer, and
// next to a buy button. spliceSellerIdentity fills them on the way out,
// so every page shares one source without each renderer carrying the
// legal config.
var (
	sellerMarker       = []byte("<!--ZV_SELLER-->")
	sellerSoldByMarker = []byte("<!--ZV_SELLER_SOLD_BY-->")
)

func spliceSellerIdentity(body []byte, c LegalConfig) []byte {
	if !bytes.Contains(body, sellerMarker) && !bytes.Contains(body, sellerSoldByMarker) {
		return body
	}
	lang := "en"
	if bytes.Contains(body, []byte(`<html lang="sv"`)) {
		lang = "sv"
	}
	wrap := func(line string) []byte {
		if line == "" {
			return nil
		}
		return []byte(`<p class="seller">` + line + `</p>`)
	}
	body = bytes.ReplaceAll(body, sellerMarker, wrap(sellerIdentityHTML(c, lang, false)))
	return bytes.ReplaceAll(body, sellerSoldByMarker, wrap(sellerIdentityHTML(c, lang, true)))
}

// renderLegalPage reads the embedded template, splices the config in,
// and writes it as a no-cache HTML response. Templating is intentionally
// just `strings.Replace` — Go's html/template would force us to escape
// every single placeholder including the ones already inside HTML
// attributes (mailto:), and a static brochure page doesn't need
// per-request data flow that warrants the extra ceremony.
//
// Refuses to render when the operator-supplied identity (company name,
// contact email, jurisdiction) is missing — shipping a page with
// "[set ZV_COMPANY_NAME]" visible to real users is worse than 503ing
// the route until the operator finishes the configuration. The
// 503 body lists the missing keys so the operator knows what to set.
func renderLegalPage(w http.ResponseWriter, name string, cfg LegalConfig) {
	if missing := cfg.missingRequired(); len(missing) > 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("This page is not yet configured. The operator must set the following environment variables before /privacy and /terms become available:\n\n  " + strings.Join(missing, "\n  ") + "\n\nSee SELFHOSTING.md for guidance on each value.\n"))
		return
	}
	body, err := readWebFile("web/" + name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d := cfg.defaulted()
	out := string(body)
	for _, sub := range [][2]string{
		{"{{COMPANY_NAME}}", d.CompanyName},
		{"{{CONTACT_EMAIL}}", d.ContactEmail},
		{"{{DPO_EMAIL}}", d.DPOEmail},
		{"{{JURISDICTION}}", d.Jurisdiction},
		{"{{LOCATION_RETENTION_DAYS}}", d.LocationRetentionDays},
		{"{{AUDIT_RETENTION_DAYS}}", d.AuditRetentionDays},
		{"{{PARTY_RETENTION_DAYS}}", d.PartyRetentionDays},
		{"{{LAST_UPDATED}}", d.LastUpdated},
		{"{{HOSTING_PROVIDER}}", d.HostingProvider},
		{"{{EMAIL_PROVIDER}}", d.EmailProvider},
		{"{{MAP_PROVIDER}}", html.EscapeString(d.MapProvider)},
		{"{{COMPANY_IDENTITY}}", legalIdentityHTML(d)},
	} {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	// Once an operator confirms their lawyer has signed off on the
	// template (ZV_LEGAL_REVIEWED=1), the yellow "draft template" notes
	// are stripped so paying customers don't see internal guidance.
	// Self-host without the flag keeps the banner — an explicit
	// reminder that the operator hasn't yet had the wording reviewed.
	if d.Reviewed {
		out = stripDraftNotes(out)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// no-store means an operator who fixes a typo in CONTACT_EMAIL
	// sees the change on next reload without the customer's browser
	// holding onto a stale copy.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// stripDraftNotes removes every `<p class="draft-note">…</p>` paragraph
// from the body. Plain string scan rather than a regexp or HTML
// parser — the templates control the markup, the markers are stable,
// and we want this to stay zero-dependency.
func stripDraftNotes(s string) string {
	const open = `<p class="draft-note">`
	const close = `</p>`
	var b strings.Builder
	b.Grow(len(s))
	rest := s
	for {
		i := strings.Index(rest, open)
		if i < 0 {
			b.WriteString(rest)
			break
		}
		j := strings.Index(rest[i:], close)
		if j < 0 {
			// Malformed template (open without close) — leave the
			// remainder untouched rather than truncating.
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		rest = rest[i+j+len(close):]
	}
	return b.String()
}
