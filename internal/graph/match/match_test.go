package match

import (
	"math"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  Hello  World  ": "hello world",
		"HELLO":            "hello",
		"a\t\nb":           "a b",
		"":                 "",
		"   ":              "",
		"Ärlig Ölstuga":    "ärlig ölstuga",
		"A\u030arsta":      "årsta",
		"Don\u2019t":       "don't",
		"\u201cHi\u201d":   `"hi"`,
		"1990\u20132000":   "1990-2000",
		"Paris.":           "paris",
		"really?! ":        "really",
		"wait ...":         "wait",
		"?":                "?",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExact(t *testing.T) {
	m := Matcher{Kind: Exact, Value: "Blue Door"}
	for _, guess := range []string{"blue door", "  BLUE   DOOR  ", "Blue Door"} {
		if !m.Match(guess).OK {
			t.Errorf("Exact should accept %q", guess)
		}
	}
	for _, guess := range []string{"bluedoor", "blue", "red door", ""} {
		if m.Match(guess).OK {
			t.Errorf("Exact should reject %q", guess)
		}
	}
}

// The author's answer and the player's guess go through the same
// Normalize, so typography on either side does not matter.
func TestExactForgivesTypography(t *testing.T) {
	m := Matcher{Kind: Exact, Value: "Don\u2019t Panic"}
	for _, guess := range []string{"don't panic", "Don\u2018t panic!", "don`t panic."} {
		if !m.Match(guess).OK {
			t.Errorf("Exact should accept %q", guess)
		}
	}
	if !(Matcher{Kind: Exact, Value: "rock-paper"}).Match("Rock\u2014paper").OK {
		t.Error("an em dash should match a hyphen")
	}
}

// A regex sees the guess as before: its punctuation is the author's to
// match.
func TestRegexKeepsPunctuation(t *testing.T) {
	m := Matcher{Kind: Regex, Value: `why\?`}
	if !m.Match("Why?").OK {
		t.Error("a regex should still see the trailing question mark")
	}
}

func TestContains(t *testing.T) {
	m := Matcher{Kind: Contains, Value: "raven"}
	if !m.Match("the RAVEN flew").OK {
		t.Error("Contains should accept a substring hit")
	}
	if m.Match("the crow flew").OK {
		t.Error("Contains should reject a miss")
	}

	// An empty needle is a misconfiguration, not a catch-all — Anything
	// is how you say that, and it says so legibly on the canvas.
	if (Matcher{Kind: Contains, Value: ""}).Match("whatever").OK {
		t.Error("Contains with an empty value must not match everything")
	}
}

func TestRegexIsAnchoredAndCaseInsensitive(t *testing.T) {
	m := Matcher{Kind: Regex, Value: `\d{4}`}
	if !m.Match("1234").OK {
		t.Error("should accept the whole-string match")
	}
	// The anchoring is the point: without \A..\z this would accept
	// "my pin is 1234", which is almost never what an author means.
	if m.Match("my pin is 1234").OK {
		t.Error("regex must be anchored to the whole guess")
	}

	if !(Matcher{Kind: Regex, Value: "(six|sex)"}).Match("SIX").OK {
		t.Error("regex must be case-insensitive")
	}

	// Fail closed. The validator rejects this at save time, but a bad
	// pattern reaching the runtime must not panic mid-event.
	if (Matcher{Kind: Regex, Value: "("}).Match("anything").OK {
		t.Error("an uncompilable pattern must match nothing")
	}
}

func TestInterval(t *testing.T) {
	m := Matcher{Kind: Interval, Low: 10, High: 20}
	for _, g := range []string{"10", "15", "20", "  15  ", "15.0"} {
		if !m.Match(g).OK {
			t.Errorf("Interval should accept %q", g)
		}
	}
	for _, g := range []string{"9", "21", "abc", ""} {
		if m.Match(g).OK {
			t.Errorf("Interval should reject %q", g)
		}
	}

	// A reversed range is an author slip. Rejecting every guess would
	// be silently broken at the event; swapping is the kind reading.
	if !(Matcher{Kind: Interval, Low: 20, High: 10}).Match("15").OK {
		t.Error("a reversed interval should still accept a value between the bounds")
	}
}

// The formula legacy computed as (value+diff-guess)/(2*diff*0.5),
// written the way it actually behaves.
func TestNearPartialCredit(t *testing.T) {
	m := Matcher{Kind: Near, Target: 1500, Tolerance: 300}

	cases := []struct {
		guess string
		want  float64
	}{
		{"1500", 1.0},
		{"1650", 0.5},
		{"1350", 0.5},
		{"1800", 0.0}, // exactly at the tolerance edge: accepted, worth nothing
		{"1200", 0.0},
	}
	for _, c := range cases {
		r := m.Match(c.guess)
		if !r.OK {
			t.Errorf("Near should accept %q (within tolerance)", c.guess)
			continue
		}
		if math.Abs(r.Multiplier-c.want) > 1e-9 {
			t.Errorf("Near(%q).Multiplier = %v, want %v", c.guess, r.Multiplier, c.want)
		}
	}

	for _, g := range []string{"1801", "1199", "0"} {
		if m.Match(g).OK {
			t.Errorf("Near should reject %q (outside tolerance)", g)
		}
	}

	// Guard against a divide-by-zero authored graph.
	if (Matcher{Kind: Near, Target: 10, Tolerance: 0}).Match("10").OK {
		t.Error("Near with zero tolerance must reject rather than divide by zero")
	}
}

func TestParseNumberToleratesHowPeopleWrite(t *testing.T) {
	m := Matcher{Kind: Interval, Low: 1400, High: 1600}
	for _, g := range []string{"1500", "1500 m", "about 1500", "1 500", "1500,0", "1500.00"} {
		if !m.Match(g).OK {
			t.Errorf("should have read a number from %q", g)
		}
	}
	// A comma as decimal separator, Swedish style.
	if !(Matcher{Kind: Interval, Low: 1, High: 2}).Match("1,5").OK {
		t.Error("should read 1,5 as 1.5")
	}
	for _, g := range []string{"", "abc", "-", "+"} {
		if m.Match(g).OK {
			t.Errorf("should not have read a number from %q", g)
		}
	}
}

func TestSetUnordered(t *testing.T) {
	m := Matcher{Kind: Set, Members: []string{"alpha", "beta", "gamma"}, Required: 2}

	for _, g := range []string{
		"alpha, beta",
		"beta,alpha",       // order irrelevant when Ordered is false
		"gamma; alpha",     //
		"alpha|beta|gamma", // more than required is fine
		"ALPHA , Beta",
	} {
		if !m.matchSet(g).OK {
			t.Errorf("Set should accept %q", g)
		}
	}

	for _, g := range []string{"alpha", "delta, epsilon", ""} {
		if m.matchSet(g).OK {
			t.Errorf("Set should reject %q (fewer than Required)", g)
		}
	}
}

func TestSetOrdered(t *testing.T) {
	m := Matcher{Kind: Set, Members: []string{"alpha", "beta", "gamma"}, Required: 2, Ordered: true}

	// Two correct and each in its own position.
	if !m.matchSet("alpha, beta, nope").OK {
		t.Error("ordered set should accept members in their declared positions")
	}
	// Two correct but swapped — this is the case Ordered exists to reject.
	if m.matchSet("beta, alpha, nope").OK {
		t.Error("ordered set must reject correct members in the wrong positions")
	}
	if !m.matchSet("alpha, beta, gamma").OK {
		t.Error("ordered set should accept a fully correct answer")
	}
}

func TestSetRequiredDefaultsToAll(t *testing.T) {
	m := Matcher{Kind: Set, Members: []string{"a", "b"}}
	if !m.matchSet("a, b").OK {
		t.Error("Required=0 should mean all members")
	}
	if m.matchSet("a").OK {
		t.Error("Required=0 should mean all members, so one is not enough")
	}
	// A Required larger than the member list clamps rather than making
	// the matcher unsatisfiable.
	if !(Matcher{Kind: Set, Members: []string{"a"}, Required: 5}).matchSet("a").OK {
		t.Error("Required beyond the member count should clamp to all")
	}
	if (Matcher{Kind: Set}).matchSet("anything").OK {
		t.Error("a Set with no members must match nothing")
	}
}

// A member appearing twice in the guess must not be double-counted into
// satisfying Required.
func TestSetDoesNotDoubleCountRepeats(t *testing.T) {
	m := Matcher{Kind: Set, Members: []string{"alpha", "beta"}, Required: 2}
	if m.matchSet("alpha, alpha").OK {
		t.Error("the same member twice must not satisfy a two-member requirement")
	}
}

func TestAnything(t *testing.T) {
	m := Matcher{Kind: Anything}
	for _, g := range []string{"", "anything at all", "   "} {
		if !m.Match(g).OK {
			t.Errorf("Anything should accept %q", g)
		}
	}
}

// The two-pass order is what makes a catch-all usable: an author adding
// "accept anything" to sweep up stragglers must not shadow the precise
// answers sitting beside it.
func TestAnyEvaluatesCatchAllLast(t *testing.T) {
	matchers := []Matcher{
		{Kind: Anything},
		{Kind: Near, Target: 100, Tolerance: 50},
	}
	r := Any(matchers, "100")
	if !r.OK {
		t.Fatal("expected a match")
	}
	if r.Multiplier != 1 {
		t.Errorf("Multiplier = %v; the specific Near matcher should have won over the catch-all", r.Multiplier)
	}

	// And the catch-all still catches what nothing else does.
	if !Any(matchers, "not a number").OK {
		t.Error("catch-all should accept what the specific matchers rejected")
	}
}

func TestAnyFirstSpecificWins(t *testing.T) {
	matchers := []Matcher{
		{Kind: Exact, Value: "left"},
		{Kind: Exact, Value: "right"},
	}
	if !Any(matchers, "right").OK {
		t.Error("should accept a later matcher in the list")
	}
	if Any(matchers, "up").OK {
		t.Error("should reject what no matcher accepts")
	}
	if Any(nil, "anything").OK {
		t.Error("an empty matcher list must accept nothing")
	}
}

// Legacy's getInterval and getDynamicScore both do CurrentAnswear[0]
// with no length check, so an empty authored value panics the request
// handler. Structured fields make that unrepresentable; this pins it.
func TestEmptyAndZeroValuesNeverPanic(t *testing.T) {
	for _, m := range []Matcher{
		{Kind: Exact},
		{Kind: Contains},
		{Kind: Regex},
		{Kind: Interval},
		{Kind: Near},
		{Kind: Set},
		{Kind: Anything},
		{Kind: "unknown-kind"},
		{},
	} {
		for _, g := range []string{"", "   ", "something"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Matcher{Kind:%q}.Match(%q) panicked: %v", m.Kind, g, r)
					}
				}()
				m.Match(g)
			}()
		}
	}
}

func TestCompileRegexRejectsBadPatterns(t *testing.T) {
	if _, err := CompileRegex("("); err == nil {
		t.Error("expected an error for an unbalanced group")
	}
	if _, err := CompileRegex(`\d+`); err != nil {
		t.Errorf("expected a valid pattern to compile: %v", err)
	}
}

func FuzzMatchNeverPanics(f *testing.F) {
	f.Add("exact", "blue", "blue")
	f.Add("regex", `\d{4}`, "1234")
	f.Add("near", "", "1500")
	f.Add("set", "", "a,b,c")
	f.Fuzz(func(t *testing.T, kind, value, guess string) {
		m := Matcher{
			Kind: Kind(kind), Value: value,
			Low: -1, High: 1, Target: 0, Tolerance: 1,
			Members: []string{value}, Required: 1,
		}
		m.Match(guess)
		Any([]Matcher{m}, guess)
	})
}
