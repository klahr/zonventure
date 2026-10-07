package server

import (
	"net/http"
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

// An unconfigured operator (no company name, contact email or
// jurisdiction under Settings → Legal) must not show placeholder text on
// /privacy and /terms to a real customer. The page 503s with what is
// missing and where to fill it in.
func TestRenderLegalPage_RefusesWhenUnconfigured(t *testing.T) {
	rr := httptest.NewRecorder()
	renderLegalPage(rr, "privacy.html", LegalConfig{})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"Settings → Legal",
		"company name",
		"contact email",
		"jurisdiction",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("503 body missing hint %q", want)
		}
	}
}

// The yellow "Draft template" boxes stay in place by default — this is
// the self-host safeguard so an operator who hasn't had the wording
// reviewed sees the warning every time they load the page.
func TestRenderLegalPage_DraftBannerByDefault(t *testing.T) {
	for _, name := range []string{"privacy.html", "terms.html"} {
		rr := httptest.NewRecorder()
		// Required identity fields supplied; Reviewed left false so
		// the draft banner SHOULD remain in the output.
		renderLegalPage(rr, name, LegalConfig{
			CompanyName:  "Acme",
			ContactEmail: "ops@example.com",
			Jurisdiction: "Stockholm, Sweden",
		})
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
			CompanyName:  "Acme",
			ContactEmail: "ops@example.com",
			Jurisdiction: "Stockholm, Sweden",
			Reviewed:     true,
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
	renderLegalPage(rr, "terms.html", LegalConfig{
		CompanyName:  "Acme",
		ContactEmail: "ops@example.com",
		Jurisdiction: "Stockholm, Sweden",
	})
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

// The seller's registration data shows on both legal pages when set,
// and leaves no trace when it isn't.
func TestRenderLegalPage_CompanyIdentity(t *testing.T) {
	base := LegalConfig{CompanyName: "Zonventure AB", ContactEmail: "hello@zonventure.example", Jurisdiction: "Sweden"}
	full := base
	full.CompanyOrgNumber = "556677-8899"
	full.CompanyVATNumber = "SE556677889901"
	full.CompanyAddress = "Storgatan 1 & 3, 111 22 Stockholm"
	for _, page := range []string{"privacy.html", "terms.html"} {
		rr := httptest.NewRecorder()
		renderLegalPage(rr, page, full)
		body := rr.Body.String()
		for _, want := range []string{"Org. no. 556677-8899", "VAT no. SE556677889901", "Storgatan 1 &amp; 3, 111 22 Stockholm"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", page, want)
			}
		}
		rr = httptest.NewRecorder()
		renderLegalPage(rr, page, base)
		body = rr.Body.String()
		if strings.Contains(body, "{{COMPANY_IDENTITY}}") || strings.Contains(body, "Org. no.") || strings.Contains(body, "VAT no.") {
			t.Errorf("%s: identity placeholder or empty labels left behind", page)
		}
	}
}

// /privacy names the map tile host: OpenStreetMap for the default
// tiles, the tile URL's host for a custom one, the operator's name
// when given.
func TestRenderLegalPage_MapProvider(t *testing.T) {
	base := LegalConfig{CompanyName: "Zonventure AB", ContactEmail: "hello@zonventure.example", Jurisdiction: "Sweden"}
	custom := base
	custom.MapTileURL = "https://tiles.example.net/{z}/{x}/{y}.png"
	named := custom
	named.MapProvider = "Example Maps GmbH (Germany)"
	for _, tc := range []struct {
		cfg  LegalConfig
		want string
	}{
		{base, "OpenStreetMap Foundation"},
		{custom, "tiles.example.net"},
		{named, "Example Maps GmbH (Germany)"},
	} {
		rr := httptest.NewRecorder()
		renderLegalPage(rr, "privacy.html", tc.cfg)
		body := rr.Body.String()
		if !strings.Contains(body, tc.want) || strings.Contains(body, "{{MAP_PROVIDER}}") {
			t.Errorf("privacy page for %+v: want map provider %q", tc.cfg.MapTileURL, tc.want)
		}
	}
}

func TestMapConfigJS(t *testing.T) {
	got := string(mapConfigJS(LegalConfig{}))
	if !strings.Contains(got, `"tile_url":"https://tile.openstreetmap.org/{z}/{x}/{y}.png"`) || strings.Contains(got, "{s}") || !strings.Contains(got, "OpenStreetMap") {
		t.Errorf("default map config = %s", got)
	}
	got = string(mapConfigJS(LegalConfig{MapTileURL: "https://tiles.example.net/{z}/{x}/{y}.png", MapTileAttribution: "© Example"}))
	if !strings.Contains(got, `"tile_url":"https://tiles.example.net/{z}/{x}/{y}.png"`) || !strings.Contains(got, `"attribution":"© Example"`) {
		t.Errorf("custom map config = %s", got)
	}
}

func TestSpliceSellerIdentity(t *testing.T) {
	cfg := LegalConfig{CompanyName: "Zonventure AB", ContactEmail: "hello@zonventure.example", CompanyOrgNumber: "556677-8899", CompanyVATNumber: "SE556677889901", CompanyAddress: "Storgatan 1, Stockholm"}
	page := []byte(`<html lang="sv"><body><form><!--ZV_SELLER_SOLD_BY--></form><footer><!--ZV_SELLER--></footer></body></html>`)
	got := string(spliceSellerIdentity(page, cfg))
	for _, want := range []string{
		`<p class="seller">Säljare: Zonventure AB · Org.nr 556677-8899 · Momsreg.nr SE556677889901 · Storgatan 1, Stockholm · <a href="mailto:hello@zonventure.example">hello@zonventure.example</a></p></form>`,
		`<footer><p class="seller">Zonventure AB · Org.nr`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	got = string(spliceSellerIdentity([]byte(`<html lang="en"><footer><!--ZV_SELLER--></footer>`), LegalConfig{}))
	if got != `<html lang="en"><footer></footer>` {
		t.Errorf("unconfigured seller: got %s", got)
	}
}
