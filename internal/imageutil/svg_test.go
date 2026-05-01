package imageutil_test

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/imageutil"
)

const trivialSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
  <circle cx="50" cy="50" r="40" fill="#ad1357"/>
</svg>`

func TestRasterizeSVG_ProducesPNG(t *testing.T) {
	out, mime, err := imageutil.RasterizeSVG([]byte(trivialSVG), 256)
	if err != nil {
		t.Fatalf("RasterizeSVG: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("bounds = %v, want 256x256", b)
	}
	// Centre pixel of the circle should carry the fill colour, not
	// the transparent canvas — confirms the renderer drew something
	// rather than handing back an empty alpha bitmap.
	_, _, _, a := img.At(128, 128).RGBA()
	if a == 0 {
		t.Fatalf("centre pixel transparent — circle didn't render")
	}
}

// Ensure script-bearing SVGs don't smuggle JS into the output: the
// returned bytes are a freshly-encoded PNG, so there's no path for
// `<script>` source to reach a viewer's browser via the stored blob.
func TestRasterizeSVG_StripsScript(t *testing.T) {
	hostile := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<script>alert('xss')</script>
		<rect width="100" height="100" fill="#34d399"/>
	</svg>`
	out, mime, err := imageutil.RasterizeSVG([]byte(hostile), 64)
	if err != nil {
		t.Fatalf("RasterizeSVG: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", mime)
	}
	if bytes.Contains(out, []byte("script")) || bytes.Contains(out, []byte("alert")) {
		t.Fatalf("PNG output contains script source — render isn't a security boundary")
	}
	if strings.Contains(string(out), "<svg") {
		t.Fatalf("PNG output contains SVG markup — wrong format somehow shipped through")
	}
}

func TestRasterizeSVG_RejectsGarbage(t *testing.T) {
	if _, _, err := imageutil.RasterizeSVG([]byte("not an svg"), 64); err == nil {
		t.Fatalf("garbage input should error")
	}
}

func TestRasterizeSVG_RejectsEmpty(t *testing.T) {
	if _, _, err := imageutil.RasterizeSVG(nil, 64); err == nil {
		t.Fatalf("empty input should error")
	}
}

func TestRasterizeSVG_BadDim(t *testing.T) {
	if _, _, err := imageutil.RasterizeSVG([]byte(trivialSVG), 0); err == nil {
		t.Fatalf("dim=0 should error")
	}
}
