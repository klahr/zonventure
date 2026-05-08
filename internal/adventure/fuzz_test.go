package adventure

import (
	"strings"
	"testing"
)

// FuzzParseAcceptJSON exercises the dual-shape parser (typed entries
// vs legacy string array) against arbitrary bytes. Invariant: never
// panics. Garbage input must come back as a clean error.
func FuzzParseAcceptJSON(f *testing.F) {
	seeds := []string{
		"",
		"[]",
		"null",
		`["alice","bob"]`,
		`[{"text":"alice","regex":false}]`,
		`[{"text":"^a.*z$","regex":true}]`,
		`{"not":"an array"}`,
		`[1,2,3]`,
		`[{"text":""}]`,
		strings.Repeat(`{"text":"x"},`, 1000),
		"[{\"text\":\" \x00\"}]",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 64*1024 {
			body = body[:64*1024]
		}
		_, _ = ParseAcceptJSON(body)
	})
}

// FuzzValidateAcceptEntries asserts that no regex pattern fed to
// ValidateAcceptEntries can panic — regexp.Compile has been hardened
// over the years but a deeply nested pattern can still consume
// stack. The wrap `(?i)\A(?:…)\z` adds another layer; this fuzz
// checks the wrap doesn't combine with user input to produce a
// pattern that crashes the parser.
func FuzzValidateAcceptEntries(f *testing.F) {
	seeds := []string{
		"alice",
		"^a.*z$",
		"(",
		"\\",
		"(?P<name>x)",
		strings.Repeat("(?:a", 100) + strings.Repeat(")", 100),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		if len(pattern) > 4096 {
			pattern = pattern[:4096]
		}
		_ = ValidateAcceptEntries([]AcceptEntry{{Text: pattern, Regex: true}})
	})
}
