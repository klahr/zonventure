package admin

import (
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
