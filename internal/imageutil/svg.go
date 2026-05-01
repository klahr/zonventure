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
	"image"
	"image/png"
	"io"
	"math"
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
