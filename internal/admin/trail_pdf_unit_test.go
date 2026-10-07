package admin

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/klahr/zonventure/internal/trail"
)

// TestDarkLeft: the print title goes white only on dark cover pictures.
func TestDarkLeft(t *testing.T) {
	for id, want := range map[string]bool{"retro-80s": true, "space": true, "detective": true, "unicorn": false, "ocean": false, "pirate": false, "jungle": false, "ice-cream": false, "rainbow": false, "candy": false, "sunshine": false, "party": false, "office": false, "pen-and-paper": false} {
		if got := darkLeft(trail.Cover(id)); got != want {
			t.Errorf("%s: dark = %v, want %v", id, got, want)
		}
	}
	if darkLeft([]byte("not a jpeg")) {
		t.Error("garbage is dark")
	}
}

// TestPrintCover: an uploaded cover prints as a JPEG cut to 16:9.
func TestPrintCover(t *testing.T) {
	for _, size := range [][2]int{{400, 400}, {1600, 600}, {320, 180}} {
		img := image.NewRGBA(image.Rect(0, 0, size[0], size[1]))
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		out := printCover(coverFile{Mime: "image/png", Content: buf.Bytes()})
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("%v: %v", size, err)
		}
		if r := float64(cfg.Width) / float64(cfg.Height); r < 1.76 || r > 1.79 {
			t.Errorf("%v → %d×%d", size, cfg.Width, cfg.Height)
		}
	}
	if printCover(coverFile{Content: []byte("nope")}) != nil {
		t.Error("garbage printed")
	}
}

// TestHeaderOn: a header's text is black on light look colours and
// white on dark ones, as the creator's readableOn picks it.
func TestHeaderOn(t *testing.T) {
	black, white := rgb{0, 0, 0}, rgb{255, 255, 255}
	for hex, want := range map[string]rgb{"#ffd319": black, "#4fc3f7": black, "#ff4fa3": black, "#db2777": white, "#6d28d9": white, "#173f6e": white} {
		if got := hexRGB(hex, rgb{}).on(); got != want {
			t.Errorf("%s: on = %v, want %v", hex, got, want)
		}
	}
}
