package admin

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/klahr/zonventure/internal/listing"
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

// cleanEventTranslations checks an event's name, blurb and meeting
// place in other languages, as the main ones are checked, trimmed, and
// leaves out a language with nothing in it. Only the event's own words
// are its to translate: the adventure's are translated in the adventure.
func cleanEventTranslations(in listing.Translations) (listing.Translations, error) {
	out := listing.Translations{}
	for lang, t := range in {
		if !validLanguageTag(lang) {
			return nil, fmt.Errorf("%q is not a language tag", lang)
		}
		if t.Title != "" || t.StartLabel != "" || t.Area != "" || t.BuyerNote != "" {
			return nil, fmt.Errorf("%s: only name, blurb and meeting_place are the event's to translate", lang)
		}
		t.Name, t.Blurb, t.MeetingPlace = strings.TrimSpace(t.Name), strings.TrimSpace(t.Blurb), strings.TrimSpace(t.MeetingPlace)
		if _, err := validateBlurb(t.Name, MaxNameLen); err != nil {
			return nil, fmt.Errorf("%s: name %w", lang, err)
		}
		if len(t.Blurb) > 1000 {
			return nil, fmt.Errorf("%s: blurb is too long (max 1000 chars)", lang)
		}
		if _, err := validateBlurb(t.MeetingPlace, MaxNameLen); err != nil {
			return nil, fmt.Errorf("%s: meeting_place %w", lang, err)
		}
		if t.Name != "" || t.Blurb != "" || t.MeetingPlace != "" {
			out[lang] = t
		}
	}
	return out, nil
}

// validTeamLang reports whether lang can be a team's language: a
// language tag, or empty for the adventure's own.
func validTeamLang(lang string) bool {
	lang = strings.TrimSpace(lang)
	return lang == "" || validLanguageTag(lang)
}
