package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderLandingPage_NoLeftoverPlaceholders renders both locales
// against the embedded template and asserts no `{{KEY}}` substring
// survives. A leftover means either a missing struct field or a typo
// in the substitution map — both are silent in production until a
// user notices the placeholder text on the live page.
func TestRenderLandingPage_NoLeftoverPlaceholders(t *testing.T) {
	for _, lang := range []string{"en", "sv"} {
		t.Run(lang, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?lang="+lang, nil)
			rec := httptest.NewRecorder()
			renderLandingPage(rec, req, "https://zv.example.com", "", "")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if strings.Contains(body, "{{") {
				// Print the first leftover for debugging.
				idx := strings.Index(body, "{{")
				end := idx + 50
				if end > len(body) {
					end = len(body)
				}
				t.Fatalf("leftover placeholder at %d: %q", idx, body[idx:end])
			}
			// `lang` attribute on <html> reflects the request locale.
			if !strings.Contains(body, `<html lang="`+lang+`"`) {
				t.Errorf("missing or wrong lang attribute for %q", lang)
			}
			// Cookie was set so a subsequent request without `?lang=`
			// gets the same locale.
			ck := rec.Result().Cookies()
			var found bool
			for _, c := range ck {
				if c.Name == langCookie && c.Value == lang {
					found = true
				}
			}
			if !found {
				t.Errorf("expected %s cookie set to %q", langCookie, lang)
			}
		})
	}
}

func TestRenderTopbarPage_NoLeftoverPlaceholders(t *testing.T) {
	// Pricing + manual pages share the topbar via renderTopbarPage.
	// Their bodies aren't translated, but the topbar chrome is — verify
	// no `{{KEY}}` leaks survive after the render for either locale.
	pages := []string{"web/pricing.html", "web/manual.html"}
	langs := []string{"en", "sv"}
	for _, p := range pages {
		for _, lang := range langs {
			t.Run(p+"/"+lang, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/?lang="+lang, nil)
				rec := httptest.NewRecorder()
				renderTopbarPage(rec, req, p)

				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				body := rec.Body.String()
				if strings.Contains(body, "{{") {
					idx := strings.Index(body, "{{")
					end := idx + 50
					if end > len(body) {
						end = len(body)
					}
					t.Fatalf("leftover placeholder at %d: %q", idx, body[idx:end])
				}
				// Active picker class should reflect the request locale.
				if lang == "sv" && !strings.Contains(body, `class="lang-link active"`) {
					t.Errorf("expected sv-active class on lang-link")
				}
			})
		}
	}
}

// TestRenderOrgPage_NoLeftoverPlaceholders covers the /o/{slug}
// template substitution path. Topbar chrome, the store copy and the
// organisation's own fields are merged in renderOrgPage, so any
// unmapped {{KEY}} surfaces as a test failure.
func TestRenderOrgPage_NoLeftoverPlaceholders(t *testing.T) {
	for _, lang := range []string{"en", "sv"} {
		t.Run(lang, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/o/acme?lang="+lang, nil)
			rec := httptest.NewRecorder()
			renderOrgPage(rec, req, orgProfile{Slug: "acme", Name: "Acme"})

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, "{{") {
				idx := strings.Index(body, "{{")
				end := idx + 50
				if end > len(body) {
					end = len(body)
				}
				t.Fatalf("leftover placeholder at %d: %q", idx, body[idx:end])
			}
		})
	}
}

func TestResolveLang_Fallbacks(t *testing.T) {
	cases := []struct {
		name           string
		query          string
		cookie         string
		acceptLanguage string
		want           string
	}{
		{"explicit query wins", "sv", "en", "fr-FR", "sv"},
		{"unknown query → next gate", "fr", "sv", "", "sv"},
		{"cookie wins over header", "", "sv", "en", "sv"},
		{"accept-language sv-SE", "", "", "sv-SE,sv;q=0.9,en;q=0.8", "sv"},
		{"accept-language unknown", "", "", "fr-FR,fr;q=0.9", "en"},
		{"no signal", "", "", "", "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/"
			if tc.query != "" {
				url = "/?lang=" + tc.query
			}
			req := httptest.NewRequest("GET", url, nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: langCookie, Value: tc.cookie})
			}
			if tc.acceptLanguage != "" {
				req.Header.Set("Accept-Language", tc.acceptLanguage)
			}
			rec := httptest.NewRecorder()
			got := resolveLang(rec, req)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRenderLandingPage_SchemaOrganization pins that the JSON-LD names
// the operator from ZV_COMPANY_NAME and omits the Organization node
// when it's unset — the generic build must not claim a publisher.
func TestRenderLandingPage_SchemaOrganization(t *testing.T) {
	for _, tc := range []struct {
		company string
		wantOrg bool
	}{
		{"", false},
		{`Acme "Quests" & Co <AB>`, true},
	} {
		t.Run(tc.company, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?lang=en", nil)
			rec := httptest.NewRecorder()
			renderLandingPage(rec, req, "https://zv.example.com", tc.company, "")
			body := rec.Body.String()
			start := strings.Index(body, `<script type="application/ld+json">`)
			if start < 0 {
				t.Fatal("no JSON-LD block")
			}
			start += len(`<script type="application/ld+json">`)
			end := strings.Index(body[start:], "</script>")
			var ld struct {
				Graph []map[string]string `json:"@graph"`
			}
			if err := json.Unmarshal([]byte(body[start:start+end]), &ld); err != nil {
				t.Fatalf("JSON-LD does not parse: %v", err)
			}
			var org map[string]string
			for _, n := range ld.Graph {
				if n["@type"] == "Organization" {
					org = n
				}
			}
			if (org != nil) != tc.wantOrg {
				t.Fatalf("Organization node present = %v, want %v", org != nil, tc.wantOrg)
			}
			if org != nil && org["name"] != tc.company {
				t.Errorf("name = %q, want %q", org["name"], tc.company)
			}
		})
	}
}

// With the database down, a page request gets the error card (503,
// HTML, in the visitor's language) and an API call the JSON error.
func TestLookupFailed_PageOrJSON(t *testing.T) {
	h := lookupFailed("session lookup failed")
	req := httptest.NewRequest("GET", "/arrange?lang=sv", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusServiceUnavailable || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(body, landingLocales["sv"].UnavailableHeading) || strings.Contains(body, "{{") {
		t.Fatalf("page: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	req = httptest.NewRequest("GET", "/api/auth/whoami", nil)
	req.Header.Set("Accept", "text/html")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"session lookup failed"`) {
		t.Fatalf("api: %d %s", rec.Code, rec.Body)
	}
}
