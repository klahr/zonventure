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
	CompanyName            string
	ContactEmail           string
	DPOEmail               string
	Jurisdiction           string
	LocationRetentionDays  string // strings so the env passes through unchanged
	AuditRetentionDays     string
	LastUpdated            string // YYYY-MM-DD; defaults to today on first render
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
	} {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// no-store means an operator who fixes a typo in CONTACT_EMAIL
	// sees the change on next reload without the customer's browser
	// holding onto a stale copy.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}
