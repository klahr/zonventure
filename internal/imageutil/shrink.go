// Package imageutil caps uploaded images to a configurable dimension.
// Applied at every upload site (puzzle images, team avatars, adventure
// covers) so the database and the wire stay small without forcing
// authors to pre-process their files.
package imageutil

import (
	"bytes"
	"errors"
	"image"
	// Register JPEG and PNG decoders in image.Decode. GIF is less
	// common for puzzle content and its blobs tend to be small
	// already; skipping keeps the import graph tight.
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
)

// DefaultMaxDim is the cap used by callers that want the standard
// policy. 512 px is enough for both avatars and in-puzzle images at
// typical phone resolutions while keeping blobs under ~60 KB after
// JPEG re-encoding.
const DefaultMaxDim = 512

// Shrink caps both dimensions of an image at maxDim, preserving aspect
// ratio, and ALWAYS re-encodes the result as JPEG. Returns the new
// bytes + "image/jpeg".
//
// The unconditional re-encode is the security boundary: a polyglot
// crafted to be both a valid PNG/JPEG and a valid HTML document
// (e.g., trailing-data tricks past the image's logical end-of-file)
// would pass the image.Decode validation and, if returned verbatim
// for under-sized inputs, ship the malicious payload to the browser
// alongside an Content-Type: image/png header. Re-encoding strips
// every byte beyond what the decoder reconstructs — the output is
// just the raw pixel data wrapped in JPEG framing, with no path to
// smuggle a sibling document through.
//
// X-Content-Type-Options: nosniff (set as a baseline by the server
// router) already blocks browsers from re-interpreting the served
// type; re-encoding here is the defence-in-depth layer that doesn't
// rely on browser headers being correctly applied at every serve
// site (overrides, proxies, etc.).
//
// Cost: a 50 KB phone JPEG decodes and re-encodes in single-digit
// milliseconds. The "skip re-encode for already-small" optimisation
// it replaces saved a tiny amount of CPU but exposed the polyglot
// surface; not worth keeping.
func Shrink(data []byte, maxDim int) ([]byte, string, error) {
	if maxDim <= 0 {
		return nil, "", errors.New("maxDim must be > 0")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dstW, dstH := srcW, srcH
	if srcW > maxDim || srcH > maxDim {
		// Preserve aspect ratio — cap the longer side at maxDim.
		dstW, dstH = maxDim, maxDim
		if srcW >= srcH {
			dstH = maxDim * srcH / srcW
			if dstH < 1 {
				dstH = 1
			}
		} else {
			dstW = maxDim * srcW / srcH
			if dstW < 1 {
				dstW = 1
			}
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}
