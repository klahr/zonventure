package domains

import (
	"embed"
	"strings"
)

//go:embed guides/*.md
var guideFS embed.FS

// Provider is a DNS provider with a step-by-step guide.
type Provider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Providers are those with a guide, in the order the console offers
// them; "other" is the generic one, last.
var Providers = []Provider{
	{"loopia", "Loopia"},
	{"onecom", "One.com"},
	{"binero", "Binero"},
	{"godaddy", "GoDaddy"},
	{"namecheap", "Namecheap"},
	{"cloudflare", "Cloudflare"},
	{"squarespace", "Squarespace"},
	{"other", "Other"},
}

// Guide is provider's steps in lang (Markdown), with the record filled
// in; English when there is none in lang, the generic one for an
// unknown provider.
func Guide(provider, lang, host, value string) string {
	known := false
	for _, p := range Providers {
		if p.ID == provider {
			known = true
		}
	}
	if !known {
		provider = "other"
	}
	b, err := guideFS.ReadFile("guides/" + provider + "." + lang + ".md")
	if err != nil {
		b, err = guideFS.ReadFile("guides/" + provider + ".en.md")
		if err != nil {
			return ""
		}
	}
	return strings.TrimSpace(strings.NewReplacer(
		"{{apex}}", Apex(host),
		"{{name}}", Name(host),
		"{{value}}", value,
	).Replace(string(b)))
}
