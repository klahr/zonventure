package icscal

import (
	"strings"
	"testing"
	"time"
)

// FuzzParseTimestamp exercises the Postgres+RFC3339 dual-shape parser
// against arbitrary input. The function is best-effort (returns ok=false
// on garbage), so the assertion is panic-freedom, not correctness.
func FuzzParseTimestamp(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"2026-05-08 12:34:56",
		"2026-05-08T12:34:56Z",
		"2026-05-08T12:34:56+02:00",
		"not-a-date",
		"99999-99-99 99:99:99",
		"\x00",
		strings.Repeat("9", 1024),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseTimestamp(s)
	})
}

// FuzzEscape exercises the iCalendar property-value escaper. The
// escape rules per RFC 5545 § 3.3.11: backslash → \\, semicolon → \;,
// comma → \,, newline → \n. The output must never contain a raw
// unescaped one of those (other than as part of the escape sequence
// itself), or the resulting calendar line is invalid.
func FuzzEscape(f *testing.F) {
	seeds := []string{
		"",
		"plain",
		"semi;colon",
		"com,ma",
		"new\nline",
		"back\\slash",
		"all four ;,\n\\",
		"\r\nCRLF",
		"\x00\x01\x02",
		strings.Repeat("\\", 1000),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Escape(s)
		// The escaped form must not contain a raw newline. CRLF would
		// terminate the property line in the rendered ics; an attacker
		// embedding \r\n in (say) an event title would inject extra
		// properties.
		if strings.Contains(out, "\n") || strings.Contains(out, "\r") {
			t.Errorf("Escape(%q) contains raw newline: %q", s, out)
		}
	})
}

// FuzzWrite drives the calendar-write end-to-end. The shape of the
// invariant is the same as the escape fuzz: no raw CRLF inside any
// property body, since unfolded line content can't carry one.
func FuzzWrite(f *testing.F) {
	seeds := []struct {
		title, desc, location string
	}{
		{"Hello", "world", "Stockholm"},
		{"Title with;semi", "desc,with,commas", "loc\nwith\nnewlines"},
		{"unicode 🎉", "räksmörgås", "Göteborg"},
		{"", "", ""},
		{strings.Repeat("a", 200), "long description " + strings.Repeat("x", 500), ""},
	}
	for _, s := range seeds {
		f.Add(s.title, s.desc, s.location)
	}
	f.Fuzz(func(t *testing.T, title, desc, location string) {
		ev := Event{
			UID:           "test@zv.example",
			GeneratedAt:   time.Now().UTC(),
			StartAt:       time.Now().UTC(),
			EndAt:         time.Now().UTC().Add(time.Hour),
			Title:         title,
			Description:   desc,
			LocationLabel: location,
			Status:        "CONFIRMED",
		}
		out, err := Bytes(ev)
		if err != nil {
			return // benign: malformed input that the writer rejects
		}
		// RFC 5545 lines fold at 75 octets; no unfolded line should
		// carry an embedded CR/LF *outside* the CRLF terminators
		// between properties. We can't easily disambiguate a folded-
		// property continuation from a malicious injection here, so
		// we just assert the writer didn't produce a NUL byte (always
		// invalid in ics) and that the body parses as some text.
		s := string(out)
		if strings.Contains(s, "\x00") {
			t.Errorf("Write produced NUL byte: title=%q out=%q", title, s)
		}
	})
}
