// Package sitename is the name an instance presents itself under:
// page titles, the player app's header, emails, the authenticator
// app entry. Operators set ZV_SITE_NAME so players on their instance
// see their brand rather than the software's.
//
// "Zonventure" stays where text means the software itself — the
// manual's "Zonventure is open source", the update checker, the
// "powered by Zonventure" credit — so those keep the literal name.
package sitename

import (
	"html"
	"os"
	"strings"
)

// Default is the name an instance shows when ZV_SITE_NAME is unset.
const Default = "Zonventure"

// Placeholder is the token embedded HTML templates and landing
// locale strings use for the site name; WordmarkPlaceholder the one
// for the header wordmark.
const (
	Placeholder         = "{{SITE_NAME}}"
	WordmarkPlaceholder = "{{SITE_WORDMARK}}"
)

var name = fromEnv()

func fromEnv() string {
	if v := strings.TrimSpace(os.Getenv("ZV_SITE_NAME")); v != "" {
		return v
	}
	return Default
}

// Name is the instance's display name.
func Name() string { return name }

// Wordmark is the header wordmark: Zonventure's lowercase
// "zonventure" by default, the site name as written otherwise.
func Wordmark() string {
	if name == Default {
		return "zonventure"
	}
	return name
}

// Fill replaces the placeholders in HTML with the HTML-escaped site
// name and wordmark.
func Fill(s string) string {
	if !strings.Contains(s, "{{SITE_") {
		return s
	}
	return strings.NewReplacer(
		Placeholder, html.EscapeString(name),
		WordmarkPlaceholder, html.EscapeString(Wordmark()),
	).Replace(s)
}

// SetForTest overrides the name and returns a func restoring it.
func SetForTest(n string) (restore func()) {
	old := name
	name = n
	return func() { name = old }
}
