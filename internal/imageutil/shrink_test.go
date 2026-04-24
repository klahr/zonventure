package imageutil_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"git.sr.ht/~klahr/zv/internal/imageutil"
)

// makeImage produces a solid-colour test image of the requested size
// in the requested encoding. Kept inline so we can exercise both
// common formats without fixture files.
func makeImage(t *testing.T, w, h int, enc string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 180, G: 30, B: 90, A: 255})
		}
	}
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

func TestShrink_LargerThanCap_ResizesAndReencodes(t *testing.T) {
	src := makeImage(t, 2000, 1500, "png")
	out, mime, err := imageutil.Shrink(src, 512)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg (re-encode)", mime)
	}
	w, h := decode(t, out)
	if w > 512 || h > 512 {
		t.Errorf("result %dx%d exceeds 512 cap", w, h)
	}
	// Aspect ratio should be preserved (landscape → wider than tall).
	if w <= h {
		t.Errorf("aspect lost: src was 2000x1500, got %dx%d", w, h)
	}
	// Want the longer side to be the cap.
	if w != 512 {
		t.Errorf("longer side = %d, want 512", w)
	}
}

func TestShrink_PortraitAspect(t *testing.T) {
	src := makeImage(t, 800, 2000, "jpeg")
	out, _, err := imageutil.Shrink(src, 512)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	w, h := decode(t, out)
	if h != 512 {
		t.Errorf("tall side = %d, want 512", h)
	}
	if w >= h {
		t.Errorf("aspect lost: src was 800x2000, got %dx%d", w, h)
	}
}

func TestShrink_AlreadySmall_PassesThrough(t *testing.T) {
	// Below the cap: we should get the original bytes back unchanged.
	// Re-encoding loses quality for no benefit.
	src := makeImage(t, 300, 200, "png")
	out, mime, err := imageutil.Shrink(src, 512)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if !bytes.Equal(src, out) {
		t.Errorf("small image was re-encoded instead of passed through")
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png (detected from original)", mime)
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

// Integration-ish: verify the shrinker trims a huge JPEG to well under
// the 5 MB upload cap — the main practical benefit for the product.
func TestShrink_DrasticSizeReduction(t *testing.T) {
	src := makeImage(t, 3000, 3000, "jpeg")
	out, _, err := imageutil.Shrink(src, 512)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	// Not a tight bound, but 3000x3000 JPEG of even a flat colour → a
	// few hundred KB; shrunk to 512 it should come down substantially.
	if len(out) >= len(src) {
		t.Errorf("output %d bytes not smaller than input %d", len(out), len(src))
	}
}
