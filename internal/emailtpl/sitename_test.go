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

// On its own domain an organisation's email is signed and sent under
// its name alone.
func TestWhiteLabelMessage(t *testing.T) {
	b := Brand{Name: "Acme Adventures", WhiteLabel: true}
	if got := signature(b, "en"); got != "Acme Adventures" {
		t.Errorf("signature = %q", got)
	}
	m := Message([]string{"a@b.test"}, "Your ticket", Classic, b, Content{Lang: "en", Heading: "Hi"})
	if m.FromName != "Acme Adventures" {
		t.Errorf("FromName = %q", m.FromName)
	}
	if m := Message([]string{"a@b.test"}, "Your ticket", Classic, Brand{Name: "Acme"}, Content{Lang: "en", Heading: "Hi"}); m.FromName != "" {
		t.Errorf("without its own domain FromName = %q", m.FromName)
	}
}
