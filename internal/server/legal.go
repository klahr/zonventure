package server

import (
	"net/http"
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
	LastUpdated           string // YYYY-MM-DD; defaults to today on first render
	HostingProvider       string // sub-processor name shown in privacy policy
	EmailProvider         string // sub-processor name shown in privacy policy
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
	if d.LastUpdated == "" {
		d.LastUpdated = time.Now().UTC().Format("2006-01-02")
	}
	if d.HostingProvider == "" {
		d.HostingProvider = "[set ZV_HOSTING_PROVIDER]"
	}
	if d.EmailProvider == "" {
		d.EmailProvider = "[set ZV_EMAIL_PROVIDER]"
	}
	return d
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
		{"{{LAST_UPDATED}}", d.LastUpdated},
		{"{{HOSTING_PROVIDER}}", d.HostingProvider},
		{"{{EMAIL_PROVIDER}}", d.EmailProvider},
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
