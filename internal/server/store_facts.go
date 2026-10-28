package server

import (
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/adventure"
)

// factTexts are an adventure's facts as its ticket page says them, each
// empty when unknown.
type factTexts struct {
	Duration, Distance, Age, Difficulty, Access, Blind string
}

// formatFacts says f in s's words. Playing time is in minutes under an
// hour and in half hours above; a walk in metres under a kilometre and
// in tenths above. The store's cards (store.html) say them the same way.
func formatFacts(lang string, s landingStrings, f adventure.Facts) factTexts {
	n := func(tmpl string, v string) string { return strings.ReplaceAll(tmpl, "{n}", v) }
	num := func(v float64) string {
		out := strconv.FormatFloat(v, 'f', -1, 64)
		if lang == "sv" {
			out = strings.ReplaceAll(out, ".", ",")
		}
		return out
	}
	var t factTexts
	switch {
	case f.Minutes <= 0:
	case f.Minutes < 60:
		t.Duration = n(s.StoreFactMinutes, strconv.Itoa(f.Minutes))
	default:
		t.Duration = n(s.StoreFactHours, num(float64((f.Minutes+15)/30)/2))
	}
	switch {
	case f.DistanceM <= 0:
	case f.DistanceM < 1000:
		t.Distance = n(s.StoreFactMetres, strconv.Itoa(f.DistanceM))
	default:
		t.Distance = n(s.StoreFactKm, num(float64((f.DistanceM+50)/100)/10))
	}
	if f.MinAge > 0 {
		t.Age = n(s.StoreFactAgeLong, strconv.Itoa(f.MinAge))
	}
	t.Difficulty = map[string]string{"easy": s.StoreFactEasy, "medium": s.StoreFactMedium, "hard": s.StoreFactHard}[f.Difficulty]
	t.Access = map[string]string{"step_free": s.StoreFactStepFree, "some_steps": s.StoreFactSomeSteps, "not_step_free": s.StoreFactNotStepFree}[f.Access]
	if f.Blind {
		t.Blind = s.StoreFactBlind
	}
	return t
}
