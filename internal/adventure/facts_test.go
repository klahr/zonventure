package adventure

import "testing"

func TestFactsValidate(t *testing.T) {
	for _, f := range []Facts{{}, {Minutes: 90, DistanceM: 2400, MinAge: 10, Difficulty: "medium", Access: "step_free"}} {
		if err := f.Validate(); err != nil {
			t.Errorf("%+v: %v", f, err)
		}
	}
	for _, f := range []Facts{{Minutes: -1}, {Minutes: MaxFactMinutes + 1}, {DistanceM: -5}, {MinAge: 120}, {Difficulty: "brutal"}, {Access: "ramps"}} {
		if f.Validate() == nil {
			t.Errorf("%+v passed", f)
		}
	}
}
