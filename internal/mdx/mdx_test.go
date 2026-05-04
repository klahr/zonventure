package mdx

import (
	"strings"
	"testing"
)

// TestSafeURLs confirms unsafe URL schemes are neutered before
// rendering. The CSP elsewhere in the app allows inline scripts, so
// a `[click](javascript:…)` link in any author surface (catalog
// prompts, intro/outro, hints, changelog) would be a one-click XSS
// in the player or arranger session if this filter regressed.
func TestSafeURLs(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		mustContain string
		mustNot     string
	}{
		{
			name:        "javascript link is neutered",
			src:         "[click me](javascript:alert(1))",
			mustContain: `href="#"`,
			mustNot:     "javascript",
		},
		{
			name:        "vbscript link is neutered",
			src:         "[old](vbscript:msgbox)",
			mustContain: `href="#"`,
			mustNot:     "vbscript",
		},
		{
			name:        "data url image is neutered",
			src:         "![x](data:text/html,<script>alert(1)</script>)",
			mustContain: `src="#"`,
			mustNot:     "data:",
		},
		{
			name:        "https link is preserved",
			src:         "[ok](https://example.com/foo)",
			mustContain: `href="https://example.com/foo"`,
		},
		{
			name:        "mailto link is preserved",
			src:         "[mail](mailto:bob@example.com)",
			mustContain: `href="mailto:bob@example.com"`,
		},
		{
			name:        "relative link is preserved",
			src:         "[home](/dashboard)",
			mustContain: `href="/dashboard"`,
		},
		{
			name:        "fragment link is preserved",
			src:         "[anchor](#section)",
			mustContain: `href="#section"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Render(tc.src)
			if tc.mustContain != "" && !strings.Contains(got, tc.mustContain) {
				t.Errorf("missing %q in output:\n%s", tc.mustContain, got)
			}
			if tc.mustNot != "" && strings.Contains(got, tc.mustNot) {
				t.Errorf("unexpected %q in output:\n%s", tc.mustNot, got)
			}
		})
	}
}

// TestRawHTMLEscaped re-asserts goldmark's default no-WithUnsafe
// posture: inline HTML in source is escaped, never passed through.
func TestRawHTMLEscaped(t *testing.T) {
	got := Render(`<script>alert(1)</script>`)
	if strings.Contains(got, "<script>") {
		t.Fatalf("raw <script> leaked into output: %s", got)
	}
}
