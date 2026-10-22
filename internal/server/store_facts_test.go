package server

import (
	"testing"

	"github.com/klahr/zonventure/internal/adventure"
)

func TestFormatFacts(t *testing.T) {
	s := landingStrings{StoreFactMinutes: "{n} min", StoreFactHours: "{n} h", StoreFactMetres: "{n} m", StoreFactKm: "{n} km",
		StoreFactAgeLong: "{n}+", StoreFactMedium: "Medium", StoreFactStepFree: "Step-free"}
	got := formatFacts("en", s, adventure.Facts{Minutes: 100, DistanceM: 2449, MinAge: 10, Difficulty: "medium", Access: "step_free"})
	want := factTexts{Duration: "1.5 h", Distance: "2.4 km", Age: "10+", Difficulty: "Medium", Access: "Step-free"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := formatFacts("sv", s, adventure.Facts{Minutes: 45, DistanceM: 800}); got.Duration != "45 min" || got.Distance != "800 m" {
		t.Errorf("short: %+v", got)
	}
	if got := formatFacts("sv", s, adventure.Facts{Minutes: 120, DistanceM: 1250}); got.Duration != "2 h" || got.Distance != "1,3 km" {
		t.Errorf("sv: %+v", got)
	}
	if got := formatFacts("en", s, adventure.Facts{}); got != (factTexts{}) {
		t.Errorf("none: %+v", got)
	}
}
