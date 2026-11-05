package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/social"
)

func TestFooterSocial(t *testing.T) {
	if got := footerSocial(social.Links{}); got != "" {
		t.Fatalf("no links: got %q", got)
	}
	got := footerSocial(social.Links{"linkedin": "https://www.linkedin.com/company/zv?a=1&b=2", "instagram": "https://www.instagram.com/zv"})
	if !strings.Contains(got, `href="https://www.linkedin.com/company/zv?a=1&amp;b=2"`) || !strings.Contains(got, `aria-label="Instagram"`) {
		t.Fatalf("got %q", got)
	}
	if strings.Index(got, "Instagram") > strings.Index(got, "LinkedIn") {
		t.Fatalf("not in network order: %q", got)
	}
}

// An organisation's page shows its own social links in its footer.
func TestRenderOrgPage_Social(t *testing.T) {
	req := httptest.NewRequest("GET", "/o/acme?lang=en", nil)
	rec := httptest.NewRecorder()
	renderOrgPage(rec, req, orgProfile{Slug: "acme", Name: "Acme", Social: social.Links{"linkedin": "https://www.linkedin.com/company/acme"}})
	if body := rec.Body.String(); !strings.Contains(body, `<a href="https://www.linkedin.com/company/acme" aria-label="LinkedIn"`) {
		t.Fatalf("no LinkedIn link in the footer")
	}
}
