package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Renders /privacy with a fully populated config and asserts the
// placeholders disappear and the substituted values appear.
func TestRenderLegalPage_PrivacySubstitutesPlaceholders(t *testing.T) {
	rr := httptest.NewRecorder()
	renderLegalPage(rr, "privacy.html", LegalConfig{
		CompanyName:           "Zonventure AB",
		ContactEmail:          "hello@zonventure.example",
		DPOEmail:              "dpo@zonventure.example",
		Jurisdiction:          "Sweden",
		LocationRetentionDays: "30",
		AuditRetentionDays:    "90",
		HostingProvider:       "Hetzner Online GmbH (Germany)",
		EmailProvider:         "Postmark (ActiveCampaign LLC, USA)",
	})
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()

	for _, marker := range []string{
		"{{COMPANY_NAME}}", "{{CONTACT_EMAIL}}", "{{DPO_EMAIL}}",
		"{{JURISDICTION}}", "{{LOCATION_RETENTION_DAYS}}",
		"{{AUDIT_RETENTION_DAYS}}", "{{LAST_UPDATED}}",
		"{{HOSTING_PROVIDER}}", "{{EMAIL_PROVIDER}}",
		"[set ZV_",
	} {
		if strings.Contains(body, marker) {
			t.Errorf("body still contains %q — placeholder not substituted", marker)
		}
	}
	for _, want := range []string{
		"Zonventure AB",
		"hello@zonventure.example",
		"Hetzner Online GmbH (Germany)",
		"Postmark (ActiveCampaign LLC, USA)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

// Defaults backfill the loud "[set ZV_*]" markers when the operator
// hasn't configured the legal env vars, so an unconfigured deployment
// is visibly broken rather than quietly serving placeholder text.
func TestRenderLegalPage_DefaultsAreVisible(t *testing.T) {
	rr := httptest.NewRecorder()
	renderLegalPage(rr, "privacy.html", LegalConfig{})

	body := rr.Body.String()
	for _, want := range []string{
		"[set ZV_COMPANY_NAME]",
		"[set ZV_CONTACT_EMAIL]",
		"[set ZV_HOSTING_PROVIDER]",
		"[set ZV_EMAIL_PROVIDER]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("default marker %q missing from rendered body", want)
		}
	}
}

// The yellow "Draft template" boxes stay in place by default — this is
// the self-host safeguard so an operator who hasn't had the wording
// reviewed sees the warning every time they load the page.
func TestRenderLegalPage_DraftBannerByDefault(t *testing.T) {
	for _, name := range []string{"privacy.html", "terms.html"} {
		rr := httptest.NewRecorder()
		renderLegalPage(rr, name, LegalConfig{CompanyName: "Acme"})
		if !strings.Contains(rr.Body.String(), `class="draft-note"`) {
			t.Errorf("%s: expected draft-note paragraph in default render", name)
		}
	}
}

// Once the operator confirms a lawyer review with ZV_LEGAL_REVIEWED=1,
// the draft-note paragraphs are stripped so customers don't see internal
// guidance ("Have this reviewed by a lawyer before publishing").
func TestRenderLegalPage_ReviewedStripsDraftBanners(t *testing.T) {
	for _, name := range []string{"privacy.html", "terms.html"} {
		rr := httptest.NewRecorder()
		renderLegalPage(rr, name, LegalConfig{
			CompanyName: "Acme",
			Reviewed:    true,
		})
		body := rr.Body.String()
		if strings.Contains(body, `class="draft-note"`) {
			t.Errorf("%s: draft-note paragraph still present after Reviewed=true", name)
		}
		if strings.Contains(body, "reviewed by a lawyer") {
			t.Errorf("%s: leaked draft copy after Reviewed=true", name)
		}
	}
}

// Spot-checks the rewritten copyright + right-of-withdrawal sections so a
// future edit that accidentally drops them gets caught here rather than
// at customer time.
func TestRenderLegalPage_TermsCarriesEUClauses(t *testing.T) {
	rr := httptest.NewRecorder()
	renderLegalPage(rr, "terms.html", LegalConfig{CompanyName: "Acme"})
	body := rr.Body.String()
	for _, want := range []string{
		"Digital Services Act",
		"Digital Millennium Copyright Act",
		"Right of withdrawal",
		"2011/83/EU",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("terms.html missing %q", want)
		}
	}
}

func TestStripDraftNotes_PreservesSurroundingMarkup(t *testing.T) {
	in := `before<p class="draft-note">drop me</p>middle<p class="draft-note">also me</p>after`
	got := stripDraftNotes(in)
	want := `beforemiddleafter`
	if got != want {
		t.Errorf("stripDraftNotes = %q, want %q", got, want)
	}
}

func TestStripDraftNotes_LeavesBodyAloneWhenNoMarkers(t *testing.T) {
	in := `<p>regular paragraph</p><h2>heading</h2>`
	if got := stripDraftNotes(in); got != in {
		t.Errorf("stripDraftNotes unexpectedly modified input: %q", got)
	}
}
