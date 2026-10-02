package emailtpl

import (
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/sitename"
)

// TestSiteNameInEmails pins that platform emails name the instance
// (ZV_SITE_NAME), in both languages, and that an org's emails credit
// the instance in their signature.
func TestSiteNameInEmails(t *testing.T) {
	defer sitename.SetForTest("Acme Quests")()
	for _, lang := range Languages {
		subject, c := PasswordReset(lang, "https://x.example/reset")
		if !strings.Contains(subject, "Acme Quests") || !strings.Contains(c.Body, "Acme Quests") {
			t.Errorf("%s: reset email doesn't name the site: %q / %q", lang, subject, c.Body)
		}
		if strings.Contains(subject+c.Body, "Zonventure") {
			t.Errorf("%s: reset email still says Zonventure", lang)
		}
		if got := signature(Platform(), lang); got != "Acme Quests" {
			t.Errorf("%s: platform signature = %q", lang, got)
		}
		if got := signature(Brand{Name: "Org"}, lang); !strings.HasPrefix(got, "Org · ") || !strings.HasSuffix(got, "Acme Quests") {
			t.Errorf("%s: org signature = %q", lang, got)
		}
	}
}
