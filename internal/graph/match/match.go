// Package match decides whether a player's guess satisfies a matcher.
//
// It is deliberately pure: no database, no clock, no I/O. Every rule in
// here is table-driven testable, which matters because these semantics
// are six years of accumulated decisions about what "correct" means at
// a real event, and they are easy to lose in a rewrite.
//
// What changed from the legacy resolver, and why:
//
//   - The in-band syntax is gone. Legacy encoded ranges as "{10..20}",
//     partial credit as "{1500#300}" and multi-part answers as
//     "a|b|c" inside the answer string itself. Those are structured
//     fields now. That removes a whole class of authoring mistake (an
//     answer that is literally "{10..20}" was unrepresentable) and it
//     removes the crash: legacy's getInterval and getDynamicScore both
//     index CurrentAnswear[0] with no length check, so an empty answer
//     value panics the request handler.
//
//   - Partial credit is expressed as the formula it always was.
//     Legacy computed (value+diff-guess)/(2*diff*0.5), which reduces to
//     1 - (guess-value)/diff. Written here as
//     max(0, 1 - abs(guess-target)/tolerance) so a reader can see what
//     it does.
//
// What deliberately did not change: normalisation, and the anchored
// case-insensitive regex wrapper. Both match internal/adventure/loader.go
// exactly, because live adventures depend on them.
package match

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Kind is the matcher discriminator. Stored as a string so an authored
// graph stays readable in JSON and in the database.
type Kind string

const (
	// Exact compares the normalised guess to the normalised value.
	Exact Kind = "exact"
	// Contains is true when the normalised value appears anywhere in
	// the normalised guess.
	Contains Kind = "contains"
	// Regex is RE2, case-insensitive, anchored to the whole guess.
	Regex Kind = "regex"
	// Interval accepts any number in [Low, High].
	Interval Kind = "interval"
	// Near accepts any number, with credit decaying to zero at
	// Tolerance away from Target.
	Near Kind = "near"
	// Set accepts a guess naming at least Required of Members.
	Set Kind = "set"
	// Anything matches every guess. Evaluated last by Any so it acts as
	// a catch-all without shadowing the specific matchers.
	Anything Kind = "anything"
)

// Matcher is one acceptance rule on a puzzle output.
//
// It is a single struct rather than an interface so it round-trips
// through JSON as plain data, which is what lets the editor render it
// from a schema and the validator check it without executing anything.
// Fields not relevant to Kind are ignored.
type Matcher struct {
	Kind Kind `json:"kind"`

	// Value is used by Exact, Contains and Regex.
	Value string `json:"value,omitempty"`

	// Low and High bound Interval, inclusive.
	Low  float64 `json:"low,omitempty"`
	High float64 `json:"high,omitempty"`

	// Target and Tolerance drive Near. Tolerance must be > 0.
	Target    float64 `json:"target,omitempty"`
	Tolerance float64 `json:"tolerance,omitempty"`

	// Members are the parts a Set guess may name.
	Members []string `json:"members,omitempty"`
	// Required is how many Members must be named. Zero means all.
	Required int `json:"required,omitempty"`
	// Ordered additionally requires every named member to appear in
	// its declared position.
	Ordered bool `json:"ordered,omitempty"`
}

// Result is the outcome of one match.
type Result struct {
	// OK reports whether the guess is accepted.
	OK bool
	// Multiplier scales any points the output awards. It is 1 for an
	// exact acceptance and between 0 and 1 for a Near partial credit.
	// A zero-multiplier match is still a match: the player answered
	// within tolerance, they just earn nothing for it.
	Multiplier float64
}

var miss = Result{OK: false, Multiplier: 0}

func hit(multiplier float64) Result { return Result{OK: true, Multiplier: multiplier} }

// Normalize prepares a string for comparison: case-folded, trimmed, and
// with internal whitespace runs collapsed to a single space.
//
// Identical in behaviour to normalise() in internal/adventure/loader.go.
// Live adventures were authored against it, so a "better" normalisation
// here would silently change which answers are accepted.
func Normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// Match evaluates one matcher against a raw guess.
func (m Matcher) Match(guess string) Result {
	switch m.Kind {
	case Anything:
		return hit(1)
	case Exact:
		if Normalize(guess) == Normalize(m.Value) {
			return hit(1)
		}
	case Contains:
		// An empty needle would match everything, which is a
		// misconfiguration rather than a catch-all — Anything is the
		// way to say that, and it says so legibly.
		needle := Normalize(m.Value)
		if needle != "" && strings.Contains(Normalize(guess), needle) {
			return hit(1)
		}
	case Regex:
		re, err := CompileRegex(m.Value)
		// A pattern that does not compile matches nothing. It cannot
		// normally reach here — the validator rejects it at save time —
		// but failing closed beats panicking mid-event.
		if err == nil && re.MatchString(Normalize(guess)) {
			return hit(1)
		}
	case Interval:
		if n, ok := parseNumber(guess); ok {
			lo, hi := m.Low, m.High
			if lo > hi {
				lo, hi = hi, lo // tolerate a reversed range rather than rejecting every guess
			}
			if n >= lo && n <= hi {
				return hit(1)
			}
		}
	case Near:
		if m.Tolerance <= 0 {
			return miss
		}
		if n, ok := parseNumber(guess); ok {
			d := math.Abs(n - m.Target)
			if d > m.Tolerance {
				return miss
			}
			return hit(math.Max(0, 1-d/m.Tolerance))
		}
	case Set:
		return m.matchSet(guess)
	}
	return miss
}

// matchSet implements N-of-M with optional ordering.
//
// Reproduces the legacy rule exactly, including its subtlety: a member
// found in the wrong position still counts toward Required, but when
// Ordered is set every counted member must have been in its own
// position. So "name these three in order, at least two right" accepts
// two correct-and-in-place, and rejects two correct-but-swapped.
func (m Matcher) matchSet(guess string) Result {
	if len(m.Members) == 0 {
		return miss
	}
	required := m.Required
	if required <= 0 || required > len(m.Members) {
		required = len(m.Members)
	}

	parts := splitParts(guess)
	used := make([]bool, len(parts))
	found := 0
	allInPlace := true

	for i, member := range m.Members {
		want := Normalize(member)
		matchedAt := -1
		// Prefer the same position, so a correct-and-in-place answer is
		// never consumed by an earlier out-of-place member.
		if i < len(parts) && !used[i] && parts[i] == want {
			matchedAt = i
		} else {
			for j, p := range parts {
				if !used[j] && p == want {
					matchedAt = j
					break
				}
			}
		}
		if matchedAt < 0 {
			continue
		}
		used[matchedAt] = true
		found++
		if matchedAt != i {
			allInPlace = false
		}
	}

	if found < required {
		return miss
	}
	if m.Ordered && !allInPlace {
		return miss
	}
	return hit(1)
}

// partSeparators are the characters a player might use between the
// parts of a multi-part answer. Legacy fixed this at "|", which nobody
// types on a phone keyboard.
const partSeparators = ",;|\n\t"

func splitParts(guess string) []string {
	raw := strings.FieldsFunc(guess, func(r rune) bool {
		return strings.ContainsRune(partSeparators, r)
	})
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if n := Normalize(p); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// parseNumber reads a number from a guess, tolerating the ways a person
// actually writes one: surrounding words, a comma decimal separator,
// thin or non-breaking spaces as thousand separators, and a unit suffix.
// "about 1 500 m" reads as 1500.
func parseNumber(guess string) (float64, bool) {
	var b strings.Builder
	seenDigit := false
	seenSep := false

	for _, r := range guess {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune(r)
			seenDigit = true
		case (r == '-' || r == '+') && b.Len() == 0:
			b.WriteRune(r)
		case (r == '.' || r == ',') && seenDigit && !seenSep:
			// First separator after digits is the decimal point. A
			// second one means this was a thousands separator, so drop
			// what we took and keep reading digits.
			b.WriteRune('.')
			seenSep = true
		case isGroupingSpace(r) && seenDigit:
			// Thousands separator inside a number; skip it.
		case seenDigit:
			// A non-numeric rune after the number ends it.
			return finishNumber(b.String())
		}
	}
	return finishNumber(b.String())
}

// isGroupingSpace reports the space characters used as thousands
// separators. Swedish convention groups with a space, and phone
// keyboards and copy-paste both produce the non-breaking variants, so
// all four have to be tolerated or "1 500" fails to parse.
func isGroupingSpace(r rune) bool {
	switch r {
	case '\u0020', // space
		'\u00a0', // no-break space
		'\u202f', // narrow no-break space
		'\u2009': // thin space
		return true
	}
	return false
}

func finishNumber(s string) (float64, bool) {
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" || s == "+" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// CompileRegex wraps a pattern the way both the matcher and the
// validator must: case-insensitive and anchored to the entire guess.
//
// Anchoring is the important part. Without \A...\z a pattern like
// `\d{4}` would accept "my pin is 1234", which is almost never what an
// author means. Exported so the validator compiles the exact same
// string the runtime will, and cannot disagree about which patterns are
// legal.
func CompileRegex(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(`(?i)\A(?:` + pattern + `)\z`)
}

// Any evaluates matchers in order and returns the first acceptance,
// with one rule: Anything matchers are considered only after every
// specific matcher has failed.
//
// That two-pass order is what makes a catch-all usable. Legacy had the
// same rule (FindNonWildcardAnswears then FindWildcardAnswears) and it
// exists because an author who adds "accept anything" to collect
// stragglers would otherwise shadow every precise answer on the same
// output.
func Any(matchers []Matcher, guess string) Result {
	for _, m := range matchers {
		if m.Kind == Anything {
			continue
		}
		if r := m.Match(guess); r.OK {
			return r
		}
	}
	for _, m := range matchers {
		if m.Kind != Anything {
			continue
		}
		if r := m.Match(guess); r.OK {
			return r
		}
	}
	return miss
}
