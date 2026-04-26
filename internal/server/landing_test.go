package server

import (
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
			renderLandingPage(rec, req)

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
