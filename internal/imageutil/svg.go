// Package-level: SVG rasterisation, used by the org-logo upload path
// so vector marks can be accepted alongside PNG/JPEG/WebP without
// exposing the stored-XSS surface that serving the SVG bytes
// directly would carry.
//
// The render is the security boundary: oksvg parses the XML, walks
// only the drawing-primitive subtree (paths, shapes, gradients), and
// emits raster pixels. `<script>`, `<foreignObject>`, event-handler
// attributes, and externally-referencing `<image href>` never reach
// the resulting bitmap because oksvg's renderer doesn't implement
// them — by design. The bytes we return are a freshly-encoded PNG
// that no longer carries any of the original SVG's source.

package imageutil

import (
	"bytes"
	"encoding/xml"
	"errors"
	"html"
	"image"
	"image/png"
	"io"
	"math"
	"regexp"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// RasterizeSVG parses an SVG document and renders it onto a square
// dim × dim RGBA bitmap, returning the PNG-encoded result and
// "image/png". The bitmap preserves the source's aspect ratio
// (letterboxed transparently on the shorter axis); SVGs without a
// viewBox or intrinsic dimensions render edge-to-edge.
func RasterizeSVG(data []byte, dim int) ([]byte, string, error) {
	if dim <= 0 {
		return nil, "", errors.New("dim must be > 0")
	}
	if len(data) == 0 {
		return nil, "", errors.New("empty svg")
	}
	// Pre-validate: oksvg's parser is lenient and returns an empty
	// icon for non-SVG input (which then renders as a transparent
	// canvas — silently wrong). Walk the document with encoding/xml
	// until we find the root element and confirm it's <svg>. Cheap,
	// strict, and rejects anything that wouldn't have rendered
	// usefully anyway.
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var foundSVG bool
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", errors.New("malformed svg: " + err.Error())
		}
		if se, ok := tok.(xml.StartElement); ok {
			foundSVG = strings.EqualFold(se.Name.Local, "svg")
			break
		}
	}
	if !foundSVG {
		return nil, "", errors.New("not an svg document")
	}
	// Inline-style → attribute pre-pass. Adobe Illustrator (and most
	// vector tools) write gradient stop colours and shape fills as
	// `style="stop-color:#xxx"` rather than `stop-color="#xxx"`, but
	// oksvg only reads the attribute form. Without this pass every
	// gradient stop is treated as colour-less and the logo renders
	// as a flat black silhouette. See expandStyleAttrs for the rules.
	data = expandStyleAttrs(data)
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	// Compute a target rect that preserves aspect ratio inside the
	// dim × dim square. ViewBox dimensions are 0 when the source
	// uses width/height attributes only — fall back to those, and
	// finally to a square target if neither is present.
	srcW := icon.ViewBox.W
	srcH := icon.ViewBox.H
	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = float64(dim), float64(dim)
	}
	aspect := srcW / srcH
	tw, th := float64(dim), float64(dim)
	if aspect > 1 {
		th = float64(dim) / aspect
	} else if aspect < 1 {
		tw = float64(dim) * aspect
	}
	ox := math.Round((float64(dim) - tw) / 2)
	oy := math.Round((float64(dim) - th) / 2)
	icon.SetTarget(ox, oy, tw, th)

	img := image.NewRGBA(image.Rect(0, 0, dim, dim))
	scanner := rasterx.NewScannerGV(dim, dim, img, img.Bounds())
	raster := rasterx.NewDasher(dim, dim, scanner)
	icon.Draw(raster, 1.0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/png", nil
}

// NormalizeUpload turns whatever an upload handler received into a
// canonical bitmap + MIME pair. SVG goes through RasterizeSVG (the
// render itself is the security boundary — `<script>` and friends
// never reach the bitmap); PNG/JPEG/WebP go through Shrink (which
// re-encodes to JPEG to defang polyglot uploads). Anything else
// returns ErrUnsupportedMIME so the caller can produce a tidy 400.
//
// Centralised here so every upload handler — org logo, adventure
// cover, future ones — agrees on the accepted set without each
// reimplementing the same switch.
func NormalizeUpload(data []byte, mime string, maxDim int) ([]byte, string, error) {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/svg+xml":
		return RasterizeSVG(data, maxDim)
	case "image/png", "image/jpeg", "image/jpg", "image/webp":
		return Shrink(data, maxDim)
	}
	return nil, "", ErrUnsupportedMIME
}

// ErrUnsupportedMIME is returned by NormalizeUpload when the
// supplied MIME isn't on the accepted list. Callers wrap this in
// a 400 with a human-readable message naming the accepted set.
var ErrUnsupportedMIME = errors.New("unsupported image type")

// elementOpenRe matches an element's opening tag (`<tag attrs>` or
// `<tag attrs/>`). Tag names allow an optional namespace prefix
// (`<ns:tag>`); attribute payload is anything up to the closing `>`.
// Doesn't match comments, processing instructions, or closing tags
// because those don't begin with `<` followed by a word character.
var elementOpenRe = regexp.MustCompile(`<(\w+(?::\w+)?)([^>]*)>`)

// attrRe pulls one `name="value"` pair out of the attribute payload
// captured by elementOpenRe. Names accept the `xml:foo` / `xlink:bar`
// shapes that show up on root-level SVG attributes.
var attrRe = regexp.MustCompile(`\s+([\w-]+(?::[\w-]+)?)\s*=\s*"([^"]*)"`)

// expandStyleAttrs walks every element opening tag in the SVG and,
// for any element carrying a `style="prop:val;prop:val"` attribute,
// emits each `prop`/`val` pair as a separate attribute *if no
// attribute with that name is already present*. The original style
// attribute stays — the transform is purely additive so an SVG that
// already had attribute-form colours behaves identically.
//
// Why regex over an XML round-trip: encoding/xml tends to mangle
// namespace prefixes and reorder attributes when re-emitting, which
// confuses oksvg's parser. Pinning the rewrite to the targeted bytes
// keeps the rest of the document byte-for-byte identical.
//
// Edge cases: attribute values containing `>` would break the regex,
// but real-world SVGs don't put `>` in attribute strings (it would
// have to be `&gt;`-encoded anyway). Attribute values containing
// `;` are fine — split happens on the style payload, never on the
// surrounding XML.
func expandStyleAttrs(data []byte) []byte {
	return elementOpenRe.ReplaceAllFunc(data, func(match []byte) []byte {
		m := elementOpenRe.FindSubmatch(match)
		attrPayload := string(m[2])
		var styleVal string
		existing := map[string]bool{}
		for _, am := range attrRe.FindAllStringSubmatch(attrPayload, -1) {
			existing[am[1]] = true
			if am[1] == "style" {
				styleVal = am[2]
			}
		}
		if styleVal == "" {
			return match
		}
		var addons strings.Builder
		for _, part := range strings.Split(styleVal, ";") {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if k == "" || existing[k] {
				continue
			}
			addons.WriteString(` `)
			addons.WriteString(k)
			addons.WriteString(`="`)
			addons.WriteString(html.EscapeString(v))
			addons.WriteString(`"`)
			existing[k] = true
		}
		if addons.Len() == 0 {
			return match
		}
		s := string(match)
		// Insert before the trailing `/>` (self-close) or `>` (open).
		if strings.HasSuffix(s, "/>") {
			return []byte(s[:len(s)-2] + addons.String() + "/>")
		}
		return []byte(s[:len(s)-1] + addons.String() + ">")
	})
}
