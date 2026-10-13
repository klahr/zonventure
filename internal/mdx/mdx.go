// Package mdx renders arranger-provided markdown to sanitised HTML.
//
// Authors type plain CommonMark — bold, italics, lists, links, inline
// code, etc. — and the server converts it once on egress for every
// player-visible field. The renderer never passes through raw HTML
// from the source, so a puzzle prompt containing "<script>alert(1)</script>"
// is rendered as the visible text "<script>alert(1)</script>" rather
// than an executable tag. That's goldmark's default behaviour: the
// WithUnsafe() option would re-enable pass-through, and we pointedly
// don't use it.
package mdx

import (
	"bytes"
	"html"
	"strconv"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	initOnce sync.Once
	md       goldmark.Markdown
)

// safeURLSchemes is the allowlist applied to every Link / Image
// Destination before rendering. goldmark's default renderer happily
// emits `<a href="javascript:…">`, which combined with a CSP that
// allows inline scripts becomes a one-click XSS in any rendered-
// markdown surface (catalog prompts, intro/outro, hints, the
// changelog notes_html on the Software-update tab). Anything not on
// this list — or any non-relative URL with a scheme we don't know —
// is rewritten to "#" by safeURLTransformer below.
var safeURLSchemes = map[string]struct{}{
	"http":   {},
	"https":  {},
	"mailto": {},
	"tel":    {},
}

// hrefIsSafe reports whether s is a relative URL or carries a scheme
// we trust. Empty / whitespace-only strings count as unsafe so a stray
// `[text]()` doesn't render an anchor that goes nowhere visibly but
// looks like a styled link.
func hrefIsSafe(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Hit a path / query / fragment delimiter before any colon →
		// the URL is relative and inherits the document origin.
		if c == '/' || c == '?' || c == '#' {
			return true
		}
		if c == ':' {
			_, ok := safeURLSchemes[strings.ToLower(s[:i])]
			return ok
		}
	}
	// No scheme separator and no path delimiter → bare token, treat as
	// a relative path (e.g. "foo.html").
	return true
}

// safeURLTransformer walks the parsed AST and neuters any Link /
// Image whose Destination uses a scheme outside safeURLSchemes.
// Anchors keep their visible text; the href is replaced with "#".
type safeURLTransformer struct{}

func (safeURLTransformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Link:
			if !hrefIsSafe(string(v.Destination)) {
				v.Destination = []byte("#")
			}
		case *ast.Image:
			if !hrefIsSafe(string(v.Destination)) {
				v.Destination = []byte("#")
			}
		case *ast.AutoLink:
			// Autolinks (`<https://example.com>`) only emit the URL the
			// parser already extracted. CommonMark restricts autolink
			// schemes to a non-dangerous set, but check anyway in case
			// a future extension widens it.
			if !hrefIsSafe(string(v.URL([]byte{}))) {
				// AutoLink doesn't expose a settable destination; the
				// safest neuter is to demote the node to plain text.
				if p := v.Parent(); p != nil {
					p.ReplaceChild(p, v, ast.NewString(v.URL([]byte{})))
				}
			}
		}
		return ast.WalkContinue, nil
	})
}

// MaxRenderBytes caps the source we'll feed to goldmark. The parser
// is fast on well-formed input but a pathological 10 MB file (deeply
// nested emphasis, mismatched delimiters) can pin a goroutine on
// quadratic behaviour. 64 KB is two orders of magnitude larger than
// the longest puzzle prompt anyone has ever written.
const MaxRenderBytes = 64 * 1024

// Render converts markdown src to HTML. Empty input returns empty
// output (handy so callers can flow the field through unchanged when
// the arranger left it blank). Sources over MaxRenderBytes are
// truncated with a marker so we never feed goldmark an unbounded
// input. On the unlikely chance goldmark fails (or panics — fuzzing
// has surfaced inputs like `0<0@0>` that crash inside the AutoLink
// parser), the raw source is returned escaped via html.EscapeString
// so the output stays XSS-safe even on the failure path.
func Render(src string) string {
	if src == "" {
		return ""
	}
	if len(src) > MaxRenderBytes {
		src = src[:MaxRenderBytes] + "\n\n_(content truncated — exceeded render limit)_"
	}
	initOnce.Do(setup)
	return renderWithRecover(src)
}

// setup builds the shared goldmark instance.
func setup() {
	// Default renderer: no WithUnsafe(), so inline or block HTML in
	// the source gets escaped rather than passed through. The Table
	// extension enables GFM-style pipe tables — "| a | b |" rows
	// with a "| --- | --- |" separator — which are what most
	// authors reach for when they think "markdown tables".
	md = goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(safeURLTransformer{}, 999),
			),
		),
	)
}

// renderWithRecover wraps the Convert call in a defer/recover so a
// panic from goldmark (slice-out-of-range on malformed autolinks,
// etc.) doesn't crash the request handler. On panic we fall back to
// the HTML-escaped raw source so the output is always safe.
func renderWithRecover(src string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = html.EscapeString(src)
		}
	}()
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return html.EscapeString(src)
	}
	return buf.String()
}

// Text converts markdown src to plain text, for the text part of an
// email: emphasis is dropped, list items keep a "- " or "1. " marker,
// and a link reads as its text followed by its address in brackets
// (just the address when the two are the same). On a goldmark failure
// the source comes back as it is.
func Text(src string) (out string) {
	if src == "" {
		return ""
	}
	if len(src) > MaxRenderBytes {
		src = src[:MaxRenderBytes]
	}
	initOnce.Do(setup)
	defer func() {
		if r := recover(); r != nil {
			out = src
		}
	}()
	source := []byte(src)
	var b strings.Builder
	textBlocks(&b, md.Parser().Parse(text.NewReader(source)), source, "")
	return strings.TrimSpace(b.String())
}

// textBlocks writes n's block children, each line led by prefix, with a
// blank line between blocks (none inside the items of a tight list).
func textBlocks(b *strings.Builder, n ast.Node, src []byte, prefix string) {
	tight := false
	if _, ok := n.(*ast.ListItem); ok {
		if l, ok := n.Parent().(*ast.List); ok {
			tight = l.IsTight
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.PreviousSibling() != nil && !tight {
			b.WriteString(strings.TrimRight(prefix, " ") + "\n")
		}
		switch v := c.(type) {
		case *ast.List:
			i := v.Start
			for item := v.FirstChild(); item != nil; item = item.NextSibling() {
				if item.PreviousSibling() != nil && !v.IsTight {
					b.WriteString(strings.TrimRight(prefix, " ") + "\n")
				}
				marker := "- "
				if v.IsOrdered() {
					marker = strconv.Itoa(i) + ". "
					i++
				}
				var inner strings.Builder
				textBlocks(&inner, item, src, "")
				for j, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
					if j > 0 {
						marker = strings.Repeat(" ", len(marker))
					}
					b.WriteString(prefix + marker + line + "\n")
				}
			}
		case *ast.Blockquote:
			textBlocks(b, v, src, prefix+"> ")
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := c.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				b.WriteString(prefix + strings.TrimRight(string(seg.Value(src)), "\n") + "\n")
			}
		case *ast.ThematicBreak:
			b.WriteString(prefix + "---\n")
		default:
			var inner strings.Builder
			textInline(&inner, c, src)
			for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
				b.WriteString(prefix + line + "\n")
			}
		}
	}
}

// textInline writes n's inline content as plain text; a table's rows
// come out one per line, cells split by " | ".
func textInline(b *strings.Builder, n ast.Node, src []byte) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			b.Write(util.ResolveNumericReferences(util.ResolveEntityNames(util.UnescapePunctuations(v.Value(src)))))
			if v.SoftLineBreak() || v.HardLineBreak() {
				b.WriteString("\n")
			}
		case *ast.String:
			b.Write(v.Value)
		case *ast.CodeSpan:
			for t := v.FirstChild(); t != nil; t = t.NextSibling() {
				if s, ok := t.(*ast.Text); ok {
					b.Write(s.Value(src))
				}
			}
		case *ast.AutoLink:
			b.Write(v.URL(src))
		case *ast.Link:
			var label strings.Builder
			textInline(&label, v, src)
			dest := string(v.Destination)
			b.WriteString(label.String())
			if dest != "#" && label.String() != dest {
				b.WriteString(" (" + dest + ")")
			}
		case *ast.RawHTML:
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				b.Write(seg.Value(src))
			}
		default:
			if c.Kind() == extast.KindTableCell && c.PreviousSibling() != nil {
				b.WriteString(" | ")
			} else if c.Type() == ast.TypeBlock && c.PreviousSibling() != nil {
				b.WriteString("\n")
			}
			textInline(b, c, src)
		}
	}
}
