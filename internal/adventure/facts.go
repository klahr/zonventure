package adventure

import (
	"fmt"
	"slices"
)

// Facts are what a buyer weighs before buying an adventure, as its
// author states them; the store shows them on the card and the ticket's
// page. A zero field is unknown and not shown. They are the public
// sibling of the author's brief, which is never shown.
type Facts struct {
	// Minutes is a typical team's playing time.
	Minutes int `json:"minutes,omitempty"`
	// DistanceM is the walk, in metres; none for one played in a spot.
	DistanceM int `json:"distance_m,omitempty"`
	// MinAge is the youngest who can play in a team of their own;
	// younger players can join one with an adult.
	MinAge int `json:"min_age,omitempty"`
	// Difficulty is one of Difficulties.
	Difficulty string `json:"difficulty,omitempty"`
	// Access is one of Accesses: whether a wheelchair or a pram can
	// follow the route. The adventure's access_note says the rest.
	Access string `json:"access,omitempty"`
	// Blind says the whole adventure can be played by someone who
	// cannot see, with a screen reader: every zone named, every puzzle
	// heard or felt, every picture described.
	Blind bool `json:"blind,omitempty"`
}

// Difficulties and Accesses are the values Facts take.
var (
	Difficulties = []string{"easy", "medium", "hard"}
	Accesses     = []string{"step_free", "some_steps", "not_step_free"}
)

// The bounds of Facts' numbers.
const (
	MaxFactMinutes   = 24 * 60
	MaxFactDistanceM = 100_000
	MaxFactMinAge    = 99
)

// Validate reports the first fact out of bounds.
func (f Facts) Validate() error {
	switch {
	case f.Minutes < 0 || f.Minutes > MaxFactMinutes:
		return fmt.Errorf("minutes must be between 1 and %d", MaxFactMinutes)
	case f.DistanceM < 0 || f.DistanceM > MaxFactDistanceM:
		return fmt.Errorf("distance_m must be between 1 and %d", MaxFactDistanceM)
	case f.MinAge < 0 || f.MinAge > MaxFactMinAge:
		return fmt.Errorf("min_age must be between 1 and %d", MaxFactMinAge)
	case f.Difficulty != "" && !slices.Contains(Difficulties, f.Difficulty):
		return fmt.Errorf("difficulty must be one of %v", Difficulties)
	case f.Access != "" && !slices.Contains(Accesses, f.Access):
		return fmt.Errorf("access must be one of %v", Accesses)
	}
	return nil
}
