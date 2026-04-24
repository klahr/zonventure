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
// ratio. Returns the new bytes + the new MIME type. If the image is
// already small enough it returns the original bytes unchanged (with
// the original MIME detected via image.Decode).
//
// Anything the stdlib can decode is re-encoded as JPEG at quality 85 —
// transparent PNGs lose their alpha channel, which is fine for photos
// and acceptable for the small set of author-facing icons we handle.
// The re-encode is deliberate: it normalises the wire format and usually
// saves 50%+ on files authors drag straight off a camera.
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
	if srcW <= maxDim && srcH <= maxDim {
		// Already small. Skip the re-encode so we don't burn quality
		// on an already-appropriate image. Caller keeps original MIME.
		return data, detectMIME(data), nil
	}

	// Preserve aspect ratio — cap the longer side at maxDim.
	dstW, dstH := maxDim, maxDim
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
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// detectMIME sniffs a tiny prefix of data to recover the MIME type.
// Used only in the pass-through branch (image already small enough);
// Shrink needs a MIME to return but we deliberately don't trust the
// Content-Type header on the incoming request.
func detectMIME(data []byte) string {
	if len(data) >= 4 {
		if data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
			return "image/jpeg"
		}
		if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
			return "image/png"
		}
		if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
			return "image/gif"
		}
	}
	return "application/octet-stream"
}
