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

// TestText checks the plain-text form an email's text part gets: no
// Markdown syntax left, links readable, list markers kept.
func TestText(t *testing.T) {
	cases := []struct{ src, want string }{
		{"Ditt lag **Team \\*1** är redo.\n\n- **Anna**: [https://x.test/a](https://x.test/a)\n- **Bo**: [hem](https://x.test/b)\n\nKlart.",
			"Ditt lag Team *1 är redo.\n\n- Anna: https://x.test/a\n- Bo: hem (https://x.test/b)\n\nKlart."},
		{"## Rubrik\n\n1. *ett*\n2. `två`\n\n> citat\n> rad", "Rubrik\n\n1. ett\n2. två\n\n> citat\n> rad"},
		{"a &lt; b &amp; c", "a < b & c"},
		{"| a | b |\n| --- | --- |\n| 1 | 2 |", "a | b\n1 | 2"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Text(c.src); got != c.want {
			t.Errorf("Text(%q) =\n%q\nwant\n%q", c.src, got, c.want)
		}
	}
}
