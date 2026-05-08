package orgslug

import (
	"strings"
	"testing"
)

// FuzzSlugify_AlwaysValid asserts the load-bearing invariant of
// Slugify: whatever you pass in, the output must satisfy Validate.
// That's the whole reason Slugify exists — auto-derived slugs from
// org names should never need a second-pass cleanup. A regression
// here would mean accepted org names that fail to save with no
// path forward for the user.
func FuzzSlugify_AlwaysValid(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"Acme",
		"Acme Inc.",
		"über-cool",
		"My Org / 2026",
		"\x00\x01",
		"------",
		"a",
		"ab",
		strings.Repeat("a", 200),
		"!@#$%^&*()",
		"日本語",
		"  spaces  everywhere  ",
		"--leading-hyphens",
		"trailing-hyphens--",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		slug := Slugify(input)
		if err := Validate(slug); err != nil {
			t.Errorf("Slugify(%q) = %q, but Validate fails: %v", input, slug, err)
		}
	})
}

// FuzzValidate exercises the validator with arbitrary strings. The
// invariant is panic-freedom; the function is a pure decision so any
// input either gets a clean nil or a clean error.
func FuzzValidate(f *testing.F) {
	seeds := []string{
		"", "a", "ab", "abc", "valid-slug", "with_underscore",
		"UPPERCASE", "with space", "trailing-",
		"-leading", "double--hyphen", "admin", "api",
		strings.Repeat("a", 200),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, slug string) {
		_ = Validate(slug) // panic-free is the assertion
	})
}
