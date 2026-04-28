package server

import (
	"net/http"
	"strings"
	"time"
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
func renderLegalPage(w http.ResponseWriter, name string, cfg LegalConfig) {
	body, err := webFS.ReadFile("web/" + name)
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
