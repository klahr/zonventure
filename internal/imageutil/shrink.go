// Package imageutil caps uploaded images to a configurable dimension.
// Applied at every upload site (puzzle images, team avatars, adventure
// covers) so the database and the wire stay small without forcing
// authors to pre-process their files.
package imageutil

import (
	"bytes"
	"errors"
	"image"
	// Register JPEG, PNG, and WebP decoders in image.Decode. GIF is
	// less common for puzzle content and its blobs tend to be small
	// already; skipping keeps the import graph tight. WebP comes
	// from golang.org/x/image (already in go.mod for the resampler)
	// — without this blank import the decoder isn't registered, so
	// uploads with Content-Type: image/webp fail with "unknown
	// format" even though the MIME guard accepts them.
	_ "golang.org/x/image/webp"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

// DefaultMaxDim is the cap used by callers that want the standard
// policy. 512 px is enough for both avatars and in-puzzle images at
// typical phone resolutions while keeping blobs under ~60 KB after
// JPEG re-encoding.
const DefaultMaxDim = 512

// MaxInputPixels caps the source image's declared dimensions before
// image.Decode allocates a pixel buffer. A small (~5 KB) PNG can
// declare 50000×50000 pixels in its IHDR — image.Decode would then
// try to allocate ~10 GB of RGBA and OOM the process. 25 megapixels
// is roughly a 5K-by-5K image, plenty for any phone photo or scan
// before downscaling. Reachable via every authoring upload (puzzle
// images, avatars, covers, logos) AND the unauthenticated team-
// avatar-by-token surface, so failing fast at this layer protects
// the public attack surface too.
const MaxInputPixels = 25_000_000

// ErrPixelBomb is returned when a decoded image's declared dimensions
// exceed MaxInputPixels. Surfaces as a 400 at upload sites since the
// caller chose the file.
var ErrPixelBomb = errors.New("image dimensions too large")

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
	dst, err := decodeAndResize(data, maxDim)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// ShrinkPNG is the alpha-preserving variant of Shrink: same decode +
// resize + re-encode pipeline, but encodes to PNG so transparency
// survives the round-trip. JPEG would composite alpha-zero pixels
// against the RGBA zero value (black), which logos with transparent
// backgrounds upload with — see the help copy on the org-logo and
// adventure-cover surfaces. The polyglot defence is unchanged: the
// re-encode itself is the security boundary, not the format choice.
//
// Used by NormalizeUpload for the logo / cover paths. Photo-side
// callers (puzzle images, team avatars) stay on the JPEG path
// because their inputs rarely carry alpha and JPEG's smaller
// blobs matter when they're served per-puzzle.
func ShrinkPNG(data []byte, maxDim int) ([]byte, string, error) {
	dst, err := decodeAndResize(data, maxDim)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/png", nil
}

// ShrinkKeepAlpha is Shrink for an image that may be transparent: a
// picture with any transparent pixel is re-encoded as PNG, as ShrinkPNG
// does, and an opaque one as JPEG, as Shrink does. Used for adventure
// media, where a photo should stay a small JPEG but a letter tile or a
// cut-out on a transparent background must not turn black.
func ShrinkKeepAlpha(data []byte, maxDim int) ([]byte, string, error) {
	dst, err := decodeAndResize(data, maxDim)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if !dst.Opaque() {
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// decodeAndResize is the shared front half of Shrink / ShrinkPNG.
// Returns an *image.RGBA capped at maxDim on the longer side with
// aspect ratio preserved.
func decodeAndResize(data []byte, maxDim int) (*image.RGBA, error) {
	if maxDim <= 0 {
		return nil, errors.New("maxDim must be > 0")
	}
	// Pixel-bomb pre-check. DecodeConfig parses just the image header,
	// so the dimensions check happens before image.Decode reaches the
	// allocator. Without this gate, a few KB of attacker bytes can
	// push the process past its memory limits.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > int64(MaxInputPixels) {
		return nil, ErrPixelBomb
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dstW, dstH := srcW, srcH
	if srcW > maxDim || srcH > maxDim {
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
	return dst, nil
}
