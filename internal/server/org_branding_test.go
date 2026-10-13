package server

import (
	"strings"
	"testing"
)

// TestOrgBrandStyleBlock pins the CSS-vars emitter the org landing
// + store-event renderers feed into the {{ORG_BRAND_OVERRIDES}}
// placeholder. Every column can be empty (clear → fall back to
// platform default); when ALL are empty the function returns ""
// so the placeholder is a no-op.
func TestOrgBrandStyleBlock(t *testing.T) {
	cases := []struct {
		name                     string
		accent, accent2, bg, ink string
		want                     string
	}{
		{
			name: "all empty produces no override",
			want: "",
		},
		{
			name:   "only accent emits one var",
			accent: "#112233",
			want:   "<style>:root{--accent:#112233;}</style>",
		},
		{
			name:    "all four emit all four vars in stable order",
			accent:  "#aa1111",
			accent2: "#bb2222",
			bg:      "#cccccc",
			ink:     "#222222",
			want:    "<style>:root{--accent:#aa1111;--accent-2:#bb2222;--bg:#cccccc;--ink:#222222;}</style>",
		},
		{
			name: "background only",
			bg:   "#fafafa",
			want: "<style>:root{--bg:#fafafa;}</style>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orgBrandStyleBlock(tc.accent, tc.accent2, tc.bg, tc.ink)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOrgBrandStyleBlock_NoStyleTagWhenAllDefault is a sanity guard
// against the override leaking an empty `<style>:root{}</style>` —
// that's still valid CSS, but it bloats the page chrome for every
// render and a regex-watchful operator would notice. The output
// must be exactly "" when no overrides are set.
func TestOrgBrandStyleBlock_NoStyleTagWhenAllDefault(t *testing.T) {
	got := orgBrandStyleBlock("", "", "", "")
	if got != "" {
		t.Errorf("expected empty string for all-default, got %q", got)
	}
	if strings.Contains(got, "<style>") {
		t.Errorf("default override leaked a <style> tag: %q", got)
	}
}

// TestHeroBgStyle_UsesOrgColours pins the /o/{slug} hero gradient to
// the org's own accents, falling back to the platform magenta.
func TestHeroBgStyle_UsesOrgColours(t *testing.T) {
	p := orgProfile{ID: 6, HasCover: true}
	p.Accent.String, p.Accent2.String = "#1f6b3a", "#c90"
	got := p.heroBgStyle()
	if !strings.Contains(got, "linear-gradient(135deg, rgba(31,107,58,.6), rgba(204,153,0,.5))") {
		t.Errorf("org colours missing: %q", got)
	}
	if got := (orgProfile{ID: 6, HasCover: true}).heroBgStyle(); !strings.Contains(got, "rgba(173,19,87,.6), rgba(226,64,128,.5)") {
		t.Errorf("default colours missing: %q", got)
	}
}
