package mdx

import (
	"strings"
	"testing"
)

// FuzzRender exercises the markdown→HTML render pipeline against
// arbitrary byte strings. The properties asserted are about safety
// rather than correctness:
//
//   - Render must never panic, regardless of input.
//   - Output must not contain a raw <script>, <iframe>, or javascript:
//     URL, even if the input tries to inject one (the safeURLTransformer
//     and goldmark's default HTML escaping are the load-bearing
//     defences here).
//
// Run with: go test -fuzz=FuzzRender ./internal/mdx
func FuzzRender(f *testing.F) {
	seeds := []string{
		"",
		"hello",
		"# heading",
		"> blockquote",
		"```\ncode\n```",
		"![alt](http://example.com/image.png)",
		"[link](http://example.com)",
		"[xss](javascript:alert(1))",
		"<script>alert(1)</script>",
		"<iframe src='evil'></iframe>",
		"![](javascript:alert(1))",
		"data:text/html,<script>alert(1)</script>",
		strings.Repeat("# ", 1000) + "deeply nested",
		strings.Repeat("\\", 10000),
		"\x00\x01\x02null bytes",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		out := Render(src)
		// XSS regression invariants: the safe-URL transformer + the
		// default goldmark renderer are supposed to neutralise the
		// payloads below. A future change that loosens either should
		// trip these checks during the fuzz run.
		lower := strings.ToLower(out)
		if strings.Contains(lower, "<script") {
			t.Errorf("output contains <script>: src=%q out=%q", src, out)
		}
		if strings.Contains(lower, "<iframe") {
			t.Errorf("output contains <iframe>: src=%q out=%q", src, out)
		}
		// "javascript:" inside text content is harmless (it's just
		// text). The dangerous shape is the scheme inside an attribute
		// — href="javascript:..." / src="javascript:...". Goldmark
		// escapes attribute values, but the safeURLTransformer is the
		// definitive defence; if it ever regresses, an inline link
		// payload would be visible here.
		if containsAttrScheme(lower, "href", "javascript:") ||
			containsAttrScheme(lower, "src", "javascript:") ||
			containsAttrScheme(lower, "href", "data:") ||
			containsAttrScheme(lower, "src", "data:") {
			t.Errorf("output contains attr=dangerous-scheme: src=%q out=%q", src, out)
		}
	})
}

// containsAttrScheme reports whether s contains attr=" or attr=' or
// bare attr= followed by the named scheme. Lower-cased input expected.
func containsAttrScheme(s, attr, scheme string) bool {
	for _, prefix := range []string{
		attr + `="` + scheme,
		attr + `='` + scheme,
		attr + `=` + scheme,
	} {
		if strings.Contains(s, prefix) {
			return true
		}
	}
	return false
}
