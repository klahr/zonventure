package public

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStoreLang(t *testing.T) {
	for _, c := range []struct {
		name, query, cookie, accept, want string
	}{
		{"query wins", "?lang=sv", "en", "de-DE", "sv"},
		{"cookie next", "", "sv", "de-DE", "sv"},
		{"browser last", "", "", "sv-SE,en;q=0.8", "sv"},
		{"region dropped", "?lang=de-AT", "", "", "de"},
		{"none", "", "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/items"+c.query, nil)
			if c.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "zv_lang", Value: c.cookie})
			}
			if c.accept != "" {
				r.Header.Set("Accept-Language", c.accept)
			}
			if got := storeLang(r); got != c.want {
				t.Errorf("storeLang = %q, want %q", got, c.want)
			}
		})
	}
}
