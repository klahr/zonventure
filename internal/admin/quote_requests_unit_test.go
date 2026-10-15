package admin

import (
	"testing"
	"time"
)

func TestQuoteStamp(t *testing.T) {
	key := make([]byte, 32)
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		name  string
		stamp string
		want  bool
	}{
		{"loaded a minute ago", quoteStamp(key, now.Add(-time.Minute)), true},
		{"loaded just now", quoteStamp(key, now.Add(-time.Second)), false},
		{"loaded last week", quoteStamp(key, now.Add(-8*24*time.Hour)), false},
		{"another key", quoteStamp([]byte("another key, another key, another"), now.Add(-time.Minute)), false},
		{"time changed", "1799999000." + quoteMAC(key, "1799999940"), false},
		{"empty", "", false},
		{"no mac", "1799999940", false},
	} {
		if got := quoteStampOK(key, tc.stamp, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestQuoteAnswersCheck(t *testing.T) {
	ok := func() quoteAnswers {
		return quoteAnswers{Occasion: "kickoff", Date: "2027-05-12", Month: "2027-05", People: 100, City: " Stockholm ", Scope: "adapted",
			Extras: []string{"logo", "nonsense", "logo", "own_questions"}, Languages: []string{"en", "sv"}}
	}
	a := ok()
	if r := a.check(); r != "" {
		t.Fatalf("good answers: %q", r)
	}
	if a.City != "Stockholm" || a.Month != "" {
		t.Errorf("not trimmed or month kept with a date: %+v", a)
	}
	if got := a.Extras; len(got) != 2 || got[0] != "own_questions" || got[1] != "logo" {
		t.Errorf("extras = %v", got)
	}
	if got := a.Languages; len(got) != 2 || got[0] != "sv" {
		t.Errorf("languages = %v", got)
	}
	for name, spoil := range map[string]func(*quoteAnswers){
		"no occasion":     func(a *quoteAnswers) { a.Occasion = "" },
		"unknown scope":   func(a *quoteAnswers) { a.Scope = "everything" },
		"no date":         func(a *quoteAnswers) { a.Date, a.Month = "", "" },
		"bad date":        func(a *quoteAnswers) { a.Date = "12/05/2027" },
		"nobody":          func(a *quoteAnswers) { a.People = 0 },
		"no city":         func(a *quoteAnswers) { a.City = "  " },
		"unknown budget":  func(a *quoteAnswers) { a.Budget = "lots" },
		"unknown time":    func(a *quoteAnswers) { a.TimeOfDay = "night" },
		"unknown length":  func(a *quoteAnswers) { a.Duration = "a week" },
		"too many people": func(a *quoteAnswers) { a.People = maxQuotePeople + 1 },
	} {
		a := ok()
		spoil(&a)
		if a.check() == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	a = ok()
	a.Date, a.Month = "", "2027-05"
	if r := a.check(); r != "" {
		t.Errorf("a month alone: %q", r)
	}
}
