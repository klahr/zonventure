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
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var (
	initOnce sync.Once
	md       goldmark.Markdown
)

// Render converts markdown src to HTML. Empty input returns empty
// output (handy so callers can flow the field through unchanged when
// the arranger left it blank). On the unlikely chance goldmark fails,
// the raw source is returned — that's safer than nothing at all, and
// the default renderer escapes any stray HTML so the output is still
// XSS-safe.
func Render(src string) string {
	if src == "" {
		return ""
	}
	initOnce.Do(func() {
		// Default renderer: no WithUnsafe(), so inline or block HTML in
		// the source gets escaped rather than passed through. The Table
		// extension enables GFM-style pipe tables — "| a | b |" rows
		// with a "| --- | --- |" separator — which are what most
		// authors reach for when they think "markdown tables".
		md = goldmark.New(
			goldmark.WithExtensions(extension.Table),
		)
	})
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return src
	}
	return buf.String()
}
