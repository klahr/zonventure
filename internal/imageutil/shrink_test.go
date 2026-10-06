package imageutil_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/klahr/zonventure/internal/imageutil"
)

// makeImage produces a solid-colour test image of the requested size
// in the requested encoding. Kept inline so we can exercise both
// common formats without fixture files.
func makeImage(t *testing.T, w, h int, enc string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 180, G: 30, B: 90, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	var err error
	switch enc {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	default:
		t.Fatalf("unknown enc %q", enc)
	}
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func decode(t *testing.T, data []byte) (w, h int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return cfg.Width, cfg.Height
}

// makeTransparentPNG produces an RGBA image where the centre pixel
// has alpha=255 (opaque red) and the corner pixel has alpha=0
// (transparent). Encoded as PNG so the alpha channel survives on
// disk. Used to pin ShrinkPNG's alpha-preservation contract — if a
// future refactor flattens to JPEG by accident, the corner pixel's
// alpha drops to 255 and this test trips.
func makeTransparentPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 200, G: 20, B: 80, A: 255}), image.Point{}, draw.Src)
	// Punch the top-left corner transparent.
	img.Set(0, 0, color.RGBA{})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestShrinkPNG_PreservesAlpha(t *testing.T) {
	src := makeTransparentPNG(t, 200, 200)
	out, mime, err := imageutil.ShrinkPNG(src, 512)
	if err != nil {
		t.Fatalf("ShrinkPNG: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, _, _, a := img.At(0, 0).RGBA()
	if a != 0 {
		t.Fatalf("top-left alpha = %d, want 0 — transparency was flattened", a>>8)
	}
}

func TestShrinkKeepAlpha_PNGOnlyWhenTransparent(t *testing.T) {
	out, mime, err := imageutil.ShrinkKeepAlpha(makeTransparentPNG(t, 200, 200), 512)
	if err != nil {
		t.Fatalf("ShrinkKeepAlpha: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("transparent: mime = %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("top-left alpha = %d, want 0 — transparency was flattened", a>>8)
	}

	out, mime, err = imageutil.ShrinkKeepAlpha(makeImage(t, 200, 200, "png"), 512)
	if err != nil {
		t.Fatalf("ShrinkKeepAlpha: %v", err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("opaque: mime = %q, want image/jpeg", mime)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("opaque output is not a JPEG: %v", err)
	}
}

func TestShrink_LargerThanCap_ResizesAndReencodes(t *testing.T) {
	// Small source + small cap: the resize path is the same at any
	// scale, and full-size inputs cost seconds under -race.
	src := makeImage(t, 200, 150, "png")
	out, mime, err := imageutil.Shrink(src, 64)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg (re-encode)", mime)
	}
	w, h := decode(t, out)
	if w > 64 || h > 64 {
		t.Errorf("result %dx%d exceeds 64 cap", w, h)
	}
	// Aspect ratio should be preserved (landscape → wider than tall).
	if w <= h {
		t.Errorf("aspect lost: src was 200x150, got %dx%d", w, h)
	}
	// Want the longer side to be the cap.
	if w != 64 {
		t.Errorf("longer side = %d, want 64", w)
	}
}

// makeBombPNG builds a tiny PNG byte stream whose IHDR chunk advertises
// the requested dimensions but whose data section is empty. DecodeConfig
// reads only signature + IHDR and reports the declared size, so this
// suffices to exercise the pixel-bomb pre-check without allocating
// width*height*4 bytes at construction time.
func makeBombPNG(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	binary.Write(&buf, binary.BigEndian, uint32(13))
	ihdrStart := buf.Len()
	buf.WriteString("IHDR")
	binary.Write(&buf, binary.BigEndian, width)
	binary.Write(&buf, binary.BigEndian, height)
	buf.WriteByte(8) // bit depth
	buf.WriteByte(2) // color type RGB
	buf.WriteByte(0) // compression
	buf.WriteByte(0) // filter
	buf.WriteByte(0) // interlace
	crc := crc32.ChecksumIEEE(buf.Bytes()[ihdrStart:])
	binary.Write(&buf, binary.BigEndian, crc)
	return buf.Bytes()
}

func TestShrink_RejectsPixelBomb(t *testing.T) {
	// 50000 × 50000 = 2.5 billion pixels → ~10 GB of RGBA. Without the
	// DecodeConfig pre-check, image.Decode would attempt to allocate
	// the buffer and OOM the test runner. A successful decode here
	// would already be a regression; check for the specific sentinel.
	bomb := makeBombPNG(50000, 50000)
	_, _, err := imageutil.Shrink(bomb, 512)
	if !errors.Is(err, imageutil.ErrPixelBomb) {
		t.Fatalf("expected ErrPixelBomb, got %v", err)
	}
}

func TestShrink_PortraitAspect(t *testing.T) {
	// JPEG source keeps the YCbCr decode path covered.
	src := makeImage(t, 80, 200, "jpeg")
	out, _, err := imageutil.Shrink(src, 64)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	w, h := decode(t, out)
	if h != 64 {
		t.Errorf("tall side = %d, want 64", h)
	}
	if w >= h {
		t.Errorf("aspect lost: src was 80x200, got %dx%d", w, h)
	}
}

func TestShrink_AlreadySmall_StillReencodes(t *testing.T) {
	// Below the cap: we re-encode anyway. The previous "skip re-
	// encode for small inputs" optimisation exposed a polyglot
	// surface — bytes that are both a valid PNG and a valid HTML
	// document would ship verbatim with image/png Content-Type.
	// Re-encoding strips any sibling-document tail; the cost is a
	// few ms per upload + a tiny quality loss, which is fine for
	// puzzle assets at phone resolution.
	src := makeImage(t, 300, 200, "png")
	out, mime, err := imageutil.Shrink(src, 512)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if bytes.Equal(src, out) {
		t.Errorf("small image was passed through verbatim — polyglot surface left open")
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg (re-encoded)", mime)
	}
	// Confirm dimensions are preserved (not shrunk because already small).
	w, h := decode(t, out)
	if w != 300 || h != 200 {
		t.Errorf("dimensions changed: got %dx%d, want 300x200", w, h)
	}
}

func TestShrink_RejectsGarbage(t *testing.T) {
	if _, _, err := imageutil.Shrink([]byte("not an image"), 512); err == nil {
		t.Fatalf("expected error on garbage input")
	}
}

func TestShrink_BadMaxDim(t *testing.T) {
	if _, _, err := imageutil.Shrink(makeImage(t, 100, 100, "png"), 0); err == nil {
		t.Fatalf("expected error on maxDim=0")
	}
}

// Integration-ish: verify the shrinker trims a large JPEG down in
// bytes — the main practical benefit for the product. Scaled down
// (400px → 32px) to keep the test fast; the ratio is what matters.
func TestShrink_DrasticSizeReduction(t *testing.T) {
	src := makeImage(t, 400, 400, "jpeg")
	out, _, err := imageutil.Shrink(src, 32)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	// Not a tight bound, but a 400x400 JPEG of even a flat colour
	// shrunk to 32px should come down substantially.
	if len(out) >= len(src) {
		t.Errorf("output %d bytes not smaller than input %d", len(out), len(src))
	}
}
