package admin

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Length caps for free-text fields that ride WebSocket state-change
// pings to every subscribed arranger / player. The DB columns are
// TEXT (unbounded); these caps live at the handler so an attacker
// who passes the JSON-body 1 MiB cap can't still smuggle a 1 MB team
// name into the broadcast pipe and force every viewer to download +
// render it on each event.
//
// Numbers are deliberately generous: real names are short, so a cap
// 5–10× longer than anything legitimate trips DoS without affecting
// legitimate users. UI inputs should mirror these via maxlength.
const (
	MaxNameLen     = 200    // team / org / adventure title / event name
	MaxAuthorLen   = 200    // adventure author byline
	MaxAreaLen     = 80     // where an adventure is played, for store cards
	MaxBlurbLen    = 4_000  // descriptions, intro/outro
	MaxPromptLen   = 16_000 // puzzle prompt markdown
	MaxHintTextLen = 4_000  // single hint body markdown
)

// The radius a required starting point may have, in metres: no tighter
// than a phone's GPS can tell, no wider than a neighbourhood.
const (
	MinStartRadiusM = 5
	MaxStartRadiusM = 5_000
)

// errNameRequired and errNameTooLong are the canonical empty / over-
// limit results from validateName. Wrapped at the handler boundary
// to bad-request 400 with the message text directly.
var (
	errNameRequired = errors.New("name required")
	errNameTooLong  = errors.New("name too long")
)

// validateName trims, rejects empty, and enforces a length cap (in
// runes — not bytes — so a 200-char limit means 200 user-visible
// characters even when they're emoji or CJK).
func validateName(s string, maxRunes int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errNameRequired
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return "", errNameTooLong
	}
	return s, nil
}

// validateBlurb is the empty-tolerant variant: the field is optional
// but if set it must fit within maxRunes. Used for description /
// intro / outro / prompt / hint text where the author may legitimately
// leave the field blank.
func validateBlurb(s string, maxRunes int) (string, error) {
	if utf8.RuneCountInString(s) > maxRunes {
		return "", errNameTooLong
	}
	return s, nil
}
